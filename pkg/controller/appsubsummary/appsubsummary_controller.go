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

package appsubsummary

import (
	"context"
	"fmt"
	"sync"

	appsubv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	subutils "github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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
	"sigs.k8s.io/wg-policy-prototypes/policy-report/pkg/api/wgpolicyk8s.io/v1alpha2"
	policyReportV1alpha2 "sigs.k8s.io/wg-policy-prototypes/policy-report/pkg/api/wgpolicyk8s.io/v1alpha2"
)

// ReconcileAppSubStatus reconciles a AppSubStatus object.
type ReconcileAppSubSummary struct {
	client.Client
	authClient kubernetes.Interface
	scheme     *runtime.Scheme
	lock       sync.Mutex
}

// resource list deployed by the appsub, per app.
type AppSubResoures struct {
	TotalClusterCount int
	Timestamp         metav1.Timestamp
	Resoures          []*corev1.ObjectReference
}

type AppSubClusterFailStatus struct {
	Cluster   string
	Phase     string
	Message   string
	Timestamp metav1.Timestamp
}

// appsub status per cluster.
type AppSubClustersFailStatus struct {
	Clusters []AppSubClusterFailStatus
}

// Add creates a new argocd cluster Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

var _ reconcile.Reconciler = &ReconcileAppSubSummary{}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	authCfg := mgr.GetConfig()
	klog.Infof("Host: %v, BearerToken: %v", authCfg.Host, authCfg.BearerToken)
	kubeClient := kubernetes.NewForConfigOrDie(authCfg)

	dsRS := &ReconcileAppSubSummary{
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
		&source.Kind{Type: &policyReportV1alpha2.PolicyReport{}},
		&handler.EnqueueRequestForObject{},
		subutils.AppSubSummaryPredicateFunc)

	if err != nil {
		return err
	}

	return nil
}

func (r *ReconcileAppSubSummary) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	instance := &policyReportV1alpha2.PolicyReport{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)

	klog.Info("Reconciling:", request.NamespacedName, " with Get err:", err)

	// create or update summary appsubstatus object in the appsub NS
	err = r.generateAppSubSummary(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileAppSubSummary) generateAppSubSummary(policyReport *policyReportV1alpha2.PolicyReport) error {
	appPolicyReportClusterList := &policyReportV1alpha2.PolicyReportList{}
	listopts := &client.ListOptions{}

	appPolicyReportClusterSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"apps.open-cluster-management.io/cluster": "true",
		},
	}

	appPolicyReportClusterLabels, err := subutils.ConvertLabels(appPolicyReportClusterSelector)
	if err != nil {
		klog.Errorf("Failed to convert managed appsubstatus label selector, err:%v", err)

		return err
	}

	listopts.LabelSelector = appPolicyReportClusterLabels
	err = r.List(context.TODO(), appPolicyReportClusterList, listopts)

	if err != nil {
		klog.Errorf("Failed to list managed appsubpackagestatus, err:%v", err)

		return err
	}

	if len(appPolicyReportClusterList.Items) == 0 {
		klog.Infof("No appsub PolicyReport Per Cluster with labels %v found", appPolicyReportClusterSelector)

		return nil
	}

	// create a map for containing all appsub status per cluster. key is appsub name
	appSubClusterFailStatusMap := make(map[string]AppSubClustersFailStatus)

	for _, appsubPolicyReportPerCluster := range appPolicyReportClusterList.Items {
		r.UpdateAppSubMapsPerCluster(appsubPolicyReportPerCluster, appSubClusterFailStatusMap)
	}

	klog.Infof("appSub Resource Map and appSub Cluster Status Map ready")
	klog.V(1).Infof("appSub Cluster Failure Status Map: %v", appSubClusterFailStatusMap)

	r.createOrUpdateAppSubPolicyReport(appSubClusterFailStatusMap, len(appPolicyReportClusterList.Items))

	klog.Infof("All AppSub Policy Report re-genreated")

	return nil
}

func (r *ReconcileAppSubSummary) UpdateAppSubMapsPerCluster(appsubPolicyReportPerCluster policyReportV1alpha2.PolicyReport,
	appSubClusterFailStatusMap map[string]AppSubClustersFailStatus) {
	cluster := appsubPolicyReportPerCluster.Namespace

	for _, result := range appsubPolicyReportPerCluster.Results {
		klog.Infof("Result:%v", result)
		appsubName, appsubNs := utils.ParseNamespacedName(result.Source)
		if appsubName == "" && appsubNs == "" {
			continue
		}

		if result.Policy == "APPSUB_FAILURE" {
			cs := AppSubClusterFailStatus{
				Cluster:   cluster,
				Phase:     string(result.Result),
				Message:   result.Description,
				Timestamp: result.Timestamp,
			}

			if clusterStatus, ok := appSubClusterFailStatusMap[result.Source]; ok {
				clusterStatus.Clusters = append(clusterStatus.Clusters, cs)
				appSubClusterFailStatusMap[result.Source] = clusterStatus
			} else {
				newClusterStatus := AppSubClustersFailStatus{}
				newClusterStatus.Clusters = append(newClusterStatus.Clusters, cs)
				appSubClusterFailStatusMap[result.Source] = newClusterStatus
			}
		}
	}
}

