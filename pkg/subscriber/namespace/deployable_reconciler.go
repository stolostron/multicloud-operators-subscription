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
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"

	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

// DeployableReconciler reconciles a Deployable object of Nmespace channel
type DeployableReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
	subscriber *Subscriber
	itemkey    types.NamespacedName
}

// Reconcile finds out all channels related to this deployable, then all subscriptions subscribing that channel and update them
func (r *DeployableReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.V(1).Info("Deployable Reconciling: ", request.NamespacedName, " deployable for subitem ", r.itemkey)

	tw := r.subscriber.itemmap[r.itemkey].Subscription.Spec.TimeWindow
	if tw != nil {
		nextRun := utils.NextStartPoint(tw, time.Now())
		klog.V(5).Infof(time.Now().String())
		klog.V(5).Infof("Reconciling deployable %v, for subscription %v, with tw %v having nextRun time %v",
			request.NamespacedName.String(), r.subscriber.itemmap[r.itemkey].Subscription.GetName(), tw, nextRun)

		if nextRun > time.Duration(0) {
			klog.V(1).Infof("Subcription %v will run after %v", request.NamespacedName.String(), nextRun)
			return reconcile.Result{RequeueAfter: nextRun}, nil
		}
	}

	result := reconcile.Result{}
	err := r.doSubscription()

	if err != nil {
		result.RequeueAfter = time.Duration(r.subscriber.synchronizer.Interval*5) * time.Second

		klog.Error("Failed to reconcile deployable for namespace subscriber with error:", err)
	}

	return result, nil
}

func (r *DeployableReconciler) doSubscription() error {
	var retryerr error

	subitem, ok := r.subscriber.itemmap[r.itemkey]

	if !ok {
		errmsg := "Failed to locate subscription item " + r.itemkey.String() + " in existing map"

		klog.Error(errmsg)

		return errors.New(errmsg)
	}

	klog.V(5).Info("Processing subscriptions: ", r.itemkey)

	dpllist := &dplv1alpha1.DeployableList{}

	subNamespace := subitem.Channel.Spec.PathName
	if subNamespace == "" {
		errmsg := "channel namespace should not be empty in channel resource of subitem " + r.itemkey.String()

		klog.Error(errmsg)

		return errors.New(errmsg)
	}

	listOptions := &client.ListOptions{Namespace: subNamespace}

	if subitem.Subscription.Spec.PackageFilter != nil && subitem.Subscription.Spec.PackageFilter.LabelSelector != nil {
		clSelector, err := utils.ConvertLabels(subitem.Subscription.Spec.PackageFilter.LabelSelector)
		if err != nil {
			klog.Error("Failed to set label selector of subscrption:", subitem.Subscription.Spec.PackageFilter.LabelSelector, " err:", err)
		}

		listOptions.LabelSelector = clSelector
	}

	err := r.List(context.TODO(), dpllist, listOptions)

	klog.V(2).Info("Got ", len(dpllist.Items), " deployable list for process from namespace ", subNamespace, " with list option:", listOptions.LabelSelector)

	if err != nil {
		klog.Error("Failed to list objecrts from namespace ", subNamespace, " err:", err)
	}

	hostkey := types.NamespacedName{Name: subitem.Subscription.Name, Namespace: subitem.Subscription.Namespace}
	syncsource := deployablesyncsource + hostkey.String()
	// subscribed k8s resource
	kvalid := r.subscriber.synchronizer.CreateValiadtor(syncsource)
	pkgMap := make(map[string]bool)

	vsub := ""

	if subitem.Subscription.Spec.PackageFilter != nil {
		vsub = subitem.Subscription.Spec.PackageFilter.Version
	}

	versionMap := utils.GenerateVersionSet(utils.DplArrayToDplPointers(dpllist.Items), vsub)

	for _, dpl := range dpllist.Items {
		klog.V(5).Infof("Updating subscription %v, with Deployable %v  ", syncsource, hostkey)

		dpltosync, validgvk, err := r.doSubscribeDeployable(subitem, dpl.DeepCopy(), versionMap, pkgMap)
		if err != nil {
			klog.V(3).Info("Skipping deployable", dpl.Name)

			if dpltosync != nil {
				retryerr = err
			}

			continue
		}

		klog.V(5).Info("Ready to register template:", hostkey, dpltosync, syncsource)

		err = r.subscriber.synchronizer.RegisterTemplate(hostkey, dpltosync, syncsource)

		if err != nil {
			err = utils.SetInClusterPackageStatus(&(subitem.Subscription.Status), dpltosync.GetName(), err, nil)

			if err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}

			pkgMap[dpltosync.GetName()] = true

			return nil
		}

		dplkey := types.NamespacedName{
			Name:      dpltosync.Name,
			Namespace: dpltosync.Namespace,
		}
		kvalid.AddValidResource(*validgvk, hostkey, dplkey)

		pkgMap[dplkey.Name] = true

		klog.V(5).Info("Finished Register ", *validgvk, hostkey, dplkey, " with err:", err)
	}

	r.subscriber.synchronizer.ApplyValiadtor(kvalid)

	return retryerr
}

