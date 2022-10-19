// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	v1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

const (
	localSuffix  = "-local"
	localCluster = "local-cluster"
)

/*

use {apiversion, kind, namespace, name} as the key to build new appsubPackaggeStatus map and existing appsubPackaggeStatus map
  - the new appsubPackaggeStatus map is from the appsubClusterStatus parameter passed from syncrhonizer
  - the existing appsubPackaggeStatus map is fetched by the namespaced name
    {appsubClusterStatus.AppSub.Namespace, appsubClusterStatus.AppSub.Name+".status"}

Create the final appsubPackaggeStatus map for containing the final updated appsubPackaggeStatus map

1. action == APPLY,
  Compare each resource in the new appsubPackaggeStatus map with existing appsubPackaggeStatus map
  - append all the resources in the new appsubPackaggeStatus map to the final appsubPackaggeStatus map,
    and delete all the resources from the existing appsubPackaggeStatus map
  - For the left resources in the existing appsubPackaggeStatus map, they should be undeployeds
    call sync.DeleteSingleSubscribedResource to delete these deployed resources from the managed cluster
	If succeed, don't need to append these deleted resources to the final appsubPackaggeStatus map
	If fail, append thse delete failed resources to the final appsubPackaggeStatus map

2. action == DELETE, this action is from func PurgeAllSubscribedResources, where
  all the deployed resources in the appsub have been removed with succeeded/failed status
  - if all resources in the new appsubPackaggeStatus map are removed without failure, just delete the relative appsubPackaggeStatus CR
  - if there is any resource who failed to be removed, the relative appsubPackaggeStatus CR should be remained, where only
    resources who failed to be removed are listed.

*/

