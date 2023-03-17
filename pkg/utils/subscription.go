// Copyright 2021 The Kubernetes Authors.
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
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	addonV1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterapi "open-cluster-management.io/api/cluster/v1beta1"
	manifestWorkV1 "open-cluster-management.io/api/work/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appsubReportV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	managedClusterView "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/view/v1beta1"

	corev1 "k8s.io/api/core/v1"
	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
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
	addonServiceAccountName      = "application-manager"
	addonServiceAccountNamespace = "open-cluster-management-agent-addon"
)

// PlacementDecisionPredicateFunctions filters PlacementDecision status decisions update
var PlacementDecisionPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newPd := e.ObjectNew.(*clusterapi.PlacementDecision)
		oldPd := e.ObjectOld.(*clusterapi.PlacementDecision)

		return !reflect.DeepEqual(newPd.Status.Decisions, oldPd.Status.Decisions)
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},

	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
}

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

var AppSubSummaryPredicateFunc = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		klog.Info("UpdateFunc oldlabels:", e.ObjectOld.GetLabels())
		var clusterLabel string
		_, oldOK := e.ObjectOld.GetLabels()["apps.open-cluster-management.io/cluster"]
		clusterLabel, newOK := e.ObjectNew.GetLabels()["apps.open-cluster-management.io/cluster"]

		if !oldOK || !newOK || clusterLabel == "" {
			klog.V(1).Infof("Not a managed cluster appSubPackageStatus updated, old: %v/%v, new: %v/%v",
				e.ObjectOld.GetNamespace(), e.ObjectOld.GetName(), e.ObjectNew.GetNamespace(), e.ObjectNew.GetName())
			return false
		}

		oldAppSubSummary, ok := e.ObjectOld.(*appsubReportV1alpha1.SubscriptionReport)
		if !ok {
			klog.V(1).Infof("Not a valid managed cluster appSubPackageStatus, old: %v/%v", e.ObjectOld.GetNamespace(), e.ObjectOld.GetName())
			return false
		}

		newAppSubSummary, ok := e.ObjectNew.(*appsubReportV1alpha1.SubscriptionReport)
		if !ok {
			klog.V(1).Infof("Not a valid managed cluster appSubPackageStatus, new: %v/%v", e.ObjectNew.GetNamespace(), e.ObjectNew.GetName())
			return false
		}

		return !equality.Semantic.DeepEqual(oldAppSubSummary, newAppSubSummary)
	},
	CreateFunc: func(e event.CreateEvent) bool {
		klog.Info("CreateFunc oldlabels:", e.Object.GetLabels())

		var clusterLabel string
		clusterLabel, ok := e.Object.GetLabels()["apps.open-cluster-management.io/cluster"]

		if !ok || clusterLabel == "" {
			klog.V(1).Infof("Not a managed cluster appSubPackageStatus created: %v/%v", e.Object.GetNamespace(), e.Object.GetName())
			return false
		}

		klog.V(1).Infof("New managed cluster appSubPackageStatus created: %v/%v", e.Object.GetNamespace(), e.Object.GetName())
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		klog.Info("DeleteFunc oldlabels:", e.Object.GetLabels())

		var clusterLabel string
		clusterLabel, ok := e.Object.GetLabels()["apps.open-cluster-management.io/cluster"]
		if !ok || clusterLabel == "" {
			klog.V(1).Infof("Not a managed cluster appSubPackageStatus deleted: %v/%v", e.Object.GetNamespace(), e.Object.GetName())
			return false
		}

		klog.Infof("managed cluster appSubPackageStatus deleted: %v/%v", e.Object.GetNamespace(), e.Object.GetName())
		return true
	},
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
	// we don't actually care about the status, when create a manifestwork for
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

