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

package deployable

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
	placementv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/deployable/utils"
	placementutils "open-cluster-management.io/multicloud-operators-subscription/pkg/placementrule/utils"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Deployable Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	erecorder, _ := utils.NewEventRecorder(mgr.GetConfig(), mgr.GetScheme())
	authClient := kubernetes.NewForConfigOrDie(mgr.GetConfig())

	return &ReconcileDeployable{
		Client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		authClient:    authClient,
		eventRecorder: erecorder,
	}
}

type placementruleMapper struct {
	client.Client
}

func (mapper *placementruleMapper) Map(obj handler.MapObject) []reconcile.Request {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	cname := obj.Meta.GetName()
	klog.V(5).Info("In placement Mapper:", cname)

	var requests []reconcile.Request

	dplList := &appv1alpha1.DeployableList{}
	listopts := &client.ListOptions{}
	err := mapper.List(context.TODO(), dplList, listopts)

	if err != nil {
		klog.Error("Failed to list deployables for placementrule mapper with error:", err)
		return requests
	}

	for _, dpl := range dplList.Items {
		if dpl.Spec.Placement == nil || dpl.Spec.Placement.PlacementRef == nil || dpl.Spec.Placement.PlacementRef.Name != obj.Meta.GetName() {
			continue
		}

		// only reconcile it's own object
		objkey := types.NamespacedName{
			Name:      dpl.GetName(),
			Namespace: dpl.GetNamespace(),
		}

		requests = append(requests, reconcile.Request{NamespacedName: objkey})
	}

	return requests
}

type clusterMapper struct {
	client.Client
}

func (mapper *clusterMapper) Map(obj handler.MapObject) []reconcile.Request {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	cname := obj.Meta.GetName()
	klog.V(5).Info("In cluster Mapper for ", cname)

	var requests []reconcile.Request

	dplList := &appv1alpha1.DeployableList{}

	listopts := &client.ListOptions{}
	err := mapper.List(context.TODO(), dplList, listopts)

	if err != nil {
		klog.Error("Failed to list deployables for cluster mapper with error:", err)
		return requests
	}

	for _, dpl := range dplList.Items {
		// only reconcile with when placement is set and not using ref
		if dpl.Spec.Placement == nil || dpl.Spec.Placement.PlacementRef == nil {
			continue
		}

		if dpl.Spec.Placement.ClusterSelector == nil {
			matched := false

			for _, cn := range dpl.Spec.Placement.Clusters {
				if cn.Name == cname {
					matched = true
				}
			}

			if !matched {
				continue
			}
		}

		objkey := types.NamespacedName{
			Name:      dpl.GetName(),
			Namespace: dpl.GetNamespace(),
		}

		requests = append(requests, reconcile.Request{NamespacedName: objkey})
	}

	return requests
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("deployable-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Deployable
	err = c.Watch(
		&source.Kind{Type: &appv1alpha1.Deployable{}},
		&handler.EnqueueRequestsFromMapFunc{ToRequests: &deployableMapper{mgr.GetClient()}}, utils.DeployablePredicateFunc)
	if err != nil {
		return err
	}

	// watch for placementrule changes
	err = c.Watch(&source.Kind{Type: &placementv1alpha1.PlacementRule{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: &placementruleMapper{mgr.GetClient()}},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				newpr := e.ObjectNew.(*placementv1alpha1.PlacementRule)
				oldpr := e.ObjectOld.(*placementv1alpha1.PlacementRule)

				return !reflect.DeepEqual(newpr.Status, oldpr.Status)
			},
		})
	if err != nil {
		return err
	}

	// watch for cluster change excluding heartbeat
	if placementutils.IsReadyACMClusterRegistry(mgr.GetAPIReader()) {
		err = c.Watch(
			&source.Kind{Type: &spokeClusterV1.ManagedCluster{}},
			&handler.EnqueueRequestsFromMapFunc{ToRequests: &clusterMapper{mgr.GetClient()}},
			placementutils.ClusterPredicateFunc,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

type deployableMapper struct {
	client.Client
}

func (mapper *deployableMapper) Map(obj handler.MapObject) []reconcile.Request {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	// rolling target deployable changed, need to update the rolling deployable
	var requests []reconcile.Request

	// enqueue itself
	requests = append(requests,
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      obj.Meta.GetName(),
				Namespace: obj.Meta.GetNamespace(),
			},
		},
	)

	// list thing for rolling update check
	dplList := &appv1alpha1.DeployableList{}
	listopts := &client.ListOptions{Namespace: obj.Meta.GetNamespace()}
	err := mapper.List(context.TODO(), dplList, listopts)

	if err != nil {
		klog.Error("Listing deployables in mapper and got error:", err)
	}

	for _, dpl := range dplList.Items {
		annotations := dpl.GetAnnotations()
		if annotations == nil || annotations[appv1alpha1.AnnotationRollingUpdateTarget] == "" {
			// not rolling
			continue
		}

		if annotations[appv1alpha1.AnnotationRollingUpdateTarget] != obj.Meta.GetName() {
			// rolling to annother one, skipping
			continue
		}

		// rolling target deployable changed, need to update the rolling deployable
		objkey := types.NamespacedName{
			Name:      dpl.GetName(),
			Namespace: dpl.GetNamespace(),
		}

		requests = append(requests, reconcile.Request{NamespacedName: objkey})
	}
	// end of rolling update check

	// reconcile hosting one, if there is change in cluster, assuming no 2-hop hosting
	hdplkey := utils.GetHostDeployableFromObject(obj.Meta)
	if hdplkey != nil && hdplkey.Name != "" {
		requests = append(requests, reconcile.Request{NamespacedName: *hdplkey})
	}

	klog.V(5).Info("Out deployable mapper with requests:", requests)

	return requests
}

// blank assignment to verify that ReconcileDeployable implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileDeployable{}

// ReconcileDeployable reconciles a Deployable object
type ReconcileDeployable struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
	authClient kubernetes.Interface
	scheme     *runtime.Scheme

	eventRecorder *utils.EventRecorder
}

