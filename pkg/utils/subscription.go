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
	"reflect"
	"strings"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SubscriptionPredicateFunctions filters status update
var SubscriptionPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		subOld := e.ObjectOld.(*appv1alpha1.Subscription)
		subNew := e.ObjectNew.(*appv1alpha1.Subscription)

		// need to process delete with finalizers
		if len(subNew.GetFinalizers()) > 0 {
			return true
		}

		// we care label change, pass it down
		if !reflect.DeepEqual(subOld.GetLabels(), subNew.GetLabels()) {
			return true
		}

		// we care annotation change. pass it down
		if !reflect.DeepEqual(subOld.GetAnnotations(), subNew.GetAnnotations()) {
			return true
		}

		// we care spec for sure
		if !reflect.DeepEqual(subOld.Spec, subNew.Spec) {
			return true
		}

		// do we care phase change?
		if subNew.Status.Phase == "" || subNew.Status.Phase != subOld.Status.Phase {
			klog.V(5).Info("We care phase..", subNew.Status.Phase, " vs ", subOld.Status.Phase)
			return true
		}

		klog.V(5).Info("Something we don't care changed")
		return false
	},
}

// GetHostSubscriptionFromObject extract the namespacedname of subscription hosting the object resource
func GetSourceFromObject(obj metav1.Object) string {
	if obj == nil {
		return ""
	}

	objanno := obj.GetAnnotations()
	if objanno == nil {
		return ""
	}

	return objanno[appv1alpha1.AnnotationSyncSource]
}

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
			klog.Info("Failed to mashall status for ", status, " with err:", err)
		}
	} else {
		pkgstatus.ResourceStatus = nil
	}

	klog.V(10).Info("Set package status: ", pkgstatus)

	clst.SubscriptionPackageStatus[pkgname] = pkgstatus
	substatus.Statuses["/"] = clst

	substatus.LastUpdateTime = metav1.Now()

	return nil
}

// UpdateSubscriptionStatus based on error message, and propagate resource status
// - nil:  success
// - others: failed, with error message in reason
func UpdateSubscriptionStatus(statusClient client.Client, templateerr error, tplunit metav1.Object, status interface{}) error {
	klog.V(10).Info("Trying to update subscription status:", templateerr, tplunit.GetNamespace(), "/", tplunit.GetName(), status)

	if tplunit == nil {
		return nil
	}

	sub := &appv1alpha1.Subscription{}
	subkey := GetHostSubscriptionFromObject(tplunit)

	if subkey == nil {
		klog.Info("The template", tplunit.GetNamespace(), "/", tplunit.GetName(), " does not have hosting subscription", tplunit.GetAnnotations())
		return nil
	}

	err := statusClient.Get(context.TODO(), *subkey, sub)

	if err != nil {
		// for all errors including not found return
		klog.Info("Failed to get subscription object ", *subkey, " to set status, error:", err)
		return err
	}

	dplkey := GetHostDeployableFromObject(tplunit)
	if dplkey == nil {
		errmsg := "Invalid status structure in subscription: " + sub.GetNamespace() + "/" + sub.Name + " nil hosting deployable"
		klog.Info(errmsg)
		err = errors.New(errmsg)

		return err
	}

	err = SetInClusterPackageStatus(&sub.Status, dplkey.Name, templateerr, status)
	if err != nil {
		klog.Error("Failed to set package status for subscription: ", sub.Namespace+"/"+sub.Name, ". error: ", err)
		return err
	}

	err = statusClient.Status().Update(context.TODO(), sub)
	// want to print out the error log before leave
	if err != nil {
		klog.Error("Failed to update status of deployable ", err)
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

	klog.V(10).Info("valiating subscription status:", pkgMap, sub.Status, clst)

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

	klog.V(10).Info("Done checking ", updated, pkgMap, sub.Status, clst)

	if updated {
		sub.Status.Statuses["/"] = clst

		klog.V(10).Info("Updating", sub.Status, sub.Status.Statuses["/"])

		sub.Status.LastUpdateTime = metav1.Now()
		err = statusClient.Status().Update(context.TODO(), sub)
		// want to print out the error log before leave
		if err != nil {
			klog.V(1).Info("Failed to update status of subscription in subscriber loop", err)
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

func FiltePackageOut(filter *appv1alpha1.PackageFilter, dpl *dplv1alpha1.Deployable) bool {
	if filter == nil {
		return false
	}

	klog.V(10).Info("checking annotations package filter: ", filter)

	matched := true

	if filter.Annotations != nil {
		dplanno := dpl.GetAnnotations()
		if dplanno == nil {
			dplanno = make(map[string]string)
		}

		for k, v := range filter.Annotations {
			if dplanno[k] != v {
				klog.V(10).Infof("Annotation filter does not match. Sub annotation is: %v; Dpl annotation value is %v;", filter.Annotations, dplanno)

				matched = false

				break
			}
		}

		if !matched {
			return true
		}
	}

	return !matched
}

//KeywordsChecker Checks if the helm chart has at least 1 keyword from the packageFilter.Keywords array
func KeywordsChecker(labelSelector *metav1.LabelSelector, ks []string) bool {
	ls := make(map[string]string)
	for _, k := range ks {
		ls[k] = "true"
	}

	return LabelsChecker(labelSelector, ls)
}

// LabelsChecker checks labels against a labelSelector
func LabelsChecker(labelSelector *metav1.LabelSelector, ls map[string]string) bool {
	clSelector, err := ConvertLabels(labelSelector)
	if err != nil {
		klog.Error("Failed to set label selector: ", labelSelector, " err:", err)
	}

	return clSelector.Matches(labels.Set(ls))
}
