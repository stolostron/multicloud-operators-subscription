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

package utils

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
)

// IsResourceOwnedByCluster checks if the deployable belongs to this controller by AnnotationManagedCluster
// only the managed cluster annotation matches
func IsResourceOwnedByCluster(obj metav1.Object, cluster types.NamespacedName) bool {
	if obj == nil {
		return false
	}

	annotations := obj.GetAnnotations()

	if annotations == nil {
		return false
	}

	if annotations[dplv1.AnnotationManagedCluster] == cluster.String() {
		return true
	}

	return false
}

// IsLocalDeployable checks if the deployable meant to be deployed
func IsLocalDeployable(instance *dplv1.Deployable) bool {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	if instance == nil {
		return false
	}

	annotations := instance.GetAnnotations()
	if annotations == nil || annotations[dplv1.AnnotationLocal] != "true" {
		return false
	}

	return true
}

// GetClusterFromResourceObject return nil if no host is found
func GetClusterFromResourceObject(obj metav1.Object) *types.NamespacedName {
	if obj == nil {
		return nil
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}

	clstr := annotations[dplv1.AnnotationManagedCluster]

	if clstr == "" {
		return nil
	}

	parsedstr := strings.Split(clstr, "/")
	if len(parsedstr) != 2 {
		return nil
	}

	host := &types.NamespacedName{Name: parsedstr[1], Namespace: parsedstr[0]}

	return host
}

// GetHostDeployableFromObject return nil if no host is found
func GetHostDeployableFromObject(obj metav1.Object) *types.NamespacedName {
	if obj == nil {
		return nil
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}

	hosttr := annotations[dplv1.AnnotationHosting]

	if hosttr == "" {
		return nil
	}

	parsedstr := strings.Split(hosttr, "/")
	if len(parsedstr) != 2 {
		return nil
	}

	host := &types.NamespacedName{Name: parsedstr[1], Namespace: parsedstr[0]}

	return host
}

// UpdateDeployableStatus based on error message, and propagate resource status
// - nil:  success
// - others: failed, with error message in reason
func UpdateDeployableStatus(statusClient client.Client, templateerr error, tplunit metav1.Object, status interface{}) error {
	dpl := &dplv1.Deployable{}
	host := GetHostDeployableFromObject(tplunit)

	if host == nil {
		klog.Info("Failed to find hosting deployable for ", tplunit)
	}

	err := statusClient.Get(context.TODO(), *host, dpl)

	if err != nil {
		// for all errors including not found return
		return err
	}

	klog.V(10).Info("Trying to update deployable status:", host, templateerr)

	dpl.Status.PropagatedStatus = nil
	if templateerr == nil {
		dpl.Status.Phase = dplv1.DeployableDeployed
		dpl.Status.Reason = ""
	} else {
		dpl.Status.Phase = dplv1.DeployableFailed
		dpl.Status.Reason = templateerr.Error()
	}

	if status != nil {
		if dpl.Status.ResourceStatus == nil {
			dpl.Status.ResourceStatus = &runtime.RawExtension{}
		}

		dpl.Status.ResourceStatus.Raw, err = json.Marshal(status)

		if err != nil {
			klog.Info("Failed to mashall status for ", host, status, " with err:", err)
		}
	}

	now := metav1.Now()
	dpl.Status.LastUpdateTime = &now
	err = statusClient.Status().Update(context.Background(), dpl)
	// want to print out the error log before leave
	if err != nil {
		klog.Error("Failed to update status of deployable ", dpl)
	}

	return err
}

//DeleteDeployableCRD deletes the Deployable CRD
func DeleteDeployableCRD(runtimeClient client.Client, crdx *clientsetx.Clientset) {
	dpllist := &dplv1.DeployableList{}
	err := runtimeClient.List(context.TODO(), dpllist, &client.ListOptions{})

	if err != nil && !kerrors.IsNotFound(err) {
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
		err = crdx.ApiextensionsV1().CustomResourceDefinitions().Delete(dplv1.SchemeGroupVersion.Group, &v1.DeleteOptions{})
		if err != nil {
			klog.Infof("Deleting deployable CRD failed. err: %s", err.Error())
		} else {
			klog.Info("deployable CRD removed")
		}
	}
}
