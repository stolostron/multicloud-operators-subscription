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
	"time"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	plrv1 "github.com/open-cluster-management/multicloud-operators-placementrule/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"

	corev1 "k8s.io/api/core/v1"
	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
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
	annotationsSep         = ","
	maxGeneratedNameLength = maxNameLength - randomLength - 1
	// klusterletagentaddon secret token reconcile
	addonServiceAccountName      = "klusterlet-addon-appmgr"
	addonServiceAccountNamespace = "open-cluster-management-agent-addon"
)

func IsSubscriptionResourceChanged(oSub, nSub *appv1.Subscription) bool {
	if IsSubscriptionBasicChanged(oSub, nSub) {
		return true
	}

	// do we care phase change?
	if nSub.Status.Phase == "" || nSub.Status.Phase != oSub.Status.Phase {
		klog.V(5).Info("We care phase..", nSub.Status.Phase, " vs ", oSub.Status.Phase)
		return true
	}

	klog.V(5).Info("Something we don't care changed")

	return false
}

// SubscriptionPredicateFunctions filters status update
var SubscriptionPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		subOld := e.ObjectOld.(*appv1.Subscription)
		subNew := e.ObjectNew.(*appv1.Subscription)

		return IsSubscriptionResourceChanged(subOld, subNew)
	},
}

func IsSubscriptionBasicChanged(o, n *appv1.Subscription) bool {
	fOsub := FilterOutTimeRelatedFields(o)
	fNSub := FilterOutTimeRelatedFields(n)

	// need to process delete with finalizers
	if !reflect.DeepEqual(fOsub.GetFinalizers(), fNSub.GetFinalizers()) {
		return true
	}

	// we care label change, pass it down
	if !reflect.DeepEqual(fOsub.GetLabels(), fNSub.GetLabels()) {
		return true
	}

	// In hub cluster, these annotations get updated by subscription reconcile
	// so remove them before comparison to avoid triggering another reconciliation.
	oldAnnotations := fOsub.GetAnnotations()
	newAnnotations := fNSub.GetAnnotations()

	if !isEqualAnnotationFiled(oldAnnotations, newAnnotations, appv1.AnnotationDeployables) {
		return true
	}

	if !isEqualAnnotationFiled(oldAnnotations, newAnnotations, appv1.AnnotationTopo) {
		return true
	}

	// When user updates the desired Git commit or tag, this annotation is expected to change as well by reconcile.
	if !isEqualAnnotationFiled(oldAnnotations, newAnnotations, appv1.AnnotationGitCommit) {
		return true
	}

	// we care annotation change. pass it down
	if !reflect.DeepEqual(oldAnnotations, newAnnotations) {
		return true
	}

	// we care spec for sure, we use the generation of 2 object to track the
	// spec version
	//https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#status-subresource
	if !reflect.DeepEqual(fOsub.Spec, fNSub.Spec) {
		return true
	}

	return false
}

func isEqualAnnotationFiled(o, n map[string]string, key string) bool {
	oDpl := o[key]
	nDpl := n[key]

	oOut := stringToSet(oDpl, annotationsSep)
	nOut := stringToSet(nDpl, annotationsSep)

	delete(o, key)
	delete(n, key)

	return reflect.DeepEqual(oOut, nOut)
}

func stringToSet(in string, sep string) map[string]struct{} {
	out := map[string]struct{}{}

	for _, w := range strings.Split(in, sep) {
		if _, ok := out[w]; !ok {
			out[w] = struct{}{}
		}
	}

	return out
}

// the input object shouldn't be changed at all
func FilterOutTimeRelatedFields(in *appv1.Subscription) *appv1.Subscription {
	if in == nil {
		return nil
	}

	out := in.DeepCopy()

	anno := out.GetAnnotations()
	if len(anno) == 0 {
		anno = map[string]string{}
	}

	//annotation that contains time
	//also remove annotations that are added and updated by the subscription controller
	timeFields := []string{"kubectl.kubernetes.io/last-applied-configuration"}

	if anno[appv1.AnnotationGitTag] == "" && anno[appv1.AnnotationGitTargetCommit] == "" {
		timeFields = append(timeFields, appv1.AnnotationGitCommit)
	}

	for _, f := range timeFields {
		delete(anno, f)
	}

	out.SetAnnotations(anno)

	//set managedFields time to empty
	outF := []metav1.ManagedFieldsEntry{}

	out.SetManagedFields(outF)
	// we don't actually care about the status, when create a deployable for
	// given subscription

	return out
}

