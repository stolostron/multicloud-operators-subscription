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
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	appsubv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appsubReportV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	subutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ReconcileAppSubStatus reconciles a AppSubStatus object.
type ReconcileAppSubSummary struct {
	client.Client
	Interval int
}

type AppSubClusterStatus struct {
	Cluster string
	Phase   string
}

// appsub cluster statuses per appsub.
type AppSubClustersStatus struct {
	Clusters          []AppSubClusterStatus
	Deployed          int
	Failed            int
	PropagationFailed int
}

// ClusterSorter sorts appsubreport results by source name.
type ClusterSorter []*appsubReportV1alpha1.SubscriptionReportResult

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
	klog.Info("Start aggregating all appsub reports based on appsubReport per cluster...")

	// create or update all app appsubReport object in the appsub NS
	err := r.generateAppSubSummary()
	if err != nil {
		klog.Warning("error while generating app sub summary: ", err)
	}

	klog.Info("Finish aggregating all appsub reports.")
}

func (r *ReconcileAppSubSummary) generateAppSubSummary() error {
	PrintMemUsage("prepare to fetch all cluster appsubReports.")

	appsubReportClusterList := &appsubReportV1alpha1.SubscriptionReportList{}
	listopts := &client.ListOptions{}

	appsubReportClusterSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"apps.open-cluster-management.io/cluster": "true",
		},
	}

	appsubReportClusterLabels, err := subutils.ConvertLabels(appsubReportClusterSelector)
	if err != nil {
		klog.Errorf("Failed to convert managed appsubstatus label selector, err:%v", err)

		return err
	}

	listopts.LabelSelector = appsubReportClusterLabels
	err = r.List(context.TODO(), appsubReportClusterList, listopts)

	if err != nil {
		klog.Errorf("Failed to list managed appsubpackagestatus, err:%v", err)

		return err
	}

	clusterAppsubReportCount := len(appsubReportClusterList.Items)

	if clusterAppsubReportCount == 0 {
		klog.Infof("No appsub Report Per Cluster with labels %v found", appsubReportClusterSelector)

		return nil
	}

	klog.Infof("cluster appSubReport Count: %v", clusterAppsubReportCount)

	PrintMemUsage("Initialize AppSub Map.")

	// create a map for containing all appsub status per cluster. key is appsub name
	appSubClusterStatusMap := make(map[string]AppSubClustersStatus)

	for _, appsubReportPerCluster := range appsubReportClusterList.Items {
		r.UpdateAppSubMapsPerCluster(appsubReportPerCluster, appSubClusterStatusMap)
	}

	appsubReportClusterList = nil

	runtime.GC()

	PrintMemUsage("AppSub Map generated.")

	r.createOrUpdateAppSubReport(appSubClusterStatusMap)

	runtime.GC()

	return nil
}

func (r *ReconcileAppSubSummary) UpdateAppSubMapsPerCluster(appsubReportPerCluster appsubReportV1alpha1.SubscriptionReport,
	appSubClusterStatusMap map[string]AppSubClustersStatus) {
	cluster := appsubReportPerCluster.Namespace

	for _, result := range appsubReportPerCluster.Results {
		appsubName, appsubNs := utils.ParseNamespacedName(result.Source)

		if appsubName == "" && appsubNs == "" {
			continue
		}

		cs := AppSubClusterStatus{
			Cluster: cluster,
			Phase:   string(result.Result),
		}

		if clusterStatus, ok := appSubClusterStatusMap[result.Source]; ok {
			if cs.Phase == "failed" {
				clusterStatus.Failed++
			} else if cs.Phase == "deployed" {
				clusterStatus.Deployed++
			} else if cs.Phase == "propagationFailed" {
				clusterStatus.PropagationFailed++
			}

			clusterStatus.Clusters = append(clusterStatus.Clusters, cs)
			appSubClusterStatusMap[result.Source] = clusterStatus
		} else {
			newClusterStatus := AppSubClustersStatus{}

			if cs.Phase == "failed" {
				newClusterStatus.Failed = 1
			} else if cs.Phase == "deployed" {
				newClusterStatus.Deployed = 1
			} else if cs.Phase == "propagationFailed" {
				newClusterStatus.PropagationFailed = 1
			}

			newClusterStatus.Clusters = append(newClusterStatus.Clusters, cs)
			appSubClusterStatusMap[result.Source] = newClusterStatus
		}
	}
}

