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
	"fmt"
	"math/rand"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"k8s.io/klog/klogr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	plrv1 "github.com/open-cluster-management/multicloud-operators-placementrule/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

const clusterRole = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: open-cluster-management:subscription-admin
rules:
- apiGroups:
  - app.k8s.io
  resources:
  - applications
  verbs:
  - '*'
- apiGroups:
  - apps.open-cluster-management.io
  resources:
  - '*'
  verbs:
  - '*'
- apiGroups:
  - ""
  resources:
  - configmaps
  - secrets
  - namespaces
  verbs:
  - '*'`

const (
	hubLogger                  = "subscription-hub-reconciler"
	defaultHookRequeueInterval = time.Second * 15
	INFOLevel                  = 1
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

//Option provide easy way to test the reconciler
type Option func(*ReconcileSubscription)

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, op ...Option) reconcile.Reconciler {
	erecorder, _ := utils.NewEventRecorder(mgr.GetConfig(), mgr.GetScheme())
	logger := klogr.New().WithName(hubLogger)

	rec := &ReconcileSubscription{
		Client: mgr.GetClient(),
		// used for the helm to run get the resource list
		cfg:                 mgr.GetConfig(),
		scheme:              mgr.GetScheme(),
		eventRecorder:       erecorder,
		logger:              logger,
		hookRequeueInterval: defaultHookRequeueInterval,
		hooks:               NewAnsibleHooks(mgr.GetClient(), logger),
	}

	for _, f := range op {
		f(rec)
	}

	return rec
}

type subscriptionMapper struct {
	client.Client
}

func (mapper *subscriptionMapper) Map(obj handler.MapObject) []reconcile.Request {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	// rolling target subscription changed, need to update the source subscription
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
	subList := &appv1.SubscriptionList{}
	listopts := &client.ListOptions{Namespace: obj.Meta.GetNamespace()}
	err := mapper.List(context.TODO(), subList, listopts)

	if err != nil {
		klog.Error("Listing subscriptions in mapper and got error:", err)
	}

	for _, sub := range subList.Items {
		annotations := sub.GetAnnotations()
		if annotations == nil || annotations[appv1.AnnotationRollingUpdateTarget] == "" {
			// not rolling
			continue
		}

		if annotations[appv1.AnnotationRollingUpdateTarget] != obj.Meta.GetName() {
			// rolling to annother one, skipping
			continue
		}

		// rolling target subscription changed, need to update the source subscription
		objkey := types.NamespacedName{
			Name:      sub.GetName(),
			Namespace: sub.GetNamespace(),
		}

		requests = append(requests, reconcile.Request{NamespacedName: objkey})
	}
	// end of rolling update check

	// reconcile hosting one, if there is change in cluster, assuming no 2-hop hosting
	hdplkey := utils.GetHostSubscriptionFromObject(obj.Meta)
	if hdplkey != nil && hdplkey.Name != "" {
		requests = append(requests, reconcile.Request{NamespacedName: *hdplkey})
	}

	klog.V(5).Info("Out subscription mapper with requests:", requests)

	return requests
}

type channelMapper struct {
	client.Client
}

func (mapper *channelMapper) Map(obj handler.MapObject) []reconcile.Request {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	// if channel is created/updated/deleted, its relative subscriptions should be reconciled.

	chn := obj.Meta.GetNamespace() + "/" + obj.Meta.GetName()

	var requests []reconcile.Request

	subList := &appv1.SubscriptionList{}
	listopts := &client.ListOptions{}
	err := mapper.List(context.TODO(), subList, listopts)

	if err != nil {
		klog.Error("Listing all subscriptions in channelMapper and got error:", err)
	}

	for _, sub := range subList.Items {
		if sub.Spec.Channel == chn {
			objkey := types.NamespacedName{
				Name:      sub.GetName(),
				Namespace: sub.GetNamespace(),
			}

			requests = append(requests, reconcile.Request{NamespacedName: objkey})
		}
	}

	klog.V(1).Info("Out channel mapper with requests:", requests)

	return requests
}

type placementRuleMapper struct {
	client.Client
}

func (mapper *placementRuleMapper) Map(obj handler.MapObject) []reconcile.Request {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	// if placementrule is created/updated/deleted, its relative subscriptions should be reconciled.

	var requests []reconcile.Request

	subList := &appv1.SubscriptionList{}
	listopts := &client.ListOptions{}
	err := mapper.List(context.TODO(), subList, listopts)

	if err != nil {
		klog.Error("Listing all subscriptions in placementRuleMapper and got error:", err)
	}

	for _, sub := range subList.Items {
		if sub.Spec.Placement != nil && sub.Spec.Placement.PlacementRef != nil {
			plRef := sub.Spec.Placement.PlacementRef

			if plRef.Name != obj.Meta.GetName() || plRef.Namespace != obj.Meta.GetNamespace() {
				continue
			}

			subKey := types.NamespacedName{Name: sub.GetName(), Namespace: sub.GetNamespace()}

			requests = append(requests, reconcile.Request{NamespacedName: subKey})
		}
	}

	klog.V(1).Info("Out placement mapper with requests:", requests)

	return requests
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("mcmhub-subscription-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Subscription
	err = c.Watch(
		&source.Kind{Type: &appv1.Subscription{}},
		&handler.EnqueueRequestsFromMapFunc{ToRequests: &subscriptionMapper{mgr.GetClient()}},
		utils.SubscriptionPredicateFunctions)
	if err != nil {
		return err
	}

	// in hub, watch the deployable created by the subscription
	err = c.Watch(
		&source.Kind{Type: &dplv1.Deployable{}},
		&handler.EnqueueRequestForOwner{IsController: true, OwnerType: &appv1.Subscription{}},
		utils.DeployablePredicateFunctions)
	if err != nil {
		return err
	}

	// in hub, watch for channel changes
	err = c.Watch(
		&source.Kind{Type: &chnv1.Channel{}},
		&handler.EnqueueRequestsFromMapFunc{ToRequests: &channelMapper{mgr.GetClient()}},
		utils.ChannelPredicateFunctions)
	if err != nil {
		return err
	}

	// in hub, watch for placement rule changes
	err = c.Watch(
		&source.Kind{Type: &plrv1.PlacementRule{}},
		&handler.EnqueueRequestsFromMapFunc{ToRequests: &placementRuleMapper{mgr.GetClient()}},
		utils.PlacementRulePredicateFunctions)
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
	logger              logr.Logger
	cfg                 *rest.Config
	scheme              *runtime.Scheme
	eventRecorder       *utils.EventRecorder
	hookRequeueInterval time.Duration
	hooks               HookProcessor
}

// CreateSubscriptionAdminRBAC checks existence of subscription-admin clusterrole and clusterrolebinding
// and creates them if not found
func (r *ReconcileSubscription) CreateSubscriptionAdminRBAC() error {
	// Create subscription admin ClusteRole
	clusterRole := subAdminClusterRole()
	foundClusterRole := &rbacv1.ClusterRole{}

	if err := r.Get(context.TODO(), types.NamespacedName{Name: clusterRole.Name}, foundClusterRole); err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Info("Creating ClusterRole ", clusterRole.Name)
			err = r.Create(context.TODO(), clusterRole)

			if err != nil {
				klog.Error("error:", err)
				return err
			}
		} else {
			klog.Error("error:", err)
			return err
		}
	}

	// Create subscription admin ClusteRoleBinding
	clusterRoleBinding := subAdminClusterRoleBinding()
	foundClusterRoleBinding := &rbacv1.ClusterRoleBinding{}

	if err := r.Get(context.TODO(), types.NamespacedName{Name: clusterRoleBinding.Name}, foundClusterRoleBinding); err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Info("Creating ClusterRoleBiding ", clusterRoleBinding.Name)
			err = r.Create(context.TODO(), clusterRoleBinding)

			if err != nil {
				klog.Error("error:", err)
				return err
			}
		} else {
			klog.Error("error:", err)
			return err
		}
	}

	return nil
}

func subAdminClusterRole() *rbacv1.ClusterRole {
	cr := &rbacv1.ClusterRole{}
	err := yaml.Unmarshal([]byte(clusterRole), &cr)

	if err != nil {
		return nil
	}

	return cr
}

func subAdminClusterRoleBinding() *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: appv1.SubscriptionAdmin,
		},
		Subjects: []rbacv1.Subject{},
		RoleRef: rbacv1.RoleRef{
			Kind: "ClusterRole",
			Name: appv1.SubscriptionAdmin,
		},
	}
}

func (r *ReconcileSubscription) setHubSubscriptionStatus(sub *appv1.Subscription) {
	// Get propagation status from the subscription deployable
	hubdpl := &dplv1.Deployable{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: sub.Name + "-deployable", Namespace: sub.Namespace}, hubdpl)

	if err == nil {
		sub.Status.Reason = hubdpl.Status.Reason
		sub.Status.Message = hubdpl.Status.Message

		if hubdpl.Status.Phase == dplv1.DeployableFailed {
			sub.Status.Phase = appv1.SubscriptionPropagationFailed
		} else if hubdpl.Status.Phase == dplv1.DeployableUnknown {
			sub.Status.Phase = appv1.SubscriptionUnknown
		} else {
			sub.Status.Phase = appv1.SubscriptionPropagated
		}
	} else {
		klog.Error(err)
	}
}

// Reconcile reads that state of the cluster for a Subscription object and makes changes based on the state read
// and what is in the Subscription.Spec
func (r *ReconcileSubscription) Reconcile(request reconcile.Request) (result reconcile.Result, returnErr error) {
	logger := r.logger.WithName(request.String())
	logger.V(INFOLevel).Info(fmt.Sprint("entry MCM Hub Reconciling subscription: ", request.String()))

	defer logger.V(INFOLevel).Info(fmt.Sprint("exist Hub Reconciling subscription: ", request.String()))

	//flag used to determine if we skip the posthook
	passedPrehook := true

	var preErr error

	instance := &appv1.Subscription{}
	oins := &appv1.Subscription{}

	defer func() {
		r.finalCommit(passedPrehook, preErr, oins, instance, request, &result)
	}()

	err := r.CreateSubscriptionAdminRBAC()
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.Get(context.TODO(), request.NamespacedName, instance)

	if err != nil {
		if errors.IsNotFound(err) {
			klog.Info("Subscription: ", request.NamespacedName, " is gone")
			// Object not found, delete existing subscriberitem if any
			if err := r.hooks.DeregisterSubscription(request.NamespacedName); err != nil {
				return reconcile.Result{}, err
			}

			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// for later comparison
	oins = instance.DeepCopy()

	// register will skip the failed clone repo
	if err := r.hooks.RegisterSubscription(request.NamespacedName); err != nil {
		logger.Error(err, "failed to register hooks, skip the subscription reconcile")

		passedPrehook = false

		return reconcile.Result{}, nil
	}

	if r.hooks.HasHooks(PreHookType, request.NamespacedName) {
		//if it's registered
		if err := r.hooks.ApplyPreHooks(request.NamespacedName); err != nil {
			logger.Error(err, "failed to apply preHook, skip the subscription reconcile")

			passedPrehook = false

			return reconcile.Result{}, nil
		}

		//if it's registered
		b, err := r.hooks.IsPreHooksCompleted(request.NamespacedName)
		if !b || err != nil {
			// used for use the status update
			preErr = fmt.Errorf("prehook for %v is not ready ", request.String())
			_ = preErr

			if err != nil {
				logger.Error(err, "failed to check prehook status, skip the subscription reconcile")
				return reconcile.Result{}, nil
			}

			result.RequeueAfter = r.hookRequeueInterval
			passedPrehook = false

			return result, nil
		}
	}

	// process as hub subscription, generate deployable to propagate
	pl := instance.Spec.Placement

	klog.Infof("Subscription: %v with placemen %#v", request.NamespacedName.String(), pl)

	//status changes below show override the prehook status
	if pl == nil {
		instance.Status.Phase = appv1.SubscriptionPropagationFailed
		instance.Status.Reason = "Placement must be specified"
	} else if pl != nil && (pl.PlacementRef != nil || pl.Clusters != nil || pl.ClusterSelector != nil) {
		//changes will be added to instance
		err = r.doMCMHubReconcile(instance)

		if err != nil {
			instance.Status.Phase = appv1.SubscriptionPropagationFailed
			instance.Status.Reason = err.Error()
			instance.Status.Statuses = nil
		} else {
			// Get propagation status from the subscription deployable
			r.setHubSubscriptionStatus(instance)
			// for object store, it takes a while for the object to be downloaded,
			// so we want to requeue to get a valid topo annotation
			if !isTopoAnnoExist(instance) {
				//skip gosec G404 since the random number is only used for requeue
				//timer
				// #nosec G404
				if result.RequeueAfter == 0 {
					result.RequeueAfter = time.Second * time.Duration(rand.Intn(10))
				}
			}
		}
	} else { //local: true
		// no longer hub subscription
		err = r.clearSubscriptionDpls(instance)
		if err != nil {
			instance.Status.Phase = appv1.SubscriptionFailed
			instance.Status.Reason = err.Error()
		}

		if instance.Status.Phase != appv1.SubscriptionFailed && instance.Status.Phase != appv1.SubscriptionSubscribed {
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

	_ = oins

	return result, nil
}

func isTopoAnnoExist(sub *appv1.Subscription) bool {
	if sub == nil {
		return false
	}

	annotation := sub.GetAnnotations()

	if len(annotation) == 0 {
		return false
	}

	v, ok := annotation[appv1.AnnotationTopo]

	if !ok {
		return false
	}

	return len(v) != 0
}

// IsSubscriptionCompleted will check:
// a, if the subscription itself is processed
// b, for each of the subscription created on managed cluster, it will check if
// it is 1, propagated and 2, subscribed
func (r *ReconcileSubscription) IsSubscriptionCompleted(subKey types.NamespacedName) (bool, error) {
	subIns := &subv1.Subscription{}
	if err := r.Get(context.TODO(), subKey, subIns); err != nil {
		if k8serrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	}

	subFailSet := map[subv1.SubscriptionPhase]struct{}{
		subv1.SubscriptionPropagationFailed: {},
		subv1.SubscriptionFailed:            {},
		subv1.SubscriptionUnknown:           {},
	}
	//check up the hub cluster status
	if _, ok := subFailSet[subIns.Status.Phase]; ok {
		return false, nil
	}

	managedStatus := subIns.Status.Statuses
	if len(managedStatus) == 0 {
		return true, nil
	}

	for cluster, cSt := range managedStatus {
		if len(cSt.SubscriptionPackageStatus) == 0 {
			continue
		}

		for pkg, pSt := range cSt.SubscriptionPackageStatus {
			if pSt.Phase != subv1.SubscriptionSubscribed {
				r.logger.Error(fmt.Errorf("cluster %s package %s is at status %s", cluster, pkg, pSt.Phase),
					"subscription is not completed")
				return false, nil
			}
		}
	}

	return true, nil
}

//finalCommit will shortcut the prehook logic if the prehook present
// if the prohook is completed, then update subscription will be update(1, the
// main spec, 2, status update)
// if the prehook, subscription is update then entry the posthook logic
//
//the requeue logic is done via set up the RequeueAfter parameter of the
//reconciel.Result
func (r *ReconcileSubscription) finalCommit(passedPrehook bool, preErr error,
	oIns, nIns *subv1.Subscription,
	request reconcile.Request, res *reconcile.Result) {
	// handle the prehook failed status update
	if !passedPrehook && nIns != nil {
		nIns.Status.Phase = appv1.SubscriptionPropagationFailed
		nIns.Status.Reason = preErr.Error()
		nIns.Status.LastUpdateTime = metav1.Now()

		if err := r.Client.Status().Update(context.TODO(), nIns.DeepCopy()); err != nil {
			if k8serrors.IsGone(err) {
				return
			}

			if res.RequeueAfter == time.Duration(0) {
				res.RequeueAfter = 1 * time.Second
				r.logger.Error(err, fmt.Sprintf("failed to update status, will retry after %s", res.RequeueAfter))
			}
		}

		return
	}

	nIns.Status = r.hooks.AppendStatusToSubscription(nIns)
	//checking, labels, annotation, certain status fields of subscriptions
	if utils.IsSubscriptionResourceChanged(oIns, nIns) { // update the instance with propagation status
		//if use instance directly, the instance will be override by the
		//update
		if utils.IsSubscriptionBasicChanged(oIns, nIns) { //if subresource enabled, the update client won't update the status
			if err := r.Client.Update(context.TODO(), nIns.DeepCopy()); err != nil {
				if k8serrors.IsGone(err) {
					return
				}

				if res.RequeueAfter == time.Duration(0) {
					res.RequeueAfter = 1 * time.Second
					r.logger.Error(err, fmt.Sprintf("failed to update status, will retry after %s", res.RequeueAfter))
				}

				return
			}
		} else {
			nIns.Status.LastUpdateTime = metav1.Now()
			if err := r.Client.Status().Update(context.TODO(), nIns.DeepCopy()); err != nil {
				if k8serrors.IsGone(err) {
					return
				}

				if res.RequeueAfter == time.Duration(0) {
					res.RequeueAfter = 1 * time.Second
					r.logger.Error(err, fmt.Sprintf("failed to update status, will retry after %s", res.RequeueAfter))
				}

				return
			}
		}

		if r.hooks.HasHooks(PostHookType, request.NamespacedName) {
			res.RequeueAfter = r.hookRequeueInterval
		}

		return
	}

	//if not post hook, quit the reconcile
	if !r.hooks.HasHooks(PostHookType, request.NamespacedName) {
		return
	}

	// nothing added to the incoming subscription, time to figure out the post hook
	//wait till the subscription is propagated
	f, err := r.IsSubscriptionCompleted(request.NamespacedName)
	if !f || err != nil {
		res.RequeueAfter = r.hookRequeueInterval
		return
	}

	// post hook will in a apply and don't report back manner
	if err != r.hooks.ApplyPostHooks(request.NamespacedName) {
		r.logger.Error(err, "failed to apply postHook, skip the subscription reconcile, err:")
	}

	nIns.Status.LastUpdateTime = metav1.Now()
	if err := r.Client.Status().Update(context.TODO(), nIns.DeepCopy()); err != nil {
		if k8serrors.IsGone(err) {
			return
		}

		if res.RequeueAfter == time.Duration(0) {
			res.RequeueAfter = 1 * time.Second
			r.logger.Error(err, fmt.Sprintf("failed to update status, will retry after %s", res.RequeueAfter))
		}
	}
}
