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
	"fmt"
	"os"
	"reflect"
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
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
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

	if err := statusClient.Get(context.TODO(), *host, dpl); err != nil {
		// for all errors including not found return
		return err
	}

	dpl.Status.PropagatedStatus = nil

	newStatus := func() dplv1.DeployableStatus {
		a := dplv1.DeployableStatus{}
		if templateerr == nil {
			a.Phase = dplv1.DeployableDeployed
			a.Reason = ""
		} else {
			a.Phase = dplv1.DeployableFailed
			a.Reason = templateerr.Error()
		}

		var err error

		if status != nil {
			a.ResourceStatus = &runtime.RawExtension{}
			a.ResourceStatus.Raw, err = json.Marshal(status)

			if err != nil {
				klog.Info("Failed to mashall status for ", host, status, " with err:", err)
			}
		}

		return a
	}()

	klog.V(1).Info("Trying to update deployable status:", host, templateerr)

	oldStatus := dpl.Status.DeepCopy()

	klog.V(1).Infof("old old: %#v, in in:%#v", *oldStatus, newStatus)

	fmt.Println(isEmptyResourceUnitStatus(newStatus.ResourceUnitStatus), isManagedStatusUpdated(*oldStatus, newStatus))

	if isEmptyResourceUnitStatus(newStatus.ResourceUnitStatus) || isManagedStatusUpdated(*oldStatus, newStatus) {
		statuStr := fmt.Sprintf("updating old %s, new %s", prettyStatus(dpl.Status), prettyStatus(newStatus))
		klog.Info(fmt.Sprintf("host %s cmp status %s ", host.String(), statuStr))

		now := metav1.Now()
		dpl.Status = newStatus
		dpl.Status.LastUpdateTime = &now

		klog.V(1).Infof("new new deploayble: %#v", dpl)

		// want to print out the error log before leave
		if err := statusClient.Status().Update(context.TODO(), dpl); err != nil {
			klog.Errorf("Failed to update status of deployable %v, err %v", dpl, err)
			return err
		}
	}

	return nil
}

func prettyStatus(a dplv1.DeployableStatus) string {
	if a.ResourceStatus != nil {
		return fmt.Sprintf("time: %v, phase %v, reason %v, msg %v, resource %v content %v\n",
			a.LastUpdateTime, a.Phase, a.Reason, a.Message, len(a.ResourceStatus.Raw), string(a.ResourceStatus.Raw))
	}

	return fmt.Sprintf("time: %v, phase %v, reason %v, msg %v, resource %v",
		a.LastUpdateTime, a.Phase, a.Reason, a.Message, 0)
}

// since this is on the managed cluster, so we don't need to check up the
// propagateStatus
func isStatusUpdated(old, in dplv1.DeployableStatus) bool {
	oldResSt, inResSt := old.ResourceUnitStatus, in.ResourceUnitStatus

	return !isEqualResourceUnitStatus(oldResSt, inResSt)
}

func isManagedStatusUpdated(old, in dplv1.DeployableStatus) bool {
	oldResSt, inResSt := old.ResourceUnitStatus, in.ResourceUnitStatus

	return isSubscriptionResourceStatusUpdated(oldResSt, inResSt)
}
func isSubscriptionResourceStatusUpdated(a, b dplv1.ResourceUnitStatus) bool {
	if isEmptyResourceUnitStatus(a) && isEmptyResourceUnitStatus(b) {
		return false
	}

	if !isEmptyResourceUnitStatus(a) && isEmptyResourceUnitStatus(b) {
		return true
	}

	if isEmptyResourceUnitStatus(a) && !isEmptyResourceUnitStatus(b) {
		return true
	}

	if a.Phase != b.Phase || a.Reason != b.Reason || a.Message != b.Message {
		return true
	}

	//status from cluster
	aRes := a.ResourceStatus
	bRes := b.ResourceStatus

	if aRes == nil && bRes == nil {
		return false
	}

	if aRes == nil && bRes != nil {
		return true
	}

	if aRes != nil && bRes == nil {
		return true
	}

	// given the UpdateDeployableStatus is called when update the host deployabe
	// from managed cluster to host, so the DeployableStatus.ResourceStatus is
	// fair to say SubscriptionStatus
	aUnitStatus := &subv1.SubscriptionStatus{}
	aerr := json.Unmarshal(aRes.Raw, aUnitStatus)

	bUnitStatus := &subv1.SubscriptionStatus{}
	berr := json.Unmarshal(bRes.Raw, bUnitStatus)

	if aerr != nil || berr != nil {
		klog.Infof("unmarshall resource status failed. aerr: %v, berr: %v", aerr, berr)
		return true
	}

	klog.V(1).Infof("aUnitStatus: %#v, bUnitStatus: %#v", aUnitStatus, bUnitStatus)

	now := metav1.Now()
	aUnitStatus.LastUpdateTime = now
	bUnitStatus.LastUpdateTime = now
	faUnit := filterOutNoiseyFieldFromPkgStatus(aUnitStatus)
	fbUnit := filterOutNoiseyFieldFromPkgStatus(bUnitStatus)

	return !isEqualSubscriptionStatus(faUnit, fbUnit)
}