func (r *ReconcileAppSubSummary) createOrUpdateAppSubReport(
	appSubClusterStatusMap map[string]AppSubClustersStatus) {
	// Find existing appSubReport for app - can assume it exists for now
	klog.Infof("appSub Cluster FailStatus Map Count: %v", len(appSubClusterStatusMap))

	for appsub, clustersStatus := range appSubClusterStatusMap {
		appsubNs, appsubName := utils.ParseNamespacedName(appsub)
		if appsubName == "" && appsubNs == "" {
			continue
		}

		klog.V(1).Infof("updating AppSubReport for appsub: %v", appsub)

		appsubReport := &appsubReportV1alpha1.SubscriptionReport{
			TypeMeta: metav1.TypeMeta{
				Kind:       "SubscriptionReport",
				APIVersion: "apps.open-cluster-management.io/v1alpha1",
			},
		}
		appsubReportKey := types.NamespacedName{
			Name:      appsubName,
			Namespace: appsubNs,
		}

		if err := r.Get(context.TODO(), appsubReportKey, appsubReport); err != nil {
			if errors.IsNotFound(err) {
				klog.Errorf("Failed to find app appsubReport err: %v", err)

				continue
			}
		}

		// Find and keep appsub resource list from original appsubReport
		appsubResources := appsubReport.Resources

		appsubSummary := appsubReport.Summary

		newAppsubReport := r.newAppSubReport(appsubNs, appsubName, appsubResources, appsubSummary, clustersStatus)

		origAppsubReport := appsubReport.DeepCopy()

		PrintMemUsage("memory usage when updating appsub AppsubReport.")

		isSame := true

		if !equality.Semantic.DeepEqual(origAppsubReport.GetLabels(), newAppsubReport.GetLabels()) {
			klog.V(1).Info("labels not same")

			isSame = false
		}

		if !equality.Semantic.DeepEqual(origAppsubReport.Summary, newAppsubReport.Summary) {
			klog.V(1).Info("Summary not same")

			isSame = false
		}

		sort.Sort(ClusterSorter(origAppsubReport.Results))
		sort.Sort(ClusterSorter(newAppsubReport.Results))

		if !equality.Semantic.DeepEqual(origAppsubReport.Results, newAppsubReport.Results) {
			klog.V(1).Info("Results not same")

			isSame = false
		}

		if !isSame {
			appsubReport.SetLabels(newAppsubReport.GetLabels())

			appsubReport.Results = newAppsubReport.Results

			appsubReport.Summary = newAppsubReport.Summary

			if err := r.Update(context.TODO(), appsubReport); err != nil {
				klog.Errorf("Failed to update appNsAppsubReport err: %v", err)

				continue
			}

			klog.V(1).Infof("AppsubReport updated, %v/%v", newAppsubReport.GetNamespace(), newAppsubReport.GetName())
		}
	}
}

func (r *ReconcileAppSubSummary) newAppSubReport(appsubNs, appsubName string,
	appsubResourceList []*corev1.ObjectReference, appsubSummary appsubReportV1alpha1.SubscriptionReportSummary,
	clustersStatus AppSubClustersStatus) *appsubReportV1alpha1.SubscriptionReport {
	newAppsubReportResults := []*appsubReportV1alpha1.SubscriptionReportResult{}

	for _, ClusterStatus := range clustersStatus.Clusters {
		newAppsubReportResult := &appsubReportV1alpha1.SubscriptionReportResult{
			Source: ClusterStatus.Cluster,
			Result: appsubReportV1alpha1.SubscriptionResult(ClusterStatus.Phase),
		}
		newAppsubReportResults = append(newAppsubReportResults, newAppsubReportResult)
	}

	iClusters, _ := strconv.Atoi(appsubSummary.Clusters)

	inProgressCount := iClusters - clustersStatus.PropagationFailed - clustersStatus.Deployed - clustersStatus.Failed
	if inProgressCount < 0 {
		klog.Warningf("inProgress Count < 0, inProgressCount: %v", inProgressCount)
		inProgressCount = 0
	}

	newAppsubReport := &appsubReportV1alpha1.SubscriptionReport{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SubscriptionReport",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      appsubName,
			Namespace: appsubNs,
			Labels: map[string]string{
				"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appsubNs+"."+appsubName),
			},
		},
		ReportType: "Application",
		Resources:  appsubResourceList,
		Results:    newAppsubReportResults,
		Summary: appsubReportV1alpha1.SubscriptionReportSummary{
			Deployed:          strconv.Itoa(clustersStatus.Deployed),
			Failed:            strconv.Itoa(clustersStatus.Failed),
			PropagationFailed: strconv.Itoa(clustersStatus.PropagationFailed),
			Clusters:          appsubSummary.Clusters,
			InProgress:        strconv.Itoa(inProgressCount),
		},
	}

	r.setOwnerReferences(appsubNs, appsubName, newAppsubReport)

	return newAppsubReport
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
