// Copyright 2021 The Kubernetes Authors.
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/ghodss/yaml"
	gerr "github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/releaseutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"

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

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	releasev1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"

	subv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	helmops "open-cluster-management.io/multicloud-operators-subscription/pkg/subscriber/helmrepo"

	rHelper "open-cluster-management.io/multicloud-operators-subscription/pkg/helmrelease/controller/helmrelease"
	rUtils "open-cluster-management.io/multicloud-operators-subscription/pkg/helmrelease/utils"
)

const (
	hookParent = "hook"
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

func getHelmTopoResources(hubClt client.Client, hubCfg *rest.Config, channel, secondChannel *chnv1.Channel,
	sub *subv1.Subscription, isAdmin bool) ([]*v1.ObjectReference, error) {
	helmRls, err := helmops.GetSubscriptionChartsOnHub(hubClt, channel, secondChannel, sub)
	if err != nil {
		klog.Errorf("failed to get the chart index for helm subscription %v, err: %v", ObjectString(sub), err)
		return nil, err
	}

	var errMsgs []string

	resources := []*v1.ObjectReference{}
	cfg := rest.CopyConfig(hubCfg)

	for _, helmRl := range helmRls {
		objList, err := GenerateResourceListByConfig(cfg, helmRl)
		if err != nil {
			return nil, gerr.Wrap(err, "failed to get object lists")
		}

		for _, obj := range objList {
			errs := validation.IsDNS1123Subdomain(obj.Name)
			if len(errs) > 0 {
				errs = append([]string{fmt.Sprintf("Invalid %s name '%s'", obj.Kind, obj.Name)}, errs...)
				errMsgs = append(errMsgs, strings.Join(errs, ","))
			}

			// No need to save the namespace object to the resource list of the appsub
			if obj.Kind == "Namespace" {
				continue
			}

			// respect object customized namespace if the appsub user is subscription admin, or apply it to appsub namespace
			if isAdmin {
				if obj.Namespace == "" {
					obj.Namespace = sub.Namespace
				}
			} else {
				obj.Namespace = sub.Namespace
			}

			resources = append(resources, obj)
		}
	}

	if len(errMsgs) > 0 {
		return resources, errors.New(strings.Join(errMsgs, ","))
	}

	return resources, nil
}

//generateResourceList generates the resource list for given HelmRelease
func generateResourceList(mgr manager.Manager, s *releasev1.HelmRelease) ([]*v1.ObjectReference, error) {
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
	if err != nil || release == nil {
		return nil, err
	}

	// parse the manifest into individual yaml content
	caps, err := rHelper.GetCapabilities(actionConfig)
	if err != nil {
		return nil, err
	}

	manifests := releaseutil.SplitManifests(release.Manifest)

	_, files, err := releaseutil.SortManifests(manifests, caps.APIVersions, releaseutil.InstallOrder)
	if err != nil {
		return nil, err
	}

	resources := []*v1.ObjectReference{}

	// for each content try to build a k8s resource, if successful add it to the list of return
	for _, file := range files {
		resList, err := kubeClient.Build(bytes.NewBufferString(file.Content), false)
		if err == nil {
			for _, resInfo := range resList {
				if resInfo.Object != nil && resInfo.Object.GetObjectKind() != nil {
					resource := &v1.ObjectReference{
						Kind:       resInfo.Object.GetObjectKind().GroupVersionKind().Kind,
						APIVersion: resInfo.Object.GetObjectKind().GroupVersionKind().Version,
						Name:       resInfo.Name,
						Namespace:  resInfo.Namespace,
					}

					resources = append(resources, resource)
				}
			}
		} else {
			klog.Warning("unable to build kubernetes objects from release manifest, using just file content: %w", err)

			if file.Head != nil {
				res := &v1.ObjectReference{
					Kind:       file.Head.Kind,
					APIVersion: file.Head.Version,
					Name:       file.Name,
					Namespace:  s.Namespace,
				}

				resources = append(resources, res)
			}
		}
	}

	return resources, nil
}

//GenerateResourceListByConfig this func and it's child funcs(downloadChart,
//generateResourceList) is a clone of from the helmrelease. Having this clone
//give us the flexiblity to modify the function parameters,which helped to pass
//test case.
//generates the resource list for given HelmRelease
func GenerateResourceListByConfig(cfg *rest.Config, s *releasev1.HelmRelease) ([]*v1.ObjectReference, error) {
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

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := mgr.Start(ctx); err != nil {
			klog.Error(err)
		}
	}()

	defer func() {
		cancel()
	}()

	if mgr.GetCache().WaitForCacheSync(ctx) {
		return generateResourceList(mgr, s)
	}

	return nil, fmt.Errorf("fail to start a manager to generate the resource list")
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