func PrintSubscriptionStatus(a *subv1.SubscriptionStatus) {
	if a == nil {
		return
	}

	fmt.Printf("a.Phase = %+v\n", a.Phase)
	fmt.Printf("a.Message = %+v\n", a.Message)
	fmt.Printf("a.Reason = %+v\n", a.Reason)

	for c, clusterStatus := range a.Statuses {
		fmt.Printf("cluster name = %+v\n", c)

		for n, p := range clusterStatus.SubscriptionPackageStatus {
			fmt.Println()

			if p == nil {
				continue
			}

			fmt.Printf("\tpkg name %s\n", n)
			fmt.Printf("\tpkg package phase %s\n", p.Phase)
			fmt.Printf("\tpkg package message %s\n", p.Message)
			fmt.Printf("\tpkg package reason %s\n", p.Reason)

			if p.ResourceStatus == nil {
				continue
			}

			fmt.Printf("\tpkg package content %s\n", string(p.ResourceStatus.Raw))
		}

		fmt.Println()
	}
}

func filterOutNoiseyFieldFromPkgStatus(a *subv1.SubscriptionStatus) *subv1.SubscriptionStatus {
	out := a.DeepCopy()

	dfields := func(d *map[string]interface{}, key string) {
		_, ok := (*d)[key]
		if ok {
			delete(*d, key)
		}
	}

	for _, clusterStatus := range out.Statuses {
		for _, p := range clusterStatus.SubscriptionPackageStatus {
			if p != nil && p.ResourceStatus != nil {
				data := new(map[string]interface{})
				if err := json.Unmarshal(p.ResourceStatus.Raw, data); err != nil {
					continue
				}

				dfields(data, "observedGeneration")
				dfields(data, "lastTransitionTime")
				dfields(data, "conditions")

				raw, err := json.Marshal(data)
				if err != nil {
					continue
				}

				p.ResourceStatus.Raw = raw
			}
		}
	}

	return out
}

func isEmptyResourceUnitStatus(a dplv1.ResourceUnitStatus) bool {
	if len(a.Message) != 0 || len(a.Phase) != 0 || len(a.Reason) != 0 || a.ResourceStatus != nil {
		return false
	}

	return true
}

func isEqualResourceUnitStatus(a, b dplv1.ResourceUnitStatus) bool {
	if isEmptyResourceUnitStatus(a) && isEmptyResourceUnitStatus(b) {
		return true
	}

	if !isEmptyResourceUnitStatus(a) && isEmptyResourceUnitStatus(b) {
		return false
	}

	if isEmptyResourceUnitStatus(a) && !isEmptyResourceUnitStatus(b) {
		return false
	}

	if a.Phase != b.Phase || a.Reason != b.Reason || a.Message != b.Message {
		return false
	}

	//status from cluster
	aRes := a.ResourceStatus
	bRes := b.ResourceStatus

	if aRes == nil && bRes == nil {
		return true
	}

	if aRes == nil && bRes != nil {
		return false
	}

	if aRes != nil && bRes == nil {
		return false
	}

	// given the UpdateDeployableStatus is called when update the host deployabe
	// from managed cluster to host, so the DeployableStatus.ResourceStatus is
	// fair to say SubscriptionStatus
	aUnitStatus := &dplv1.ResourceUnitStatus{}
	aerr := json.Unmarshal(aRes.Raw, aUnitStatus)

	bUnitStatus := &dplv1.ResourceUnitStatus{}
	berr := json.Unmarshal(bRes.Raw, bUnitStatus)

	if aerr != nil || berr != nil {
		klog.Infof("unmarshall resource status failed. aerr: %v, berr: %v", aerr, berr)
		return true
	}

	klog.V(1).Infof("aUnitStatus: %#v, bUnitStatus: %#v", aUnitStatus, bUnitStatus)

	now := metav1.Now()
	aUnitStatus.LastUpdateTime = &now
	bUnitStatus.LastUpdateTime = &now

	return reflect.DeepEqual(aUnitStatus, bUnitStatus)
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
			dpl := dpl
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
		err = crdx.ApiextensionsV1().CustomResourceDefinitions().Delete(context.TODO(), dplv1.SchemeGroupVersion.Group, v1.DeleteOptions{})
		if err != nil {
			klog.Infof("Deleting deployable CRD failed. err: %s", err.Error())
		} else {
			klog.Info("deployable CRD removed")
		}
	}
}
