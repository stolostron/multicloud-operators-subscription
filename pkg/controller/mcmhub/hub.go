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

package mcmhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	gerr "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/ghodss/yaml"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	dplutils "github.com/open-cluster-management/multicloud-operators-deployable/pkg/utils"
	plrv1alpha1 "github.com/open-cluster-management/multicloud-operators-placementrule/pkg/apis/apps/v1"
	releasev1 "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	subutil "github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	awsutils "github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils/aws"
)

// doMCMHubReconcile process Subscription on hub - distribute it via deployable
func (r *ReconcileSubscription) doMCMHubReconcile(sub *appv1alpha1.Subscription) error {
	substr := fmt.Sprintf("%v/%v", sub.GetNamespace(), sub.GetName())
	klog.V(2).Infof("entry doMCMHubReconcile %v", substr)

	defer klog.V(2).Infof("exix doMCMHubReconcile %v", substr)

	targetSub, updateSub, err := r.updateSubscriptionToTarget(sub)
	if err != nil {
		return err
	}

	primaryChannel, secondaryChannel, err := r.getChannel(sub)

	if err != nil {
		klog.Errorf("Failed to find a channel for subscription: %s", sub.GetName())
		return err
	}

	if (primaryChannel != nil && secondaryChannel != nil) &&
		(primaryChannel.Spec.Type != secondaryChannel.Spec.Type) {
		klog.Errorf("he type of primary and secondary channels is different. primary channel type: %s, secondary channel type: %s",
			primaryChannel.Spec.Type, secondaryChannel.Spec.Type)

		newError := fmt.Errorf("he type of primary and secondary channels is different. primary channel type: %s, secondary channel type: %s",
			primaryChannel.Spec.Type, secondaryChannel.Spec.Type)

		return newError
	}

	chnAnnotations := primaryChannel.GetAnnotations()

	if chnAnnotations[appv1.AnnotationResourceReconcileLevel] != "" {
		// When channel reconcile rate is changed, this label is used to trigger
		// managed cluster to pick up the channel change and adjust reconcile rate.
		sublabels := sub.GetLabels()

		if sublabels == nil {
			sublabels = make(map[string]string)
		}

		sublabels[appv1.AnnotationResourceReconcileLevel] = chnAnnotations[appv1.AnnotationResourceReconcileLevel]
		klog.Info("Adding subscription label ", appv1.AnnotationResourceReconcileLevel, ": ", chnAnnotations[appv1.AnnotationResourceReconcileLevel])
		sub.SetLabels(sublabels)
	}

	updateSubDplAnno := false

	switch tp := strings.ToLower(string(primaryChannel.Spec.Type)); tp {
	case chnv1alpha1.ChannelTypeGit, chnv1alpha1.ChannelTypeGitHub:
		updateSubDplAnno, err = r.UpdateGitDeployablesAnnotation(sub)
	case chnv1alpha1.ChannelTypeHelmRepo:
		updateSubDplAnno, err = UpdateHelmTopoAnnotation(r.Client, r.cfg, primaryChannel, secondaryChannel, sub)
	case chnv1alpha1.ChannelTypeObjectBucket:
		updateSubDplAnno, err = r.updateObjectBucketAnnotation(sub, primaryChannel, secondaryChannel, objectBucketParent)
	default:
		updateSubDplAnno = r.UpdateDeployablesAnnotation(sub, deployableParent)
	}

	if err != nil {
		return err
	}

	klog.Infof("subscription: %v/%v, update Subscription: %v, update Subscription Deployable Annotation: %v",
		sub.GetNamespace(), sub.GetName(), updateSub, updateSubDplAnno)

	dpl, err := r.prepareDeployableForSubscription(sub, nil)
	if err != nil {
		return err
	}

	// if the subscription has the rollingupdate-target annotation, create a new deploayble as the target deployable of the subscription deployable
	targetDpl, err := r.createTargetDplForRollingUpdate(sub, targetSub)

	if err != nil {
		return err
	}

	if targetDpl != nil {
		dplAnno := setTargetDplAnnotation(sub, dpl, targetDpl)
		dpl.SetAnnotations(dplAnno)
	}

	found := &dplv1alpha1.Deployable{}
	dplkey := types.NamespacedName{Name: dpl.Name, Namespace: dpl.Namespace}
	err = r.Get(context.TODO(), dplkey, found)

	if err != nil && apierrors.IsNotFound(err) {
		klog.V(1).Info("Creating Deployable - ", "namespace: ", dpl.Namespace, ", name: ", dpl.Name)
		err = r.Create(context.TODO(), dpl)

		//record events
		addtionalMsg := "Depolyable " + dplkey.String() + " created in the subscription namespace for deploying the subscription to managed clusters"
		r.eventRecorder.RecordEvent(sub, "Deploy", addtionalMsg, err)

		return err
	} else if err != nil {
		return err
	}

	updateTargetAnno := checkRollingUpdateAnno(found, targetDpl, dpl)

	updateSubDpl := checkSubDeployables(found, dpl)

	if updateSub || updateSubDpl || updateTargetAnno {
		klog.V(1).Infof("updateSub: %v, updateSubDpl: %v, updateTargetAnno: %v", updateSub, updateSubDpl, updateTargetAnno)

		if targetDpl != nil {
			klog.V(5).Infof("targetDpl: %#v", targetDpl)
		}

		dpl.Spec.DeepCopyInto(&found.Spec)
		// may need to check owner ID and backoff it if is not owned by this subscription

		foundanno := setFoundDplAnnotation(found, dpl, targetDpl, updateTargetAnno)
		found.SetAnnotations(foundanno)

		setFoundDplLabel(found, sub)

		klog.V(5).Infof("Updating Deployable: %#v, ref dpl: %#v", found, dpl)

		err = r.Update(context.TODO(), found)

		//record events
		addtionalMsg := "Depolyable " + dplkey.String() + " updated in the subscription namespace for deploying the subscription to managed clusters"
		r.eventRecorder.RecordEvent(sub, "Deploy", addtionalMsg, err)

		if err != nil {
			return err
		}
	} else {
		klog.V(1).Infof("update Subscription status, sub:%v/%v", sub.GetNamespace(), sub.GetName())
		err = r.updateSubscriptionStatus(sub, found, primaryChannel)
	}

	return err
}

