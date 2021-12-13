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

package placementrule

import (
	"context"

	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/placementrule/utils"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new PlacementRule Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	authCfg := mgr.GetConfig()
	authCfg.QPS = 100.0
	authCfg.Burst = 200
	kubeClient := kubernetes.NewForConfigOrDie(authCfg)

	return &ReconcilePlacementRule{Client: mgr.GetClient(), scheme: mgr.GetScheme(), authClient: kubeClient}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Enable the concurrent reconcile in case some placementrule could take longer to generate cluster decisions
	c, err := controller.New("placementrule-controller", mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 10,
	})
	if err != nil {
		return err
	}

	// Watch for changes to PlacementRule
	err = c.Watch(&source.Kind{Type: &appv1alpha1.PlacementRule{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	if utils.IsReadyACMClusterRegistry(mgr.GetAPIReader()) {
		cpMapper := &ClusterPlacementRuleMapper{mgr.GetClient()}
		err = c.Watch(
			&source.Kind{Type: &spokeClusterV1.ManagedCluster{}},
			handler.EnqueueRequestsFromMapFunc(cpMapper.Map),
			utils.ClusterPredicateFunc,
		)

		if err != nil {
			return err
		}
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcilePlacementRule{}

// ReconcilePlacementRule reconciles a PlacementRule object
type ReconcilePlacementRule struct {
	client.Client
	authClient kubernetes.Interface
	scheme     *runtime.Scheme
}

// ClusterPlacementRuleMapper is defined for PlacementRule to watch clusters
type ClusterPlacementRuleMapper struct {
	client.Client
}

// Map triggers all placements.
func (mapper *ClusterPlacementRuleMapper) Map(obj client.Object) []reconcile.Request {
	plList := &appv1alpha1.PlacementRuleList{}

	listopts := &client.ListOptions{}
	err := mapper.List(context.TODO(), plList, listopts)

	if err != nil {
		klog.Error("Failed to list placement rules in mapper with err:", err)
	}

	var requests []reconcile.Request

	for _, pl := range plList.Items {
		objkey := types.NamespacedName{
			Name:      pl.GetName(),
			Namespace: pl.GetNamespace(),
		}

		requests = append(requests, reconcile.Request{NamespacedName: objkey})
	}

	klog.Infof("Those placementRules are triggered due to managed Cluster status change: %v", requests)

	return requests
}

// PolicyPlacementRuleMapper is defined for PlacementRule to watch policies
type PolicyPlacementRuleMapper struct {
	client.Client
}

// Map triggers all placements
func (mapper *PolicyPlacementRuleMapper) Map(obj client.Object) []reconcile.Request {
	cname := obj.GetName()

	klog.Info("In policy Mapper for ", cname)

	plList := &appv1alpha1.PlacementRuleList{}

	listopts := &client.ListOptions{}
	err := mapper.List(context.TODO(), plList, listopts)

	if err != nil {
		klog.Error("Failed to list placement rules in mapper with err:", err)
	}

	var requests []reconcile.Request

	for _, pl := range plList.Items {
		objkey := types.NamespacedName{
			Name:      pl.GetName(),
			Namespace: pl.GetNamespace(),
		}

		requests = append(requests, reconcile.Request{NamespacedName: objkey})
	}

	return requests
}

// Reconcile reads that state of the cluster for a PlacementRule object and makes changes based on the state read
// and what is in the PlacementRule.Spec
// a Deployment as an example
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=multicloud-apps.io,resources=placementrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multicloud.io,resources=placementrules/status,verbs=get;update;patch
func (r *ReconcilePlacementRule) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Fetch the PlacementRule instance
	instance := &appv1alpha1.PlacementRule{}
	err := r.Get(ctx, request.NamespacedName, instance)

	klog.Info("Reconciling:", request.NamespacedName, " with Get err:", err)

	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	orgclmap := make(map[string]string)
	for _, cl := range instance.Status.Decisions {
		orgclmap[cl.ClusterName] = cl.ClusterNamespace
	}

	// do nothing if not using mcm as scheduler (user set it to something else)
	scname := instance.Spec.SchedulerName
	if scname != "" && scname != appv1alpha1.SchedulerNameDefault && scname != appv1alpha1.SchedulerNameMCM {
		return reconcile.Result{}, nil
	}

	err = r.hubReconcile(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	updated := false

	for _, cl := range instance.Status.Decisions {
		ns, ok := orgclmap[cl.ClusterName]
		if !ok || ns != cl.ClusterNamespace {
			updated = true
			break
		}

		delete(orgclmap, cl.ClusterName)
	}

	if !updated && len(orgclmap) > 0 {
		updated = true
	}

	// reconcile finished check if need to upadte the resource
	if updated {
		klog.Info("Update placementrule ", instance.Name, " with decisions: ", instance.Status.Decisions)
		err = r.Status().Update(context.TODO(), instance)

		klog.V(1).Info("Status update", request.NamespacedName, " with err:", err)
	}

	err = r.syncPlacementDecisions(ctx, instance.Namespace, instance.Name)
	if err != nil {
		klog.Warning("err:", err)
	}

	klog.V(1).Info("Reconciling - finished.", request.NamespacedName)

	return reconcile.Result{}, nil
}

func (r *ReconcilePlacementRule) UpdateStatus(request reconcile.Request, instance *appv1alpha1.PlacementRule) error {
	err := r.Status().Update(context.TODO(), instance)

	if err != nil {
		klog.Error("Error returned when updating placementrule decisions:", err, " ,instance:", instance)
	}

	return err
}