func (r *ReconcileAppSubSummary) createOrUpdateAppSubPolicyReport(
	appSubClusterFailStatusMap map[string]AppSubClustersFailStatus, clusterCount int) {

	// Find existing policyReport for app - can assume it exists for now

	for appsub, clustersFailStatus := range appSubClusterFailStatusMap {
		appsubNs, appsubName := utils.ParseNamespacedName(appsub)
		if appsubName == "" && appsubNs == "" {
			continue
		}

		klog.V(1).Infof("updating PolicyReport for appsub: %v", appsub)

		appsubPolicyReport := &policyReportV1alpha2.PolicyReport{}
		appsubPolicyReportKey := types.NamespacedName{
			Name:      appsubName + "-policyreport-appsub-status",
			Namespace: appsubNs,
		}

		if err := r.Get(context.TODO(), appsubPolicyReportKey, appsubPolicyReport); err != nil {
			if errors.IsNotFound(err) {
				klog.Errorf("Failed to find app policyReport err: %v", err)

				continue
			}
		}

		// Find and keep appsub resource list from original policy report
		policyReportResult := &v1alpha2.PolicyReportResult{}
		for _, result := range appsubPolicyReport.Results {
			if result.Policy == "APPSUB_RESOURCE_LIST" {
				policyReportResult = result.DeepCopy()
			}
		}

		newPolicyReport := r.newAppPolicyReport(appsubNs, appsubName, policyReportResult, clustersFailStatus, clusterCount)

		origAppsubPolicyReport := appsubPolicyReport.DeepCopy()

		if !equality.Semantic.DeepEqual(origAppsubPolicyReport.GetLabels(), newPolicyReport.GetLabels()) ||
			!equality.Semantic.DeepEqual(origAppsubPolicyReport.Results, newPolicyReport.Results) ||
			!equality.Semantic.DeepEqual(origAppsubPolicyReport.Scope, newPolicyReport.Scope) ||
			!equality.Semantic.DeepEqual(origAppsubPolicyReport.Summary, newPolicyReport.Summary) {
			appsubPolicyReport.SetLabels(newPolicyReport.GetLabels())

			appsubPolicyReport.Results = newPolicyReport.Results

			appsubPolicyReport.Scope = newPolicyReport.Scope
			appsubPolicyReport.Summary = newPolicyReport.Summary

			if err := r.Update(context.TODO(), appsubPolicyReport); err != nil {
				klog.Errorf("Failed to update appNsHubSubSummaryStatus err: %v", err)

				continue
			}

			klog.V(1).Infof("policyReport updated, %v/%v", newPolicyReport.GetNamespace(), newPolicyReport.GetName())
		}
	}
}

func (r *ReconcileAppSubSummary) newAppPolicyReport(appsubNs, appsubName string,
	appsubResourceList *policyReportV1alpha2.PolicyReportResult,
	clustersFailStatus AppSubClustersFailStatus, clusterCount int) *policyReportV1alpha2.PolicyReport {
	newPolicyReportResults := []*policyReportV1alpha2.PolicyReportResult{}

	newPolicyReportResults = append(newPolicyReportResults, appsubResourceList)

	for _, ClustersFailStatus := range clustersFailStatus.Clusters {
		newPolicyReportResult := &policyReportV1alpha2.PolicyReportResult{
			Policy:      "APPSUB_FAILURE",
			Source:      ClustersFailStatus.Cluster,
			Description: ClustersFailStatus.Message,
			Result:      policyReportV1alpha2.PolicyResult("fail"),
			Timestamp:   ClustersFailStatus.Timestamp,
		}
		newPolicyReportResults = append(newPolicyReportResults, newPolicyReportResult)
	}

	newPolicyReport := &policyReportV1alpha2.PolicyReport{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PolicyReport",
			APIVersion: "wgpolicyk8s.io/v1alpha2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      appsubName + "-policyreport-appsub-status",
			Namespace: appsubNs,
			Labels: map[string]string{
				"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appsubNs+"."+appsubName),
			},
		},
		Results: newPolicyReportResults,
		Scope: &corev1.ObjectReference{
			Kind:      "appsub",
			Name:      appsubName,
			Namespace: appsubNs,
		},
		Summary: v1alpha2.PolicyReportSummary{
			// TODO: Have to get total cluster count for app from appsub?
			Pass:  clusterCount - len(clustersFailStatus.Clusters),
			Fail:  len(clustersFailStatus.Clusters),
			Warn:  0,
			Error: 0,
			Skip:  0,
		},
	}

	r.setOwnerReferences(appsubNs, appsubName, newPolicyReport)

	return newPolicyReport
}

func (r *ReconcileAppSubSummary) setOwnerReferences(subNs, subName string, obj metav1.Object) {
	subKey := types.NamespacedName{Name: subName, Namespace: subNs}
	owner := &appsubv1.Subscription{}

	if err := r.Get(context.TODO(), subKey, owner); err != nil {
		klog.Errorf("Failed to set owner references for %s. err: %v", obj.GetName(), err)

		return
	}

	obj.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind())})
}