//checkSubDeployables check differences between existing subscription dpl (found) and new subscription dpl (dpl)
// This is caused by end-user updates on the orignal subscription.
func checkSubDeployables(found, dpl *dplv1alpha1.Deployable) bool {
	if !reflect.DeepEqual(found.Spec.Overrides, dpl.Spec.Overrides) {
		klog.V(1).Infof("different override, found: %#v, dpl: %#v", found.Spec.Overrides, dpl.Spec.Overrides)
		return true
	}

	if !reflect.DeepEqual(found.Spec.Placement, dpl.Spec.Placement) {
		klog.V(1).Infof("different placement: found: %v, dpl: %v", found.Spec.Placement, dpl.Spec.Placement)
		return true
	}

	if !reflect.DeepEqual(found.GetLabels(), dpl.GetLabels()) {
		klog.V(1).Infof("different label: found: %v, dpl: %v", found.GetLabels(), dpl.GetLabels())
		return true
	}

	//compare template difference
	org := &appv1.Subscription{}
	err := json.Unmarshal(dpl.Spec.Template.Raw, org)

	if err != nil {
		klog.V(5).Info("Error in unmarshall, err:", err, " |template: ", string(dpl.Spec.Template.Raw))
		return false
	}

	fnd := &appv1.Subscription{}
	err = json.Unmarshal(found.Spec.Template.Raw, fnd)

	if err != nil {
		klog.V(5).Info("Error in unmarshall, err:", err, " |template: ", string(found.Spec.Template.Raw))
		return false
	}

	fOrg := utils.FilterOutTimeRelatedFields(org)
	fFnd := utils.FilterOutTimeRelatedFields(fnd)

	if !reflect.DeepEqual(fOrg, fFnd) {
		klog.V(5).Infof("different template: found:\n %v\n, dpl:\n %v\n", org, fnd)
		return true
	}

	return false
}

// if there exists target subscription, update the source subscription based on its target subscription.
// return the target subscription and updated flag
func (r *ReconcileSubscription) updateSubscriptionToTarget(sub *appv1alpha1.Subscription) (*appv1alpha1.Subscription, bool, error) {
	//Fetch target subcription if exists
	annotations := sub.GetAnnotations()

	if annotations == nil || annotations[appv1alpha1.AnnotationRollingUpdateTarget] == "" {
		klog.V(5).Info("Empty annotation or No rolling update target in annotations", annotations)

		err := r.clearSubscriptionTargetDpl(sub)

		return nil, false, err
	}

	targetSub := &appv1alpha1.Subscription{}
	targetSubKey := types.NamespacedName{
		Namespace: sub.Namespace,
		Name:      annotations[appv1alpha1.AnnotationRollingUpdateTarget],
	}
	err := r.Get(context.TODO(), targetSubKey, targetSub)

	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.Infof("target Subscription is gone: %#v.", targetSubKey)

			return nil, false, nil
		}
		// Error reading the object - requeue the request.
		klog.Infof("fetching target Subscription failed: %#v.", err)

		return nil, false, err
	}

	//compare the source subscription with the target subscription. The source subscription placemen is alwayas applied.
	updated := false

	if !reflect.DeepEqual(sub.GetLabels(), targetSub.GetLabels()) {
		klog.V(1).Infof("old label: %#v, new label: %#v", sub.GetLabels(), targetSub.GetLabels())
		sub.SetLabels(targetSub.GetLabels())

		updated = true
	}

	if !reflect.DeepEqual(sub.Spec.Overrides, targetSub.Spec.Overrides) {
		klog.V(1).Infof("old override: %#v, new override: %#v", sub.Spec.Overrides, targetSub.Spec.Overrides)
		sub.Spec.Overrides = targetSub.Spec.Overrides

		updated = true
	}

	if !reflect.DeepEqual(sub.Spec.Channel, targetSub.Spec.Channel) {
		klog.V(1).Infof("old channel: %#v, new channel: %#v", sub.Spec.Channel, targetSub.Spec.Channel)
		sub.Spec.Channel = targetSub.Spec.Channel

		updated = true
	}

	if !reflect.DeepEqual(sub.Spec.Package, targetSub.Spec.Package) {
		klog.V(1).Infof("old Package: %#v, new Package: %#v", sub.Spec.Package, targetSub.Spec.Package)
		sub.Spec.Package = targetSub.Spec.Package

		updated = true
	}

	if !reflect.DeepEqual(sub.Spec.PackageFilter, targetSub.Spec.PackageFilter) {
		klog.V(1).Infof("old PackageFilter: %#v, new PackageFilter: %#v", sub.Spec.PackageFilter, targetSub.Spec.PackageFilter)
		sub.Spec.PackageFilter = targetSub.Spec.PackageFilter

		updated = true
	}

	if !reflect.DeepEqual(sub.Spec.PackageOverrides, targetSub.Spec.PackageOverrides) {
		klog.V(1).Infof("old PackageOverrides: %#v, new PackageOverrides: %#v", sub.Spec.PackageOverrides, targetSub.Spec.PackageOverrides)
		sub.Spec.PackageOverrides = targetSub.Spec.PackageOverrides

		updated = true
	}

	if !reflect.DeepEqual(sub.Spec.TimeWindow, targetSub.Spec.TimeWindow) {
		klog.V(1).Infof("old TimeWindow: %#v, new TimeWindow: %#v", sub.Spec.TimeWindow, targetSub.Spec.TimeWindow)
		sub.Spec.TimeWindow = targetSub.Spec.TimeWindow

		updated = true
	}

	return targetSub, updated, nil
}

//setFoundDplAnnotation set target dpl annotation to the source dpl annoation
func setFoundDplAnnotation(found, dpl, targetDpl *dplv1alpha1.Deployable, updateTargetAnno bool) map[string]string {
	foundanno := found.GetAnnotations()
	if foundanno == nil {
		foundanno = make(map[string]string)
	}

	foundanno[dplv1alpha1.AnnotationIsGenerated] = "true"
	foundanno[dplv1alpha1.AnnotationLocal] = "false"

	if updateTargetAnno {
		if targetDpl != nil {
			foundanno[appv1alpha1.AnnotationRollingUpdateTarget] = targetDpl.GetName()
		} else {
			delete(foundanno, appv1alpha1.AnnotationRollingUpdateTarget)
		}

		subDplAnno := dpl.GetAnnotations()
		if subDplAnno == nil {
			subDplAnno = make(map[string]string)
		}

		if subDplAnno[appv1alpha1.AnnotationRollingUpdateMaxUnavailable] > "" {
			foundanno[appv1alpha1.AnnotationRollingUpdateMaxUnavailable] = subDplAnno[appv1alpha1.AnnotationRollingUpdateMaxUnavailable]
		} else {
			delete(foundanno, appv1alpha1.AnnotationRollingUpdateMaxUnavailable)
		}
	}

	return foundanno
}

