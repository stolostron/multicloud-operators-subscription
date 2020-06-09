// Copyright 2019 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mcmhub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/ghodss/yaml"
	gerr "github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/resource"

	helmclient "github.com/operator-framework/operator-sdk/pkg/helm/client"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	dplUtils "github.com/open-cluster-management/multicloud-operators-deployable/pkg/utils"
	releasev1 "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/apis/apps/v1"

	rUtils "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/utils"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	helmops "github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber/helmrepo"
)

const (
	sep = ","
)

func ObjectString(obj metav1.Object) string {
	return fmt.Sprintf("%v/%v", obj.GetNamespace(), obj.GetName())
}

func UpdateHelmTopoAnnotation(hubClt client.Client, hubCfg *rest.Config, sub *subv1.Subscription) bool {
	subanno := sub.GetAnnotations()
	if len(subanno) == 0 {
		subanno = make(map[string]string)
	}

	helmRls, err := helmops.GetSubscriptionChartsOnHub(hubClt, sub)
	if err != nil {
		klog.Errorf("failed to get the chart index for helm subscription %v, err: %v", ObjectString(sub), err)
		return false
	}

	expectTopo, err := generateResrouceList(hubCfg, helmRls)
	if err != nil {
		klog.Errorf("failed to get the resource info for helm subscription %v, err: %v", ObjectString(sub), err)
		return false
	}

	if subanno[subv1.AnnotationTopo] != expectTopo {
		subanno[subv1.AnnotationTopo] = expectTopo
		sub.SetAnnotations(subanno)

		return true
	}

	return false
}

func generateResrouceList(hubCfg *rest.Config, helmRls []*releasev1.HelmRelease) (string, error) {
	res := make([]string, 0)
	cfg := rest.CopyConfig(hubCfg)

	for _, helmRl := range helmRls {
		resList, err := GenerateResourceListByConfig(cfg, helmRl)
		if err != nil {
			return "", gerr.Wrap(err, "failed to get resource string")
		}

		res = append(res, parseResourceList(resList))
	}

	return strings.Join(res, sep), nil
}

type resourceUnit struct {
	name      string
	namespace string
	kind      string
	addition  int
}

func (r resourceUnit) String() string {
	return fmt.Sprintf("%v/%v/%v/%v", r.kind, r.namespace, r.name, r.addition)
}

func parseResourceList(rs kube.ResourceList) string {
	res := make([]string, 0)
	for _, resInfo := range rs {
		res = append(res, infoToUnit(resInfo).String())
	}

	return strings.Join(res, sep)
}

func infoToUnit(ri *resource.Info) resourceUnit {
	addition := processAddition(ri.Object)

	return resourceUnit{
		name:      ri.Name,
		namespace: ri.Namespace,
		kind:      ri.Object.GetObjectKind().GroupVersionKind().Kind,
		addition:  addition,
	}
}

func processAddition(obj runtime.Object) int {
	// need to  add more replicas related type over here
	switch k := obj.GetObjectKind().GroupVersionKind().Kind; k {
	case "Deployment", "ReplicaSet", "StatefulSet":
		return getAdditionValue(obj)
	default:
		return 0
	}
}

func getAdditionValue(obj runtime.Object) int {
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		klog.Error(err)
		return -1
	}

	spec := unstructuredObj["spec"]
	if md, ok := spec.(map[string]interface{}); ok {
		if v, f := md["replicas"]; f {
			res := int(v.(int64))
			return res
		}
	}

	return -1
}

func updateResourceListViaDeployableMap(allDpls map[string]*dplv1.Deployable) (string, error) {
	res := []string{}

	for _, dpl := range allDpls {
		tpl, err := dplUtils.GetUnstructuredTemplateFromDeployable(dpl)
		if err != nil {
			return "", gerr.Wrap(err, "deployable can't convert to unstructured.Unstructured, can lead to incorrect resource list")
		}

		rUnit := resourceUnit{
			name:      tpl.GetName(),
			namespace: tpl.GetNamespace(),
			kind:      tpl.GetKind(),
			addition:  processAddition(tpl),
		}

		res = append(res, rUnit.String())
	}

	return strings.Join(res, sep), nil
}

