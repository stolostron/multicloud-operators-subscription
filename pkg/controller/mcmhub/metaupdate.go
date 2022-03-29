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
	"encoding/json"
	"fmt"
	"os"

	"github.com/ghodss/yaml"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/releaseutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	cached "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasev1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"

	subv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"

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
		chartsDir = "/tmp/hr-charts"
	}

	chartDir, err := rUtils.DownloadChart(configMap, secret, chartsDir, s)
	klog.V(3).Info("ChartDir: ", chartDir)

	if err != nil {
		klog.Error(err, " - Failed to download the chart")
		return "", err
	}

	return chartDir, nil
}

func getHelmTopoResources(helmRls []*releasev1.HelmRelease, hubClt client.Client, hubCfg *rest.Config, rm meta.RESTMapper,
	sub *subv1.Subscription, isAdmin bool) []*v1.ObjectReference {
	resources := []*v1.ObjectReference{}
	cfg := rest.CopyConfig(hubCfg)

	for _, helmRl := range helmRls {
		objList := generateResourceList(hubClt, rm, cfg, helmRl)

		for _, obj := range objList {
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

	return resources
}

//generateResourceList generates the resource list for given HelmRelease
func generateResourceList(client client.Client, rm meta.RESTMapper, cfg *restclient.Config, s *releasev1.HelmRelease) []*v1.ObjectReference {
	chartDir, err := downloadChart(client, s)
	if err != nil {
		klog.Warning(err, " - Failed to download the chart")

		return nil
	}

	var values map[string]interface{}

	reqBodyBytes := new(bytes.Buffer)

	err = json.NewEncoder(reqBodyBytes).Encode(s.Spec)
	if err != nil {
		klog.Warning(err, " - Failed to encode spec")

		return nil
	}

	err = yaml.Unmarshal(reqBodyBytes.Bytes(), &values)
	if err != nil {
		klog.Warning(err, " - Failed to Unmarshal the spec ", s.Spec)

		return nil
	}

	klog.V(3).Info("ChartDir: ", chartDir)

	chart, err := loader.LoadDir(chartDir)
	if err != nil {
		klog.Warning("failed to load chart dir: %w", err)

		return nil
	}

	rcg, err := newRESTClientGetter(rm, cfg, s.Namespace)
	if err != nil {
		klog.Warning("failed to get REST client getter from manager: %w", err)

		return nil
	}

	kubeClient := kube.New(rcg)

	actionConfig := &action.Configuration{}
	if err := actionConfig.Init(rcg, s.GetNamespace(), "secret", func(_ string, _ ...interface{}) {}); err != nil {
		klog.Warning("failed to initialized actionConfig: %w", err)

		return nil
	}

	install := action.NewInstall(actionConfig)
	install.ReleaseName = s.Name
	install.Namespace = s.Namespace
	install.DryRun = true
	install.ClientOnly = true
	install.Replace = true

	release, err := install.Run(chart, values)
	if err != nil || release == nil {
		return nil
	}

	// parse the manifest into individual yaml content
	caps, err := rHelper.GetCapabilities(actionConfig)
	if err != nil {
		klog.Warning("failed to helm install dry-run: %w", err)

		return nil
	}

	manifests := releaseutil.SplitManifests(release.Manifest)

	_, files, err := releaseutil.SortManifests(manifests, caps.APIVersions, releaseutil.InstallOrder)
	if err != nil {
		klog.Warning("failed to sort manifests: %w", err)

		return nil
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

	return resources
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

func newRESTClientGetter(rm meta.RESTMapper, cfg *restclient.Config, ns string) (genericclioptions.RESTClientGetter, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}

	cdc := cached.NewMemCacheClient(dc)

	return &restClientGetter{
		restConfig:      cfg,
		discoveryClient: cdc,
		restMapper:      rm,
		namespaceConfig: &namespaceClientConfig{ns},
	}, nil
}
