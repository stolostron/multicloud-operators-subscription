/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helmrelease

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/ghodss/yaml"

	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/storage"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog"
	helmclient "open-cluster-management.io/multicloud-operators-subscription/pkg/helmrelease/client"
	helmoperator "open-cluster-management.io/multicloud-operators-subscription/pkg/helmrelease/release"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/helmrelease/utils"
)

//newHelmOperatorManagerFactory create a new manager returns a helmManagerFactory
func (r ReconcileHelmRelease) newHelmOperatorManagerFactory(
	s *appv1.HelmRelease) (helmoperator.ManagerFactory, error) {
	if s.GetDeletionTimestamp() != nil {
		return helmoperator.NewManagerFactory(r.Manager, ""), nil
	}

	chartDir, err := downloadChart(r.GetClient(), s)
	if err != nil {
		klog.Error(err, " - Failed to download the chart")
		return nil, err
	}

	klog.V(3).Info("ChartDir: ", chartDir)

	f := helmoperator.NewManagerFactory(r.Manager, chartDir)

	return f, nil
}

//newHelmOperatorManager returns a newly created helm operator manager
func (r ReconcileHelmRelease) newHelmOperatorManager(
	s *appv1.HelmRelease, request reconcile.Request, factory helmoperator.ManagerFactory) (helmoperator.Manager, error) {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(s.GroupVersionKind())
	o.SetNamespace(request.Namespace)
	o.SetName(request.Name)

	err := r.GetClient().Get(context.TODO(), request.NamespacedName, o)
	if err != nil {
		klog.Error(err, " - Failed to lookup resource")
		return nil, err
	}

	manager, err := factory.NewManager(o, nil)
	if err != nil {
		klog.Error(err, " - Failed to get helm operator manager")
		return nil, err
	}

	return manager, nil
}

//downloadChart downloads the chart
func downloadChart(client client.Client, s *appv1.HelmRelease) (string, error) {
	configMap, err := utils.GetConfigMap(client, s.Namespace, s.Repo.ConfigMapRef)
	if err != nil {
		klog.Error(err)
		return "", err
	}

	secret, err := utils.GetSecret(client, s.Namespace, s.Repo.SecretRef)
	if err != nil {
		klog.Error(err, " - Failed to retrieve secret ", s.Repo.SecretRef.Name)
		return "", err
	}

	chartsDir := os.Getenv(appv1.ChartsDir)
	if chartsDir == "" {
		chartsDir, err = ioutil.TempDir("/tmp", "charts")
		if err != nil {
			klog.Error(err, " - Can not create tempdir")
			return "", err
		}
	}

	chartDir, err := utils.DownloadChart(configMap, secret, chartsDir, s)
	klog.V(3).Info("ChartDir: ", chartDir)

	if err != nil {
		klog.Error(err, " - Failed to download the chart")
		return "", err
	}

	return chartDir, nil
}

//generateResourceList generates the resource list for given HelmRelease
func generateResourceList(mgr manager.Manager, s *appv1.HelmRelease) (kube.ResourceList, error) {
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
		return nil, fmt.Errorf("failed to load chart dir, most likely the given chart name is incorrect: %w", err)
	}

	clientv1, err := v1.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get core/v1 client: %w", err)
	}

	storageBackend := storage.Init(driver.NewSecrets(clientv1.Secrets(s.GetNamespace())))

	rcg, err := helmclient.NewRESTClientGetter(mgr, s.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get REST client getter from manager: %w", err)
	}

	kubeClient := kube.New(rcg)

	actionConfig := &action.Configuration{
		RESTClientGetter: rcg,
		Releases:         storageBackend,
		KubeClient:       kubeClient,
		Log:              func(_ string, _ ...interface{}) {},
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
