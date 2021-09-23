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
	"runtime"
	"sort"
	"time"

	appsubv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	subutils "github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/wg-policy-prototypes/policy-report/pkg/api/wgpolicyk8s.io/v1alpha2"
	policyReportV1alpha2 "sigs.k8s.io/wg-policy-prototypes/policy-report/pkg/api/wgpolicyk8s.io/v1alpha2"
)

// ReconcileAppSubStatus reconciles a AppSubStatus object.
type ReconcileAppSubSummary struct {
	client.Client
	Interval int
}

type AppSubClusterFailStatus struct {
	Cluster string
	Phase   string
}

// appsub status per cluster.
type AppSubClustersFailStatus struct {
	Clusters []AppSubClusterFailStatus
}

// ClusterSorter sorts policyreport results by source name.
type ClusterSorter []*v1alpha2.PolicyReportResult

func (a ClusterSorter) Len() int           { return len(a) }
func (a ClusterSorter) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ClusterSorter) Less(i, j int) bool { return a[i].Source < a[j].Source }

func Add(mgr manager.Manager, interval int) error {
	dsRS := &ReconcileAppSubSummary{
		Client:   mgr.GetClient(),
		Interval: interval,
	}

	return mgr.Add(dsRS)
}

func (r *ReconcileAppSubSummary) Start(ctx context.Context) error {
	go wait.Until(func() {
		r.houseKeeping()
	}, time.Duration(r.Interval)*time.Second, ctx.Done())

	return nil
}

func (r *ReconcileAppSubSummary) houseKeeping() {
	klog.Info("Start aggregating all appsub policyReports based on policyReport per cluster...")

	// create or update all app policyReport object in the appsub NS
	r.generateAppSubSummary()

	klog.Info("Finish aggregating all appsub policyReports.")
}

func (r *ReconcileAppSubSummary) generateAppSubSummary() error {
	PrintMemUsage("prepare to fetch all cluster policyReports.")

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

	clusterPolicyReportCount := len(appPolicyReportClusterList.Items)

	if clusterPolicyReportCount == 0 {
		klog.Infof("No appsub PolicyReport Per Cluster with labels %v found", appPolicyReportClusterSelector)

		return nil
	}

	klog.Infof("cluster PolicyReport Count: %v", clusterPolicyReportCount)

	PrintMemUsage("Initialize AppSub Map.")

	// create a map for containing all appsub status per cluster. key is appsub name
	appSubClusterFailStatusMap := make(map[string]AppSubClustersFailStatus)

	for _, appsubPolicyReportPerCluster := range appPolicyReportClusterList.Items {
		r.UpdateAppSubMapsPerCluster(appsubPolicyReportPerCluster, appSubClusterFailStatusMap)
	}

	appPolicyReportClusterList = nil

	runtime.GC()

	PrintMemUsage("AppSub Map generated.")

	r.createOrUpdateAppSubPolicyReport(appSubClusterFailStatusMap)

	runtime.GC()

	return nil
}

func (r *ReconcileAppSubSummary) UpdateAppSubMapsPerCluster(appsubPolicyReportPerCluster policyReportV1alpha2.PolicyReport,
	appSubClusterFailStatusMap map[string]AppSubClustersFailStatus) {
	cluster := appsubPolicyReportPerCluster.Namespace

	for _, result := range appsubPolicyReportPerCluster.Results {
		appsubName, appsubNs := utils.ParseNamespacedName(result.Source)

		if appsubName == "" && appsubNs == "" {
			continue
		}

		if result.Policy == "APPSUB_FAILURE" {
			cs := AppSubClusterFailStatus{
				Cluster: cluster,
				Phase:   string(result.Result),
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
	appSubClusterFailStatusMap map[string]AppSubClustersFailStatus) {
	// Find existing policyReport for app - can assume it exists for now

	klog.Infof("appSub Cluster FailStatus Map Count: %v", len(appSubClusterFailStatusMap))

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

		clusterCount := appsubPolicyReport.Summary.Pass + appsubPolicyReport.Summary.Fail

		// Find and keep appsub resource list from original policy report
		policyReportResult := &v1alpha2.PolicyReportResult{}

		for _, result := range appsubPolicyReport.Results {
			if result.Policy == "APPSUB_RESOURCE_LIST" {
				policyReportResult = result.DeepCopy()

				break
			}
		}

		newPolicyReport := r.newAppPolicyReport(appsubNs, appsubName, policyReportResult, clustersFailStatus, clusterCount)

		origAppsubPolicyReport := appsubPolicyReport.DeepCopy()

		PrintMemUsage("memory usage when updating appsub PolicyReport.")

		isSame := true

		if !equality.Semantic.DeepEqual(origAppsubPolicyReport.GetLabels(), newPolicyReport.GetLabels()) {
			klog.V(1).Info("labels not same")

			isSame = false
		}

		if !equality.Semantic.DeepEqual(origAppsubPolicyReport.Scope, newPolicyReport.Scope) {
			klog.V(1).Info("Scope not same")

			isSame = false
		}

		if !equality.Semantic.DeepEqual(origAppsubPolicyReport.Summary, newPolicyReport.Summary) {
			klog.V(1).Info("Summary not same")

			isSame = false
		}

		sort.Sort(ClusterSorter(origAppsubPolicyReport.Results))
		sort.Sort(ClusterSorter(newPolicyReport.Results))

		if !equality.Semantic.DeepEqual(origAppsubPolicyReport.Results, newPolicyReport.Results) {
			klog.V(1).Info("Results not same")

			isSame = false
		}

		if !isSame {
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
			Policy: "APPSUB_FAILURE",
			Source: ClustersFailStatus.Cluster,
			Result: policyReportV1alpha2.PolicyResult("fail"),
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

func PrintMemUsage(title string) {
	var m runtime.MemStats

	runtime.ReadMemStats(&m)

	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	klog.Infof("%v", title)
	klog.Infof("Alloc = %v MiB", bToMb(m.Alloc))
	klog.Infof("\tTotalAlloc = %v MiB", bToMb(m.TotalAlloc))
	klog.Infof("\tSys = %v MiB", bToMb(m.Sys))
	klog.Infof("\tNumGC = %v\n", m.NumGC)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
