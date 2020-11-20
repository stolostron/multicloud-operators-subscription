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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/resource"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	cached "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	releasev1 "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/apis/apps/v1"

	rUtils "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/utils"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	helmops "github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber/helmrepo"
)

const (
	sep              = ","
	sepRes           = "/"
	deployableParent = "deployable"
	helmChartParent  = "helmchart"
	hookParent       = "hook"
)

var _ genericclioptions.RESTClientGetter = &restClientGetter{}

type restClientGetter struct {
	restConfig      *rest.Config
	discoveryClient discovery.CachedDiscoveryInterface
	restMapper      meta.RESTMapper
	namespaceConfig clientcmd.ClientConfig
}

func (c *restClientGetter) ToRESTConfig() (*rest.Config, error) {
	return c.restConfig, nil
}

func (c *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return c.discoveryClient, nil
}

func (c *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	return c.restMapper, nil
}

func (c *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return c.namespaceConfig
}

var _ clientcmd.ClientConfig = &namespaceClientConfig{}

type namespaceClientConfig struct {
	namespace string
}

func (c namespaceClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, nil
}

func (c namespaceClientConfig) ClientConfig() (*rest.Config, error) {
	return nil, nil
}

func (c namespaceClientConfig) Namespace() (string, bool, error) {
	return c.namespace, false, nil
}

func (c namespaceClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return nil
}

func ObjectString(obj metav1.Object) string {
	return fmt.Sprintf("%v/%v", obj.GetNamespace(), obj.GetName())
}

func UpdateHelmTopoAnnotation(hubClt client.Client, hubCfg *rest.Config, sub *subv1.Subscription, insecureSkipVeriy bool) bool {
	subanno := sub.GetAnnotations()
	if len(subanno) == 0 {
		subanno = make(map[string]string)
	}

	helmRls, err := helmops.GetSubscriptionChartsOnHub(hubClt, sub, insecureSkipVeriy)
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

		res = append(res, parseHelmResourceList(fmt.Sprintf("%v-", helmRl.GetName()), resList))
	}

	return strings.Join(res, sep), nil
}

type resourceUnit struct {
	// it should be deployable or helmchart
	parentType string
	// for helm resource, it will prefix with this when doing dry-run
	namePrefix string
	name       string
	namespace  string
	kind       string
	addition   int
}

func (r resourceUnit) String() string {
	return fmt.Sprintf("%v/%v/%v/%v/%v/%v", r.parentType, r.namePrefix, r.kind, r.namespace, r.name, r.addition)
}

func parseHelmResourceList(helmName string, rs kube.ResourceList) string {
	res := make([]string, 0)

	for _, resInfo := range rs {
		t := infoToUnit(resInfo)
		res = append(res, addParentInfo(&t, helmChartParent, helmName).String())
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

func addParentInfo(ri *resourceUnit, ptype, prefix string) resourceUnit {
	ri.parentType = ptype
	ri.namePrefix = prefix

	rname := ri.name
	ri.name = strings.Replace(rname, prefix, "", 1)

	return *ri
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

// generate resource string from a deployable map
func updateResourceListViaDeployableMap(allDpls map[string]*dplv1.Deployable) (string, error) {
	res := []string{}

	for _, dpl := range allDpls {
		tpl, err := GetDeployableTemplateAsUnstructrure(dpl)
		if err != nil {
			return "", gerr.Wrap(err, "deployable can't convert to unstructured.Unstructured, can lead to incorrect resource list")
		}

		rUnit := resourceUnit{
			parentType: deployableParent,
			namePrefix: "",
			name:       tpl.GetName(),
			namespace:  tpl.GetNamespace(),
			kind:       tpl.GetKind(),
			addition:   processAddition(tpl),
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

	rcg, err := newRESTClientGetter(mgr, s.Namespace)
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

//GenerateResourceListByConfig this func and it's child funcs(downloadChart,
//generateResourceList) is a clone of from the helmrelease. Having this clone
//give us the flexiblity to modify the function parameters,which helped to pass
//test case.
//generates the resource list for given HelmRelease
func GenerateResourceListByConfig(cfg *rest.Config, s *releasev1.HelmRelease) (kube.ResourceList, error) {
	dryRunEventRecorder := record.NewBroadcaster()

	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		LeaderElection:     false,
		DryRunClient:       true,
		EventBroadcaster:   dryRunEventRecorder,
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
		dryRunEventRecorder.Shutdown()
	}()

	if mgr.GetCache().WaitForCacheSync(stop) {
		return generateResourceList(mgr, s)
	}

	return nil, fmt.Errorf("fail to start a manager to generate the resource list")
}

func GetDeployableTemplateAsUnstructrure(dpl *dplv1.Deployable) (*unstructured.Unstructured, error) {
	if dpl == nil || dpl.Spec.Template == nil {
		return nil, gerr.New("nil deployable skip conversion")
	}

	out := &unstructured.Unstructured{}

	b, err := dpl.Spec.Template.MarshalJSON()
	if err != nil {
		return nil, gerr.Wrap(err, "failed to convert template object to raw")
	}

	if err := json.Unmarshal(b, out); err != nil {
		return nil, gerr.Wrap(err, "failed to convert template raw to unstructured")
	}

	return out, nil
}

func (r *ReconcileSubscription) overridePrehookTopoAnnotation(subIns *subv1.Subscription) {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}
	applied := r.hooks.GetLastAppliedInstance(subKey)

	anno := subIns.GetAnnotations()

	anno[subv1.AnnotationTopo] = applied.pre

	subIns.SetAnnotations(anno)
}

func (r *ReconcileSubscription) appendAnsiblejobToSubsriptionAnnotation(anno map[string]string, subKey types.NamespacedName) map[string]string {
	if len(anno) == 0 {
		anno = map[string]string{}
	}

	applied := r.hooks.GetLastAppliedInstance(subKey)

	topo := anno[subv1.AnnotationTopo]

	tPreJobs := applied.pre
	tPostJobs := applied.post

	if len(tPreJobs) != 0 {
		if len(topo) == 0 {
			topo = tPreJobs
		} else {
			topo = fmt.Sprintf("%s,%s", topo, tPreJobs)
		}
	}

	if len(tPostJobs) != 0 {
		if len(topo) == 0 {
			topo = tPostJobs
		} else {
			topo = fmt.Sprintf("%s,%s", topo, tPostJobs)
		}
	}

	anno[subv1.AnnotationTopo] = topo

	return anno
}

func newRESTClientGetter(mgr manager.Manager, ns string) (genericclioptions.RESTClientGetter, error) {
	cfg := mgr.GetConfig()

	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}

	cdc := cached.NewMemCacheClient(dc)
	rm := mgr.GetRESTMapper()

	return &restClientGetter{
		restConfig:      cfg,
		discoveryClient: cdc,
		restMapper:      rm,
		namespaceConfig: &namespaceClientConfig{ns},
	}, nil
}
