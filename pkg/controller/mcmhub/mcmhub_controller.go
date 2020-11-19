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
	"strings"
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
	reconcileName              = "subscription-hub-reconciler"
	defaultHookRequeueInterval = time.Second * 15
	INFOLevel                  = 1
	placementRuleFlag          = "--fired-by-placementrule"
)

var defaulRequeueInterval = time.Second * 3

// Add creates a new Subscription Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

//Option provide easy way to test the reconciler
type Option func(*ReconcileSubscription)

func resetHubGitOps(g GitOps) Option {
	return func(r *ReconcileSubscription) {
		r.hubGitOps = g
	}
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, op ...Option) reconcile.Reconciler {
	erecorder, _ := utils.NewEventRecorder(mgr.GetConfig(), mgr.GetScheme())
	logger := klogr.New().WithName(reconcileName)

	gitOps := NewHookGit(mgr.GetClient(), setHubGitOpsLogger(logger))

	rec := &ReconcileSubscription{
		name:   reconcileName,
		Client: mgr.GetClient(),
		// used for the helm to run get the resource list
		cfg:                 mgr.GetConfig(),
		scheme:              mgr.GetScheme(),
		eventRecorder:       erecorder,
		logger:              logger,
		hookRequeueInterval: defaultHookRequeueInterval,
		hooks:               NewAnsibleHooks(mgr.GetClient(), defaultHookRequeueInterval, setLogger(logger), setGitOps(gitOps)),
		hubGitOps:           gitOps,
	}

	for _, f := range op {
		f(rec)
	}

	rec.hooks.ResetGitOps(rec.hubGitOps)

	//this is used to start up the git watcher
	if err := mgr.Add(rec.hubGitOps); err != nil {
		logger.Error(err, "failed to start git watcher")
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
			// rolling to another one, skipping
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

			// appsub PlacementRef namespace could be empty, apply appsub namespace as the PlacementRef namespace
			if plRef.Namespace == "" {
				plRef.Namespace = sub.Namespace
			}

			if plRef.Name != obj.Meta.GetName() || plRef.Namespace != obj.Meta.GetNamespace() {
				continue
			}

			// in Reconcile(), removed the below suffix flag when processing the subscription
			subKey := types.NamespacedName{Name: sub.GetName() + placementRuleFlag + obj.Meta.GetResourceVersion(), Namespace: sub.GetNamespace()}

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
	name string
	client.Client
	logger              logr.Logger
	cfg                 *rest.Config
	scheme              *runtime.Scheme
	eventRecorder       *utils.EventRecorder
	hookRequeueInterval time.Duration
	hooks               HookProcessor
	hubGitOps           GitOps
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

		// the sub.Status.Message is aggregated over the doMCMHubReconcile
		if sub.Status.Message == "" {
			sub.Status.Message = hubdpl.Status.Message
		}

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
	logger.Info(fmt.Sprint("entry MCM Hub Reconciling subscription: ", request.String()))

	defer logger.Info(fmt.Sprint("exist Hub Reconciling subscription: ", request.String()))

	//flag used to determine if we skip the posthook
	passedPrehook := true

	//flag used to determine if the reconcile came from a placementrule decision change then force register
	placementDecisionUpdated := false
	placementRuleRv := ""

	if strings.Contains(request.Name, placementRuleFlag) {
		placementDecisionUpdated = true
		placementRuleRv = after(request.Name, placementRuleFlag)
		request.Name = strings.TrimSuffix(request.Name, placementRuleRv)
		request.Name = strings.TrimSuffix(request.Name, placementRuleFlag)
		request.NamespacedName = types.NamespacedName{Name: request.Name, Namespace: request.Namespace}
	}

	var preErr error

	instance := &appv1.Subscription{}
	oins := &appv1.Subscription{}

	defer func() {
		r.finalCommit(passedPrehook, preErr, oins, instance, request, &result)
	}()

	err := r.CreateSubscriptionAdminRBAC()
	if err != nil {
		logger.Error(err, "failed create subscriberitem admin RBAC")
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

			r.hubGitOps.DeregisterBranch(request.NamespacedName)

			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// for later comparison
	oins = instance.DeepCopy()

	// process as hub subscription, generate deployable to propagate
	pl := instance.Spec.Placement

	klog.V(2).Infof("Subscription: %v with placement %#v", request.NamespacedName.String(), pl)

	//status changes below show override the prehook status
	if pl == nil {
		instance.Status.Phase = appv1.SubscriptionPropagationFailed
		instance.Status.Reason = "Placement must be specified"
	} else if pl != nil && (pl.PlacementRef != nil || pl.Clusters != nil || pl.ClusterSelector != nil) {
		r.hubGitOps.RegisterBranch(instance)
		// register will skip the failed clone repo
		if err := r.hooks.RegisterSubscription(instance, placementDecisionUpdated, placementRuleRv); err != nil {
			logger.Error(err, "failed to register hooks, skip the subscription reconcile")
			preErr = fmt.Errorf("failed to register hooks, err: %v", err)

			passedPrehook = false

			return reconcile.Result{}, nil
		}

		if r.hooks.HasHooks(PreHookType, request.NamespacedName) {
			preErr = fmt.Errorf("prehook for %v is not ready ", request.String())

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
				_ = preErr

				r.overridePrehookTopoAnnotation(instance)

				if err != nil {
					logger.Error(err, "failed to check prehook status, skip the subscription reconcile")
					return reconcile.Result{}, nil
				}

				result.RequeueAfter = r.hookRequeueInterval
				passedPrehook = false

				return result, nil
			}
		}

		//changes will be added to instance
		err = r.doMCMHubReconcile(instance)

		if err != nil {
			r.logger.Error(err, "failed to process on doMCMHubReconcile")
			instance.Status.Phase = appv1.SubscriptionPropagationFailed
			instance.Status.Reason = err.Error()
			instance.Status.Statuses = nil
			returnErr = err
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
	} else { //local: true and handle change true to false
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

	//local mode
	if subIns.Spec.Placement.Local != nil && *(subIns.Spec.Placement.Local) {
		return true, nil
	}

	// need to wait for managed cluster reporting back
	// When placmentrule doesn't have target cluster decision list, managed clusters status is empty.
	// In this case, check the clusters list by checking the placementrule
	// If it's indeed empty cluster list then treat the subscription as completed.
	managedStatus := subIns.Status.Statuses
	if len(managedStatus) == 0 {
		clusters, err := GetClustersByPlacement(subIns, r.Client, r.logger)
		if err != nil {
			return false, err
		}

		return len(clusters) == 0, nil
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
	r.logger.Info("Enter finalCommit...")
	defer r.logger.Info("Eixt finalCommit...")
	// meaning the subscription is deleted
	if nIns.GetName() == "" || !oIns.GetDeletionTimestamp().IsZero() {
		r.logger.Info("instace is delete, don't run update logic")
		return
	}

	if utils.IsSubscriptionBasicChanged(oIns, nIns) { //if subresource enabled, the update client won't update the status
		if err := r.Client.Update(context.TODO(), nIns.DeepCopy(), &client.UpdateOptions{FieldManager: r.name}); err != nil {
			if res.RequeueAfter == time.Duration(0) {
				res.RequeueAfter = defaulRequeueInterval
				r.logger.Error(err, fmt.Sprintf("%s failed to update spec or metadata, will retry after %s", PrintHelper(nIns), res.RequeueAfter))
			}

			return
		}

		// due to the predict func, it's necessary to re-queue, since the status
		// change isn't committed yet
		if res.RequeueAfter == time.Duration(0) {
			res.RequeueAfter = defaulRequeueInterval
			r.logger.Info(fmt.Sprintf("%s on spec or annotation update success flow, will retry after %s", PrintHelper(nIns), res.RequeueAfter))
		}

		return
	}

	r.logger.Info(fmt.Sprintf("spec or metadata of %s is updated", PrintHelper(nIns)))
	//update status early to make sure the status is ready for post hook to
	//consume
	if !passedPrehook {
		nIns.Status = r.hooks.AppendPreHookStatusToSubscription(nIns)
		nIns.Status.Phase = appv1.SubscriptionPropagationFailed
		nIns.Status.Reason = preErr.Error()
		nIns.Status.Statuses = appv1.SubscriptionClusterStatusMap{}
	} else {
		nIns.Status = r.hooks.AppendStatusToSubscription(nIns)
	}

	if utils.IsHubRelatedStatusChanged(oIns.Status.DeepCopy(), nIns.Status.DeepCopy()) {
		nIns.Status.LastUpdateTime = metav1.Now()

		err := r.Client.Status().Patch(context.TODO(), nIns, client.MergeFrom(oIns), &client.PatchOptions{FieldManager: r.name})
		if err != nil && !k8serrors.IsNotFound(err) {
			// If it was a NotFound error, the object was probably already deleted so just ignore the error and return the existing result.
			if res.RequeueAfter == time.Duration(0) {
				res.RequeueAfter = defaulRequeueInterval
				r.logger.Error(err, fmt.Sprintf("failed to update status, will retry after %s", res.RequeueAfter))
			}

			return
		}

		if res.RequeueAfter == time.Duration(0) {
			res.RequeueAfter = defaulRequeueInterval
			r.logger.Info(fmt.Sprintf("only update status, will retry %s for possible posthook", res.RequeueAfter))
		}

		return
	}

	//if not post hook, quit the reconcile
	if !r.hooks.HasHooks(PostHookType, request.NamespacedName) {
		r.logger.Info("no post hooks, exit the reconcile.")
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

	nIns.Status = r.hooks.AppendStatusToSubscription(nIns)
	nIns.Status.LastUpdateTime = metav1.Now()

	err = r.Client.Status().Patch(context.TODO(), nIns, client.MergeFrom(oIns), &client.PatchOptions{FieldManager: r.name})
	if err != nil && !k8serrors.IsNotFound(err) {
		// If it was a NotFound error, the object was probably already deleted so just ignore the error and return the existing result.
		if res.RequeueAfter == time.Duration(0) {
			res.RequeueAfter = defaulRequeueInterval
			r.logger.Error(err, fmt.Sprintf("failed to update status, will retry after %s", res.RequeueAfter))
		}

		return
	}
}

func after(value string, a string) string {
	// Get substring after a string.
	pos := strings.LastIndex(value, a)
	if pos == -1 {
		return ""
	}

	adjustedPos := pos + len(a)

	if adjustedPos >= len(value) {
		return ""
	}

	return value[adjustedPos:]
}

func PrintHelper(o metav1.Object) types.NamespacedName { //nolint
	return types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}
}