//updateDplLabel update found dpl label subacription-pause
func setFoundDplLabel(found *dplv1alpha1.Deployable, sub *appv1alpha1.Subscription) {
	label := found.GetLabels()
	if label == nil {
		label = make(map[string]string)
	}

	labelPause := "false"
	if subutil.GetPauseLabel(sub) {
		labelPause = "true"
	}

	label[dplv1alpha1.LabelSubscriptionPause] = labelPause

	found.SetLabels(label)
}

//setSuscriptionLabel update subscription labels. for now only set subacription-pause label
func setSuscriptionLabel(sub *appv1alpha1.Subscription) {
	label := sub.GetLabels()
	if label == nil {
		label = make(map[string]string)
	}

	labelPause := "false"
	if subutil.GetPauseLabel(sub) {
		labelPause = "true"
	}

	label[dplv1alpha1.LabelSubscriptionPause] = labelPause

	sub.SetLabels(label)
}

//SetTargetDplAnnotation set target dpl annotation to the source dpl annoation
func setTargetDplAnnotation(sub *appv1alpha1.Subscription, dpl, targetDpl *dplv1alpha1.Deployable) map[string]string {
	dplAnno := dpl.GetAnnotations()

	if dplAnno == nil {
		dplAnno = make(map[string]string)
	}

	dplAnno[appv1alpha1.AnnotationRollingUpdateTarget] = targetDpl.GetName()

	subAnno := sub.GetAnnotations()
	if subAnno[appv1alpha1.AnnotationRollingUpdateMaxUnavailable] > "" {
		dplAnno[appv1alpha1.AnnotationRollingUpdateMaxUnavailable] = subAnno[appv1alpha1.AnnotationRollingUpdateMaxUnavailable]
	}

	return dplAnno
}

// checkRollingUpdateAnno check if there needs to update rolling update target annotation to the subscription deployable
func checkRollingUpdateAnno(found, targetDpl, subDpl *dplv1alpha1.Deployable) bool {
	foundanno := found.GetAnnotations()

	if foundanno == nil {
		foundanno = make(map[string]string)
	}

	subDplAnno := subDpl.GetAnnotations()
	if subDplAnno == nil {
		subDplAnno = make(map[string]string)
	}

	if foundanno[appv1alpha1.AnnotationRollingUpdateMaxUnavailable] != subDplAnno[appv1alpha1.AnnotationRollingUpdateMaxUnavailable] {
		return true
	}

	updateTargetAnno := false

	if targetDpl != nil {
		if foundanno[appv1alpha1.AnnotationRollingUpdateTarget] != targetDpl.GetName() {
			updateTargetAnno = true
		}
	} else {
		if foundanno[appv1alpha1.AnnotationRollingUpdateTarget] > "" {
			updateTargetAnno = true
		}
	}

	return updateTargetAnno
}

//GetChannelNamespaceType get the channel namespace and channel type by the given subscription
func (r *ReconcileSubscription) GetChannelNamespaceType(s *appv1alpha1.Subscription) (string, string, string) {
	chNameSpace := ""
	chName := ""
	chType := ""

	if s.Spec.Channel != "" {
		strs := strings.Split(s.Spec.Channel, "/")
		if len(strs) == 2 {
			chNameSpace = strs[0]
			chName = strs[1]
		} else {
			chNameSpace = s.Namespace
		}
	}

	chkey := types.NamespacedName{Name: chName, Namespace: chNameSpace}
	chobj := &chnv1alpha1.Channel{}
	err := r.Get(context.TODO(), chkey, chobj)

	if err == nil {
		chType = string(chobj.Spec.Type)
	}

	return chNameSpace, chName, chType
}

func GetSubscriptionRefChannel(clt client.Client, s *appv1.Subscription) (*chnv1.Channel, *chnv1.Channel, error) {
	primaryChannel, err := parseGetChannel(clt, s.Spec.Channel)

	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.Errorf("primary channel %s not found for subscription %s/%s", s.Spec.Channel, s.GetNamespace(), s.GetName())

			return nil, nil, err
		}
	}

	secondaryChannel, err := parseGetChannel(clt, s.Spec.SecondaryChannel)

	if err != nil {
		klog.Errorf("secondary channel %s not found for subscription %s/%s", s.Spec.SecondaryChannel, s.GetNamespace(), s.GetName())

		return nil, nil, err
	}

	return primaryChannel, secondaryChannel, err
}

func parseGetChannel(clt client.Client, channelName string) (*chnv1.Channel, error) {
	if channelName == "" {
		return nil, nil
	}

	chNameSpace := ""
	chName := ""
	strs := strings.Split(channelName, "/")

	if len(strs) == 2 {
		chNameSpace = strs[0]
		chName = strs[1]
	}

	chkey := types.NamespacedName{Name: chName, Namespace: chNameSpace}
	channel := &chnv1.Channel{}
	err := clt.Get(context.TODO(), chkey, channel)

	if err != nil {
		return nil, err
	}

	return channel, nil
}

func (r *ReconcileSubscription) getChannel(s *appv1alpha1.Subscription) (*chnv1alpha1.Channel, *chnv1alpha1.Channel, error) {
	return GetSubscriptionRefChannel(r.Client, s)
}

// GetChannelGeneration get the channel generation
func (r *ReconcileSubscription) GetChannelGeneration(s *appv1alpha1.Subscription) (string, error) {
	chNameSpace := ""
	chName := ""

	if s.Spec.Channel != "" {
		strs := strings.Split(s.Spec.Channel, "/")
		if len(strs) == 2 {
			chNameSpace = strs[0]
			chName = strs[1]
		} else {
			chNameSpace = s.Namespace
		}
	}

	chkey := types.NamespacedName{Name: chName, Namespace: chNameSpace}
	chobj := &chnv1alpha1.Channel{}
	err := r.Get(context.TODO(), chkey, chobj)

	if err != nil {
		return "", err
	}

	return strconv.FormatInt(chobj.Generation, 10), nil
}

