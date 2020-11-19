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

package agenttoken

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	ghsub "github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber/git"
	hrsub "github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber/helmrepo"
	nssub "github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber/namespace"
	ossub "github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber/objectbucket"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Subscription Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
// If standalone = true, it will only reconcile standalone subscriptions without hosting subscription from ACM hub.
// If standalone = false, it will only reconcile subscriptions that are propagated from ACM hub.
func Add(mgr manager.Manager, hubconfig *rest.Config, standalone bool) error {
	hubclient, err := client.New(hubconfig, client.Options{})
	if err != nil {
		klog.Error("Failed to generate client to hub cluster with error:", err)
		return err
	}

	subs := make(map[string]appv1.Subscriber)

	if nssub.GetdefaultNsSubscriber() == nil {
		errmsg := "default namespace subscriber is not initialized"
		klog.Error(errmsg)

		return errors.NewServiceUnavailable(errmsg)
	}

	subs[chnv1.ChannelTypeNamespace] = nssub.GetdefaultNsSubscriber()
	subs[chnv1.ChannelTypeHelmRepo] = hrsub.GetDefaultSubscriber()
	subs[chnv1.ChannelTypeGitHub] = ghsub.GetDefaultSubscriber()
	subs[chnv1.ChannelTypeGit] = ghsub.GetDefaultSubscriber()
	subs[chnv1.ChannelTypeObjectBucket] = ossub.GetDefaultSubscriber()

	return add(mgr, newReconciler(mgr, hubclient))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, hubclient client.Client) reconcile.Reconciler {
	rec := &ReconcileAgentToken{
		Client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		hubclient: hubclient,
	}

	return rec
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("agent-token-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to klusterlet-addon-appmgr service account in open-cluster-management-agent-addon namespace.
	err = c.Watch(&source.Kind{Type: &corev1.ServiceAccount{}}, &handler.EnqueueRequestForObject{}, utils.ServiceAccountPredicateFunctions)
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileSubscription implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAgentToken{}

// ReconcileAppMgrToken reconciles a service account object
type ReconcileAgentToken struct {
	client.Client
	hubclient client.Client
	scheme    *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Subscription object and makes changes based on the state read
// and what is in the Subscription.Spec
func (r *ReconcileAgentToken) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.Info(request.Name)
	klog.Info(request.Namespace)
	klog.Info(request.NamespacedName)
	klog.Info(request.String())
	/*
		klog.Info("Standalone/Endpoint Reconciling subscription: ", request.NamespacedName)
		defer klog.Info("Exit Reconciling subscription: ", request.NamespacedName)



		instance := &appv1.Subscription{}
		err := r.Get(context.TODO(), request.NamespacedName, instance)

		if err != nil {
			if errors.IsNotFound(err) {
				klog.Info("Subscription: ", request.NamespacedName, " is gone")

				// Object not found, delete existing subscriberitem if any
				for _, sub := range r.subscribers {
					if err := sub.UnsubscribeItem(request.NamespacedName); err != nil {
						return reconcile.Result{RequeueAfter: time.Second * 2}, err
					}
				}

				objKind := schema.GroupVersionKind{Group: "", Kind: SecretKindStr, Version: "v1"}
				err := r.DeleteReferredObjects(request.NamespacedName, objKind)

				if err != nil {
					klog.Errorf("Had error %v while processing the referred secert", err)
				}

				objKind = schema.GroupVersionKind{Group: "", Kind: ConfigMapKindStr, Version: "v1"}
				err = r.DeleteReferredObjects(request.NamespacedName, objKind)

				if err != nil {
					klog.Errorf("Had error %v while processing the referred secert", err)
				}

				return reconcile.Result{}, err
			}
			// Error reading the object - requeue the request.
			return reconcile.Result{}, err
		}

		annotations := instance.GetAnnotations()
		pl := instance.Spec.Placement

		if pl != nil && pl.Local != nil && *pl.Local {
			// If standalone = true, reconcile standalone subscriptions without hosting subscription from ACM hub.
			// If standalone = false, reconcile subscriptions that are propagated from ACM hub. These subscriptions have this annotation.
			if (strings.EqualFold(annotations[appv1.AnnotationHosting], "") && r.standalone) ||
				(!strings.EqualFold(annotations[appv1.AnnotationHosting], "") && !r.standalone) {
				reconcileErr := r.doReconcile(instance)

				// doReconcile updates the subscription. Later this function fails to update the subscription status
				// if the same subscription resource is used because it has already been updated by reconcile.
				// Get the newly updated subscription resource.
				_ = r.Get(context.TODO(), request.NamespacedName, instance)

				instance.Status.Phase = appv1.SubscriptionSubscribed
				instance.Status.Reason = ""

				if reconcileErr != nil {
					instance.Status.Phase = appv1.SubscriptionFailed
					instance.Status.Reason = reconcileErr.Error()
					klog.Errorf("doReconcile got error %v", reconcileErr)
				}

				// if the subscription pause lable is true, stop updating subscription status.
				if subutil.GetPauseLabel(instance) {
					klog.Info("updating subscription status: ", request.NamespacedName, " is paused")
					return reconcile.Result{}, nil
				}

				instance.Status.LastUpdateTime = metav1.Now()

				// calculate the requeue time for updating the timewindow status
				nextStatusUpateAt := time.Duration(0)

				if instance.Spec.TimeWindow == nil {
					instance.Status.Message = subscriptionActive
				} else {
					if utils.IsInWindow(instance.Spec.TimeWindow, r.clk()) {
						instance.Status.Message = subscriptionActive
					} else {
						instance.Status.Message = subscriptionBlock
					}
					nextStatusUpateAt = utils.NextStatusReconcile(instance.Spec.TimeWindow, r.clk())

					klog.Infof("Next time window status reconciliation will occur in " + nextStatusUpateAt.String())
				}

				err = r.Status().Update(context.TODO(), instance)

				result := reconcile.Result{RequeueAfter: nextStatusUpateAt}

				if err != nil {
					klog.Errorf("failed to update status for subscription %v with error %v retry after 1 second", request.NamespacedName, err)

					result.RequeueAfter = 1 * time.Second
				}

				return result, err
			}
		} else {
			klog.Infof("Subscription %v is no longer local subscription. Remove subscription packages.", request.NamespacedName)
			// no longer local
			// if the subscription pause lable is true, stop unsubscription here.
			if subutil.GetPauseLabel(instance) {
				klog.Info("unsubscribing: ", request.NamespacedName, " is paused")
				return reconcile.Result{}, nil
			}

			for _, sub := range r.subscribers {
				_ = sub.UnsubscribeItem(request.NamespacedName)
			}
		}*/

	return reconcile.Result{}, nil
}