// assuming the message is only storing the window info, following format:
// cluster1:active,cluster2:block
func isSameMessage(aMsg, bMsg string) bool {
	aMap, bMap := stringToMap(aMsg), stringToMap(bMsg)
	if len(aMap) != len(bMap) {
		return false
	}

	for ak, av := range aMap {
		if bv, ok := bMap[ak]; !ok || av != bv {
			return false
		}
	}

	return true
}

func stringToMap(msg string) map[string]string {
	cunits := strings.Split(msg, ",")
	out := map[string]string{}

	for _, val := range cunits {
		u := strings.Split(val, ":")

		if len(u) == 2 {
			out[u[0]] = u[1]
		} else if len(u) == 1 {
			out[u[0]] = ""
		}
	}

	return out
}

func IsHubRelatedStatusChanged(old, nnew *appv1.SubscriptionStatus) bool {
	if !isAnsibleStatusEqual(old.AnsibleJobsStatus, nnew.AnsibleJobsStatus) {
		return true
	}

	if old.Phase != nnew.Phase || !isSameMessage(old.Message, nnew.Message) {
		return true
	}

	//care about the managed subscription status
	if !isEqualSubClusterStatus(old.Statuses, nnew.Statuses) {
		return true
	}

	return false
}

func isAnsibleStatusEqual(a, b appv1.AnsibleJobsStatus) bool {
	if a.LastPosthookJob != b.LastPosthookJob {
		return false
	}

	if a.LastPrehookJob != b.LastPrehookJob {
		return false
	}

	return true
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

// ChannelPredicateFunctions filters channel spec update
var ChannelPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newChn := e.ObjectNew.(*chnv1.Channel)
		oldChn := e.ObjectOld.(*chnv1.Channel)

		oldAnnotations := oldChn.GetAnnotations()
		newAnnotations := newChn.GetAnnotations()

		if !reflect.DeepEqual(oldAnnotations, newAnnotations) {
			return true
		}

		return !reflect.DeepEqual(newChn.Spec, oldChn.Spec)
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},

	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
}

// PlacementRulePredicateFunctions filters PlacementRule status decisions update
var PlacementRulePredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newPlr := e.ObjectNew.(*plrv1.PlacementRule)
		oldPlr := e.ObjectOld.(*plrv1.PlacementRule)

		return !reflect.DeepEqual(newPlr.Status.Decisions, oldPlr.Status.Decisions)
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},

	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
}

// ServiceAccountPredicateFunctions watches for changes in klusterlet-addon-appmgr service account in open-cluster-management-agent-addon namespace
var ServiceAccountPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newSA := e.ObjectNew.(*corev1.ServiceAccount)

		if strings.EqualFold(newSA.Namespace, addonServiceAccountNamespace) && strings.EqualFold(newSA.Name, addonServiceAccountName) {
			return true
		}

		return false
	},
	CreateFunc: func(e event.CreateEvent) bool {
		sa := e.Object.(*corev1.ServiceAccount)

		if strings.EqualFold(sa.Namespace, addonServiceAccountNamespace) && strings.EqualFold(sa.Name, addonServiceAccountName) {
			return true
		}

		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		sa := e.Object.(*corev1.ServiceAccount)

		if strings.EqualFold(sa.Namespace, addonServiceAccountNamespace) && strings.EqualFold(sa.Name, addonServiceAccountName) {
			return true
		}

		return false
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

	klog.V(1).Infof("Set package status, pkg: %v, pkgstatus: %#v", pkgname, pkgstatus)

	clst.SubscriptionPackageStatus[pkgname] = pkgstatus
	newStatus.Statuses["/"] = clst

	newStatus.LastUpdateTime = metav1.Now()

	if isEmptySubscriptionStatus(newStatus) || !isEqualSubscriptionStatus(substatus, newStatus) {
		newStatus.DeepCopyInto(substatus)
	}

	return nil
}

func isEmptySubscriptionStatus(a *appv1.SubscriptionStatus) bool {
	if a == nil {
		return true
	}

	if len(a.Message) != 0 || len(a.Phase) != 0 || len(a.Reason) != 0 || len(a.Statuses) != 0 {
		return false
	}

	return true
}

