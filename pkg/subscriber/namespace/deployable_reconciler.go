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

package namespace

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"

	kubesynchronizer "github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

// DeployableReconciler reconciles a Deployable object of Nmespace channel
type DeployableReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
	subscriber *NsSubscriber
	itemkey    types.NamespacedName
}

func NewNsDeployableReconciler(hub client.Client, nsSubscribe *NsSubscriber, ikey types.NamespacedName) *DeployableReconciler {
	return &DeployableReconciler{
		Client:     hub,
		subscriber: nsSubscribe,
		itemkey:    ikey,
	}
}

// making sure the update subscription deployable from hub is respected even the current time window is blocked
func (r *DeployableReconciler) isUpdateLinkedSubscription(request reconcile.Request) bool {
	dpl := &dplv1alpha1.Deployable{}

	if err := r.Client.Get(context.TODO(), request.NamespacedName, dpl); err != nil {
		klog.Errorf("failed to get deployable from hub, err: %v", err)
		return false
	}

	subkey := r.itemkey

	dplTpl := &unstructured.Unstructured{}

	if dpl.Spec.Template == nil {
		klog.Error(errors.New(fmt.Sprintf("%v deployable without template", dpl.Name)))

		return false
	}

	err := json.Unmarshal(dpl.Spec.Template.Raw, dplTpl)
	if err != nil {
		klog.Error(errors.Wrap(err, "unable to unmarshal deployable  template"))

		return false
	}

	if dplTpl.GetName() == subkey.Name && dplTpl.GetNamespace() == subkey.Namespace {
		return true
	}

	return false
}

// Reconcile finds out all channels related to this deployable, then all subscriptions subscribing that channel and update them
func (r *DeployableReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.V(1).Infof("deployable reconciling: %v deployable for subitem %v", request.NamespacedName, r.itemkey.String())

	tw := r.subscriber.itemmap[r.itemkey].Subscription.Spec.TimeWindow
	if !r.isUpdateLinkedSubscription(request) && tw != nil {
		nextRun := utils.NextStartPoint(tw, time.Now())
		klog.V(5).Infof(time.Now().String())
		klog.V(5).Infof("reconciling deployable %v, for subscription %v, with tw %v having nextRun time %v",
			request.NamespacedName.String(), r.subscriber.itemmap[r.itemkey].Subscription.GetName(), tw, nextRun)

		if nextRun > time.Duration(0) {
			klog.V(1).Infof("subcription %v will run after %v", request.NamespacedName.String(), nextRun)
			return reconcile.Result{RequeueAfter: nextRun}, nil
		}
	}

	// if the subscription pause lable is true, stop subscription here.
	if utils.GetPauseLabel(r.subscriber.itemmap[r.itemkey].Subscription) {
		klog.Infof("Namspace Subscription %v is paused.", request.NamespacedName.String())
		return reconcile.Result{}, nil
	}

	result := reconcile.Result{}
	err := r.doSubscription()

	if err != nil {
		result.RequeueAfter = time.Duration(rand.IntnRange(1, 10)) * time.Second

		klog.Errorf("failed to reconcile deployable %v for namespace subscriber with error: %+v", request.String(), err)
	}

	return result, nil
}

func (r *DeployableReconciler) doSubscription() error {
	subitem, ok := r.subscriber.itemmap[r.itemkey]

	if !ok {
		return errors.Errorf("failed to locate subscription item %v in existing map", r.itemkey.String())
	}

	klog.V(5).Info("Processing subscriptions: ", r.itemkey)

	dpllist := &dplv1alpha1.DeployableList{}

	subNamespace := subitem.Channel.Spec.Pathname
	if subNamespace == "" && r.itemkey.String() != "/" {
		return errors.Errorf("channel pathName should not be empty in channel resource of subitem: %v ", r.itemkey.String())
	}

	listOptions := &client.ListOptions{Namespace: subNamespace}

	if subitem.Subscription.Spec.PackageFilter != nil && subitem.Subscription.Spec.PackageFilter.LabelSelector != nil {
		clSelector, err := utils.ConvertLabels(subitem.Subscription.Spec.PackageFilter.LabelSelector)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed to parse label selector of subscrption %v", subitem.Subscription.Spec.PackageFilter.LabelSelector))
		}

		listOptions.LabelSelector = clSelector
	}

	err := r.List(context.TODO(), dpllist, listOptions)

	klog.V(2).Info("Got ", len(dpllist.Items), " deployable list for process from namespace ", subNamespace, " with list option:", listOptions.LabelSelector)

	if err != nil {
		return errors.Wrapf(err, "failed to list objects from namespace %v ", subNamespace)
	}

	hostkey := types.NamespacedName{Name: subitem.Subscription.Name, Namespace: subitem.Subscription.Namespace}
	syncsource := deployablesyncsource + hostkey.String()

	// subscribed k8s resource
	dplOrder := []kubesynchronizer.DplUnit{}

	vsub := ""

	if subitem.Subscription.Spec.PackageFilter != nil {
		vsub = subitem.Subscription.Spec.PackageFilter.Version
	}

	versionMap := utils.GenerateVersionSet(utils.DplArrayToDplPointers(dpllist.Items), vsub)

	var retryerr error

	for _, dpl := range dpllist.Items {
		klog.V(5).Infof("Updating subscription %v, with Deployable %v  ", syncsource, hostkey)

		dpltosync, validgvk, err := r.doSubscribeDeployable(subitem, dpl.DeepCopy(), versionMap, map[string]bool{})
		if err != nil {
			klog.V(3).Info("Skipping deployable", dpl.Name)

			if dpltosync != nil {
				retryerr = err
			}

			continue
		}

		dplU := kubesynchronizer.DplUnit{
			Dpl: dpltosync,
			Gvk: *validgvk,
		}

		dplOrder = append(dplOrder, dplU)

		klog.V(5).Info("Ready to register template:", hostkey, dpltosync, syncsource)

		if err != nil {
			err = utils.SetInClusterPackageStatus(&(subitem.Subscription.Status), dpltosync.GetName(), err, nil)

			if err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}

			return errors.Wrap(err, "failed to update subscription status")
		}

		dplkey := types.NamespacedName{
			Name:      dpltosync.Name,
			Namespace: dpltosync.Namespace,
		}

		klog.V(5).Info("Finished Register ", *validgvk, hostkey, dplkey, " with err:", err)
	}

	if retryerr != nil {
		return retryerr
	}

	return r.subscriber.synchronizer.AddTemplates(syncsource, hostkey, dplOrder)
}