// UpdateDeployablesAnnotation set all deployables subscribed by the subscription to the apps.open-cluster-management.io/deployables annotation
func (r *ReconcileSubscription) UpdateDeployablesAnnotation(sub *appv1alpha1.Subscription, parentType string) bool {
	orgdplmap := make(map[string]bool)
	organno := sub.GetAnnotations()

	if organno != nil {
		dpls := organno[appv1alpha1.AnnotationDeployables]
		if dpls != "" {
			dplkeys := strings.Split(dpls, ",")
			for _, dplkey := range dplkeys {
				orgdplmap[dplkey] = true
			}
		}
	}

	// map[string]*deployable
	allDpls := r.getSubscriptionDeployables(sub)

	// changes in order of deployables does not mean changes in deployables
	updated := false

	for k := range allDpls {
		if _, ok := orgdplmap[k]; !ok {
			updated = true
			break
		}

		delete(orgdplmap, k)
	}

	if !updated && len(orgdplmap) > 0 {
		updated = true
	}

	if updated {
		dplstr := ""
		for dplkey := range allDpls {
			if dplstr != "" {
				dplstr += ","
			}

			dplstr += dplkey
		}

		klog.Info("subscription updated for ", sub.Namespace, "/", sub.Name, " new deployables:", dplstr)

		subanno := sub.GetAnnotations()
		if subanno == nil {
			subanno = make(map[string]string)
		}

		subanno[appv1alpha1.AnnotationDeployables] = dplstr

		sub.SetAnnotations(subanno)
	}

	// Check and add cluster-admin annotation for multi-namepsace application
	updated = r.AddClusterAdminAnnotation(sub)

	topoFlag := extracResourceListFromDeployables(sub, allDpls, parentType)

	return updated || topoFlag
}

// clearSubscriptionDpls clear the subscription deployable and its rolling update target deployable if exists.
func (r *ReconcileSubscription) clearSubscriptionDpls(sub *appv1alpha1.Subscription) error {
	klog.V(5).Info("No longer hub, deleting sbscription deploayble")

	hubdpl := &dplv1alpha1.Deployable{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: sub.Name + "-deployable", Namespace: sub.Namespace}, hubdpl)

	if err == nil {
		// no longer hub, check owner-reference and delete if it is generated.
		owners := hubdpl.GetOwnerReferences()
		for _, owner := range owners {
			if owner.UID == sub.UID {
				err = r.Delete(context.TODO(), hubdpl)
				if err != nil {
					klog.V(5).Infof("Error in deleting sbuscription target deploayble: %#v, err: %#v ", hubdpl, err)
					return err
				}
			}
		}
	}

	err = r.clearSubscriptionTargetDpl(sub)

	return err
}

// clearSubscriptionTargetDpls clear the subscription target deployable if exists.
func (r *ReconcileSubscription) clearSubscriptionTargetDpl(sub *appv1alpha1.Subscription) error {
	klog.V(5).Info("deleting sbscription target deploayble")

	// delete target deployable if exists.
	hubTargetDpl := &dplv1alpha1.Deployable{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: sub.Name + "-target-deployable", Namespace: sub.Namespace}, hubTargetDpl)

	if err == nil {
		// check owner-reference and delete if it is generated.
		owners := hubTargetDpl.GetOwnerReferences()
		for _, owner := range owners {
			if owner.UID == sub.UID {
				err = r.Delete(context.TODO(), hubTargetDpl)
				if err != nil {
					klog.Infof("Error in deleting sbuscription target deploayble: %#v, err: %v", hubTargetDpl, err)
					return err
				}
			}
		}
	}

	return nil
}

func (r *ReconcileSubscription) prepareDeployableForSubscription(sub, rootSub *appv1alpha1.Subscription) (*dplv1alpha1.Deployable, error) {
	// Fetch the Subscription instance
	subep := sub.DeepCopy()
	b := true
	subep.Spec.Placement = &plrv1alpha1.Placement{Local: &b}
	subep.Spec.Overrides = nil
	subep.ResourceVersion = ""
	subep.UID = ""

	subep.CreationTimestamp = metav1.Time{}
	subep.Generation = 1
	subep.SelfLink = ""

	subepanno := r.updateSubAnnotations(sub)

	if rootSub == nil {
		subep.Name = sub.GetName() + appv1alpha1.SubscriptionNameSuffix
		subepanno[dplv1alpha1.AnnotationSubscription] = subep.Namespace + "/" + sub.GetName()
	} else {
		subep.Name = rootSub.GetName() + appv1alpha1.SubscriptionNameSuffix
		subepanno[dplv1alpha1.AnnotationSubscription] = rootSub.Namespace + "/" + rootSub.GetName()
	}
	// set channel generation as annotation
	if subep.Spec.Channel != "" {
		chng, err := r.GetChannelGeneration(subep)
		if err == nil {
			subepanno[appv1alpha1.AnnotationChannelGeneration] = chng
		}
	}

	subep.SetAnnotations(subepanno)
	subep.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   appv1alpha1.SchemeGroupVersion.Group,
		Version: appv1alpha1.SchemeGroupVersion.Version,
		Kind:    "Subscription",
	})
	(&appv1alpha1.SubscriptionStatus{}).DeepCopyInto(&subep.Status)

	setSuscriptionLabel(subep)

	rawep, err := json.Marshal(subep)
	if err != nil {
		klog.Info("Error in mashalling subscription ", subep, err)
		return nil, err
	}

	// propagate subscription-pause label to the subscription deployable
	labelPause := "false"
	if subutil.GetPauseLabel(sub) {
		labelPause = "true"
	}

	dpl := &dplv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployable",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      sub.Name + "-deployable",
			Namespace: sub.Namespace,
			Labels: map[string]string{
				dplv1alpha1.LabelSubscriptionPause: labelPause,
			},
			Annotations: map[string]string{
				dplv1alpha1.AnnotationLocal:       "false",
				dplv1alpha1.AnnotationIsGenerated: "true",
				appv1.AnnotationHosting:           fmt.Sprintf("%s/%s", sub.GetNamespace(), sub.GetName()),
			},
		},
		Spec: dplv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Raw: rawep,
			},
			Placement: sub.Spec.Placement,
		},
	}

	ownerSub := sub
	if rootSub != nil {
		ownerSub = rootSub
	}

	if err = controllerutil.SetControllerReference(ownerSub, dpl, r.scheme); err != nil {
		return nil, err
	}

	// apply "/" override to template, and carry other overrides to deployable.
	for _, ov := range sub.Spec.Overrides {
		if ov.ClusterName == "/" {
			tplobj := &unstructured.Unstructured{}
			err = json.Unmarshal(dpl.Spec.Template.Raw, tplobj)

			if err != nil {
				klog.Info("Error in unmarshall, err:", err, " |template: ", string(dpl.Spec.Template.Raw))
				return nil, err
			}

			tplobj, err = dplutils.OverrideTemplate(tplobj, ov.ClusterOverrides)
			if err != nil {
				klog.Info("Error in overriding obj ", tplobj, err)
				return nil, err
			}

			dpl.Spec.Template.Raw, err = json.Marshal(tplobj)
			if err != nil {
				klog.Info("Error in mashalling obj ", tplobj, err)
				return nil, err
			}
		} else {
			dplov := dplv1alpha1.Overrides{}
			ov.DeepCopyInto(&dplov)
			dpl.Spec.Overrides = append(dpl.Spec.Overrides, dplov)
		}
	}

	return dpl, nil
}

