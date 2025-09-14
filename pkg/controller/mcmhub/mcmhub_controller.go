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

package mcmhub

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"k8s.io/klog/v2/klogr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"

	clusterapi "open-cluster-management.io/api/cluster/v1beta1"
	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/metrics"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
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
  - '*'
- apiGroups:
  - cluster.open-cluster-management.io
  - register.open-cluster-management.io
  - clusterview.open-cluster-management.io
  resources:
  - managedclustersets/join
  - managedclustersets/bind
  - managedclusters/accept
  - managedclustersets
  - managedclusters
  - managedclustersetbindings
  - placements
  - placementdecisions
  verbs:
  - get
  - list
  - watch
  - create
  - update
  - patch`

const (
	reconcileName                     = "subscription-hub-reconciler"
	defaultHookRequeueInterval        = time.Second * 30
	INFOLevel                         = 1
	placementDecisionFlag             = "--fired-by-placementdecision"
	subscriptionActive         string = "Active"
	subscriptionBlock          string = "Blocked"
)

var defaulRequeueInterval = time.Second * 15

// Add creates a new Subscription Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// Option provide easy way to test the reconciler
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
		restMapper:          mgr.GetRESTMapper(),
		eventRecorder:       erecorder,
		logger:              logger,
		hookRequeueInterval: defaultHookRequeueInterval,
		hooks:               NewAnsibleHooks(mgr.GetClient(), defaultHookRequeueInterval, setLogger(logger), setGitOps(gitOps)),
		hubGitOps:           gitOps,
		clk:                 time.Now,
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

func (mapper *subscriptionMapper) Map(ctx context.Context, obj *appv1.Subscription) []reconcile.Request {
	klog.Info("Entering subscription mapper")
	defer klog.Info("Exiting  subscription mapper")

	// rolling target subscription changed, need to update the source subscription
	var requests []reconcile.Request

	// enqueue itself
	requests = append(requests,
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			},
		},
	)

	// list thing for rolling update check
	subList := &appv1.SubscriptionList{}
	listopts := &client.ListOptions{Namespace: obj.GetNamespace()}
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

		if annotations[appv1.AnnotationRollingUpdateTarget] != obj.GetName() {
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
	hdplkey := utils.GetHostSubscriptionFromObject(obj)
	if hdplkey != nil && hdplkey.Name != "" {
		requests = append(requests, reconcile.Request{NamespacedName: *hdplkey})
	}

	klog.V(1).Info("Out subscription mapper with requests:", requests)

	return requests
}

type channelMapper struct {
	client.Client
}

func (mapper *channelMapper) Map(ctx context.Context, obj *chnv1.Channel) []reconcile.Request {
	klog.Info("Entering channel mapper")
	defer klog.Info("Exiting channel mapper")

	// if channel is created/updated/deleted, its relative subscriptions should be reconciled.

	chn := obj.GetNamespace() + "/" + obj.GetName()

	var requests []reconcile.Request

	subList := &appv1.SubscriptionList{}
	listopts := &client.ListOptions{}
	err := mapper.List(context.TODO(), subList, listopts)

	if err != nil {
		klog.Error("Listing all subscriptions in channelMapper and got error:", err)
	}

	for _, sub := range subList.Items {
		if sub.Spec.Channel == chn || sub.Spec.SecondaryChannel == chn {
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

type placementDecisionMapper struct {
	client.Client
}

func (mapper *placementDecisionMapper) Map(ctx context.Context, obj *clusterapi.PlacementDecision) []reconcile.Request {
	klog.Info("Entering placementdecision mapper")
	defer klog.Info("Exiting placementdecision mapper")

	// if placementdecision is created/updated/deleted, its relative subscriptions should be reconciled.

	var requests []reconcile.Request

	subList := &appv1.SubscriptionList{}
	listopts := &client.ListOptions{}
	err := mapper.List(context.TODO(), subList, listopts)

	if err != nil {
		klog.Error("Listing all subscriptions in placementDecisionMapper and got error:", err)
	}

	// get the placement name from the placementdecision
	placementName := obj.GetLabels()[placementLabel]
	if placementName == "" {
		placementName = obj.GetLabels()[placementRuleLabel]

		if placementName == "" {
			return nil
		}
	}

	for _, sub := range subList.Items {
		if sub.Spec.Placement != nil && sub.Spec.Placement.PlacementRef != nil {
			plRef := sub.Spec.Placement.PlacementRef

			// appsub PlacementRef namespace could be empty, apply appsub namespace as the PlacementRef namespace
			if plRef.Namespace == "" {
				plRef.Namespace = sub.Namespace
			}

			if plRef.Name != placementName || plRef.Namespace != obj.GetNamespace() {
				continue
			}

			// If there is no cluster in placement decision, no reconcile.
			placementDecision := &clusterapi.PlacementDecision{}
			err := mapper.Get(context.TODO(), types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, placementDecision)

			if err != nil && !k8serrors.IsNotFound(err) {
				klog.Error("failed to get placementdecision error:", err)

				continue
			}

			// in Reconcile(), removed the below suffix flag when processing the subscription
			subKey := types.NamespacedName{Name: sub.GetName() + placementDecisionFlag + obj.GetResourceVersion(), Namespace: sub.GetNamespace()}

			requests = append(requests, reconcile.Request{NamespacedName: subKey})
		}
	}

	klog.V(1).Info("Out placement mapper with requests:", requests)

	return requests
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	skipValidation := true
	c, err := controller.New("mcmhub-subscription-controller", mgr, controller.Options{
		Reconciler:         r,
		SkipNameValidation: &skipValidation,
	})

	if err != nil {
		return err
	}

	// Watch for changes to primary resource Subscription
	smapper := &subscriptionMapper{mgr.GetClient()}
	err = c.Watch(
		source.Kind(mgr.GetCache(),
			&appv1.Subscription{},
			handler.TypedEnqueueRequestsFromMapFunc(smapper.Map),
			utils.SubscriptionPredicateFunctions,
		),
	)

	if err != nil {
		return err
	}

	// in hub, watch for channel changes
	cMapper := &channelMapper{mgr.GetClient()}
	err = c.Watch(
		source.Kind(mgr.GetCache(),
			&chnv1.Channel{},
			handler.TypedEnqueueRequestsFromMapFunc(cMapper.Map),
			utils.ChannelPredicateFunctions,
		),
	)

	if err != nil {
		return err
	}

	// in hub, watch for placement decision changes
	if utils.IsReadyPlacementDecision(mgr.GetAPIReader()) {
		pdMapper := &placementDecisionMapper{mgr.GetClient()}
		err = c.Watch(
			source.Kind(mgr.GetCache(),
				&clusterapi.PlacementDecision{},
				handler.TypedEnqueueRequestsFromMapFunc(pdMapper.Map),
				utils.PlacementDecisionPredicateFunctions,
			),
		)

		if err != nil {
			return err
		}
	}

	return nil
}

type clock func() time.Time

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
	restMapper          meta.RESTMapper
	clk                 clock
}

// CreateSubscriptionAdminRBAC checks existence of subscription-admin clusterrole and clusterrolebinding
// and creates them if not found
func CreateSubscriptionAdminRBAC(r client.Client) error {
	tries := 1
	for tries < 4 {
		// Create subscription admin ClusteRole
		clusterRole := subAdminClusterRole()
		foundClusterRole := &rbacv1.ClusterRole{}

		if err := r.Get(context.TODO(), types.NamespacedName{Name: clusterRole.Name}, foundClusterRole); err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Infof("ClusterRole %s not found. Creating it.", clusterRole.Name)
				err = r.Create(context.TODO(), clusterRole)

				if err != nil {
					klog.Error("failed to create the open-cluster-management:subscription-admin clusterRole: error:", err)
					return err
				}
			} else {
				klog.Error("error:", err)
				return err
			}
		} else {
			if !equality.Semantic.DeepEqual(clusterRole.Rules, foundClusterRole.Rules) {
				foundClusterRole.Rules = clusterRole.Rules
				err = r.Update(context.TODO(), foundClusterRole, &client.UpdateOptions{})

				if err != nil {
					klog.Error("failed to update the open-cluster-management:subscription-admin clusterRole: error:", err)
					return err
				}
			}

			klog.Infof("ClusterRole %s exists.", clusterRole.Name)

			break
		}

		time.Sleep(5 * time.Second)

		tries++
	}

	tries = 1
	for tries < 4 {
		// Create subscription admin ClusteRoleBinding
		clusterRoleBinding := subAdminClusterRoleBinding()
		foundClusterRoleBinding := &rbacv1.ClusterRoleBinding{}

		if err := r.Get(context.TODO(), types.NamespacedName{Name: clusterRoleBinding.Name}, foundClusterRoleBinding); err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Infof("ClusterRoleBiding %s not found. Creating it.", clusterRoleBinding.Name)
				err = r.Create(context.TODO(), clusterRoleBinding)

				if err != nil {
					klog.Error("error:", err)
					return err
				}
			} else {
				klog.Error("error:", err)
				return err
			}
		} else {
			klog.Infof("ClusterRoleBiding %s exists.", clusterRoleBinding.Name)
			break
		}

		time.Sleep(5 * time.Second)

		tries++
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

// Reconcile reads that state of the cluster for a Subscription object and makes changes based on the state read
// and what is in the Subscription.Spec
func (r *ReconcileSubscription) Reconcile(ctx context.Context, request reconcile.Request) (result reconcile.Result, returnErr error) {
	logger := r.logger.WithName(request.String())
	logger.Info(fmt.Sprint("entry MCM Hub Reconciling subscription: ", request.String()))

	defer logger.Info(fmt.Sprint("exit Hub Reconciling subscription: ", request.String()))

	//flag used to indicate Git branch connection intialiazion failed
	passedBranchRegistration := true

	//flag used to determine if we skip the posthook
	passedPrehook := true

	//flag used to determine if the reconcile came from a placement decision change then force register
	placementDecisionUpdated := false
	placementDecisionRv := ""

	if strings.Contains(request.Name, placementDecisionFlag) {
		placementDecisionUpdated = true
		placementDecisionRv = after(request.Name, placementDecisionFlag)
		request.Name = strings.TrimSuffix(request.Name, placementDecisionRv)
		request.Name = strings.TrimSuffix(request.Name, placementDecisionFlag)
		request.NamespacedName = types.NamespacedName{Name: request.Name, Namespace: request.Namespace}
	}

	var preErr error

	localPlacement := false

	instance := &appv1.Subscription{}
	oins := &appv1.Subscription{}

	defer func() {
		r.finalCommit(passedBranchRegistration, passedPrehook, preErr, oins, instance, request, &result, localPlacement)
	}()

	err := r.Get(context.TODO(), request.NamespacedName, instance)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Info("Subscription: ", request.NamespacedName, " is gone")
			klog.Infof("Clean up all the manifestWorks owned by appsub: %v", request.NamespacedName)

			cleanupErr := r.cleanupManifestWork(request.NamespacedName)
			if cleanupErr != nil {
				klog.Warning("error while cleanup manifestwork ", cleanupErr)
			}

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

	klog.Infof("Subscription: %v with placement %#v", request.NamespacedName.String(), pl)

	//status changes below show override the prehook status
	if pl == nil {
		instance.Status.Phase = appv1.SubscriptionPropagationFailed
		instance.Status.Reason = "Placement must be specified"

		metrics.PropagationFailedPullTime.
			WithLabelValues(instance.Namespace, instance.Name).
			Observe(0)
	} else if pl != nil && (pl.PlacementRef != nil || pl.Clusters != nil || pl.ClusterSelector != nil) && (pl.Local != nil && *pl.Local) {
		logger.Info("both local placement and remote placement are defined in the subscription")

		instance.Status.Phase = appv1.SubscriptionPropagationFailed
		instance.Status.Reason = "local placement and remote placement cannot be used together"

		metrics.PropagationFailedPullTime.
			WithLabelValues(instance.Namespace, instance.Name).
			Observe(0)
	} else if pl != nil && (pl.PlacementRef != nil || pl.Clusters != nil || pl.ClusterSelector != nil) {
		primaryChannel, _, err := r.getChannel(instance)
		if err != nil {
			klog.Errorf("Failed to find a channel for subscription: %s", instance.GetName())

			instance.Status.Phase = appv1.SubscriptionPropagationFailed
			instance.Status.Reason = fmt.Sprintf("Failed to find channel: %s", err.Error())

			metrics.PropagationFailedPullTime.
				WithLabelValues(instance.Namespace, instance.Name).
				Observe(0)

			return reconcile.Result{}, nil
		}
		// This block is only for Git subscription
		if strings.EqualFold(string(primaryChannel.Spec.Type), chnv1.ChannelTypeGit) ||
			strings.EqualFold(string(primaryChannel.Spec.Type), chnv1.ChannelTypeGitHub) {
			if err := r.hubGitOps.RegisterBranch(instance); err != nil {
				logger.Error(err, "failed to initialize Git connection")
				preErr = fmt.Errorf("failed to initialize Git connection, err: %w", err)
				passedBranchRegistration = false

				metrics.PropagationFailedPullTime.
					WithLabelValues(instance.Namespace, instance.Name).
					Observe(0)

				return reconcile.Result{}, nil
			}

			// register will skip the failed clone repo
			if err := r.hooks.RegisterSubscription(instance, placementDecisionUpdated, placementDecisionRv); err != nil {
				logger.Error(err, "failed to register hooks, skip the subscription reconcile")
				preErr = fmt.Errorf("failed to register hooks, err: %w", err)
				passedPrehook = false

				metrics.PropagationFailedPullTime.
					WithLabelValues(instance.Namespace, instance.Name).
					Observe(0)

				return reconcile.Result{}, nil
			}

			if r.hooks.HasHooks(PreHookType, request.NamespacedName) {
				preErr = fmt.Errorf("prehook for %v is not ready ", request.String())

				//if it's registered
				if err := r.hooks.ApplyPreHooks(request.NamespacedName); err != nil {
					logger.Error(err, "failed to apply preHook, skip the subscription reconcile")

					passedPrehook = false

					metrics.PropagationFailedPullTime.
						WithLabelValues(instance.Namespace, instance.Name).
						Observe(0)

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
						metrics.PropagationFailedPullTime.
							WithLabelValues(instance.Namespace, instance.Name).
							Observe(0)

						return reconcile.Result{}, nil
					}

					result.RequeueAfter = r.hookRequeueInterval
					passedPrehook = false

					metrics.PropagationFailedPullTime.
						WithLabelValues(instance.Namespace, instance.Name).
						Observe(0)

					klog.Infof("prehooks not complete, appsub: %v, err: %v", request.NamespacedName.String(), err)

					return result, nil
				}

				klog.Infof("prehooks complete, appsub: %v", request.NamespacedName.String())

				instance.Status.Phase = appv1.PreHookSucessful
				instance.Status.Reason = ""
				instance.Status.LastUpdateTime = metav1.Now()
				instance.Status.Statuses = appv1.SubscriptionClusterStatusMap{}
			}
		}

		//changes will be added to instance
		startTime := time.Now().UnixMilli()
		err = r.doMCMHubReconcile(instance)
		endTime := time.Now().UnixMilli()

		if err != nil {
			r.logger.Error(err, "failed to process on doMCMHubReconcile")
			metrics.PropagationFailedPullTime.
				WithLabelValues(instance.Namespace, instance.Name).
				Observe(float64(endTime - startTime))

			instance.Status.Phase = appv1.SubscriptionPropagationFailed
			instance.Status.Reason = err.Error()
			instance.Status.Statuses = nil
			preErr = err
			returnErr = err
		} else {
			metrics.PropagationSuccessfulPullTime.
				WithLabelValues(instance.Namespace, instance.Name).
				Observe(float64(endTime - startTime))
			// Clear prev reconcile errors
			instance.Status.Phase = appv1.SubscriptionPropagated
			instance.Status.Message = ""
			instance.Status.Reason = ""
		}
	} else { //local: true and handle change true to false
		// no longer hub subscription
		localPlacement = true

		if !utils.IsHostingAppsub(instance) {
			klog.Infof("Clean up all the manifestWorks owned by appsub: %v/%v", instance.GetNamespace(), instance.GetName())

			cleanupErr := r.cleanupManifestWork(types.NamespacedName{
				Namespace: instance.Namespace,
				Name:      instance.Name,
			})
			if cleanupErr != nil {
				klog.Warning("error while cleanup manifestwork ", cleanupErr)
			}
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

		metrics.PropagationSuccessfulPullTime.
			WithLabelValues(instance.Namespace, instance.Name).
			Observe(0)
	}

	return result, nil
}

// IsSubscriptionCompleted will check:
// a, if the subscription itself is processed
// b, for each of the subscription created on managed cluster, it will check if
// it is 1, propagated and 2, subscribed
func (r *ReconcileSubscription) IsSubscriptionCompleted(subKey types.NamespacedName) (bool, error) {
	subIns := &appv1.Subscription{}
	if err := r.Get(context.TODO(), subKey, subIns); err != nil {
		if k8serrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	}

	subFailSet := map[appv1.SubscriptionPhase]struct{}{
		appv1.SubscriptionPropagationFailed: {},
		appv1.SubscriptionFailed:            {},
		appv1.SubscriptionUnknown:           {},
	}
	//check up the hub cluster status
	if _, ok := subFailSet[subIns.Status.Phase]; ok {
		return false, nil
	}

	//local mode
	if subIns.Spec.Placement.Local != nil && *(subIns.Spec.Placement.Local) {
		return true, nil
	}

	appsubReport := &appSubStatusV1alpha1.SubscriptionReport{}
	if err := r.Get(context.TODO(), subKey, appsubReport); err != nil {
		return false, err
	}

	// if there are no cluster matches or
	// not all subscriptions are deployed/inprogress in matching clusters
	if appsubReport.Summary.Clusters == "0" {
		return false, nil
	}

	numClusters, err := strconv.Atoi(appsubReport.Summary.Clusters)
	if err != nil {
		return false, err
	}

	numDeployed, err := strconv.Atoi(appsubReport.Summary.Deployed)
	if err != nil {
		return false, err
	}

	numInProgress, err := strconv.Atoi(appsubReport.Summary.InProgress)
	if err != nil {
		return false, err
	}

	if (numDeployed + numInProgress) != numClusters {
		return false, nil
	}

	if numInProgress > 0 {
		return false, nil
	}

	return true, nil
}

// finalCommit will shortcut the prehook logic if the prehook present
// if the prohook is completed, then update subscription will be update(1, the
// main spec, 2, status update)
// if the prehook, subscription is update then entry the posthook logic
//
// the requeue logic is done via set up the RequeueAfter parameter of the
// reconciel.Result
func (r *ReconcileSubscription) finalCommit(passedBranchRegistration bool, passedPrehook bool, preErr error,
	oIns, nIns *appv1.Subscription,
	request reconcile.Request, res *reconcile.Result, localPlacement bool) {
	r.logger.Info(fmt.Sprintf("Entering finalCommit, passedBranchRegistration:%v, passedPrehook:%v", passedBranchRegistration, passedPrehook))
	defer r.logger.Info("Exit finalCommit...")

	if localPlacement {
		r.logger.Info(fmt.Sprintf("skip finalCommit for local subscription, appsub: %v/%v", nIns.Namespace, nIns.Name))

		res.RequeueAfter = time.Duration(0)

		return
	}
	// meaning the subscription is deleted
	if nIns.GetName() == "" || !oIns.GetDeletionTimestamp().IsZero() {
		r.logger.Info("instace is delete, don't run update logic")
		return
	}

	// time window calculation
	if nIns.Spec.TimeWindow == nil {
		nIns.Status.Message = subscriptionActive
	} else {
		if utils.IsInWindow(nIns.Spec.TimeWindow, r.clk()) {
			nIns.Status.Message = subscriptionActive
		} else {
			nIns.Status.Message = subscriptionBlock
		}
	}

	if !passedBranchRegistration {
		nIns.Status = r.hooks.AppendPreHookStatusToSubscription(nIns)
		nIns.Status.Phase = appv1.SubscriptionPropagationFailed
		nIns.Status.Reason = preErr.Error()
		nIns.Status.Statuses = appv1.SubscriptionClusterStatusMap{}

		if utils.IsHubRelatedStatusChanged(oIns.Status.DeepCopy(), nIns.Status.DeepCopy()) {
			nIns.Status.LastUpdateTime = metav1.Now()

			err := r.Client.Status().Patch(context.TODO(), nIns, client.MergeFrom(oIns),
				&client.SubResourcePatchOptions{PatchOptions: client.PatchOptions{FieldManager: r.name}})

			if err != nil && !k8serrors.IsNotFound(err) {
				// If it was a NotFound error, the object was probably already deleted so just ignore the error and return the existing result.
				if res.RequeueAfter == time.Duration(0) {
					res.RequeueAfter = defaulRequeueInterval
					r.logger.Error(err, fmt.Sprintf("failed to update status with channel registration error, will retry after %s", res.RequeueAfter))
				}

				return
			}
		}

		r.logger.Info(fmt.Sprintf("Failed to register the channel, %s status updated", PrintHelper(nIns)))

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

	r.logger.Info(fmt.Sprintf("spec or metadata of %s is updated, passedPrehook: %v", PrintHelper(nIns), passedPrehook))
	//update status early to make sure the status is ready for post hook to	consume
	if !passedPrehook {
		nIns.Status = r.hooks.AppendPreHookStatusToSubscription(nIns)
		nIns.Status.Phase = appv1.SubscriptionPropagationFailed
		nIns.Status.Reason = preErr.Error()
		nIns.Status.Statuses = appv1.SubscriptionClusterStatusMap{}
	} else {
		nIns.Status = r.hooks.AppendStatusToSubscription(nIns)
	}

	klog.Infof("oIns status reason: %v", oIns.Status.Reason)
	klog.Infof("nIns status reason: %v", nIns.Status.Reason)

	if utils.IsHubRelatedStatusChanged(oIns.Status.DeepCopy(), nIns.Status.DeepCopy()) {
		nIns.Status.LastUpdateTime = metav1.Now()

		err := r.Client.Status().Patch(context.TODO(), nIns, client.MergeFrom(oIns),
			&client.SubResourcePatchOptions{PatchOptions: client.PatchOptions{FieldManager: r.name}})
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
			r.logger.Info(fmt.Sprintf("appsub status updated, will retry %v for possible posthooks. appsub: %v", res.RequeueAfter, PrintHelper(nIns)))
		}

		return
	}

	if !passedPrehook {
		res.RequeueAfter = r.hookRequeueInterval

		r.logger.Info(fmt.Sprintf("prehooks not complete yet. appsub: %v", PrintHelper(nIns)))

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
		r.logger.Info(fmt.Sprintf("appsub not complete yet, appsub: %v", request.NamespacedName))
		res.RequeueAfter = r.hookRequeueInterval

		return
	}

	// post hook will in a apply and don't report back manner
	if !errors.Is(err, r.hooks.ApplyPostHooks(request.NamespacedName)) {
		r.logger.Error(err, "failed to apply postHook, skip the subscription reconcile, err:")
	}

	nIns.Status = r.hooks.AppendStatusToSubscription(nIns)

	if utils.IsHubRelatedStatusChanged(oIns.Status.DeepCopy(), nIns.Status.DeepCopy()) {
		nIns.Status.LastUpdateTime = metav1.Now()

		err = r.Client.Status().Patch(context.TODO(), nIns, client.MergeFrom(oIns),
			&client.SubResourcePatchOptions{PatchOptions: client.PatchOptions{FieldManager: r.name}})
		if err != nil && !k8serrors.IsNotFound(err) {
			// If it was a NotFound error, the object was probably already deleted so just ignore the error and return the existing result.
			if res.RequeueAfter == time.Duration(0) {
				res.RequeueAfter = defaulRequeueInterval
				r.logger.Error(err, fmt.Sprintf("failed to update status, will retry after %s", res.RequeueAfter))
			}

			return
		}
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