func (sync *KubeSynchronizer) SyncAppsubClusterStatus(appsub *appv1.Subscription,
	appsubClusterStatus SubscriptionClusterStatus,
	skipOrphanDelete *bool, skipUpdate *bool) error {
	klog.Infof("cluster: %v, appsub: %v/%v, action: %v, hub:%v, standalone:%v\n", appsubClusterStatus.Cluster,
		appsubClusterStatus.AppSub.Namespace, appsubClusterStatus.AppSub.Name, appsubClusterStatus.Action, sync.hub, sync.standalone)

	skipOrphanDel := false
	if skipOrphanDelete != nil {
		skipOrphanDel = *skipOrphanDelete
	}

	skipUpd := false
	if skipUpdate != nil {
		skipUpd = *skipUpdate
	}

	// Get existing appsubstatus on managed cluster, if it exists
	appsubName := appsubClusterStatus.AppSub.Name
	pkgstatusNs := appsubClusterStatus.AppSub.Namespace
	isLocalCluster := (sync.hub && !sync.standalone) ||
		(appsubClusterStatus.Cluster == localCluster && strings.HasSuffix(appsubName, localSuffix)) ||
		(sync.standalone && strings.HasSuffix(appsubName, localSuffix))

	if isLocalCluster || sync.standalone && skipOrphanDel {
		if strings.HasSuffix(appsubName, localSuffix) {
			appsubName = appsubName[:len(appsubName)-6]
		}
	}

	pkgstatusName := appsubName

	pkgstatus := &v1alpha1.SubscriptionStatus{
		TypeMeta: metaV1.TypeMeta{
			Kind:       "SubscriptionStatus",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
	}
	foundPkgStatus := true

	if err := sync.LocalClient.Get(context.TODO(),
		client.ObjectKey{Name: pkgstatusName, Namespace: pkgstatusNs}, pkgstatus); err != nil {
		if errors.IsNotFound(err) {
			foundPkgStatus = false
		} else {
			klog.Errorf("failed to get package status, err: %v", err)

			return err
		}
	}

	// If the incoming new appstatus is only one HelmRelease kind resource, skip the appsubstatus sync-up.
	// Later the helmrelease controller will update the actual resources to the appsubstatus
	if shouldSkip(appsubClusterStatus, foundPkgStatus, *pkgstatus) {
		return nil
	}

	if foundPkgStatus && skipUpd {
		klog.Infof("Skip update appsubstatus(%v/%v) due to skipUpdate flag but update AppsubReport",
			pkgstatus.Namespace, pkgstatus.Name)

		// Update result in cluster AppsubReport
		if err := updateAppsubReportResult(sync.RemoteClient, appsubClusterStatus.AppSub.Namespace,
			appsubName, appsubClusterStatus.Cluster, false,
			sync.standalone, isLocalCluster); err != nil {
			return err
		}

		return nil
	}

	if appsubClusterStatus.Action == "APPLY" {
		// Skip helmrelease on local-cluster
		if isLocalCluster && len(appsubClusterStatus.SubscriptionPackageStatus) == 1 &&
			strings.EqualFold(appsubClusterStatus.SubscriptionPackageStatus[0].Kind, "HelmRelease") &&
			strings.EqualFold(appsubClusterStatus.SubscriptionPackageStatus[0].APIVersion, "apps.open-cluster-management.io/v1") &&
			appsubClusterStatus.SubscriptionPackageStatus[0].Phase != string(v1alpha1.PackageDeployFailed) {
			klog.V(1).Infof("Skip create appsubstatus(%v/%v) for HelmRelease", pkgstatus.Namespace, pkgstatus.Name)

			// Create cluster report so the helm release controller on the standalone could update it
			_, err := getClusterAppsubReport(sync.RemoteClient, appsubClusterStatus.Cluster, true)
			if err != nil {
				return err
			}

			return nil
		}

		newUnitStatus := []v1alpha1.SubscriptionUnitStatus{}

		for _, resource := range appsubClusterStatus.SubscriptionPackageStatus {
			klog.V(1).Infof("resource status - Name: %v, Namespace: %v, Apiversion: %v, Kind: %v, Phase: %v, Message: %v\n",
				resource.Name, resource.Namespace, resource.APIVersion, resource.Kind, resource.Phase, resource.Message)

			uS := &v1alpha1.SubscriptionUnitStatus{
				Name:           resource.Name,
				APIVersion:     resource.APIVersion,
				Kind:           resource.Kind,
				Namespace:      resource.Namespace,
				Phase:          v1alpha1.PackagePhase(resource.Phase),
				Message:        resource.Message,
				LastUpdateTime: metaV1.Time{Time: time.Now()},
			}
			newUnitStatus = append(newUnitStatus, *uS)
		}

		klog.V(2).Infof("Subscription unit statuses:%v", newUnitStatus)

		if !foundPkgStatus {
			if appsub != nil {
				sync.recordAppSubStatusEvents(appsub, "Create", newUnitStatus)
			}

			// Create new appsubstatus
			pkgstatus = buildAppSubStatus(pkgstatusName, pkgstatusNs, appsubName,
				appsubClusterStatus.AppSub.Namespace, appsubClusterStatus.Cluster, newUnitStatus)
			klog.Infof("Creating new appsubstatus: %v/%v", pkgstatus.Namespace, pkgstatus.Name)

			// Create appsubstatus on appSub NS
			if err := sync.LocalClient.Create(context.TODO(), pkgstatus); err != nil {
				klog.Errorf("Error in creating appsubstatus:%v/%v, err:%v", appsubClusterStatus.AppSub.Namespace, pkgstatusName, err)

				return err
			}
		} else {
			if isLocalCluster && foundPkgStatus && len(pkgstatus.Statuses.SubscriptionStatus) == 1 &&
				strings.EqualFold(pkgstatus.Statuses.SubscriptionStatus[0].Kind, "HelmRelease") &&
				strings.EqualFold(pkgstatus.Statuses.SubscriptionStatus[0].APIVersion, "apps.open-cluster-management.io/v1") &&
				pkgstatus.Statuses.SubscriptionStatus[0].Phase == v1alpha1.PackageDeployFailed {
				klog.Infof("Do not skip orphan delete appsubstatus(%v/%v) for failed HelmRelease", pkgstatus.Namespace, pkgstatus.Name)

				skipOrphanDel = false
			}

			if !skipOrphanDel {
				// Find legacy unit status to be deleted - exist in the appsub itself not the appsubstatus
				legacyUnitStatuses := sync.getResourcesByLegacySubStatus(appsub)
				for _, legacyResource := range legacyUnitStatuses {
					for _, newResource := range newUnitStatus { // search for resources with same kind to find version
						if legacyResource.Kind == newResource.Kind {
							legacyResource.APIVersion = newResource.APIVersion
							break
						}
					}
				}
				// Update existing appsubstatus - only update subscription unit statuses
				oldUnitStatuses := append(pkgstatus.Statuses.SubscriptionStatus, legacyUnitStatuses...)

				// Find unit status to be deleted - exist previously but not in the new unit status
				deleteUnitStatuses := []v1alpha1.SubscriptionUnitStatus{}
				for _, oldResource := range oldUnitStatuses {
					found := false
					for _, newResource := range newUnitStatus {
						if oldResource.Name == newResource.Name &&
							oldResource.Namespace == newResource.Namespace &&
							oldResource.Kind == newResource.Kind &&
							oldResource.APIVersion == newResource.APIVersion {
							found = true
							break
						}
					}

					if !found {
						deleteUnitStatuses = append(deleteUnitStatuses, oldResource)
					}
				}

				for _, resource := range deleteUnitStatuses {
					klog.Infof("Delete subscription unit kind:%v resource:%v/%v", resource.Kind, resource.Namespace, resource.Name)

					hostSub := types.NamespacedName{
						Namespace: appsubClusterStatus.AppSub.Namespace,
						Name:      appsubName,
					}
					if err := sync.DeleteSingleSubscribedResource(hostSub, resource); err != nil {
						klog.Errorf("Error deleting subscription resource:%v", err)

						failedUnitStatus := resource.DeepCopy()
						failedUnitStatus.Phase = v1alpha1.PackageDeployFailed
						failedUnitStatus.Message = err.Error()

						newUnitStatus = append(newUnitStatus, *failedUnitStatus)
					}
				}

				// if legacy statuses exist  - remove them
				if len(legacyUnitStatuses) != 0 {
					if err := sync.LocalClient.Get(context.TODO(),
						client.ObjectKey{Name: appsub.Name, Namespace: appsub.Namespace}, appsub); err != nil {
						klog.Errorf("failed to get appsub to remove legacy statuses, err: %v", err)
					}
					appsub.Status.Statuses = appv1.SubscriptionClusterStatusMap{}
					if err := sync.LocalClient.Status().Update(context.TODO(), appsub); err != nil {
						klog.Errorf("failed to remove legacy appsub statuses, err: %v", err)
					}
				}
			}

			// If all packages are removed - delete appsubstatus
			if len(newUnitStatus) == 0 {
				klog.V(1).Infof("Delete appsubstatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
				if err := sync.LocalClient.Delete(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error delete appsubstatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatus.Name, err)
					return err
				}

				klog.V(1).Infof("Delete result from cluster AppsubReport:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
				if err := deleteAppsubReportResult(sync.RemoteClient, appsubClusterStatus.AppSub.Namespace,
					appsubName, appsubClusterStatus.Cluster, sync.standalone); err != nil {
					return err
				}

				return nil
			}

			klog.V(1).Infof("Update on managed cluster, appsubstatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)

			if appsub != nil {
				sync.recordAppSubStatusEvents(appsub, "Update", newUnitStatus)
			}

			pkgstatus.Statuses.SubscriptionStatus = newUnitStatus
			if err := sync.LocalClient.Update(context.TODO(), pkgstatus); err != nil {
				klog.Errorf("Error in updating on managed cluster, appsubstatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatusName, err)
				return err
			}
		}

		// Check if there are any package failures
		deployFailed := false

		for _, resource := range appsubClusterStatus.SubscriptionPackageStatus {
			if v1alpha1.PackagePhase(resource.Phase) == v1alpha1.PackageDeployFailed {
				deployFailed = true
				break
			}
		}

		// Update result in cluster AppsubReport
		if err := updateAppsubReportResult(sync.RemoteClient, appsubClusterStatus.AppSub.Namespace,
			appsubName, appsubClusterStatus.Cluster, deployFailed,
			sync.standalone, isLocalCluster); err != nil {
			return err
		}
	}

	if appsubClusterStatus.Action == "DELETE" {
		klog.Infof("Delete existing appsubstatus: %v/%v", pkgstatus.Namespace, pkgstatus.Name)

		failedUnitStatuses := []v1alpha1.SubscriptionUnitStatus{}
		newUnitStatus := []v1alpha1.SubscriptionUnitStatus{}

		if appsub != nil {
			// Check if the subscription still exist
			if err := sync.LocalClient.Get(context.TODO(),
				client.ObjectKey{Name: appsubName, Namespace: appsub.Namespace}, appsub); err != nil {
				if k8serrors.IsNotFound(err) {
					klog.Errorf("failed to get appsub err: %v", err)
				}
			} else {
				for _, resource := range appsubClusterStatus.SubscriptionPackageStatus {
					uS := &v1alpha1.SubscriptionUnitStatus{
						Name:           resource.Name,
						APIVersion:     resource.APIVersion,
						Kind:           resource.Kind,
						Namespace:      resource.Namespace,
						Phase:          v1alpha1.PackagePhase(resource.Phase),
						Message:        resource.Message,
						LastUpdateTime: metaV1.Time{Time: time.Now()},
					}
					newUnitStatus = append(newUnitStatus, *uS)

					if resource.Phase == string(v1alpha1.PackageDeployFailed) {
						uS := &v1alpha1.SubscriptionUnitStatus{
							Name:      resource.Name,
							Namespace: appsubClusterStatus.AppSub.Namespace,
							Phase:     v1alpha1.PackagePhase(resource.Phase),
							Message:   resource.Message,
							LastUpdateTime: metaV1.Time{
								Time: time.Now(),
							},
						}

						failedUnitStatuses = append(failedUnitStatuses, *uS)
					}
				}

			}
			sync.recordAppSubStatusEvents(appsub, "Delete", newUnitStatus)
		}

		if len(failedUnitStatuses) == 0 {
			if foundPkgStatus {
				klog.V(1).Infof("Delete from managed cluster, appsubstatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)

				if err := sync.LocalClient.Delete(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error delete from managed cluster, appsubstatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatus.Name, err)
					return err
				}
			}

			klog.V(1).Infof("Delete result from cluster AppsubReport:%v/%v", pkgstatus.Namespace, pkgstatus.Name)

			if err := deleteAppsubReportResult(sync.RemoteClient, appsubClusterStatus.AppSub.Namespace,
				appsubName, appsubClusterStatus.Cluster, sync.standalone); err != nil {
				return err
			}
		} else {
			klog.V(2).Infof("%v subscription resources failed to delete", len(failedUnitStatuses))

			if !foundPkgStatus {
				klog.Infof("appSubStatus already deleted, ignore update")
			} else {
				klog.V(1).Infof("Update on managed cluster, appsubstatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
				pkgstatus.Statuses.SubscriptionStatus = failedUnitStatuses

				if err := sync.LocalClient.Update(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error in updating on managed cluster, appsubstatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatusName, err)
					return err
				}

				// Check if there are any package failures
				deployFailed := false

				for _, resource := range appsubClusterStatus.SubscriptionPackageStatus {
					if v1alpha1.PackagePhase(resource.Phase) == v1alpha1.PackageDeployFailed {
						deployFailed = true
						break
					}
				}

				// Update result in cluster AppsubReport
				if err := updateAppsubReportResult(sync.RemoteClient, appsubClusterStatus.AppSub.Namespace,
					appsubName, appsubClusterStatus.Cluster, deployFailed,
					sync.standalone, isLocalCluster); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (sync *KubeSynchronizer) recordAppSubStatusEvents(appsub *appv1.Subscription, action string,
	pkgStatuses []v1alpha1.SubscriptionUnitStatus) {
	curUser := ""

	if encodedUser, ok := appsub.GetAnnotations()[appv1.AnnotationUserIdentity]; ok {
		curUser = utils.Base64StringDecode(encodedUser)
	}

	packageStatuses := fmt.Sprintf("AppSub: '%s/%s'; User: '%s'; Action: '%s'; ", appsub.Namespace, appsub.Name, curUser, action)
	packageStatuses += "PackageStatus: 'Name|Namespace|Apiversion|Kind|Phase|Message|LastUpdateTime"

	for _, resource := range pkgStatuses {
		pkgmsg := fmt.Sprintf(",%s|%s|%s|%s|%s|%s|%s", resource.Name, resource.Namespace,
			resource.APIVersion, resource.Kind, resource.Phase, resource.Message, resource.LastUpdateTime.Format("2006-01-02 15:04:05"))
		packageStatuses += pkgmsg
	}

	packageStatuses += "'"
	sync.eventrecorder.RecordEvent(appsub, action, packageStatuses, nil)
}

func buildAppSubStatus(statusName, statusNs, appsubName, appsubNs, cluster string,
	unitStatuses []v1alpha1.SubscriptionUnitStatus) *v1alpha1.SubscriptionStatus {
	pkgstatus := &v1alpha1.SubscriptionStatus{
		TypeMeta: metaV1.TypeMeta{
			Kind:       "SubscriptionStatus",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
	}
	pkgstatus.Namespace = statusNs
	pkgstatus.Name = statusName

	labels := map[string]string{
		"apps.open-cluster-management.io/cluster":              cluster,
		"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appsubNs+"."+appsubName),
	}
	pkgstatus.Labels = labels

	pkgstatus.Statuses.SubscriptionStatus = unitStatuses

	return pkgstatus
}

func updateAppsubReportResult(rClient client.Client, appsubNs, appsubName,
	clusterAppsubReportNs string, deployFailed, standalone, isLocalCluster bool) error {
	// For managed clusters, get cluster AppsubReport
	var appsubReport *v1alpha1.SubscriptionReport

	var err error

	if standalone {
		// Check if a hosting-subscription exists
		appsub := &appv1.Subscription{}

		if isLocalCluster && !strings.HasSuffix(appsubName, localSuffix) {
			appsubName += localSuffix
		}

		if err := rClient.Get(context.TODO(),
			client.ObjectKey{Name: appsubName, Namespace: appsubNs}, appsub); err != nil {
			klog.Errorf("failed to appsub to check host-subscription for deployment from standalone controller, err: %v", err)
			return err
		}

		annotations := appsub.GetAnnotations()
		if annotations == nil || annotations["apps.open-cluster-management.io/hosting-subscription"] == "" {
			klog.Infof("Standalone appsub, skip create/update of entry in cluter report")
			return nil
		}

		if strings.HasSuffix(appsubName, localSuffix) {
			appsubName = appsubName[:len(appsubName)-6]
		}

		clusterAppsubReportNs = localCluster

		klog.V(1).Infof("Standalone appsub for helm, continue")
	}

	appsubReport, err = getClusterAppsubReport(rClient, clusterAppsubReportNs, true)
	if err != nil {
		klog.Errorf("Error getting cluster AppsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)
		return err
	}

	result := v1alpha1.SubscriptionResult("deployed")
	if deployFailed {
		result = v1alpha1.SubscriptionResult("failed")
	}

	// Update result in AppsubReport
	prResultFoundIndex := -1

	prResultSource := appsubNs + "/" + appsubName

	for i, result := range appsubReport.Results {
		if result.Source == prResultSource {
			prResultFoundIndex = i
			break
		}
	}

	klog.V(1).Infof("Update AppsubReport: %v/%v, resultIndex:%v", appsubReport.Namespace, appsubReport.Name, prResultFoundIndex)

	if prResultFoundIndex < 0 {
		klog.V(1).Infof("Add result (source:%v) to appsubReport", prResultSource)

		prFailedResult := &v1alpha1.SubscriptionReportResult{
			Source:    prResultSource,
			Result:    result,
			Timestamp: metaV1.Timestamp{Seconds: time.Now().Unix()},
		}
		appsubReport.Results = append(appsubReport.Results, prFailedResult)
	} else if prResultFoundIndex >= 0 && appsubReport.Results[prResultFoundIndex].Result != result {
		appsubReport.Results[prResultFoundIndex].Result = result
	} else {
		return nil
	}

	if err := rClient.Update(context.TODO(), appsubReport); err != nil {
		klog.Errorf("Error in updating on hub, AppsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)
		return err
	}

	return nil
}

func deleteAppsubReportResult(rClient client.Client, appsubNs, appsubName, clusterAppsubReportNs string,
	standalone bool) error {
	source := appsubNs + "/" + appsubName
	klog.V(1).Infof("Delete AppsubReport result, Namespace:%v, source:%v", clusterAppsubReportNs, source)

	// For managed clusters, get cluster appsubReport, for standalone get app appsubReport
	var appsubReport *v1alpha1.SubscriptionReport

	var err error

	if standalone {
		// Check if a hosting-subscription exists
		appsub := &appv1.Subscription{}

		if err := rClient.Get(context.TODO(),
			client.ObjectKey{Name: appsubName, Namespace: appsubNs}, appsub); err != nil {
			klog.Errorf("failed to appsub to check host-subscription for deployment from standalone controller, err: %v", err)
			return err
		}

		annotations := appsub.GetAnnotations()
		if annotations == nil || annotations["apps.open-cluster-management.io/hosting-subscription"] == "" {
			klog.Infof("Standalone appsub, skip delete entry from cluster report")
			return nil
		}

		clusterAppsubReportNs = localCluster

		klog.V(1).Infof("Standalone appsub for helm, continue")
	}

	appsubReport, err = getClusterAppsubReport(rClient, clusterAppsubReportNs, false)

	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Cluster AppsubReport not found:%v/%v, skip deleting appsubReport result source:%v", appsubReport.Namespace, appsubReport.Name, source)
			return nil
		}

		klog.Errorf("Error getting cluster appsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)

		return err
	}

	// Find the corresponding result from the appsubReport and remove it if it exists
	prResultFoundIndex := -1

	for i, result := range appsubReport.Results {
		if result.Source == source {
			prResultFoundIndex = i
			break
		}
	}

	klog.V(1).Infof("Update appsubReport: %v/%v, resultIndex:%v", appsubReport.Namespace, appsubReport.Name, prResultFoundIndex)

	if prResultFoundIndex >= 0 {
		appsubReport.Results = append(appsubReport.Results[:prResultFoundIndex], appsubReport.Results[prResultFoundIndex+1:]...)
		if err := rClient.Update(context.TODO(), appsubReport); err != nil {
			klog.Errorf("Error in updating on hub, appsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)
			return err
		}
	} else {
		klog.V(2).Infof("Result (source:%v) not found in appsubReport, no update required to cluster appsubReport", source)
	}

	return nil
}

func getClusterAppsubReport(rClient client.Client, clusterAppsubReportNs string, create bool) (*v1alpha1.SubscriptionReport, error) {
	appsubReport := &v1alpha1.SubscriptionReport{
		TypeMeta: metaV1.TypeMeta{
			Kind:       "SubscriptionReport",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
	}

	appsubReport.Namespace = clusterAppsubReportNs
	appsubReport.Name = clusterAppsubReportNs
	klog.V(1).Infof("Get cluster appSubReport: %v/%v", appsubReport.Namespace, appsubReport.Name)

	if err := rClient.Get(context.TODO(),
		client.ObjectKey{Name: appsubReport.Name, Namespace: appsubReport.Namespace}, appsubReport); err != nil {
		if errors.IsNotFound(err) {
			if create {
				klog.V(1).Infof("AppsubReport: %v/%v not found, create it.", appsubReport.Namespace, appsubReport.Name)

				labels := map[string]string{
					"apps.open-cluster-management.io/cluster": "true",
				}
				appsubReport.Labels = labels
				appsubReport.ReportType = "Cluster"

				// Set summary stats to "n/a"
				appsubReport.Summary.Clusters = "n/a"
				appsubReport.Summary.Deployed = "n/a"
				appsubReport.Summary.InProgress = "n/a"
				appsubReport.Summary.Failed = "n/a"
				appsubReport.Summary.PropagationFailed = "n/a"

				if err := rClient.Create(context.TODO(), appsubReport); err != nil {
					klog.Errorf("Error in creating on hub, appsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)
					return appsubReport, err
				}
			} else {
				return appsubReport, err
			}
		} else {
			klog.Errorf("Error getting AppsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)
			return appsubReport, err
		}
	}

	return appsubReport, nil
}

func shouldSkip(appsubClusterStatus SubscriptionClusterStatus, foundPkgStatus bool,
	pkgstatus v1alpha1.SubscriptionStatus) bool {
	if len(appsubClusterStatus.SubscriptionPackageStatus) == 1 &&
		strings.EqualFold(appsubClusterStatus.SubscriptionPackageStatus[0].Kind, "HelmRelease") &&
		strings.EqualFold(appsubClusterStatus.SubscriptionPackageStatus[0].APIVersion, "apps.open-cluster-management.io/v1") &&
		appsubClusterStatus.SubscriptionPackageStatus[0].Phase != string(v1alpha1.PackageDeployFailed) {
		if foundPkgStatus &&
			appsubClusterStatus.SubscriptionPackageStatus[0].Name != pkgstatus.Statuses.SubscriptionStatus[0].Name {
			klog.Infof("Update the HelmRelease kind resource to appsub status because HelmRelease changed. appsub: %v/%v",
				appsubClusterStatus.AppSub.Namespace, appsubClusterStatus.AppSub.Name)
		} else {
			klog.Infof("Don't update the HelmRelease kind resource to appsub status. appsub: %v/%v",
				appsubClusterStatus.AppSub.Namespace, appsubClusterStatus.AppSub.Name)

			return true
		}
	}

	return false
}

func (sync *KubeSynchronizer) getResourcesByLegacySubStatus(appsub *appv1.Subscription) []v1alpha1.SubscriptionUnitStatus {
	appsubStatuses := []v1alpha1.SubscriptionUnitStatus{}
	if appsub == nil {
		return appsubStatuses
	}

	if i := strings.Index(appsub.Spec.Channel, "/"); i != -1 {
		chnNamespace := appsub.Spec.Channel[i+1:]
		appsubStatusMap := appsub.Status.Statuses

		for _, v := range appsubStatusMap {
			for pkgName := range v.SubscriptionPackageStatus {
				// pkgName ex: acm-hive-openshift-releases-chn-0-ClusterImageSet-img4.6.3-x86-64-appsub
				resource := strings.TrimPrefix(pkgName, chnNamespace+"-") // trim away namespace
				// grab start of resource name
				if idx := strings.Index(resource, "-"); idx != -1 {
					resourceKind := resource[:idx]   // grab kind name
					resourceName := resource[idx+1:] // grab resource name

					appsubStatus := v1alpha1.SubscriptionUnitStatus{}
					appsubStatus.Kind = resourceKind
					appsubStatus.Name = resourceName
					appsubStatuses = append(appsubStatuses, appsubStatus)
				}
			}
		}
	}

	return appsubStatuses
}