func (r *ReconcileSubscription) updateSubAnnotations(sub *appv1alpha1.Subscription) map[string]string {
	subepanno := make(map[string]string)

	origsubanno := sub.GetAnnotations()

	// User and Group annotations
	subepanno[appv1alpha1.AnnotationUserIdentity] = strings.Trim(origsubanno[appv1alpha1.AnnotationUserIdentity], "")
	subepanno[appv1alpha1.AnnotationUserGroup] = strings.Trim(origsubanno[appv1alpha1.AnnotationUserGroup], "")

	// Keep Git related annotations from the source subscription.
	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationWebhookEventCount], "") {
		subepanno[appv1alpha1.AnnotationWebhookEventCount] = origsubanno[appv1alpha1.AnnotationWebhookEventCount]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationGitBranch], "") {
		subepanno[appv1alpha1.AnnotationGitBranch] = origsubanno[appv1alpha1.AnnotationGitBranch]
	} else if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationGithubBranch], "") {
		subepanno[appv1alpha1.AnnotationGitBranch] = origsubanno[appv1alpha1.AnnotationGithubBranch]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationGitPath], "") {
		subepanno[appv1alpha1.AnnotationGitPath] = origsubanno[appv1alpha1.AnnotationGitPath]
	} else if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationGithubPath], "") {
		subepanno[appv1alpha1.AnnotationGitPath] = origsubanno[appv1alpha1.AnnotationGithubPath]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationBucketPath], "") {
		subepanno[appv1alpha1.AnnotationBucketPath] = origsubanno[appv1alpha1.AnnotationBucketPath]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationClusterAdmin], "") && r.AddClusterAdminAnnotation(sub) {
		subepanno[appv1alpha1.AnnotationClusterAdmin] = origsubanno[appv1alpha1.AnnotationClusterAdmin]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationCurrentNamespaceScoped], "") {
		subepanno[appv1alpha1.AnnotationCurrentNamespaceScoped] = origsubanno[appv1alpha1.AnnotationCurrentNamespaceScoped]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationResourceReconcileOption], "") {
		subepanno[appv1alpha1.AnnotationResourceReconcileOption] = origsubanno[appv1alpha1.AnnotationResourceReconcileOption]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationGitTargetCommit], "") {
		subepanno[appv1alpha1.AnnotationGitTargetCommit] = origsubanno[appv1alpha1.AnnotationGitTargetCommit]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationGitTag], "") {
		subepanno[appv1alpha1.AnnotationGitTag] = origsubanno[appv1alpha1.AnnotationGitTag]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationGitCloneDepth], "") {
		subepanno[appv1alpha1.AnnotationGitCloneDepth] = origsubanno[appv1alpha1.AnnotationGitCloneDepth]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationResourceReconcileLevel], "") {
		subepanno[appv1alpha1.AnnotationResourceReconcileLevel] = origsubanno[appv1alpha1.AnnotationResourceReconcileLevel]
	}

	if !strings.EqualFold(origsubanno[appv1alpha1.AnnotationManualReconcileTime], "") {
		subepanno[appv1alpha1.AnnotationManualReconcileTime] = origsubanno[appv1alpha1.AnnotationManualReconcileTime]
	}

	// Add annotation for git path and branch
	// It is recommended to define Git path and branch in subscription annotations but
	// this code is to support those that already use ConfigMap.
	if sub.Spec.PackageFilter != nil && sub.Spec.PackageFilter.FilterRef != nil {
		subscriptionConfigMap := &corev1.ConfigMap{}
		subcfgkey := types.NamespacedName{
			Name:      sub.Spec.PackageFilter.FilterRef.Name,
			Namespace: sub.Namespace,
		}

		err := r.Get(context.TODO(), subcfgkey, subscriptionConfigMap)
		if err != nil {
			klog.Error("Failed to get PackageFilter.FilterRef of subsciption, error: ", err)
		} else {
			gitPath := subscriptionConfigMap.Data["path"]
			if gitPath != "" {
				subepanno[appv1alpha1.AnnotationGitPath] = gitPath
			}
			gitBranch := subscriptionConfigMap.Data["branch"]
			if gitBranch != "" {
				subepanno[appv1alpha1.AnnotationGitBranch] = gitBranch
			}
		}
	}

	return subepanno
}