func (r *DeployableReconciler) doSubscribeDeployable(subitem *SubscriberItem, dpl *dplv1alpha1.Deployable,
	versionMap map[string]utils.VersionRep, pkgMap map[string]bool) (*dplv1alpha1.Deployable, *schema.GroupVersionKind, error) {
	if subitem.Subscription.Spec.Package != "" && subitem.Subscription.Spec.Package != dpl.Name {
		errmsg := "Name does not match, skiping:" + subitem.Subscription.Spec.Package + "|" + dpl.Name
		klog.V(3).Info(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	if utils.FiltePackageOut(subitem.Subscription.Spec.PackageFilter, dpl) {
		errmsg := "Filte out by package filter " + dpl.Name
		klog.Info(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	if !utils.IsDeployableInVersionSet(versionMap, dpl) {
		errmsg := "Filte out by version map " + dpl.Name
		klog.Info(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	template := &unstructured.Unstructured{}

	if dpl.Spec.Template == nil {
		errmsg := "Processing local deployable without template:" + dpl.Name
		klog.Error(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	err := json.Unmarshal(dpl.Spec.Template.Raw, template)
	if err != nil {
		errmsg := "Processing local deployable with template: " + dpl.Name + " with error " + err.Error()

		klog.Error(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	template, err = utils.OverrideResourceBySubscription(template, dpl.GetName(), subitem.Subscription)
	if err != nil {
		err = utils.SetInClusterPackageStatus(&(subitem.Subscription.Status), dpl.GetName(), err, nil)
		if err != nil {
			klog.Info("error in overriding for package: ", err)
		}

		pkgMap[dpl.GetName()] = true

		return nil, nil, err
	}

	// owner reference does not work with cluster scoped subscription
	if !subitem.clusterscoped {
		err = controllerutil.SetControllerReference(subitem.Subscription, template, r.subscriber.scheme)
		if err != nil {
			errmsg := "Adding owner reference to template, got error:" + err.Error()
			klog.Error(errmsg)

			return nil, nil, errors.New(errmsg)
		}
	}

	orggvk := template.GetObjectKind().GroupVersionKind()
	validgvk := r.subscriber.synchronizer.GetValidatedGVK(orggvk)

	if validgvk == nil {
		gvkerr := errors.New("Resource " + orggvk.String() + " is not supported")
		err = utils.SetInClusterPackageStatus(&(subitem.Subscription.Status), dpl.GetName(), gvkerr, nil)

		if err != nil {
			klog.Info("error in setting in cluster package status :", err)
		}

		pkgMap[dpl.GetName()] = true

		return dpl, nil, gvkerr
	}

	if r.subscriber.synchronizer.KubeResources[*validgvk].Namespaced {
		if !subitem.clusterscoped || template.GetNamespace() == "" {
			template.SetNamespace(subitem.Subscription.Namespace)
		}
	}

	dpl.Spec.Template.Raw, err = json.Marshal(template)

	if err != nil {
		klog.Warning("Mashaling template, got error:", err)
		return nil, nil, err
	}

	annotations := dpl.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[dplv1alpha1.AnnotationLocal] = "true"
	dpl.SetAnnotations(annotations)

	return dpl, validgvk, nil
}