// GetHostSubscriptionFromObject extract the namespacedname of subscription hosting the object resource
func GetHostSubscriptionFromObject(obj metav1.Object) *types.NamespacedName {
	if obj == nil {
		return nil
	}

	objanno := obj.GetAnnotations()
	if objanno == nil {
		return nil
	}

	sourcestr := objanno[appv1.AnnotationHosting]
	if sourcestr == "" {
		return nil
	}

	parsedstr := strings.Split(sourcestr, "/")
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

func UpdateLastUpdateTime(clt client.Client, instance *appv1.Subscription) {
	curSub := &appv1.Subscription{}
	if err := clt.Get(context.TODO(), types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, curSub); err != nil {
		klog.Warning("Failed to get appsub to update LastUpdateTime", err)
		return
	}

	curSub.Status.LastUpdateTime = metav1.Now()

	if err := clt.Status().Update(context.TODO(), curSub); err != nil {
		klog.Warning("Failed to update LastUpdateTime", err)
	}
}

// OverrideResourceBySubscription alter the given template with overrides
func OverrideResourceBySubscription(template *unstructured.Unstructured,
	pkgName string, instance *appv1.Subscription) (*unstructured.Unstructured, error) {
	ovs := prepareOverrides(pkgName, instance)

	return OverrideTemplate(template, ovs)
}

func prepareOverrides(pkgName string, instance *appv1.Subscription) []appv1.ClusterOverride {
	if instance == nil || instance.Spec.PackageOverrides == nil {
		return nil
	}

	var overrides []appv1.ClusterOverride

	// go over clsuters to find matching override
	for _, ov := range instance.Spec.PackageOverrides {
		if ov.PackageName != pkgName {
			continue
		}

		for _, pov := range ov.PackageOverrides {
			overrides = append(overrides, appv1.ClusterOverride(pov))
		}
	}

	return overrides
}

// KeywordsChecker Checks if the helm chart has at least 1 keyword from the packageFilter.Keywords array
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

// DeleteSubscriptionCRD deletes the Subscription CRD
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

		_, err := crdx.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), "multiclusterhubs.operator.open-cluster-management.io", metav1.GetOptions{})

		if err != nil && kerrors.IsNotFound(err) {
			klog.Info("This is not ACM hub cluster. Deleting subscription CRD.")
			// now get rid of the crd.
			err = crdx.ApiextensionsV1().CustomResourceDefinitions().Delete(context.TODO(), "subscriptions.apps.open-cluster-management.io", metav1.DeleteOptions{})
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
		delete(objanno, appv1.AnnotationHostingDeployable)
		delete(objanno, appv1.AnnotationChannelType)
	}

	if len(objanno) > 0 {
		obj.SetAnnotations(objanno)
	} else {
		obj.SetAnnotations(nil)
	}

	return obj
}