func (r *ReconcileSubscription) updateSubscriptionStatus(sub *appv1alpha1.Subscription, found *dplv1alpha1.Deployable, chn *chnv1alpha1.Channel) error {
	r.logger.Info(fmt.Sprintf("entry doMCMHubReconcile:updateSubscriptionStatus %s", PrintHelper(sub)))
	defer r.logger.Info(fmt.Sprintf("exit doMCMHubReconcile:updateSubscriptionStatus %s", PrintHelper(sub)))

	newsubstatus := appv1alpha1.SubscriptionStatus{}

	newsubstatus.AnsibleJobsStatus = *sub.Status.AnsibleJobsStatus.DeepCopy()

	newsubstatus.Phase = appv1alpha1.SubscriptionPropagated
	newsubstatus.Message = ""
	newsubstatus.Reason = ""

	msg := ""

	if found.Status.Phase == dplv1alpha1.DeployableFailed {
		newsubstatus.Statuses = nil
	} else {
		newsubstatus.Statuses = make(map[string]*appv1alpha1.SubscriptionPerClusterStatus)

		for cluster, cstatus := range found.Status.PropagatedStatus {
			clusterSubStatus := &appv1alpha1.SubscriptionPerClusterStatus{}
			subPkgStatus := make(map[string]*appv1alpha1.SubscriptionUnitStatus)

			if cstatus.ResourceStatus != nil {
				mcsubstatus := &appv1alpha1.SubscriptionStatus{}
				err := json.Unmarshal(cstatus.ResourceStatus.Raw, mcsubstatus)
				if err != nil {
					klog.Infof("Failed to unmashall ResourceStatus from target cluster: %v, in deployable: %v/%v", cluster, found.GetNamespace(), found.GetName())
					return err
				}

				if msg == "" {
					msg = fmt.Sprintf("%s:%s", cluster, mcsubstatus.Message)
				} else {
					msg += fmt.Sprintf(",%s:%s", cluster, mcsubstatus.Message)
				}

				//get status per package if exist, for namespace/objectStore/helmRepo channel subscription status
				for _, lcStatus := range mcsubstatus.Statuses {
					for pkg, pkgStatus := range lcStatus.SubscriptionPackageStatus {
						subPkgStatus[pkg] = getStatusPerPackage(pkgStatus, chn)
					}
				}

				//if no status per package, apply status.<per cluster>.resourceStatus, for github channel subscription status
				if len(subPkgStatus) == 0 {
					subUnitStatus := &appv1alpha1.SubscriptionUnitStatus{}
					subUnitStatus.LastUpdateTime = mcsubstatus.LastUpdateTime
					subUnitStatus.Phase = mcsubstatus.Phase
					subUnitStatus.Message = mcsubstatus.Message
					subUnitStatus.Reason = mcsubstatus.Reason

					subPkgStatus["/"] = subUnitStatus
				}
			}

			clusterSubStatus.SubscriptionPackageStatus = subPkgStatus

			newsubstatus.Statuses[cluster] = clusterSubStatus
		}
	}

	newsubstatus.LastUpdateTime = sub.Status.LastUpdateTime
	klog.V(5).Info("Check status for ", sub.Namespace, "/", sub.Name, " with ", newsubstatus)
	newsubstatus.Message = msg

	if !utils.IsEqualSubScriptionStatus(&sub.Status, &newsubstatus) {
		klog.V(1).Infof("check subscription status sub: %v/%v, substatus: %#v, newsubstatus: %#v",
			sub.Namespace, sub.Name, sub.Status, newsubstatus)

		//perserve the Ansiblejob status
		newsubstatus.DeepCopyInto(&sub.Status)
	}

	return nil
}

func getStatusPerPackage(pkgStatus *appv1alpha1.SubscriptionUnitStatus, chn *chnv1alpha1.Channel) *appv1alpha1.SubscriptionUnitStatus {
	subUnitStatus := &appv1alpha1.SubscriptionUnitStatus{}

	switch chn.Spec.Type {
	case "HelmRepo":
		subUnitStatus.LastUpdateTime = pkgStatus.LastUpdateTime

		if pkgStatus.ResourceStatus != nil {
			setHelmSubUnitStatus(pkgStatus.ResourceStatus, subUnitStatus)
		} else {
			subUnitStatus.Phase = pkgStatus.Phase
			subUnitStatus.Message = pkgStatus.Message
			subUnitStatus.Reason = pkgStatus.Reason
		}
	default:
		subUnitStatus = pkgStatus
	}

	return subUnitStatus
}

func setHelmSubUnitStatus(pkgResourceStatus *runtime.RawExtension, subUnitStatus *appv1alpha1.SubscriptionUnitStatus) {
	if pkgResourceStatus == nil || subUnitStatus == nil {
		klog.Errorf("failed to setHelmSubUnitStatus due to pkgResourceStatus %v or subUnitStatus %v is nil", pkgResourceStatus, subUnitStatus)
		return
	}

	helmAppStatus := &releasev1.HelmAppStatus{}
	err := json.Unmarshal(pkgResourceStatus.Raw, helmAppStatus)

	if err != nil {
		klog.Error("Failed to unmashall pkgResourceStatus to helm condition. err: ", err)
	}

	subUnitStatus.Phase = "Subscribed"
	subUnitStatus.Message = ""
	subUnitStatus.Reason = ""

	messages := []string{}
	reasons := []string{}

	for _, condition := range helmAppStatus.Conditions {
		if strings.Contains(string(condition.Reason), "Error") {
			subUnitStatus.Phase = "Failed"

			messages = append(messages, condition.Message)

			reasons = append(reasons, string(condition.Reason))
		}
	}

	if len(messages) > 0 {
		subUnitStatus.Message = strings.Join(messages, ", ")
	}

	if len(reasons) > 0 {
		subUnitStatus.Reason = strings.Join(reasons, ", ")
	}
}