func (r *DeployableReconciler) doSubscribeDeployable(subitem *NsSubscriberItem, dpl *dplv1alpha1.Deployable,
	versionMap map[string]utils.VersionRep, pkgMap map[string]bool) (*dplv1alpha1.Deployable, *schema.GroupVersionKind, error) {
	if subitem.Subscription.Spec.Package != "" && subitem.Subscription.Spec.Package != dpl.Name {
		return nil, nil, errors.Errorf("package name does not match, skiping package: %v on deployable %v", subitem.Subscription.Spec.Package, dpl.Name)
	}

	if !utils.CanPassPackageFilter(subitem.Subscription.Spec.PackageFilter, dpl) {
		return nil, nil, errors.Errorf("failed to pass package filter-annotations filter, deployable %v", dpl.Name)
	}

	if !utils.IsDeployableInVersionSet(versionMap, dpl) {
		return nil, nil, errors.Errorf("failed to pass package filter-version filter, deployable %v", dpl.Name)
	}

	template := &unstructured.Unstructured{}

	if dpl.Spec.Template == nil {
		return nil, nil, errors.Errorf("processing local deployable %v without template", dpl.Name)
	}

	err := json.Unmarshal(dpl.Spec.Template.Raw, template)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "processing local deployable %v", dpl.Name)
	}

	//if the deployable namespace is not defined, set it to the subscription namespace
	if template.GetNamespace() == "" {
		template.SetNamespace(subitem.Subscription.GetNamespace())
	}

	template, err = utils.OverrideResourceBySubscription(template, dpl.GetName(), subitem.Subscription)
	if err != nil {
		err = utils.SetInClusterPackageStatus(&(subitem.Subscription.Status), dpl.GetName(), err, nil)
		pkgMap[dpl.GetName()] = true

		return nil, nil, err
	}

	// owner reference does not work with cluster scoped subscription
	if !subitem.clusterscoped {
		err = controllerutil.SetControllerReference(subitem.Subscription, template, r.subscriber.scheme)
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to add ower reference")
		}
	}

	orggvk := template.GetObjectKind().GroupVersionKind()
	validgvk := r.subscriber.synchronizer.GetValidatedGVK(orggvk)

	if validgvk == nil {
		gvkerr := errors.Errorf("orggvk resource %v is not supported", orggvk.String())
		err = utils.SetInClusterPackageStatus(&(subitem.Subscription.Status), dpl.GetName(), gvkerr, nil)

		if err != nil {
			klog.Info("error in setting in cluster package status :", err)
		}

		pkgMap[dpl.GetName()] = true

		return dpl, nil, gvkerr
	}

	if r.subscriber.synchronizer.IsResourceNamespaced(*validgvk) {
		if !subitem.clusterscoped || template.GetNamespace() == "" {
			template.SetNamespace(subitem.Subscription.Namespace)
		}
	}

	dpl.Spec.Template.Raw, err = json.Marshal(template)

	if err != nil {
		klog.Warning("Mashaling template, got error:", err)
		return nil, nil, errors.Wrap(err, "failed to mashaling template")
	}

	annotations := dpl.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[dplv1alpha1.AnnotationLocal] = "true"
	dpl.SetAnnotations(annotations)

	return dpl, validgvk, nil
}