// RemoveSubOwnerRef removes RHACM specific owner reference from subscription
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
		if strings.EqualFold(chType, chnv1.ChannelTypeObjectBucket) {
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

// CompareManifestWork compare two manifestWorks and return true if they are equal.
func CompareManifestWork(oldManifestWork, newManifestWork *manifestWorkV1.ManifestWork) bool {
	if len(oldManifestWork.Spec.Workload.Manifests) != len(newManifestWork.Spec.Workload.Manifests) {
		klog.V(1).Infof("oldManifestWork length: %v, newManifestWork length: %v",
			len(oldManifestWork.Spec.Workload.Manifests), len(newManifestWork.Spec.Workload.Manifests))
		return false
	}

	for i := 0; i < len(oldManifestWork.Spec.Workload.Manifests); i++ {
		oldManifest := &unstructured.Unstructured{}
		newManifest := &unstructured.Unstructured{}

		err := json.Unmarshal(oldManifestWork.Spec.Workload.Manifests[i].Raw, oldManifest)
		if err != nil {
			klog.Errorf("failed to unmarshal old manifestwork, err: %v", err)
			return false
		}

		klog.V(1).Infof("=====\n oldManifest: %#v", oldManifest)

		err = json.Unmarshal(newManifestWork.Spec.Workload.Manifests[i].Raw, newManifest)
		if err != nil {
			klog.Errorf("failed to unmarshal new manifestwork, err: %v", err)
			return false
		}

		klog.V(1).Infof("=====\n newManifest: %#v", newManifest)

		if !isSameUnstructured(oldManifest, newManifest) {
			klog.V(1).Infof("old template: %#v, new template: %#v", oldManifest, newManifest)
			return false
		}
	}

	return true
}

// isSameUnstructured compares the two unstructured object.
// The comparison ignores the metadata and status field, and check if the two objects are semantically equal.
func isSameUnstructured(obj1, obj2 *unstructured.Unstructured) bool {
	obj1Copy := obj1.DeepCopy()
	obj2Copy := obj2.DeepCopy()

	// Compare gvk, name, namespace at first
	if obj1Copy.GroupVersionKind() != obj2Copy.GroupVersionKind() {
		return false
	}

	if obj1Copy.GetName() != obj2Copy.GetName() {
		return false
	}

	if obj1Copy.GetNamespace() != obj2Copy.GetNamespace() {
		return false
	}

	// Compare label and annotations
	if !equality.Semantic.DeepEqual(obj1Copy.GetLabels(), obj2Copy.GetLabels()) {
		return false
	}

	if !equality.Semantic.DeepEqual(obj1Copy.GetAnnotations(), obj2Copy.GetAnnotations()) {
		return false
	}

	// Compare semantically after removing metadata and status field
	delete(obj1Copy.Object, "metadata")
	delete(obj2Copy.Object, "metadata")
	delete(obj1Copy.Object, "status")
	delete(obj2Copy.Object, "status")

	return equality.Semantic.DeepEqual(obj1Copy.Object, obj2Copy.Object)
}

// IsHostingAppsub return true if contains hosting annotation
func IsHostingAppsub(appsub *appv1.Subscription) bool {
	if appsub == nil {
		return false
	}

	annotations := appsub.GetAnnotations()
	if annotations == nil {
		return false
	}

	_, ok := annotations[appv1.AnnotationHosting]

	return ok
}

// ParseAPIVersion return group and version from a given apiVersion string
func ParseAPIVersion(apiVersion string) (string, string) {
	parsedstr := strings.Split(apiVersion, "/")
	if len(parsedstr) == 1 {
		return "", parsedstr[0]
	}

	if len(parsedstr) != 2 {
		return "", ""
	}

	return parsedstr[0], parsedstr[1]
}

// ParseNamespacedName return namespace and name from a given "namespace/name" string
func ParseNamespacedName(namespacedName string) (string, string) {
	parsedstr := strings.Split(namespacedName, "/")

	if len(parsedstr) != 2 {
		klog.Infof("invalid namespacedName: %v", namespacedName)
		return "", ""
	}

	return parsedstr[0], parsedstr[1]
}

// FetchChannelReferences best-effort to return the channel secret and configmap if they exist
func FetchChannelReferences(clt client.Client, chn chnv1.Channel) (sec *corev1.Secret, cm *corev1.ConfigMap) {
	if chn.Spec.SecretRef != nil {
		secret := &corev1.Secret{}

		chnseckey := types.NamespacedName{
			Name:      chn.Spec.SecretRef.Name,
			Namespace: chn.GetNamespace(),
		}

		if chn.Spec.SecretRef.Namespace != "" {
			chnseckey.Namespace = chn.Spec.SecretRef.Namespace
		}

		if err := clt.Get(context.TODO(), chnseckey, secret); err != nil {
			klog.Warningf("failed to get reference secret from channel err: %v, chnseckey: %v", err, chnseckey)
		} else {
			sec = secret
		}
	}

	if chn.Spec.ConfigMapRef != nil {
		configMap := &corev1.ConfigMap{}

		chncmkey := types.NamespacedName{
			Name:      chn.Spec.ConfigMapRef.Name,
			Namespace: chn.GetNamespace(),
		}

		if chn.Spec.ConfigMapRef.Namespace != "" {
			chncmkey.Namespace = chn.Spec.ConfigMapRef.Namespace
		}

		if err := clt.Get(context.TODO(), chncmkey, configMap); err != nil {
			klog.Warningf("failed to get reference configmap from channel err: %v, chnseckey: %v", err, chncmkey)
		} else {
			cm = configMap
		}
	}

	return sec, cm
}

// IsReadyManagedClusterView check if managed cluster view API is ready or not.
func IsReadyManagedClusterView(clReader client.Reader) bool {
	viewList := &managedClusterView.ManagedClusterViewList{}

	listopts := &client.ListOptions{}

	err := clReader.List(context.TODO(), viewList, listopts)
	if err != nil {
		klog.Error("Managed Cluster View API NOT ready: ", err)

		return false
	}

	klog.Info("Managed Cluster View API is ready")

	return true
}

// IsReadyPlacementDecision check if Placement Decision API is ready or not.
func IsReadyPlacementDecision(clReader client.Reader) bool {
	pdlist := &clusterapi.PlacementDecisionList{}

	listopts := &client.ListOptions{}

	err := clReader.List(context.TODO(), pdlist, listopts)
	if err != nil {
		klog.Error("Placement Decision API NOT ready: ", err)

		return false
	}

	cmalist := &addonV1alpha1.ClusterManagementAddOnList{}

	err = clReader.List(context.TODO(), cmalist, listopts)
	if err != nil {
		klog.Error("Cluster Management Addon API NOT ready: ", err)

		return false
	}

	klog.Info("Placement Decision and Cluster Management Addon APIs are ready")

	return true
}

func CreateClusterManagementAddon(clt client.Client) {
	cma := &addonV1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: "application-manager",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterManagementAddOn",
			APIVersion: "addon.open-cluster-management.io/v1alpha1",
		},
		Spec: addonV1alpha1.ClusterManagementAddOnSpec{
			AddOnMeta: addonV1alpha1.AddOnMeta{
				Description: "Processes events and other requests to managed resources.",
				DisplayName: "Application Manager",
			},
		},
	}

	err := clt.Create(context.TODO(), cma)
	if err != nil {
		klog.Error(err.Error())
	}
}

// DetectPlacementDecision - Detect the Placement Decision API every 10 seconds. the controller will be exited when it is ready
// The controller will be auto restarted by the multicluster-operators-application deployment CR later.
//
//nolint:unparam
func DetectPlacementDecision(ctx context.Context, clReader client.Reader, clt client.Client) {
	if !IsReadyPlacementDecision(clReader) {
		go wait.UntilWithContext(ctx, func(ctx context.Context) {
			if IsReadyPlacementDecision(clReader) {
				CreateClusterManagementAddon(clt)

				os.Exit(1)
			}
		}, time.Duration(10)*time.Second)
	}
}

func GetClientConfigFromKubeConfig(kubeconfigFile string) (*rest.Config, error) {
	if len(kubeconfigFile) > 0 {
		return getClientConfig(kubeconfigFile)
	}

	return nil, errors.New("no kubeconfig file found")
}

func getClientConfig(kubeConfigFile string) (*rest.Config, error) {
	kubeConfigBytes, err := ioutil.ReadFile(filepath.Clean(kubeConfigFile))
	if err != nil {
		return nil, err
	}

	kubeConfig, err := clientcmd.NewClientConfigFromBytes(kubeConfigBytes)
	if err != nil {
		return nil, err
	}

	clientConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	return clientConfig, nil
}