func IsEqualSubScriptionStatus(o, n *appv1.SubscriptionStatus) bool {
	return isEqualSubscriptionStatus(o, n)
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

	if !isSameMessage(a.Message, b.Message) {
		return false
	}

	if a.Phase != b.Phase || a.Reason != b.Reason {
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
		w, ok := b[k]
		if ok {
			if v == nil && w == nil {
				continue
			}

			if v != nil && w != nil && isEqualSubPerClusterStatus(v.SubscriptionPackageStatus, w.SubscriptionPackageStatus) {
				continue
			}

			return false
		}

		return false
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

	if !isSameMessage(a.Message, b.Message) {
		return false
	}

	if a.Phase != b.Phase || a.Reason != b.Reason ||
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
// status - current object status
// tplunit - new content of the current object
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

	newStatus := sub.Status.DeepCopy()

	if deletePkg {
		DeleteInClusterPackageStatus(newStatus, dplkey.Name, templateerr, status)
	} else {
		err = SetInClusterPackageStatus(newStatus, dplkey.Name, templateerr, status)
		if err != nil {
			klog.Error("Failed to set package status for subscription: ", sub.Namespace+"/"+sub.Name, ". error: ", err)
			return err
		}
	}

	if isEmptySubscriptionStatus(newStatus) || !isEqualSubscriptionStatus(&sub.Status, newStatus) {
		newStatus.DeepCopyInto(&sub.Status)
		sub.Status.LastUpdateTime = metav1.Now()

		if err := statusClient.Status().Update(context.TODO(), sub); err != nil {
			// want to print out the error log before leave
			klog.Errorf("Failed to update subscription status. sub: %v/%v, err: %v", sub.GetNamespace(), sub.GetName(), err)
			return err
		}
	}

	return nil
}

func SkipOrUpdateSubscriptionStatus(clt client.Client, oldSub *appv1.Subscription) error {
	curSub := &appv1.Subscription{}

	if err := clt.Get(context.TODO(), types.NamespacedName{Name: oldSub.GetName(), Namespace: oldSub.GetNamespace()}, oldSub); err != nil {
		return err
	}

	oldStatus := &oldSub.Status
	upStatus := &curSub.Status

	if isEmptySubscriptionStatus(upStatus) || !isEqualSubscriptionStatus(oldStatus, upStatus) {
		oldSub.Status = *upStatus
		oldSub.Status.LastUpdateTime = metav1.Now()

		if err := clt.Status().Update(context.TODO(), oldSub); err != nil {
			return err
		}
	}

	return nil
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

// AllowApplyTemplate check if the template is allowed to apply based on its hosting subscription pause label
// return false if the hosting subscription is paused.
func AllowApplyTemplate(localClient client.Client, template *unstructured.Unstructured) bool {
	// if the template is subscription kind, allow its update
	if strings.EqualFold(template.GetKind(), "Subscription") {
		return true
	}

	//if the template is not subscription kind, check if its hosting subscription is paused.
	sub := &appv1.Subscription{}
	subkey := GetHostSubscriptionFromObject(template)

	if subkey == nil {
		klog.V(1).Infof("The template does not have hosting subscription. template: %v/%v, annoation: %#v",
			template.GetNamespace(), template.GetName(), template.GetAnnotations())
		return true
	}

	err := localClient.Get(context.TODO(), *subkey, sub)

	if err != nil {
		klog.V(1).Infof("Failed to get subscription object. sub: %v, error: %v", *subkey, err)
		return true
	}

	if GetPauseLabel(sub) {
		return false
	}

	return true
}

// IsResourceAllowed checks if the resource is on application subscription's allow list. The allow list is used only
// if the subscription is created by subscription-admin user.
func IsResourceAllowed(resource unstructured.Unstructured, allowlist map[string]map[string]string, isAdmin bool) bool {
	// If subscription-admin, honor the allow list
	if isAdmin {
		// If allow list is empty, all resources are allowed for deploy
		if len(allowlist) == 0 {
			return true
		}

		return (allowlist[resource.GetAPIVersion()][resource.GetKind()] != "" ||
			allowlist[resource.GetAPIVersion()]["*"] != "")
	}

	// If not subscription-admin, ignore the allow list and don't allow policy
	return resource.GetAPIVersion() != "policy.open-cluster-management.io/v1"
}

// IsResourceDenied checks if the resource is on application subscription's deny list. The deny list is used only
// if the subscription is created by subscription-admin user.
func IsResourceDenied(resource unstructured.Unstructured, denyList map[string]map[string]string, isAdmin bool) bool {
	// If subscription-admin, honor the deny list
	if isAdmin {
		// If deny list is empty, all resources are NOT denied
		if len(denyList) == 0 {
			return false
		}

		return (denyList[resource.GetAPIVersion()][resource.GetKind()] != "" ||
			denyList[resource.GetAPIVersion()]["*"] != "")
	}

	// If not subscription-admin, ignore the deny list
	return false
}

// GetAllowDenyLists returns subscription's allow and deny lists as maps. It returns empty map if there is no list.
func GetAllowDenyLists(subscription appv1.Subscription) (map[string]map[string]string, map[string]map[string]string) {
	allowedGroupResources := make(map[string]map[string]string)

	if subscription.Spec.Allow != nil {
		for _, allowGroup := range subscription.Spec.Allow {
			for _, resource := range allowGroup.Kinds {
				klog.Info("allowing to deploy resource " + allowGroup.APIVersion + "/" + resource)

				if allowedGroupResources[allowGroup.APIVersion] == nil {
					allowedGroupResources[allowGroup.APIVersion] = make(map[string]string)
				}

				allowedGroupResources[allowGroup.APIVersion][resource] = resource
			}
		}
	}

	deniedGroupResources := make(map[string]map[string]string)

	if subscription.Spec.Deny != nil {
		for _, denyGroup := range subscription.Spec.Deny {
			for _, resource := range denyGroup.Kinds {
				klog.Info("denying to deploy resource " + denyGroup.APIVersion + "/" + resource)

				if deniedGroupResources[denyGroup.APIVersion] == nil {
					deniedGroupResources[denyGroup.APIVersion] = make(map[string]string)
				}

				deniedGroupResources[denyGroup.APIVersion][resource] = resource
			}
		}
	}

	return allowedGroupResources, deniedGroupResources
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
			annotations := sub.GetAnnotations()
			if !strings.EqualFold(annotations[appv1.AnnotationHosting], "") {
				klog.Infof("Found %s", sub.SelfLink)
				// remove all finalizers
				sub = *sub.DeepCopy()
				sub.SetFinalizers([]string{})
				err = runtimeClient.Update(context.TODO(), &sub) // #nosec G601 requires "k8s.io/apimachinery/pkg/runtime" object
				if err != nil {
					klog.Warning(err)
				}
			}
		}

		_, err := crdx.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), "multiclusterhubs.operator.open-cluster-management.io", v1.GetOptions{})

		if err != nil && kerrors.IsNotFound(err) {
			klog.Info("This is not ACM hub cluster. Deleting subscription CRD.")
			// now get rid of the crd.
			err = crdx.ApiextensionsV1().CustomResourceDefinitions().Delete(context.TODO(), "subscriptions.apps.open-cluster-management.io", v1.DeleteOptions{})
			if err != nil {
				klog.Infof("Deleting subscription CRD failed. err: %s", err.Error())
			} else {
				klog.Info("subscription CRD removed")
			}
		} else {
			klog.Info("This is ACM hub cluster. Deleting propagated subscriptions only. Not deleting subscription CRD.")
			for _, sub := range sublist.Items {
				annotations := sub.GetAnnotations()
				if !strings.EqualFold(annotations[appv1.AnnotationHosting], "") {
					klog.Infof("Deleting %s", sub.SelfLink)
					err = runtimeClient.Delete(context.TODO(), &sub) // #nosec G601 requires "k8s.io/apimachinery/pkg/runtime" object
					if err != nil {
						klog.Warning(err)
					}
				}
			}
		}
	}
}

