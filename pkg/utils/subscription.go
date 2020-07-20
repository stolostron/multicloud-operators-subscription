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
	"crypto/sha1" // #nosec G505 Used only to generate random value to be used to generate hash string
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"

	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	//Max is 52 chars but as helm add behind the scene extension -delete-registrations for some objects
	//The new limit is 31 chars
	maxNameLength = 52 - len("-delete-registrations")
	randomLength  = 5
	//minus 1 because we add a dash
	maxGeneratedNameLength = maxNameLength - randomLength - 1
)

// SubscriptionPredicateFunctions filters status update
var SubscriptionPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		subOld := e.ObjectOld.(*appv1.Subscription)
		subNew := e.ObjectNew.(*appv1.Subscription)

		// need to process delete with finalizers
		if len(subNew.GetFinalizers()) > 0 {
			return true
		}

		// we care label change, pass it down
		if !reflect.DeepEqual(subOld.GetLabels(), subNew.GetLabels()) {
			return true
		}

		// In hub cluster, these annotations get updated by subscription reconcile
		// so remove them before comparison to avoid triggering another reconciliation.
		oldAnnotations := subOld.GetAnnotations()
		newAnnotations := subNew.GetAnnotations()
		delete(oldAnnotations, appv1.AnnotationDeployables)
		delete(oldAnnotations, appv1.AnnotationTopo)
		delete(newAnnotations, appv1.AnnotationDeployables)
		delete(newAnnotations, appv1.AnnotationTopo)

		// we care annotation change. pass it down
		if !reflect.DeepEqual(oldAnnotations, newAnnotations) {
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

// DeployablePredicateFunctions filters status update
var DeployablePredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newdpl := e.ObjectNew.(*dplv1.Deployable)
		olddpl := e.ObjectOld.(*dplv1.Deployable)

		return !reflect.DeepEqual(newdpl.Status, olddpl.Status)
	},
	CreateFunc: func(e event.CreateEvent) bool {
		newdpl := e.Object.(*dplv1.Deployable)

		labels := newdpl.GetLabels()

		// Git type subscription reconciliation deletes and recreates deployables.
		if strings.EqualFold(labels[chnv1.KeyChannelType], chnv1.ChannelTypeGitHub) ||
			strings.EqualFold(labels[chnv1.KeyChannelType], chnv1.ChannelTypeGit) {
			return false
		}

		return true
	},

	DeleteFunc: func(e event.DeleteEvent) bool {
		dpl := e.Object.(*dplv1.Deployable)

		labels := dpl.GetLabels()

		// Git type subscription reconciliation deletes and recreates deployables.
		if strings.EqualFold(labels[chnv1.KeyChannelType], chnv1.ChannelTypeGitHub) ||
			strings.EqualFold(labels[chnv1.KeyChannelType], chnv1.ChannelTypeGit) {
			return false
		}

		return true
	},
}

// GetSourceFromObject extract the namespacedname of subscription hosting the object resource
func GetSourceFromObject(obj metav1.Object) string {
	if obj == nil {
		return ""
	}

	objanno := obj.GetAnnotations()
	if objanno == nil {
		return ""
	}

	return objanno[appv1.AnnotationSyncSource]
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

	sourcestr := objanno[appv1.AnnotationSyncSource]
	if sourcestr == "" {
		return nil
	}

	pos := strings.Index(sourcestr, "-")
	if pos == -1 {
		return nil
	}

	hosttr := sourcestr[pos+1:]

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
func SetInClusterPackageStatus(substatus *appv1.SubscriptionStatus, pkgname string, pkgerr error, status interface{}) error {
	newStatus := substatus.DeepCopy()
	if newStatus.Statuses == nil {
		newStatus.Statuses = make(map[string]*appv1.SubscriptionPerClusterStatus)
	}

	clst := newStatus.Statuses["/"]
	if clst == nil || clst.SubscriptionPackageStatus == nil {
		clst = &appv1.SubscriptionPerClusterStatus{}
		clst.SubscriptionPackageStatus = make(map[string]*appv1.SubscriptionUnitStatus)
	}

	pkgstatus := clst.SubscriptionPackageStatus[pkgname]
	if pkgstatus == nil {
		pkgstatus = &appv1.SubscriptionUnitStatus{}
	}

	if pkgerr == nil {
		pkgstatus.Phase = appv1.SubscriptionSubscribed
		pkgstatus.Reason = ""
		pkgstatus.Message = ""
	} else {
		pkgstatus.Phase = appv1.SubscriptionFailed
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

	klog.V(5).Info("Set package status: ", pkgstatus)

	clst.SubscriptionPackageStatus[pkgname] = pkgstatus
	newStatus.Statuses["/"] = clst

	newStatus.LastUpdateTime = metav1.Now()

	if !isEqualSubscriptionStatus(substatus, newStatus) {
		newStatus.DeepCopyInto(substatus)
	}

	return nil
}

func isEqualSubscriptionStatus(a, b *appv1.SubscriptionStatus) bool {
	if a == nil && b == nil {
		return true
	}

	if a != nil && b == nil {
		return false
	}

	if a == nil && b != nil {
		return false
	}

	if a.Message != b.Message || a.Phase != b.Phase || a.Reason != b.Reason {
		return false
	}

	aMap, bMap := a.Statuses, b.Statuses
	if len(aMap) != len(bMap) {
		return false
	}

	if len(aMap) == 0 {
		return true
	}

	return isEqualSubClusterStatus(aMap, bMap)
}

func isEqualSubClusterStatus(a, b map[string]*appv1.SubscriptionPerClusterStatus) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if w, ok := b[k]; !ok || !isEqualSubPerClusterStatus(v.SubscriptionPackageStatus, w.SubscriptionPackageStatus) {
			return false
		}
	}

	return true
}

