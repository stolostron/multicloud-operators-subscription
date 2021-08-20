/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package appsubstatus

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	appsubv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	appSubStatusV1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	subutils "github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// ReconcileAppSubStatus reconciles a AppSubStatus object.
type ReconcileAppSubStatus struct {
	client.Client
	authClient kubernetes.Interface
	scheme     *runtime.Scheme
	lock       sync.Mutex
}

// Add creates a new argocd cluster Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

var _ reconcile.Reconciler = &ReconcileAppSubStatus{}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	authCfg := mgr.GetConfig()
	klog.Infof("Host: %v, BearerToken: %v", authCfg.Host, authCfg.BearerToken)
	kubeClient := kubernetes.NewForConfigOrDie(authCfg)

	dsRS := &ReconcileAppSubStatus{
		Client:     mgr.GetClient(),
		scheme:     mgr.GetScheme(),
		authClient: kubeClient,
		lock:       sync.Mutex{},
	}

	return dsRS
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	klog.Infof("Add appsubstatus-controller to mgr")

	// Create a new controller
	c, err := controller.New("appsubstatus-controller", mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 10,
	})
	if err != nil {
		return err
	}

	// Watch appsubpackagestatus changes
	err = c.Watch(
		&source.Kind{Type: &appSubStatusV1alpha1.SubscriptionPackageStatus{}},
		&handler.EnqueueRequestForObject{},
		subutils.AppSubPackageStatusPredicateFunc)

	if err != nil {
		return err
	}

	return nil
}

func (r *ReconcileAppSubStatus) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	instance := &appSubStatusV1alpha1.SubscriptionPackageStatus{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)

	klog.Info("Reconciling:", request.NamespacedName, " with Get err:", err)

	subNs, subName := subutils.GetHostSubscriptionNSFromObject(request.NamespacedName.Name)
	//clusterName := request.NamespacedName.Namespace

	// create or update summary appsubstatus object in the appsub NS
	err = r.generateAppSubSummary(subNs, subName, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileAppSubStatus) generateAppSubSummary(subNs, subName string,
	newAppSubStatus *appSubStatusV1alpha1.SubscriptionPackageStatus) error {
	klog.Infof("generating appsubsummarystatus summary for appsub %v/%v", subNs, subName)

	managedSubPackageStatusList := &appSubStatusV1alpha1.SubscriptionPackageStatusList{}
	listopts := &client.ListOptions{}

	managedSubStatusSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", subNs+"."+subName),
		},
	}

	managedSubStatusLabels, err := subutils.ConvertLabels(managedSubStatusSelector)
	if err != nil {
		klog.Errorf("Failed to convert managed appsubstatus label selector, err:%v", err)

		return err
	}

	listopts.LabelSelector = managedSubStatusLabels
	err = r.List(context.TODO(), managedSubPackageStatusList, listopts)

	if err != nil {
		klog.Errorf("Failed to list managed appsubpackagestatus, err:%v", err)

		return err
	}

	if len(managedSubPackageStatusList.Items) == 0 {
		klog.Infof("No managed appsubpackagestatus with labels %v found", managedSubStatusSelector)

		// no appsubpackagestatus in managed cluster NS - appsubsummary status can be deleted
		appNsHubSubSummaryStatus := &appSubStatusV1alpha1.SubscriptionSummaryStatus{}
		appNsHubSubSummaryStatusKey := types.NamespacedName{
			Name:      subName,
			Namespace: subNs,
		}

		if err := r.Get(context.TODO(), appNsHubSubSummaryStatusKey, appNsHubSubSummaryStatus); err == nil {
			err = r.Delete(context.TODO(), appNsHubSubSummaryStatus)
			if err != nil {
				klog.Infof("Removed appsumsummary status %s/%s", subNs, subName)
			}
		}

		return err
	}

	deployedClusters := []string{}
	failedDeployClusters := []string{}
	failedPropagationClusters := []string{}

	for _, managedSubPackageStatus := range managedSubPackageStatusList.Items {
		// 1. if managedSubStatus is the new updated appSubstatus, apply the new one in the summary aggregation
		// cached client may not be able to get the latest update
		if newAppSubStatus != nil && managedSubPackageStatus.Name == newAppSubStatus.Name &&
			managedSubPackageStatus.Namespace == newAppSubStatus.Namespace {
			managedSubPackageStatus = *newAppSubStatus.DeepCopy()
		}

		// Get cluster name from hosting label
		lbls := managedSubPackageStatus.GetLabels()

		clusterName := lbls["apps.open-cluster-management.io/cluster"]
		if clusterName == "" {
			clusterName = managedSubPackageStatus.Namespace
		}

		failed := false
		for _, pkgStatus := range managedSubPackageStatus.Statuses.SubscriptionPackageStatus {
			if pkgStatus.Phase == v1alpha1.PackagePropagationFailed {
				// For propagation failed, all packages must have this phase
				failedPropagationClusters = append(failedPropagationClusters, clusterName)
				failed = true

				break
			} else if pkgStatus.Phase == v1alpha1.PackageDeployFailed {
				failedDeployClusters = append(failedDeployClusters, clusterName)
				failed = true

				break
			}
		}

		if !failed {
			deployedClusters = append(deployedClusters, clusterName)
		}
	}

	err = r.createOrUpdateAppNsHubSubSummaryStatus(subNs, subName, deployedClusters, failedDeployClusters, failedPropagationClusters)

	klog.V(1).Infof("appsubsummarystatus summary for appsub %v/%v. Deployed: %v/%v; DeployFailed: %v/%v; PropadationFailed: %v:%v; err: %v ",
		subNs, subName, len(deployedClusters), deployedClusters, len(failedDeployClusters), failedDeployClusters,
		len(failedPropagationClusters), failedPropagationClusters, err)

	return err
}