// RemoveSubAnnotations removes RHACM specific annotations from subscription
func RemoveSubAnnotations(obj *unstructured.Unstructured) *unstructured.Unstructured {
	objanno := obj.GetAnnotations()
	if objanno != nil {
		delete(objanno, appv1.AnnotationClusterAdmin)
		delete(objanno, appv1.AnnotationHosting)
		delete(objanno, appv1.AnnotationSyncSource)
		delete(objanno, dplv1.AnnotationHosting)
		delete(objanno, appv1.AnnotationChannelType)
	}

	if len(objanno) > 0 {
		obj.SetAnnotations(objanno)
	} else {
		obj.SetAnnotations(nil)
	}

	return obj
}

// RemoveSubAnnotations removes RHACM specific owner reference from subscription
func RemoveSubOwnerRef(obj *unstructured.Unstructured) *unstructured.Unstructured {
	ownerRefs := obj.GetOwnerReferences()
	newOwnerRefs := []metav1.OwnerReference{}

	for _, ownerRef := range ownerRefs {
		if !strings.EqualFold(ownerRef.Kind, "Subscription") {
			newOwnerRefs = append(newOwnerRefs, ownerRef)
		}
	}

	if len(newOwnerRefs) > 0 {
		obj.SetOwnerReferences(newOwnerRefs)
	} else {
		obj.SetOwnerReferences(nil)
	}

	return obj
}