// Reconcile reads that state of the cluster for a Deployable object and makes changes based on the state read
// and what is in the Deployable.Spec
func (r *ReconcileDeployable) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the Deployable instance
	instance := &appv1alpha1.Deployable{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	klog.Info("Reconciling:", request.NamespacedName, " with Get err:", err)

	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// validate all deployables, remove the deployables whose hosting deployables are gone
			err = r.validateDeployables()

			klog.Info("Reconciling - finished.", request.NamespacedName, " with Get err:", err)

			return reconcile.Result{}, err
		}
		// Error reading the object - requeue the request.
		klog.Info("Reconciling - finished.", request.NamespacedName, " with Get err:", err)

		return reconcile.Result{}, err
	}

	savedStatus := instance.Status.DeepCopy()

	// try if it is a hub deployable
	huberr := r.handleDeployable(instance)

	newStatus := instance.Status.DeepCopy()

	if huberr != nil {
		newStatus.Phase = appv1alpha1.DeployableFailed
		newStatus.PropagatedStatus = nil
		newStatus.Reason = huberr.Error()
	}

	newPropagatedStatus := make(map[string]*appv1alpha1.ResourceUnitStatus)

	for k, v := range instance.Status.PropagatedStatus {
		newPropagatedStatus[k] = v
	}

	// only update hub deployable. no need to update propagated deployable.
	if instance.Spec.Placement != nil {
		err = r.Update(context.TODO(), instance)

		if err != nil {
			klog.Error("Error returned when updating instance:", err, "instance:", instance)
			return reconcile.Result{}, err
		}

		// reconcile finished check if need to upadte the resource
		if len(instance.GetObjectMeta().GetFinalizers()) == 0 {
			if !reflect.DeepEqual(savedStatus, newStatus) ||
				!reflect.DeepEqual(savedStatus.PropagatedStatus, newPropagatedStatus) {
				now := metav1.Now()
				newStatus.LastUpdateTime = &now

				newStatus.PropagatedStatus = newPropagatedStatus

				klog.V(5).Infof("instance: %v/%v, Update status: %#v",
					instance.GetNamespace(), instance.GetName(),
					newStatus)

				utils.PrintPropagatedStatus(newStatus.PropagatedStatus, "New Propagated Status: ")

				instance.Status = *newStatus

				err = r.Status().Update(context.TODO(), instance)

				if err != nil {
					klog.Error("Error returned when updating status:", err, "instance:", instance)
					return reconcile.Result{}, err
				}
			}
		}
	}

	klog.Info("Reconciling - finished.", request.NamespacedName, " with Get err:", err)

	return reconcile.Result{}, nil
}