func isEqualSubPerClusterStatus(a, b map[string]*appv1.SubscriptionUnitStatus) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if w, ok := b[k]; !ok || !isEqualSubscriptionUnitStatus(v, w) {
			return false
		}
	}

	return true
}

func isEqualSubscriptionUnitStatus(a, b *appv1.SubscriptionUnitStatus) bool {
	if a == nil && b == nil {
		return true
	}

	if a != nil && b == nil {
		return false
	}

	if a == nil && b != nil {
		return false
	}

	if a.Phase != b.Phase || a.Message != b.Message || a.Reason != b.Reason ||
		!reflect.DeepEqual(a.ResourceStatus, b.ResourceStatus) {
		return false
	}

	return true
}

// DeleteInClusterPackageStatus deletes a package status
func DeleteInClusterPackageStatus(substatus *appv1.SubscriptionStatus, pkgname string, pkgerr error, status interface{}) {
	if substatus.Statuses != nil {
		clst := substatus.Statuses["/"]
		if clst != nil && clst.SubscriptionPackageStatus != nil {
			klog.V(3).Info("Deleting " + pkgname + " from the package status.")
			delete(clst.SubscriptionPackageStatus, pkgname)
		}
	}

	substatus.LastUpdateTime = metav1.Now()
}

// UpdateSubscriptionStatus based on error message, and propagate resource status
// - nil:  success
// - others: failed, with error message in reason
func UpdateSubscriptionStatus(statusClient client.Client, templateerr error, tplunit metav1.Object, status interface{}, deletePkg bool) error {
	klog.V(10).Info("Trying to update subscription status:", templateerr, tplunit.GetNamespace(), "/", tplunit.GetName(), status)

	if tplunit == nil {
		return nil
	}

	sub := &appv1.Subscription{}
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

		return errors.New(errmsg)
	}

	if deletePkg {
		DeleteInClusterPackageStatus(&sub.Status, dplkey.Name, templateerr, status)
	} else {
		err = SetInClusterPackageStatus(&sub.Status, dplkey.Name, templateerr, status)
		if err != nil {
			klog.Error("Failed to set package status for subscription: ", sub.Namespace+"/"+sub.Name, ". error: ", err)
			return err
		}
	}

	err = statusClient.Status().Update(context.TODO(), sub)
	// want to print out the error log before leave
	if err != nil {
		klog.Error("Failed to update status of deployable ", err)
	}

	return err
}

