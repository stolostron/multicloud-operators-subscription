// Copyright 2020 The Kubernetes Authors.
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

package main

import (
	"context"
	"fmt"
	"os"

	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplapis "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis"
	releaseapis "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/apis"
	subapis "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

var (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
)

func main() {
	namespace := v1.NamespaceAll

	cfg, err := config.GetConfig()
	if err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	klog.Infof("Starting uninstall crds...")

	runtimeClient, err := client.New(cfg, client.Options{})
	if err != nil {
		klog.Infof("Error building runtime clientset: %s", err)
		os.Exit(1)
	}

	// create the clientset for the CR
	mgr, err := manager.New(cfg, manager.Options{
		Namespace:          namespace,
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
	})
	if err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// create the clientset for the CRDs
	crdx, err := clientsetx.NewForConfig(cfg)
	if err != nil {
		klog.Infof("Error building cluster registry clientset: %s", err.Error())
		os.Exit(1)
	}

	//append helmreleases.apps.open-cluster-management.io to scheme
	if err = releaseapis.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error("unable add helmreleases.apps.open-cluster-management.io APIs to scheme: ", err)
		os.Exit(1)
	}

	//append subscriptions.apps.open-cluster-management.io to scheme
	if err = subapis.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error("unable add subscriptions.apps.open-cluster-management.io APIs to scheme: ", err)
		os.Exit(1)
	}

	//append deployables.apps.open-cluster-management.io to scheme
	if err = dplapis.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error("unable add deployables.apps.open-cluster-management.io APIs to scheme: ", err)
		os.Exit(1)
	}

	// If this CRD exists, it is the ACM hub cluster.
	_, err = crdx.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), "multiclusterhubs.operator.open-cluster-management.io", v1.GetOptions{})

	if err != nil && kerrors.IsNotFound(err) {
		klog.Info("This is not ACM hub cluster. Deleting helmrelease and deployable CRDs.")

		// handle helmrelease crd
		utils.DeleteHelmReleaseCRD(runtimeClient, crdx)

		// handle deployable crd
		utils.DeleteDeployableCRD(runtimeClient, crdx)
	} else {
		klog.Info("This is ACM hub cluster. Skip deleting helmrelease and deployable CRDs.")
	}

	// handle subscription crd
	utils.DeleteSubscriptionCRD(runtimeClient, crdx)
}