func IsSubscriptionBeDeleted(clt client.Client, subKey types.NamespacedName) bool {
	subIns := &appv1.Subscription{}

	if err := clt.Get(context.TODO(), subKey, subIns); err != nil {
		return kerrors.IsNotFound(err)
	}

	return !subIns.GetDeletionTimestamp().IsZero()
}

// IsHub determines the hub cluster by listing multiclusterhubs resource items
func IsHub(config *rest.Config) bool {
	var dl dynamic.ResourceInterface

	multiclusterHubGVR := schema.GroupVersionResource{
		Group:    "operator.open-cluster-management.io",
		Version:  "v1",
		Resource: "multiclusterhubs",
	}

	dynamicClient := dynamic.NewForConfigOrDie(config)

	dl = dynamicClient.Resource(multiclusterHubGVR)

	objlist, err := dl.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.Infof("No multiclusterHub resource found, err: %v", err)
			return false
		}

		klog.Infof("Listing multiclusterHub resource failed, exit... err: %v", err)
		os.Exit(1)
	}

	if objlist == nil {
		klog.Infof("obj list is nil, exit...")
		os.Exit(1)
	}

	objCount := len(objlist.Items) //nolint
	klog.Infof("multiclusterHub resource count: %v", objCount)

	return objCount > 0
}

// GetReconcileRate determines reconcile rate based on channel annotations
func GetReconcileRate(chnAnnotations, subAnnotations map[string]string) string {
	rate := "medium"

	// If the channel does not have reconcile-level, default it to medium
	if chnAnnotations[appv1.AnnotationResourceReconcileLevel] == "" {
		klog.Info("Setting reconcile-level to default: medium")

		rate = "medium"
	} else {
		if strings.EqualFold(chnAnnotations[appv1.AnnotationResourceReconcileLevel], "off") {
			rate = "off"
		} else if strings.EqualFold(chnAnnotations[appv1.AnnotationResourceReconcileLevel], "low") {
			rate = "low"
		} else if strings.EqualFold(chnAnnotations[appv1.AnnotationResourceReconcileLevel], "medium") {
			rate = "medium"
		} else if strings.EqualFold(chnAnnotations[appv1.AnnotationResourceReconcileLevel], "high") {
			rate = "high"
		} else {
			klog.Info("Channel's reconcile-level has unknown value: ", chnAnnotations[appv1.AnnotationResourceReconcileLevel])
			klog.Info("Setting it to medium")

			rate = "medium"
		}
	}

	// Reconcile level can be overridden to be
	if strings.EqualFold(subAnnotations[appv1.AnnotationResourceReconcileLevel], "off") {
		klog.Infof("Overriding channel's reconcile rate %s to turn it off", rate)
		rate = "off"
	}

	return rate
}

// GetReconcileInterval determines reconcile loop interval based on reconcileRate setting
func GetReconcileInterval(reconcileRate, chType string) (time.Duration, time.Duration, int) {
	interval := 3 * time.Minute       // reconcile interval
	retryInterval := 90 * time.Second // re-try interval when reconcile fails
	retryCount := 1                   // number of re-tries when reconcile fails

	if strings.EqualFold(reconcileRate, "low") {
		klog.Infof("setting auto-reconcile rate to low")

		interval = 1 * time.Hour // every hour
		retryInterval = 3 * time.Minute
		retryCount = 3
	} else if strings.EqualFold(reconcileRate, "medium") {
		klog.Infof("setting auto-reconcile rate to medium")

		interval = 3 * time.Minute // every 3 minutes
		if strings.EqualFold(chType, chnv1.ChannelTypeHelmRepo) {
			interval = 15 * time.Minute
		}
		retryInterval = 90 * time.Second
		retryCount = 1
	} else if strings.EqualFold(reconcileRate, "high") {
		klog.Infof("setting auto-reconcile rate to high")

		interval = 2 * time.Minute // every 2 minutes
		retryInterval = 60 * time.Second
		retryCount = 1
	}

	return interval, retryInterval, retryCount
}

func SetPartOfLabel(s *appv1.Subscription, rsc *unstructured.Unstructured) {
	rscLbls := AddPartOfLabel(s, rsc.GetLabels())
	if rscLbls != nil {
		rsc.SetLabels(rscLbls)
	}
}

func AddPartOfLabel(s *appv1.Subscription, m map[string]string) map[string]string {
	partOfLbl := s.Labels["app.kubernetes.io/part-of"]
	if partOfLbl != "" {
		if m == nil {
			m = make(map[string]string)
		}

		m["app.kubernetes.io/part-of"] = partOfLbl
	}

	return m
}
