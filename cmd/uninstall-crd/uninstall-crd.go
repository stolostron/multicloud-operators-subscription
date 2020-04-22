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

	dplapis "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	releaseapis "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/apis"
	releasev1 "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/apis/apps/v1"
	subapis "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
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

	// handle helmrelease crd
	hrlist := &releasev1.HelmReleaseList{}
	err = runtimeClient.List(context.TODO(), hrlist, &client.ListOptions{})

	if err != nil && !errors.IsNotFound(err) {
		klog.Infof("HelmRelease kind is gone. err: %s", err.Error())
		os.Exit(0)
	} else {
		for _, hr := range hrlist.Items {
			klog.V(1).Infof("Found %s", hr.SelfLink)
			// remove all finalizers
			hr = *hr.DeepCopy()
			hr.SetFinalizers([]string{})
			err = runtimeClient.Update(context.TODO(), &hr)
			if err != nil {
				klog.Warning(err)
			}
		}
		// now get rid of the crd
		err = crdx.ApiextensionsV1().CustomResourceDefinitions().Delete("helmreleases.apps.open-cluster-management.io", &v1.DeleteOptions{})
		if err != nil {
			klog.Infof("Deleting helmrelease CRD failed. err: %s", err.Error())
		} else {
			klog.Info("helmrelease CRD removed")
		}
	}

	// handle subscription crd
	sublist := &subv1.SubscriptionList{}
	err = runtimeClient.List(context.TODO(), sublist, &client.ListOptions{})

	if err != nil && !errors.IsNotFound(err) {
		klog.Infof("subscription kind is gone. err: %s", err.Error())
		os.Exit(0)
	} else {
		for _, sub := range sublist.Items {
			klog.V(1).Infof("Found %s", sub.SelfLink)
			// remove all finalizers
			sub = *sub.DeepCopy()
			sub.SetFinalizers([]string{})
			err = runtimeClient.Update(context.TODO(), &sub)
			if err != nil {
				klog.Warning(err)
			}
		}
		// now get rid of the crd
		err = crdx.ApiextensionsV1().CustomResourceDefinitions().Delete("subscriptions.apps.open-cluster-management.io", &v1.DeleteOptions{})
		if err != nil {
			klog.Infof("Deleting subscription CRD failed. err: %s", err.Error())
		} else {
			klog.Info("subscription CRD removed")
		}
	}

	// handle deployable crd
	dpllist := &dplv1.DeployableList{}
	err = runtimeClient.List(context.TODO(), dpllist, &client.ListOptions{})

	if err != nil && !errors.IsNotFound(err) {
		klog.Infof("deployable kind is gone. err: %s", err.Error())
		os.Exit(0)
	} else {
		for _, dpl := range dpllist.Items {
			klog.V(1).Infof("Found %s", dpl.SelfLink)
			// remove all finalizers
			dpl = *dpl.DeepCopy()
			dpl.SetFinalizers([]string{})
			err = runtimeClient.Update(context.TODO(), &dpl)
			if err != nil {
				klog.Warning(err)
			}
		}
		// now get rid of the crd
		err = crdx.ApiextensionsV1().CustomResourceDefinitions().Delete("deployables.apps.open-cluster-management.io", &v1.DeleteOptions{})
		if err != nil {
			klog.Infof("Deleting deployable CRD failed. err: %s", err.Error())
		} else {
			klog.Info("deployable CRD removed")
		}
	}
}
