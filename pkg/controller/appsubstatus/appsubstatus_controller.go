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
	"strings"
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
	// Create a new controller
	c, err := controller.New("appsubstatus-controller", mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 10,
	})
	if err != nil {
		return err
	}

	// Watch appsubstatus changes
	err = c.Watch(
		&source.Kind{Type: &appSubStatusV1alpha1.SubscriptionStatus{}},
		&handler.EnqueueRequestForObject{},
		subutils.AppSubStatusPredicateFunc)

	if err != nil {
		return err
	}

	return nil
}

func (r *ReconcileAppSubStatus) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	instance := &appSubStatusV1alpha1.SubscriptionStatus{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)

	klog.Info("Reconciling:", request.NamespacedName, " with Get err:", err)

	subNs, subName := subutils.GetHostSubscriptionNSFromObject(request.NamespacedName.Name)
	clusterName := request.NamespacedName.Namespace

	// sync appsubstatus object from the managed cluster NS to the appsub NS
	klog.Infof("syncing appsubstatus object for appsub %v/%v", subNs, subName)

	deletedAppSubStatus := &appSubStatusV1alpha1.SubscriptionStatus{}
	newAppSubStatus := &appSubStatusV1alpha1.SubscriptionStatus{}

	if err != nil {
		if errors.IsNotFound(err) {
			// appsubstatus object in managed cluster NS not found, remove it from the appsub NS
			deletedAppSubStatus, _ = r.DeleteAppNSManagedSubStatus(request.NamespacedName, subNs)
		}
	} else {
		// create or update the new appsubstatus object in cluster NS into the appsubNS
		newAppSubStatus, _ = r.createOrUpdateAppNSManagedSubStatus(request.NamespacedName, instance, subNs, subName, clusterName)
	}

	klog.Infof("appsubstatus object for appsub %v/%v synced", subNs, subName)

	// create or update summary appsubstatus object in the appsub NS
	err = r.generateAppSubSummary(subNs, subName, deletedAppSubStatus, newAppSubStatus)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileAppSubStatus) generateAppSubSummary(subNs, subName string,
	deletedAppSubStatus, newAppSubStatus *appSubStatusV1alpha1.SubscriptionStatus) error {
	klog.Infof("generating appsubstatus summary for appsub %v/%v", subNs, subName)

	managedSubStatusList := &appSubStatusV1alpha1.SubscriptionStatusList{}
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
	listopts.Namespace = subNs
	err = r.List(context.TODO(), managedSubStatusList, listopts)

	if err != nil {
		klog.Error("Failed to list managed appsubstatus, err:", err)

		return err
	}

	if len(managedSubStatusList.Items) == 0 {
		klog.Infof("No managed appsubstatus with labels %v found in appsub NS %v", managedSubStatusSelector, subNs)

		return nil
	}

	// subStatusClusterResults get all cluster status for the appsub.  key: cluster anme, value: true - deployed, false - failed
	subStatusClusterResults := make(map[string]bool)

	for _, managedSubStatus := range managedSubStatusList.Items {
		// dealing with cached managedSubStatus
		// 1. if managedSubStatus is the deleted appSubstatus, don't aggregate it
		if deletedAppSubStatus != nil && managedSubStatus.Name == deletedAppSubStatus.Name &&
			managedSubStatus.Namespace == deletedAppSubStatus.Namespace {
			continue
		}

		// 2. if managedSubStatus is the new updated appSubstatus, apply the new one in the summary aggregation
		// cached client may not be able to get the latest update
		if newAppSubStatus != nil && managedSubStatus.Name == newAppSubStatus.Name &&
			managedSubStatus.Namespace == newAppSubStatus.Namespace {
			managedSubStatus = *newAppSubStatus.DeepCopy()
		}

		parsedstr := strings.Split(managedSubStatus.GetName(), ".")
		if len(parsedstr) != 4 {
			klog.Errorf("Invalid managed appSubStatus name: %v/%v", managedSubStatus.GetNamespace(), managedSubStatus.GetName())

			continue
		}

		clusterName := parsedstr[0]

		if clusterName == "test-cluster-11" {
			klog.V(1).Infof("Package list: %v ", managedSubStatus.Statuses.SubscriptionPackageStatus)
		}

		subStatusClusterResults[clusterName] = true

		for _, pkgStatus := range managedSubStatus.Statuses.SubscriptionPackageStatus {
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

	err = r.createOrUpdateAppNsHubSubStatus(subNs, subName, deployedCount, failedCount, deployedClusters, failedClusters)

	klog.V(1).Infof("appsubstatus summary for appsub %v/%v generated. Deployed: %v/%v; Failed: %v/%v; err: %v ", subNs, subName,
		deployedCount, deployedClusters, failedCount, failedClusters, err)

	return err
}

func (r *ReconcileAppSubStatus) createOrUpdateAppNsHubSubStatus(subNs, subName string,
	deployedCount, failedCount int, deployedClusters, failedClusters []string) error {
	appNsHubSubStatus := &appSubStatusV1alpha1.SubscriptionStatus{}
	appNsHubSubStatusKey := types.NamespacedName{
		Name:      subName + ".status",
		Namespace: subNs,
	}

	if err := r.Get(context.TODO(), appNsHubSubStatusKey, appNsHubSubStatus); err != nil {
		if errors.IsNotFound(err) {
			appNsHubSubStatus = r.newAppNsHubSubStatus(subNs, subName, deployedCount, failedCount, deployedClusters, failedClusters)

			if err := r.Create(context.TODO(), appNsHubSubStatus); err != nil {
				klog.Errorf("Failed to create appNsHubSubStatus err: %v", err)

				return err
			}

			klog.Infof("new appNsHubSubStatus created, %s/%s",
				appNsHubSubStatus.GetNamespace(), appNsHubSubStatus.GetName())

			return nil
		}
	}

	origAppNsHubSubStatus := appNsHubSubStatus.DeepCopy()

	appNsHubSubStatus.SetLabels(map[string]string{
		"apps.open-cluster-management.io/hub-subscription": subName,
	})

	appNsHubSubStatus.Summary.DeployedSummary.Count = deployedCount
	appNsHubSubStatus.Summary.DeployedSummary.Clusters = deployedClusters
	appNsHubSubStatus.Summary.FailedSummary.Count = failedCount
	appNsHubSubStatus.Summary.FailedSummary.Clusters = failedClusters

	r.setOwnerReferences(subNs, subName, appNsHubSubStatus)

	if !reflect.DeepEqual(origAppNsHubSubStatus.Summary, appNsHubSubStatus.Summary) {
		if err := r.Update(context.TODO(), appNsHubSubStatus); err != nil {
			klog.Errorf("Failed to update appNSManagedSubStatus err: %v", err)

			return err
		}

		klog.Infof("appNSManagedSubStatus updated, %s/%s",
			appNsHubSubStatus.GetNamespace(), appNsHubSubStatus.GetName())
	}

	return nil
}

func (r *ReconcileAppSubStatus) newAppNsHubSubStatus(subNs, subName string,
	deployedCount, failedCount int, deployedClusters, failedClusters []string) *appSubStatusV1alpha1.SubscriptionStatus {
	appNsHubSubStatus := &appSubStatusV1alpha1.SubscriptionStatus{}

	appNsHubSubStatus.SetName(subName + ".status")
	appNsHubSubStatus.SetNamespace(subNs)
	appNsHubSubStatus.SetLabels(map[string]string{
		"apps.open-cluster-management.io/hub-subscription": subName,
	})

	appNsHubSubStatus.Summary.DeployedSummary.Count = deployedCount
	appNsHubSubStatus.Summary.DeployedSummary.Clusters = deployedClusters
	appNsHubSubStatus.Summary.FailedSummary.Count = failedCount
	appNsHubSubStatus.Summary.FailedSummary.Clusters = failedClusters

	r.setOwnerReferences(subNs, subName, appNsHubSubStatus)

	return appNsHubSubStatus
}

func (r *ReconcileAppSubStatus) createOrUpdateAppNSManagedSubStatus(clusterNsManagedSubStatusKey types.NamespacedName,
	clusterNsManagedSubStatus *appSubStatusV1alpha1.SubscriptionStatus,
	subNs, subName, clusterName string) (*appSubStatusV1alpha1.SubscriptionStatus, error) {
	appNsManagedSubStatus := &appSubStatusV1alpha1.SubscriptionStatus{}
	appNsManagedSubStatusKey := types.NamespacedName{
		Name:      clusterNsManagedSubStatusKey.Namespace + "." + clusterNsManagedSubStatusKey.Name,
		Namespace: subNs,
	}

	if err := r.Get(context.TODO(), appNsManagedSubStatusKey, appNsManagedSubStatus); err != nil {
		if errors.IsNotFound(err) {
			appNsManagedSubStatus = r.newAppNsManagedSubStatus(clusterNsManagedSubStatus, subNs, subName, clusterName)

			if err := r.Create(context.TODO(), appNsManagedSubStatus); err != nil {
				klog.Errorf("Failed to create appNSManagedSubStatus err: %v", err)

				return nil, err
			}

			klog.Infof("new appNSManagedSubStatus created, %s/%s",
				appNsManagedSubStatus.GetNamespace(), appNsManagedSubStatus.GetName())

			return appNsManagedSubStatus, nil
		}
	}

	newLabels := clusterNsManagedSubStatus.GetLabels()
	delete(newLabels, "apps.open-cluster-management.io/cluster")

	appNsManagedSubStatus.SetLabels(newLabels)
	appNsManagedSubStatus.Statuses = *clusterNsManagedSubStatus.Statuses.DeepCopy()

	r.setOwnerReferences(subNs, subName, appNsManagedSubStatus)

	if err := r.Update(context.TODO(), appNsManagedSubStatus); err != nil {
		klog.Errorf("Failed to update appNSManagedSubStatus err: %v", err)

		return nil, err
	}

	klog.Infof("appNSManagedSubStatus updated, %s/%s",
		appNsManagedSubStatus.GetNamespace(), appNsManagedSubStatus.GetName())

	return appNsManagedSubStatus, nil
}

func (r *ReconcileAppSubStatus) newAppNsManagedSubStatus(clusterNsManagedSubStatus *appSubStatusV1alpha1.SubscriptionStatus,
	subNs, subName, clusterName string) *appSubStatusV1alpha1.SubscriptionStatus {
	appNsManagedSubStatus := &appSubStatusV1alpha1.SubscriptionStatus{}

	appNsManagedSubStatus.SetName(clusterName + "." + subNs + "." + subName + ".status")
	appNsManagedSubStatus.SetNamespace(subNs)

	newLabels := clusterNsManagedSubStatus.GetLabels()
	delete(newLabels, "apps.open-cluster-management.io/cluster")

	appNsManagedSubStatus.SetLabels(newLabels)
	appNsManagedSubStatus.Statuses = *clusterNsManagedSubStatus.Statuses.DeepCopy()

	r.setOwnerReferences(subNs, subName, appNsManagedSubStatus)

	return appNsManagedSubStatus
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

// DeleteManagedAppSubStatusFromAppNS delete the managed appSubStatus object from the app NS.
func (r *ReconcileAppSubStatus) DeleteAppNSManagedSubStatus(ClusterNsManagedSubStatusKey types.NamespacedName,
	subNs string) (*appSubStatusV1alpha1.SubscriptionStatus, error) {
	appNsManagedSubStatusKey := types.NamespacedName{
		Name:      ClusterNsManagedSubStatusKey.Namespace + "." + ClusterNsManagedSubStatusKey.Name,
		Namespace: subNs,
	}

	appNsManagedAppSubStatus := r.GetAppNsManagedAppSubStatus(appNsManagedSubStatusKey)

	if appNsManagedAppSubStatus != nil {
		err := r.Delete(context.TODO(), appNsManagedAppSubStatus)
		if err != nil {
			klog.Warningf("Error in deleting existing appsubStatus from appsub NS, key: %v, err: %v ", appNsManagedSubStatusKey.String(), err)

			return nil, err
		}
	}

	return appNsManagedAppSubStatus, nil
}

// GetAppNsManagedAppSubStatus get the managed appSubStatus object from the app NS.
func (r *ReconcileAppSubStatus) GetAppNsManagedAppSubStatus(appNsManagedSubStatusKey types.NamespacedName) *appSubStatusV1alpha1.SubscriptionStatus {
	AppNsManagedSubStatus := &appSubStatusV1alpha1.SubscriptionStatus{}

	err := r.Get(context.TODO(), appNsManagedSubStatusKey, AppNsManagedSubStatus)
	if err == nil {
		return AppNsManagedSubStatus
	}

	klog.Warningf("appSubstatus not found in the appsub NS: key: %v", appNsManagedSubStatusKey.String())

	return nil
}
