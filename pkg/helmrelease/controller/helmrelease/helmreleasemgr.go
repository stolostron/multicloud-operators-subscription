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
	"context"
	"os"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog"
	helmoperator "open-cluster-management.io/multicloud-operators-subscription/pkg/helmrelease/release"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/helmrelease/utils"
)

// newHelmOperatorManagerFactory create a new manager returns a helmManagerFactory
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

	klog.Info("ChartDir: ", chartDir)

	f := helmoperator.NewManagerFactory(r.Manager, chartDir)

	return f, nil
}

// newHelmOperatorManager returns a newly created helm operator manager
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

// downloadChart downloads the chart
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
		chartsDir = "/tmp/hr-charts"
	}

	chartDir, err := utils.DownloadChart(configMap, secret, chartsDir, s)
	klog.V(3).Info("ChartDir: ", chartDir)

	if err != nil {
		klog.Error(err, " - Failed to download the chart")
		return "", err
	}

	return chartDir, nil
}