// ValidatePackagesInSubscriptionStatus validate the status struture for packages
func ValidatePackagesInSubscriptionStatus(statusClient client.StatusClient, sub *appv1.Subscription, pkgMap map[string]bool) error {
	var err error

	updated := false

	if sub.Status.Statuses == nil {
		sub.Status.Statuses = make(map[string]*appv1.SubscriptionPerClusterStatus)
		updated = true
	}

	clst := sub.Status.Statuses["/"]
	if clst == nil {
		clst = &appv1.SubscriptionPerClusterStatus{}
		updated = true
	}

	klog.V(10).Info("valiating subscription status:", pkgMap, sub.Status, clst)

	if clst.SubscriptionPackageStatus == nil {
		clst.SubscriptionPackageStatus = make(map[string]*appv1.SubscriptionUnitStatus)
		updated = true
	}

	for k := range clst.SubscriptionPackageStatus {
		if _, ok := pkgMap[k]; !ok {
			updated = true

			delete(clst.SubscriptionPackageStatus, k)
		} else {
			pkgst := clst.SubscriptionPackageStatus[k]
			if pkgst.Phase == appv1.SubscriptionFailed {
				updated = true
			}
			delete(pkgMap, k)
		}
	}

	for k := range pkgMap {
		updated = true
		pkgst := &appv1.SubscriptionUnitStatus{}
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
	pkgName string, instance *appv1.Subscription) (*unstructured.Unstructured, error) {
	ovs := prepareOverrides(pkgName, instance)

	return OverrideTemplate(template, ovs)
}

func prepareOverrides(pkgName string, instance *appv1.Subscription) []dplv1.ClusterOverride {
	if instance == nil || instance.Spec.PackageOverrides == nil {
		return nil
	}

	var overrides []dplv1.ClusterOverride

	// go over clsuters to find matching override
	for _, ov := range instance.Spec.PackageOverrides {
		if ov.PackageName != pkgName {
			continue
		}

		for _, pov := range ov.PackageOverrides {
			overrides = append(overrides, dplv1.ClusterOverride(pov))
		}
	}

	return overrides
}

type objAnno interface {
	GetAnnotations() map[string]string
}

// FilterPackageOut process the package filter logic
func CanPassPackageFilter(filter *appv1.PackageFilter, obj objAnno) bool {
	if filter == nil {
		return true
	}

	if filter.Annotations == nil || len(filter.Annotations) == 0 {
		return true
	}

	klog.V(5).Info("checking annotations package filter: ", filter)

	objAnno := obj.GetAnnotations()
	if len(objAnno) == 0 {
		return false
	}

	for k, v := range filter.Annotations {
		if objAnno[k] != v {
			klog.V(5).Infof("Annotation filter does not match. Sub annotation is: %v; Dpl annotation value is %v;", filter.Annotations, objAnno)

			return false
		}
	}

	return true
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

// GetReleaseName alters the given name in a deterministic way if the length exceed the maximum character
func GetReleaseName(base string) (string, error) {
	if len(base) > maxNameLength {
		h := sha1.New() // #nosec G401 Used only to generate random value to be used to generate hash string
		_, err := h.Write([]byte(base))

		if err != nil {
			klog.Error("Failed to generate sha1 hash for: ", base, " error: ", err)
			return "", err
		}

		sha1Hash := hex.EncodeToString(h.Sum(nil))

		//minus 1 because adding "-"
		base = base[:maxGeneratedNameLength]

		return fmt.Sprintf("%s-%s", base, sha1Hash[:randomLength]), nil
	}

	return base, nil
}

// GetPauseLabel check if the subscription-pause label exists
func GetPauseLabel(instance *appv1.Subscription) bool {
	labels := instance.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	if labels[appv1.LabelSubscriptionPause] != "" && strings.EqualFold(labels[appv1.LabelSubscriptionPause], "true") {
		return true
	}

	return false
}

//DeleteSubscriptionCRD deletes the Subscription CRD
func DeleteSubscriptionCRD(runtimeClient client.Client, crdx *clientsetx.Clientset) {
	sublist := &appv1.SubscriptionList{}
	err := runtimeClient.List(context.TODO(), sublist, &client.ListOptions{})

	if err != nil && !kerrors.IsNotFound(err) {
		klog.Infof("subscription kind is gone. err: %s", err.Error())
		os.Exit(0)
	} else {
		for _, sub := range sublist.Items {
			klog.V(1).Infof("Found %s", sub.SelfLink)
			// remove all finalizers
			sub = *sub.DeepCopy()
			sub.SetFinalizers([]string{})
			err = runtimeClient.Update(context.TODO(), &sub) // #nosec G601 requires "k8s.io/apimachinery/pkg/runtime" object
			if err != nil {
				klog.Warning(err)
			}
		}
		// now get rid of the crd
		err = crdx.ApiextensionsV1().CustomResourceDefinitions().Delete(context.TODO(), "subscriptions.apps.open-cluster-management.io", v1.DeleteOptions{})
		if err != nil {
			klog.Infof("Deleting subscription CRD failed. err: %s", err.Error())
		} else {
			klog.Info("subscription CRD removed")
		}
	}
}