func (r *ReconcileSubscription) getSubscriptionDeployables(sub *appv1alpha1.Subscription) map[string]*dplv1alpha1.Deployable {
	allDpls := make(map[string]*dplv1alpha1.Deployable)

	dplList := &dplv1alpha1.DeployableList{}

	chNameSpace, chName, chType := r.GetChannelNamespaceType(sub)

	dplNamespace := chNameSpace

	if utils.IsGitChannel(chType) {
		// If Git channel, deployables are created in the subscription namespace
		dplNamespace = sub.Namespace
	}

	dplListOptions := &client.ListOptions{Namespace: dplNamespace}

	if sub.Spec.PackageFilter != nil && sub.Spec.PackageFilter.LabelSelector != nil {
		matchLbls := sub.Spec.PackageFilter.LabelSelector.MatchLabels
		matchLbls[chnv1alpha1.KeyChannel] = chName
		matchLbls[chnv1alpha1.KeyChannelType] = chType
		clSelector, err := dplutils.ConvertLabels(sub.Spec.PackageFilter.LabelSelector)

		if err != nil {
			klog.Error("Failed to set label selector of subscrption:", sub.Spec.PackageFilter.LabelSelector, " err: ", err)
			return nil
		}

		dplListOptions.LabelSelector = clSelector
	} else {
		// Handle deployables from multiple channels in the same namespace
		subLabel := make(map[string]string)
		subLabel[chnv1alpha1.KeyChannel] = chName
		subLabel[chnv1alpha1.KeyChannelType] = chType

		if utils.IsGitChannel(chType) {
			subscriptionNameLabel := types.NamespacedName{
				Name:      sub.Name,
				Namespace: sub.Namespace,
			}
			subscriptionNameLabelStr := strings.ReplaceAll(subscriptionNameLabel.String(), "/", "-")

			subLabel[appv1alpha1.LabelSubscriptionName] = subscriptionNameLabelStr
		}

		labelSelector := &metav1.LabelSelector{
			MatchLabels: subLabel,
		}
		chSelector, err := dplutils.ConvertLabels(labelSelector)

		if err != nil {
			klog.Error("Failed to set label selector. err: ", chSelector, " err: ", err)
			return nil
		}

		dplListOptions.LabelSelector = chSelector
	}

	// Sleep so that all deployables are fully created
	time.Sleep(3 * time.Second)

	err := r.Client.List(context.TODO(), dplList, dplListOptions)

	if err != nil {
		klog.Error("Failed to list objects from sbuscription namespace ", sub.Namespace, " err: ", err)
		return nil
	}

	klog.V(1).Info("Hub Subscription found Deployables:", len(dplList.Items))

	for _, dpl := range dplList.Items {
		if !r.checkDeployableBySubcriptionPackageFilter(sub, dpl) {
			continue
		}

		dplkey := types.NamespacedName{Name: dpl.Name, Namespace: dpl.Namespace}.String()
		allDpls[dplkey] = dpl.DeepCopy()
	}

	return allDpls
}

func checkDplPackageName(sub *appv1alpha1.Subscription, dpl dplv1alpha1.Deployable) bool {
	dpltemplate := &unstructured.Unstructured{}

	if dpl.Spec.Template != nil {
		err := json.Unmarshal(dpl.Spec.Template.Raw, dpltemplate)
		if err == nil {
			dplPkgName := dpltemplate.GetName()
			if sub.Spec.Package != "" && sub.Spec.Package != dplPkgName {
				klog.Info("Name does not match, skiping:", sub.Spec.Package, "|", dplPkgName)
				return false
			}
		}
	}

	return true
}

func (r *ReconcileSubscription) checkDeployableBySubcriptionPackageFilter(sub *appv1alpha1.Subscription, dpl dplv1alpha1.Deployable) bool {
	dplanno := dpl.GetAnnotations()

	if sub.Spec.PackageFilter != nil {
		if dplanno == nil {
			dplanno = make(map[string]string)
		}

		if !checkDplPackageName(sub, dpl) {
			return false
		}

		annotations := sub.Spec.PackageFilter.Annotations

		//append deployable template annotations to deployable annotations only if they don't exist in the deployable annotations
		dpltemplate := &unstructured.Unstructured{}

		if dpl.Spec.Template != nil {
			err := json.Unmarshal(dpl.Spec.Template.Raw, dpltemplate)
			if err == nil {
				dplTemplateAnno := dpltemplate.GetAnnotations()
				for k, v := range dplTemplateAnno {
					if dplanno[k] == "" {
						dplanno[k] = v
					}
				}
			}
		}

		vdpl := dpl.GetAnnotations()[dplv1alpha1.AnnotationDeployableVersion]

		klog.V(5).Info("checking annotations package filter: ", annotations)

		if annotations != nil {
			matched := true

			for k, v := range annotations {
				if dplanno[k] != v {
					matched = false
					break
				}
			}

			if !matched {
				return false
			}
		}

		vsub := sub.Spec.PackageFilter.Version
		if vsub != "" {
			vmatch := subutil.SemverCheck(vsub, vdpl)
			klog.V(5).Infof("version check is %v; subscription version filter condition is %v, deployable version is: %v", vmatch, vsub, vdpl)

			if !vmatch {
				return false
			}
		}
	}

	return true
}

// createTargetDplForRollingUpdate create a new deployable to contain the target subscription
func (r *ReconcileSubscription) createTargetDplForRollingUpdate(sub, targetSub *appv1alpha1.Subscription) (*dplv1alpha1.Deployable, error) {
	if targetSub == nil {
		return nil, nil
	}

	targetSubDpl, err := r.prepareDeployableForSubscription(targetSub, sub)

	if err != nil {
		klog.V(3).Infof("Prepare target Subscription deployable failed: %#v.", err)
		return nil, err
	}

	targetSubDpl.Name = sub.Name + "-target-deployable"
	targetSubDpl.Namespace = sub.Namespace

	err = r.updateTargetSubscriptionDeployable(sub, targetSubDpl)

	return targetSubDpl, err
}

