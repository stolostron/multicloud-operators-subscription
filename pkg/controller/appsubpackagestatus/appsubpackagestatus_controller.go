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

package appsubpackagestatus

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	appsubv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
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

// ReconcileAppSubPackageStatus reconciles a AppSubStatus object.
type ReconcileAppSubPackageStatus struct {
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

var _ reconcile.Reconciler = &ReconcileAppSubPackageStatus{}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	authCfg := mgr.GetConfig()
	klog.Infof("Host: %v, BearerToken: %v", authCfg.Host, authCfg.BearerToken)
	kubeClient := kubernetes.NewForConfigOrDie(authCfg)

	dsRS := &ReconcileAppSubPackageStatus{
		Client:     mgr.GetClient(),
		scheme:     mgr.GetScheme(),
		authClient: kubeClient,
		lock:       sync.Mutex{},
	}

	return dsRS
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	klog.Infof("Add appsubpackagestatus-controller to mgr")

	// Create a new controller
	c, err := controller.New("appsubpackagestatus-controller", mgr, controller.Options{
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

func (r *ReconcileAppSubPackageStatus) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	instance := &appSubStatusV1alpha1.SubscriptionPackageStatus{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)

	klog.Info("Reconciling:", request.NamespacedName, " with Get err:", err)

	subNs, subName := subutils.GetHostSubscriptionNSFromObject(request.NamespacedName.Name)
	//clusterName := request.NamespacedName.Namespace

	// sync appsubpackagestatus object from the managed cluster NS to the appsub NS
	klog.Infof("syncing appsubpackagestatus object for appsub %v/%v", subNs, subName)

	deletedAppSubStatus := &appSubStatusV1alpha1.SubscriptionPackageStatus{}

	// create or update summary appsubstatus object in the appsub NS
	err = r.generateAppSubSummary(subNs, subName, deletedAppSubStatus, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileAppSubPackageStatus) generateAppSubSummary(subNs, subName string,
	deletedAppSubStatus, newAppSubStatus *appSubStatusV1alpha1.SubscriptionPackageStatus) error {
	klog.Infof("generating appsubsummarystatus summary for appsub %v/%v", subNs, subName)

	managedSubPackageStatusList := &appSubStatusV1alpha1.SubscriptionPackageStatusList{}
	listopts := &client.ListOptions{}

	managedSubStatusSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"apps.open-cluster-management.io/hosting-subscription": subNs + "." + subName,
		},
	}

	managedSubStatusLabels, err := subutils.ConvertLabels(managedSubStatusSelector)
	if err != nil {
		klog.Error("Failed to convert managed appsubstatus label selector, err:", err)

		return err
	}

	listopts.LabelSelector = managedSubStatusLabels
	err = r.List(context.TODO(), managedSubPackageStatusList, listopts)

	if err != nil {
		klog.Error("Failed to list managed appsubpackagestatus, err:", err)

		return err
	}

	if len(managedSubPackageStatusList.Items) == 0 {
		klog.Infof("No managed appsubpackagestatus with labels %v found in appsub NS %v", managedSubStatusSelector, subNs)

		// no appsubpackagestatus in managed cluster NS - appsubsummary status can be deleted
		appNsHubSubSummaryStatus := &appSubStatusV1alpha1.SubscriptionSummaryStatus{}
		appNsHubSubSummaryStatusKey := types.NamespacedName{
			Name:      subName + ".status",
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

	// subStatusClusterResults get all cluster status for the appsub.  key: cluster anme, value: true - deployed, false - failed
	subStatusClusterResults := make(map[string]bool)

	for _, managedSubPackageStatus := range managedSubPackageStatusList.Items {
		// dealing with cached managedSubStatus
		// 1. if managedSubStatus is the deleted appSubstatus, don't aggregate it
		if deletedAppSubStatus != nil && managedSubPackageStatus.Name == deletedAppSubStatus.Name &&
			managedSubPackageStatus.Namespace == deletedAppSubStatus.Namespace {
			continue
		}

		// 2. if managedSubStatus is the new updated appSubstatus, apply the new one in the summary aggregation
		// cached client may not be able to get the latest update
		if newAppSubStatus != nil && managedSubPackageStatus.Name == newAppSubStatus.Name &&
			managedSubPackageStatus.Namespace == newAppSubStatus.Namespace {
			managedSubPackageStatus = *newAppSubStatus.DeepCopy()
		}

		clusterName := managedSubPackageStatus.Namespace

		subStatusClusterResults[clusterName] = true

		for _, pkgStatus := range managedSubPackageStatus.Statuses.SubscriptionPackageStatus {
			if pkgStatus.Phase != "Deployed" {
				subStatusClusterResults[clusterName] = false

				break
			}
		}
	}

	deployedCount := 0
	deployedClusters := []string{}

	failedCount := 0
	failedClusters := []string{}

	for cluster, subStatus := range subStatusClusterResults {
		if subStatus {
			deployedCount++

			if len(deployedClusters) < 10 {
				deployedClusters = append(deployedClusters, cluster)
			}
		} else {
			failedCount++

			if len(failedClusters) < 10 {
				failedClusters = append(failedClusters, cluster)
			}
		}
	}

	err = r.createOrUpdateAppNsHubSubSummaryStatus(subNs, subName, deployedCount, failedCount, deployedClusters, failedClusters)

	klog.V(1).Infof("appsubsummarystatus summary for appsub %v/%v generated. Deployed: %v/%v; Failed: %v/%v; err: %v ", subNs, subName,
		deployedCount, deployedClusters, failedCount, failedClusters, err)

	return err
}

func (r *ReconcileAppSubPackageStatus) createOrUpdateAppNsHubSubSummaryStatus(subNs, subName string,
	deployedCount, failedCount int, deployedClusters, failedClusters []string) error {
	appNsHubSubSummaryStatus := &appSubStatusV1alpha1.SubscriptionSummaryStatus{}
	appNsHubSubSummaryStatusKey := types.NamespacedName{
		Name:      subName + ".status",
		Namespace: subNs,
	}

	if err := r.Get(context.TODO(), appNsHubSubSummaryStatusKey, appNsHubSubSummaryStatus); err != nil {
		if errors.IsNotFound(err) {
			appNsHubSubSummaryStatus = r.newAppNsHubSubSummaryStatus(subNs, subName, deployedCount, failedCount, deployedClusters, failedClusters)

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

	appNsHubSubSummaryStatus.SetLabels(map[string]string{
		"apps.open-cluster-management.io/hub-subscription": subName,
	})

	appNsHubSubSummaryStatus.Summary.DeployedSummary.Count = deployedCount
	appNsHubSubSummaryStatus.Summary.DeployedSummary.Clusters = deployedClusters
	appNsHubSubSummaryStatus.Summary.FailedSummary.Count = failedCount
	appNsHubSubSummaryStatus.Summary.FailedSummary.Clusters = failedClusters

	r.setOwnerReferences(subNs, subName, appNsHubSubSummaryStatus)

	if !reflect.DeepEqual(origAppNsHubSubSummaryStatus.Summary, appNsHubSubSummaryStatus.Summary) {
		if err := r.Update(context.TODO(), appNsHubSubSummaryStatus); err != nil {
			klog.Errorf("Failed to update appNSManagedSubStatus err: %v", err)

			return err
		}

		klog.Infof("appNSManagedSubStatus updated, %s/%s",
			appNsHubSubSummaryStatus.GetNamespace(), appNsHubSubSummaryStatus.GetName())
	}

	return nil
}

func (r *ReconcileAppSubPackageStatus) newAppNsHubSubSummaryStatus(subNs, subName string,
	deployedCount, failedCount int, deployedClusters, failedClusters []string) *appSubStatusV1alpha1.SubscriptionSummaryStatus {
	appNsHubSubSummaryStatus := &appSubStatusV1alpha1.SubscriptionSummaryStatus{}

	appNsHubSubSummaryStatus.SetName(subName + ".status")
	appNsHubSubSummaryStatus.SetNamespace(subNs)
	appNsHubSubSummaryStatus.SetLabels(map[string]string{
		"apps.open-cluster-management.io/hub-subscription": subName,
	})

	appNsHubSubSummaryStatus.Summary.DeployedSummary.Count = deployedCount
	appNsHubSubSummaryStatus.Summary.DeployedSummary.Clusters = deployedClusters
	appNsHubSubSummaryStatus.Summary.FailedSummary.Count = failedCount
	appNsHubSubSummaryStatus.Summary.FailedSummary.Clusters = failedClusters

	r.setOwnerReferences(subNs, subName, appNsHubSubSummaryStatus)

	return appNsHubSubSummaryStatus
}

func (r *ReconcileAppSubPackageStatus) setOwnerReferences(subNs, subName string, obj metav1.Object) {
	subKey := types.NamespacedName{Name: subName, Namespace: subNs}
	owner := &appsubv1.Subscription{}

	if err := r.Get(context.TODO(), subKey, owner); err != nil {
		klog.Error(err, fmt.Sprintf("Failed to set owner references for %s", obj.GetName()))

		return
	}

	obj.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind())})
}
