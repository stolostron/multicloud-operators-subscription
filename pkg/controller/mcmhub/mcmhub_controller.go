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
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Subscription Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	erecorder, _ := utils.NewEventRecorder(mgr.GetConfig(), mgr.GetScheme())

	rec := &ReconcileSubscription{
		Client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		eventRecorder: erecorder,
	}

	return rec
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("mcmhub-subscription-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Subscription
	err = c.Watch(&source.Kind{Type: &appv1alpha1.Subscription{}}, &handler.EnqueueRequestForObject{}, utils.SubscriptionPredicateFunctions)
	if err != nil {
		return err
	}

	// in hub, watch the deployable created by the subscription
	err = c.Watch(&source.Kind{Type: &dplv1alpha1.Deployable{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &appv1alpha1.Subscription{},
	}, predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			newdpl := e.ObjectNew.(*dplv1alpha1.Deployable)
			olddpl := e.ObjectOld.(*dplv1alpha1.Deployable)

			return !reflect.DeepEqual(newdpl.Status, olddpl.Status)
		},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileSubscription implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileSubscription{}

// ReconcileSubscription reconciles a Subscription object
type ReconcileSubscription struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
	scheme        *runtime.Scheme
	eventRecorder *utils.EventRecorder
}

// Reconcile reads that state of the cluster for a Subscription object and makes changes based on the state read
// and what is in the Subscription.Spec
func (r *ReconcileSubscription) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.Info("MCM Hub Reconciling subscription: ", request.NamespacedName)

	instance := &appv1alpha1.Subscription{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)

	if err != nil {
		if errors.IsNotFound(err) {
			klog.Info("Subscription: ", request.NamespacedName, " is gone")
			// Object not found, delete existing subscriberitem if any

			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// save original status
	orgst := instance.Status.DeepCopy()

	// process as hub subscription, generate deployable to propagate
	pl := instance.Spec.Placement
	if pl != nil && (pl.PlacementRef != nil || pl.Clusters != nil || pl.ClusterSelector != nil) {
		err = r.doMCMHubReconcile(instance)

		instance.Status.Phase = appv1alpha1.SubscriptionPropagated
		if err != nil {
			instance.Status.Phase = appv1alpha1.SubscriptionFailed
			instance.Status.Reason = err.Error()
		}
	} else {
		// no longer hub subscription
		if instance.Status.Phase != appv1alpha1.SubscriptionFailed && instance.Status.Phase != appv1alpha1.SubscriptionSubscribed {
			instance.Status.Phase = ""
			instance.Status.Message = ""
			instance.Status.Reason = ""
		}

		if instance.Status.Statuses != nil {
			localkey := types.NamespacedName{}.String()
			for k := range instance.Status.Statuses {
				if k != localkey {
					delete(instance.Status.Statuses, k)
				}
			}
		}
	}

	result := reconcile.Result{}

	if !reflect.DeepEqual(*orgst, instance.Status) {
		klog.Info("MCM Hub updating subscriptoin status to ", instance.Status)

		instance.Status.LastUpdateTime = metav1.Now()

		err = r.Status().Update(context.TODO(), instance)

		if err != nil {
			klog.Error("Failed to update status for mcm hub subscription ", request.NamespacedName, " with error: ", err, " retry after 1 seconds")

			result.RequeueAfter = 1 * time.Second
		}
	}

	return result, nil
}