func (r *ReconcileAppSubStatus) createOrUpdateAppNsHubSubSummaryStatus(subNs, subName string,
	deployedClusters, failedClusters, propgationFailedClusters []string) error {
	appNsHubSubSummaryStatus := &appSubStatusV1alpha1.SubscriptionSummaryStatus{}
	appNsHubSubSummaryStatusKey := types.NamespacedName{
		Name:      subName,
		Namespace: subNs,
	}

	if err := r.Get(context.TODO(), appNsHubSubSummaryStatusKey, appNsHubSubSummaryStatus); err != nil {
		if errors.IsNotFound(err) {
			appNsHubSubSummaryStatus = r.newAppNsHubSubSummaryStatus(subNs, subName, deployedClusters, failedClusters, propgationFailedClusters)

			if err := r.Create(context.TODO(), appNsHubSubSummaryStatus); err != nil {
				klog.Errorf("Failed to create appNsHubSubSummaryStatus err: %v", err)

				return err
			}

			klog.Infof("new appNsHubSubSummaryStatus created, %s/%s",
				appNsHubSubSummaryStatus.GetNamespace(), appNsHubSubSummaryStatus.GetName())

			return nil
		}
	}

	origAppNsHubSubSummaryStatus := appNsHubSubSummaryStatus.DeepCopy()

	r.addAppNsHubSubSummaryStatus(appNsHubSubSummaryStatus, subNs, subName, deployedClusters, failedClusters, propgationFailedClusters)

	if !reflect.DeepEqual(origAppNsHubSubSummaryStatus.Summary, appNsHubSubSummaryStatus.Summary) {
		if err := r.Update(context.TODO(), appNsHubSubSummaryStatus); err != nil {
			klog.Errorf("Failed to update appNsHubSubSummaryStatus err: %v", err)

			return err
		}

		klog.Infof("appNSHub appSubSummaryStatus updated, %s/%s",
			appNsHubSubSummaryStatus.GetNamespace(), appNsHubSubSummaryStatus.GetName())
	}

	return nil
}

func (r *ReconcileAppSubStatus) newAppNsHubSubSummaryStatus(subNs, subName string,
	deployedClusters, failedDeployClusters, failedPropagationClusters []string) *appSubStatusV1alpha1.SubscriptionSummaryStatus {
	appNsHubSubSummaryStatus := &appSubStatusV1alpha1.SubscriptionSummaryStatus{}

	appNsHubSubSummaryStatus.SetName(subName)
	appNsHubSubSummaryStatus.SetNamespace(subNs)

	r.addAppNsHubSubSummaryStatus(appNsHubSubSummaryStatus, subNs, subName, deployedClusters, failedDeployClusters, failedPropagationClusters)

	return appNsHubSubSummaryStatus
}

func (r *ReconcileAppSubStatus) addAppNsHubSubSummaryStatus(appNsHubSubSummaryStatus *appSubStatusV1alpha1.SubscriptionSummaryStatus,
	subNs, subName string, deployedClusters, failedDeployClusters, failedPropagationClusters []string) {
	appNsHubSubSummaryStatus.SetLabels(map[string]string{
		"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", subNs+"."+subName),
	})

	appNsHubSubSummaryStatus.Summary.DeployedSummary.Count = len(deployedClusters)
	appNsHubSubSummaryStatus.Summary.DeployedSummary.Clusters = truncClusterList(deployedClusters)
	appNsHubSubSummaryStatus.Summary.DeployFailedSummary.Count = len(failedDeployClusters)
	appNsHubSubSummaryStatus.Summary.DeployFailedSummary.Clusters = truncClusterList(failedDeployClusters)
	appNsHubSubSummaryStatus.Summary.PropagationFailedSummary.Count = len(failedPropagationClusters)
	appNsHubSubSummaryStatus.Summary.PropagationFailedSummary.Clusters = truncClusterList(failedPropagationClusters)

	r.setOwnerReferences(subNs, subName, appNsHubSubSummaryStatus)
}

func (r *ReconcileAppSubStatus) setOwnerReferences(subNs, subName string, obj metav1.Object) {
	subKey := types.NamespacedName{Name: subName, Namespace: subNs}
	owner := &appsubv1.Subscription{}

	if err := r.Get(context.TODO(), subKey, owner); err != nil {
		klog.Error(err, fmt.Sprintf("Failed to set owner references for %s", obj.GetName()))

		return
	}

	obj.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind())})
}

func truncClusterList(clusters []string) []string {
	if len(clusters) > 10 {
		return clusters[0:10]
	}
	return clusters
}
