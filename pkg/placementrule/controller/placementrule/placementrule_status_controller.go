// Copyright 2022 The Kubernetes Authors.
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

package placementrule

import (
	"context"
	"reflect"

	plrv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// PlacementRuleStatusPredicateFunctions filters PlacementRule status decisions update
var placementRuleStatusPredicateFunctions = predicate.Funcs{
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

func AddStatusRec(mgr manager.Manager) error {
	return addRec(mgr, genReconciler(mgr))
}

func genReconciler(mgr manager.Manager) reconcile.Reconciler {
	authCfg := mgr.GetConfig()
	authCfg.QPS = 100.0
	authCfg.Burst = 200

	return &ReconcilePlacementRuleStatus{Client: mgr.GetClient()}
}

func addRec(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New("placementrule-status-controller", mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 1,
	})
	if err != nil {
		return err
	}

	// Watch for changes to PlacementRule Status
	err = c.Watch(source.Kind(mgr.GetCache(), &plrv1.PlacementRule{}), &handler.EnqueueRequestForObject{},
		placementRuleStatusPredicateFunctions)
	if err != nil {
		return err
	}

	klog.Info("Successfully added placementrule-status-controller watching PlacementRule Status changes")

	return nil
}

var _ reconcile.Reconciler = &ReconcilePlacementRuleStatus{}

type ReconcilePlacementRuleStatus struct {
	client.Client
}

// Reconcile reads that state of the cluster for a PlacementRule object and makes changes based on the state read
// and what is in the PlacementRule.Status
// a Deployment as an example
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=multicloud-apps.io,resources=placementrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multicloud.io,resources=placementrules/status,verbs=get;update;patch
func (r *ReconcilePlacementRuleStatus) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Fetch the PlacementRule instance
	instance := &plrv1.PlacementRule{}
	err := r.Get(ctx, request.NamespacedName, instance)

	klog.Info("Reconciling Status:", request.NamespacedName, " with Get err:", err)

	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	err = r.syncPlacementDecisions(ctx, *instance)
	if err != nil {
		klog.Error("err:", err)
	}

	klog.V(1).Info("Reconciling Status - finished.", request.NamespacedName)

	return reconcile.Result{}, err
}