func extracResourceListFromDeployables(sub *appv1.Subscription, allDpls map[string]*dplv1.Deployable) bool {
	subanno := sub.GetAnnotations()
	if len(subanno) == 0 {
		subanno = make(map[string]string)
	}

	expectTopo, err := updateResourceListViaDeployableMap(allDpls)
	if err != nil {
		klog.Errorf("failed to get the resource info for subscription %v, err: %v", ObjectString(sub), err)
		return false
	}

	if subanno[subv1.AnnotationTopo] != expectTopo {
		subanno[subv1.AnnotationTopo] = expectTopo
		sub.SetAnnotations(subanno)

		return true
	}

	return false
}

//downloadChart downloads the chart
func downloadChart(client client.Client, s *releasev1.HelmRelease) (string, error) {
	configMap, err := rUtils.GetConfigMap(client, s.Namespace, s.Repo.ConfigMapRef)
	if err != nil {
		klog.Error(err)
		return "", err
	}

	secret, err := rUtils.GetSecret(client, s.Namespace, s.Repo.SecretRef)
	if err != nil {
		klog.Error(err, " - Failed to retrieve secret ", s.Repo.SecretRef.Name)
		return "", err
	}

	chartsDir := os.Getenv(releasev1.ChartsDir)
	if chartsDir == "" {
		chartsDir, err = ioutil.TempDir("/tmp", "charts")
		if err != nil {
			klog.Error(err, " - Can not create tempdir")
			return "", err
		}
	}

	chartDir, err := rUtils.DownloadChart(configMap, secret, chartsDir, s)
	klog.V(3).Info("ChartDir: ", chartDir)

	if err != nil {
		klog.Error(err, " - Failed to download the chart")
		return "", err
	}

	return chartDir, nil
}

//generateResourceList generates the resource list for given HelmRelease
func generateResourceList(mgr manager.Manager, s *releasev1.HelmRelease) (kube.ResourceList, error) {
	chartDir, err := downloadChart(mgr.GetClient(), s)
	if err != nil {
		klog.Error(err, " - Failed to download the chart")
		return nil, err
	}

	var values map[string]interface{}

	reqBodyBytes := new(bytes.Buffer)

	err = json.NewEncoder(reqBodyBytes).Encode(s.Spec)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(reqBodyBytes.Bytes(), &values)
	if err != nil {
		klog.Error(err, " - Failed to Unmarshal the spec ", s.Spec)
		return nil, err
	}

	klog.V(3).Info("ChartDir: ", chartDir)

	chart, err := loader.LoadDir(chartDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart dir: %w", err)
	}

	rcg, err := helmclient.NewRESTClientGetter(mgr, s.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get REST client getter from manager: %w", err)
	}

	kubeClient := kube.New(rcg)

	actionConfig := &action.Configuration{}
	if err := actionConfig.Init(rcg, s.GetNamespace(), "secret", func(_ string, _ ...interface{}) {}); err != nil {
		return nil, fmt.Errorf("failed to initialized actionConfig: %w", err)
	}

	install := action.NewInstall(actionConfig)
	install.ReleaseName = s.Name
	install.Namespace = s.Namespace
	install.DryRun = true
	install.ClientOnly = true
	install.Replace = true

	release, err := install.Run(chart, values)
	if err != nil {
		return nil, err
	}

	resources, err := kubeClient.Build(bytes.NewBufferString(release.Manifest), false)
	if err != nil {
		return nil, fmt.Errorf("unable to build kubernetes objects from release manifest: %w", err)
	}

	return resources, nil
}

//GenerateResourceListByConfig generates the resource list for given HelmRelease
func GenerateResourceListByConfig(cfg *rest.Config, s *releasev1.HelmRelease) (kube.ResourceList, error) {
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		LeaderElection:     false,
	})

	if err != nil {
		return nil, err
	}

	stop := make(chan struct{})

	go func() {
		if err := mgr.Start(stop); err != nil {
			klog.Error(err)
		}
	}()

	defer func() {
		close(stop)
	}()

	if mgr.GetCache().WaitForCacheSync(stop) {
		return generateResourceList(mgr, s)
	}

	return nil, fmt.Errorf("fail to start a manager to generate the resource list")
}
