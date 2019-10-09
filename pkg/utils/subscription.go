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
	"errors"
	"strings"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetHostSubscriptionFromObject extract the namespacedname of subscription hosting the object resource
func GetHostSubscriptionFromObject(obj metav1.Object) *types.NamespacedName {
	if obj == nil {
		return nil
	}

	objanno := obj.GetAnnotations()
	if objanno == nil {
		return nil
	}

	hosttr := objanno[appv1alpha1.AnnotationHosting]

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

// SetInClusterPackageStatus creates status strcuture and fill status
func SetInClusterPackageStatus(substatus *appv1alpha1.SubscriptionStatus, pkgname string, pkgerr error, status interface{}) error {
	if substatus.Statuses == nil {
		substatus.Statuses = make(map[string]*appv1alpha1.SubscriptionPerClusterStatus)
	}

	clst := substatus.Statuses["/"]
	if clst == nil || clst.SubscriptionPackageStatus == nil {
		clst = &appv1alpha1.SubscriptionPerClusterStatus{}
		clst.SubscriptionPackageStatus = make(map[string]*appv1alpha1.SubscriptionUnitStatus)
	}

	pkgstatus := clst.SubscriptionPackageStatus[pkgname]
	if pkgstatus == nil {
		pkgstatus = &appv1alpha1.SubscriptionUnitStatus{}
	}

	if pkgerr == nil {
		pkgstatus.Phase = appv1alpha1.SubscriptionSubscribed
		pkgstatus.Reason = ""
		pkgstatus.Message = ""
	} else {
		pkgstatus.Phase = appv1alpha1.SubscriptionFailed
		pkgstatus.Reason = pkgerr.Error()
	}

	var err error

	pkgstatus.LastUpdateTime = metav1.Now()

	if status != nil {
		if pkgstatus.ResourceStatus == nil {
			pkgstatus.ResourceStatus = &runtime.RawExtension{}
		}

		pkgstatus.ResourceStatus.Raw, err = json.Marshal(status)

		if err != nil {
			glog.Info("Failed to mashall status for ", status, " with err:", err)
		}
	} else {
		pkgstatus.ResourceStatus = nil
	}

	glog.V(10).Info("Set package status: ", pkgstatus)

	clst.SubscriptionPackageStatus[pkgname] = pkgstatus
	substatus.Statuses["/"] = clst

	substatus.LastUpdateTime = metav1.Now()

	return nil
}

// UpdateSubscriptionStatus based on error message, and propagate resource status
// - nil:  success
// - others: failed, with error message in reason
func UpdateSubscriptionStatus(statusClient client.Client, templateerr error, tplunit metav1.Object, status interface{}) error {
	glog.V(10).Info("Trying to update subscription status:", templateerr, tplunit.GetNamespace(), "/", tplunit.GetName(), status)

	if tplunit == nil {
		return nil
	}

	sub := &appv1alpha1.Subscription{}
	subkey := GetHostSubscriptionFromObject(tplunit)

	if subkey == nil {
		glog.Info("The template", tplunit.GetNamespace(), "/", tplunit.GetName(), " does not have hosting subscription", tplunit.GetAnnotations())
		return nil
	}

	err := statusClient.Get(context.TODO(), *subkey, sub)

	if err != nil {
		// for all errors including not found return
		glog.Info("Failed to get subscription object ", *subkey, " to set status, error:", err)
		return err
	}

	dplkey := GetHostDeployableFromObject(tplunit)
	if dplkey == nil {
		errmsg := "Invalid status structure in subscription: " + sub.GetNamespace() + "/" + sub.Name + " nil hosting deployable"
		glog.Info(errmsg)
		err = errors.New(errmsg)

		return err
	}

	err = SetInClusterPackageStatus(&sub.Status, dplkey.Name, templateerr, status)
	if err != nil {
		glog.Error("Failed to set package status for subscription: ", sub.Namespace+"/"+sub.Name, ". error: ", err)
		return err
	}

	err = statusClient.Status().Update(context.TODO(), sub)
	// want to print out the error log before leave
	if err != nil {
		glog.Error("Failed to update status of deployable ", err)
	}

	return err
}

// ValidatePackagesInSubscriptionStatus validate the status struture for packages
func ValidatePackagesInSubscriptionStatus(statusClient client.StatusClient, sub *appv1alpha1.Subscription, pkgMap map[string]bool) error {
	var err error

	updated := false

	if sub.Status.Statuses == nil {
		sub.Status.Statuses = make(map[string]*appv1alpha1.SubscriptionPerClusterStatus)
		updated = true
	}

	clst := sub.Status.Statuses["/"]
	if clst == nil {
		clst = &appv1alpha1.SubscriptionPerClusterStatus{}
		updated = true
	}

	glog.V(10).Info("valiating subscription status:", pkgMap, sub.Status, clst)

	if clst.SubscriptionPackageStatus == nil {
		clst.SubscriptionPackageStatus = make(map[string]*appv1alpha1.SubscriptionUnitStatus)
		updated = true
	}

	for k := range clst.SubscriptionPackageStatus {
		if _, ok := pkgMap[k]; !ok {
			updated = true

			delete(clst.SubscriptionPackageStatus, k)
		} else {
			pkgst := clst.SubscriptionPackageStatus[k]
			if pkgst.Phase == appv1alpha1.SubscriptionFailed {
				updated = true
			}
			delete(pkgMap, k)
		}
	}

	for k := range pkgMap {
		updated = true
		pkgst := &appv1alpha1.SubscriptionUnitStatus{}
		clst.SubscriptionPackageStatus[k] = pkgst
	}

	glog.V(10).Info("Done checking ", updated, pkgMap, sub.Status, clst)

	if updated {
		sub.Status.Statuses["/"] = clst

		glog.V(10).Info("Updating", sub.Status, sub.Status.Statuses["/"])

		sub.Status.LastUpdateTime = metav1.Now()
		err = statusClient.Status().Update(context.TODO(), sub)
		// want to print out the error log before leave
		if err != nil {
			glog.Error("Failed to update status of deployable ", err)
		}
	}

	return err
}

// OverrideResourceBySubscription alter the given template with overrides
func OverrideResourceBySubscription(template *unstructured.Unstructured,
	pkgName string, instance *appv1alpha1.Subscription) (*unstructured.Unstructured, error) {
	ovs := prepareOverrides(pkgName, instance)

	return OverrideTemplate(template, ovs)
}

func prepareOverrides(pkgName string, instance *appv1alpha1.Subscription) []dplv1alpha1.ClusterOverride {
	if instance == nil || instance.Spec.PackageOverrides == nil {
		return nil
	}

	var overrides []dplv1alpha1.ClusterOverride

	// go over clsuters to find matching override
	for _, ov := range instance.Spec.PackageOverrides {
		if ov.PackageName != pkgName {
			continue
		}

		for _, pov := range ov.PackageOverrides {
			overrides = append(overrides, dplv1alpha1.ClusterOverride(pov))
		}
	}

	return overrides
}