func (r *ReconcileSubscription) updateTargetSubscriptionDeployable(sub *appv1alpha1.Subscription, targetSubDpl *dplv1alpha1.Deployable) error {
	targetKey := types.NamespacedName{
		Namespace: targetSubDpl.Namespace,
		Name:      targetSubDpl.Name,
	}

	found := &dplv1alpha1.Deployable{}
	err := r.Get(context.TODO(), targetKey, found)

	if err != nil && apierrors.IsNotFound(err) {
		klog.Info("Creating target Deployable - ", "namespace: ", targetSubDpl.Namespace, ", name: ", targetSubDpl.Name)
		err = r.Create(context.TODO(), targetSubDpl)

		//record events
		addtionalMsg := "target Depolyable " + targetKey.String() + " created in the subscription namespace"
		r.eventRecorder.RecordEvent(sub, "Deploy", addtionalMsg, err)

		return err
	} else if err != nil {
		return err
	}

	orgTpl := &unstructured.Unstructured{}
	err = json.Unmarshal(targetSubDpl.Spec.Template.Raw, orgTpl)

	if err != nil {
		klog.V(5).Info("Error in unmarshall target subscription deployable template, err:", err, " |template: ", string(targetSubDpl.Spec.Template.Raw))
		return err
	}

	fndTpl := &unstructured.Unstructured{}
	err = json.Unmarshal(found.Spec.Template.Raw, fndTpl)

	if err != nil {
		klog.V(5).Info("Error in unmarshall target found subscription deployable template, err:", err, " |template: ", string(found.Spec.Template.Raw))
		return err
	}

	if !reflect.DeepEqual(orgTpl, fndTpl) || !reflect.DeepEqual(targetSubDpl.Spec.Overrides, found.Spec.Overrides) {
		klog.V(5).Infof("Updating target Deployable. orig: %#v, found: %#v", targetSubDpl, found)

		targetSubDpl.Spec.DeepCopyInto(&found.Spec)

		foundanno := found.GetAnnotations()
		if foundanno == nil {
			foundanno = make(map[string]string)
		}

		foundanno[dplv1alpha1.AnnotationIsGenerated] = "true"
		foundanno[dplv1alpha1.AnnotationLocal] = "false"
		found.SetAnnotations(foundanno)

		klog.V(5).Info("Updating Deployable - ", "namespace: ", targetSubDpl.Namespace, " ,name: ", targetSubDpl.Name)

		err = r.Update(context.TODO(), found)

		//record events
		addtionalMsg := "target Depolyable " + targetKey.String() + " updated in the subscription namespace"
		r.eventRecorder.RecordEvent(sub, "Deploy", addtionalMsg, err)

		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ReconcileSubscription) initObjectStore(channel *chnv1alpha1.Channel) (*awsutils.Handler, string, error) {
	var err error

	awshandler := &awsutils.Handler{}

	pathName := channel.Spec.Pathname

	if pathName == "" {
		errmsg := "Empty Pathname in channel " + channel.Spec.Pathname
		klog.Error(errmsg)

		return nil, "", errors.New(errmsg)
	}

	if strings.HasSuffix(pathName, "/") {
		last := len(pathName) - 1
		pathName = pathName[:last]
	}

	loc := strings.LastIndex(pathName, "/")
	endpoint := pathName[:loc]
	bucket := pathName[loc+1:]

	accessKeyID := ""
	secretAccessKey := ""
	region := ""

	if channel.Spec.SecretRef != nil {
		channelSecret := &corev1.Secret{}
		chnseckey := types.NamespacedName{
			Name:      channel.Spec.SecretRef.Name,
			Namespace: channel.Namespace,
		}

		if err := r.Get(context.TODO(), chnseckey, channelSecret); err != nil {
			return nil, "", gerr.Wrap(err, "failed to get reference secret from channel")
		}

		err = yaml.Unmarshal(channelSecret.Data[awsutils.SecretMapKeyAccessKeyID], &accessKeyID)
		if err != nil {
			klog.Error("Failed to unmashall accessKey from secret with error:", err)

			return nil, "", err
		}

		err = yaml.Unmarshal(channelSecret.Data[awsutils.SecretMapKeySecretAccessKey], &secretAccessKey)
		if err != nil {
			klog.Error("Failed to unmashall secretaccessKey from secret with error:", err)

			return nil, "", err
		}

		regionData := channelSecret.Data[awsutils.SecretMapKeyRegion]

		if len(regionData) > 0 {
			err = yaml.Unmarshal(regionData, &region)
			if err != nil {
				klog.Error("Failed to unmashall region from secret with error:", err)

				return nil, "", err
			}
		}
	}

	klog.V(1).Info("Trying to connect to object bucket ", endpoint, "|", bucket)

	if err := awshandler.InitObjectStoreConnection(endpoint, accessKeyID, secretAccessKey, region); err != nil {
		klog.Error(err, "unable initialize object store settings")

		return nil, "", err
	}
	// Check whether the connection is setup successfully
	if err := awshandler.Exists(bucket); err != nil {
		klog.Error(err, "Unable to access object store bucket ", bucket, " for channel ", channel.Name)

		return nil, "", err
	}

	return awshandler, bucket, nil
}

func (r *ReconcileSubscription) updateObjectBucketAnnotation(
	sub *appv1alpha1.Subscription, channel, secondaryChannel *chnv1alpha1.Channel, parentType string) (bool, error) {
	awsHandler, bucket, err := r.initObjectStore(channel)
	if err != nil {
		klog.Error(err, "Unable to access object store: ")

		if secondaryChannel != nil {
			klog.Infof("trying the secondary channel %s", secondaryChannel.Name)
			// Try with secondary channel
			awsHandler, bucket, err = r.initObjectStore(secondaryChannel)

			if err != nil {
				klog.Error(err, "Unable to access object store with channel ", channel.Name)

				return false, err
			}
		} else {
			klog.Error(err, "Unable to access object store with channel ", channel.Name)

			return false, err
		}
	}

	var folderName *string

	annotations := sub.GetAnnotations()
	bucketPath := annotations[appv1.AnnotationBucketPath]

	if bucketPath != "" {
		folderName = &bucketPath
	}

	keys, err := awsHandler.List(bucket, folderName)
	klog.V(5).Infof("object keys: %v", keys)

	if err != nil {
		klog.Error("Failed to list objects in bucket ", bucket)

		return false, err
	}

	allDpls := make(map[string]*dplv1alpha1.Deployable)

	// converting template from obeject store to DPL
	for _, key := range keys {
		tplb, err := awsHandler.Get(bucket, key)
		if err != nil {
			klog.Error("Failed to get object ", key, " in bucket ", bucket)

			return false, err
		}

		// skip empty body object store
		if len(tplb.Content) == 0 {
			continue
		}

		dpl := &dplv1.Deployable{}
		dpl.Name = generateDplNameFromKey(key)
		dpl.Namespace = bucket
		dpl.Spec.Template = &runtime.RawExtension{}
		dpl.GenerateName = tplb.GenerateName
		verionAnno := map[string]string{dplv1.AnnotationDeployableVersion: tplb.Version}
		dpl.SetAnnotations(verionAnno)
		err = yaml.Unmarshal(tplb.Content, dpl.Spec.Template)

		if err != nil {
			klog.Error("Failed to unmashall ", bucket, "/", key, " err:", err)
			continue
		}

		klog.V(5).Infof("Retived Dpl: %v", dpl)

		dplkey := types.NamespacedName{Name: dpl.Name, Namespace: dpl.Namespace}.String()
		allDpls[dplkey] = dpl
	}

	topoFlag := extracResourceListFromDeployables(sub, allDpls, parentType)

	return topoFlag, nil
}

func generateDplNameFromKey(key string) string {
	return strings.ReplaceAll(key, "/", "-")
}
