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

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	policyReportV1alpha2 "sigs.k8s.io/wg-policy-prototypes/policy-report/pkg/api/wgpolicyk8s.io/v1alpha2"
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

func (sync *KubeSynchronizer) SyncAppsubClusterStatus(appsubClusterStatus SubscriptionClusterStatus) error {
	klog.V(1).Infof("cluster: %v, appsub: %v/%v, action: %v, hub:%v, standalone:%v\n", appsubClusterStatus.Cluster,
		appsubClusterStatus.AppSub.Namespace, appsubClusterStatus.AppSub.Name, appsubClusterStatus.Action, sync.hub, sync.standalone)

	// Get existing appsubpackagestatus on managed cluster, if it exists
	isLocalCluster := sync.hub && !sync.standalone

	appsubName := appsubClusterStatus.AppSub.Name
	pkgstatusNs := appsubClusterStatus.AppSub.Namespace
	pkgstatusName := pkgstatusNs + "." + appsubName

	if isLocalCluster {
		if strings.HasSuffix(appsubName, "-local") {
			appsubName = appsubName[:len(appsubName)-6]
			pkgstatusName = pkgstatusNs + "." + appsubName
		}

		pkgstatusNs = appsubClusterStatus.Cluster
	}

	pkgstatus := &v1alpha1.SubscriptionPackageStatus{}

	foundPkgStatus := true
	if err := sync.LocalClient.Get(context.TODO(),
		client.ObjectKey{Name: pkgstatusName, Namespace: pkgstatusNs}, pkgstatus); err != nil {

		if errors.IsNotFound(err) {
			foundPkgStatus = false
		} else {
			return err
		}
	}

	if appsubClusterStatus.Action == "APPLY" {
		newUnitStatus := []v1alpha1.SubscriptionUnitStatus{}
		for _, resource := range appsubClusterStatus.SubscriptionPackageStatus {
			klog.V(1).Infof("resource status - Name: %v, Namespace: %v, Apiversion: %v, Kind: %v, Phase: %v, Message: %v\n",
				resource.Name, resource.Namespace, resource.ApiVersion, resource.Kind, resource.Phase, resource.Message)

			uS := &v1alpha1.SubscriptionUnitStatus{
				Name:           resource.Name,
				ApiVersion:     resource.ApiVersion,
				Kind:           resource.Kind,
				Namespace:      appsubClusterStatus.AppSub.Namespace,
				Phase:          v1alpha1.PackagePhase(resource.Phase),
				Message:        resource.Message,
				LastUpdateTime: metaV1.Time{Time: time.Now()},
			}
			newUnitStatus = append(newUnitStatus, *uS)
		}

		klog.V(2).Infof("Subscription unit statuses:%v", newUnitStatus)

		if !foundPkgStatus {
			// Create new appsubpackagestatus
			pkgstatus = buildAppSubPackageStatus(pkgstatusName, pkgstatusNs, appsubName,
				appsubClusterStatus.AppSub.Namespace, appsubClusterStatus.Cluster, newUnitStatus)
			klog.Infof("Creating new appsubpackagestatus: %v/%v", pkgstatus.Namespace, pkgstatus.Name)

			// Create appsubpackagestatus on appSub NS
			if err := sync.LocalClient.Create(context.TODO(), pkgstatus); err != nil {
				klog.Errorf("Error in creating on managed cluster, appsubpackagestatus:%v/%v, err:%v", appsubClusterStatus.AppSub.Namespace, pkgstatusName, err)
				return err
			}
		} else {
			klog.Infof("Update existing appsubpackagestatus: %v/%v", pkgstatus.Namespace, pkgstatus.Name)

			// Update existing appsubpackagestatus - only update subscription unit statuses
			oldUnitStatuses := pkgstatus.Statuses.SubscriptionPackageStatus

			// Find unit status to be deleted - exist previously but not in the new unit status
			deleteUnitStatuses := []v1alpha1.SubscriptionUnitStatus{}
			for _, oldResource := range oldUnitStatuses {

				found := false
				for _, newResource := range newUnitStatus {
					if oldResource.Name == newResource.Name &&
						oldResource.Namespace == newResource.Namespace &&
						oldResource.Kind == newResource.Kind &&
						oldResource.ApiVersion == newResource.ApiVersion {

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

			// If all packages are removed - delete appsubpackagestatus
			if len(newUnitStatus) == 0 {
				if isLocalCluster {
					pkgstatus.Namespace = appsubClusterStatus.Cluster
				}

				klog.V(1).Infof("Delete  appsubpackagestatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
				if err := sync.LocalClient.Delete(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error delete appsubpackagestatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatus.Name, err)
					return err
				}

				klog.V(1).Infof("Delete result from cluster policy report:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
				if err := deletePolicyReportResult(sync.RemoteClient, appsubClusterStatus.AppSub.Namespace,
					appsubName, appsubClusterStatus.Cluster, sync.standalone, appsubClusterStatus.SubscriptionPackageStatus); err != nil {
					return err
				}

				return nil
			}

			klog.V(1).Infof("Update on managed cluster, appsubpackagestatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
			pkgstatus.Statuses.SubscriptionPackageStatus = newUnitStatus
			if err := sync.LocalClient.Update(context.TODO(), pkgstatus); err != nil {
				klog.Errorf("Error in updating on managed cluster, appsubpackagestatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatusName, err)
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

		// Update result in cluster policy report
		if err := updatePolicyReportResult(sync.RemoteClient, appsubClusterStatus.AppSub.Namespace,
			appsubName, appsubClusterStatus.Cluster, deployFailed, sync.standalone, appsubClusterStatus.SubscriptionPackageStatus); err != nil {
			return err
		}
	}

	if appsubClusterStatus.Action == "DELETE" {
		klog.Infof("Delete existing appsubpackagestatus: %v/%v", pkgstatus.Namespace, pkgstatus.Name)

		failedUnitStatuses := []v1alpha1.SubscriptionUnitStatus{}
		for _, resource := range appsubClusterStatus.SubscriptionPackageStatus {
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

		if isLocalCluster {
			pkgstatus.Namespace = appsubClusterStatus.Cluster
		}

		if len(failedUnitStatuses) == 0 {
			if foundPkgStatus {
				klog.V(1).Infof("Delete from managed cluster, appsubpackagestatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
				if err := sync.LocalClient.Delete(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error delete from managed cluster, appsubpackagestatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatus.Name, err)
					return err
				}

				klog.V(1).Infof("Delete result from cluster policy report:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
				if err := deletePolicyReportResult(sync.RemoteClient, appsubClusterStatus.AppSub.Namespace,
					appsubName, appsubClusterStatus.Cluster, sync.standalone, appsubClusterStatus.SubscriptionPackageStatus); err != nil {
					return err
				}
			}
		} else {
			klog.V(2).Infof("%v subscription resources failed to delete", len(failedUnitStatuses))

			if !foundPkgStatus {
				// TODO: appsubpackagestatus doesn't exist - recreate it?
				klog.Infof("appSubPackageStatus already deleted, ignore update")
			} else {
				klog.V(1).Infof("Update on managed cluster, appsubpackagestatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
				pkgstatus.Statuses.SubscriptionPackageStatus = failedUnitStatuses
				if err := sync.LocalClient.Update(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error in updating on managed cluster, appsubpackagestatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatusName, err)
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

				// Update result in cluster policy report
				if err := updatePolicyReportResult(sync.RemoteClient, appsubClusterStatus.AppSub.Namespace,
					appsubName, appsubClusterStatus.Cluster, deployFailed, sync.standalone, appsubClusterStatus.SubscriptionPackageStatus); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func buildAppSubPackageStatus(statusName, statusNs, appsubName, appsubNs, cluster string,
	unitStatuses []v1alpha1.SubscriptionUnitStatus) *v1alpha1.SubscriptionPackageStatus {

	pkgstatus := &v1alpha1.SubscriptionPackageStatus{}
	pkgstatus.Namespace = statusNs
	pkgstatus.Name = statusName

	labels := map[string]string{
		"apps.open-cluster-management.io/cluster":              cluster,
		"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appsubNs+"."+appsubName),
	}
	pkgstatus.Labels = labels

	pkgstatus.Statuses.SubscriptionPackageStatus = unitStatuses
	return pkgstatus
}

func updatePolicyReportResult(rClient client.Client, appsubNs, appsubName, clusterPolicyReportNs string, deployFailed, standalone bool, appsubUnitStatus []SubscriptionUnitStatus) error {
	// For managed clusters, get cluster policy reports, for standalone get app policy report
	var policyReport *policyReportV1alpha2.PolicyReport
	var err error
	if standalone {
		policyReport, err = getAppPolicyReport(rClient, appsubNs, appsubName, appsubUnitStatus)
		if err != nil {
			klog.Errorf("Error getting app policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
			return err
		}
	} else {
		policyReport, err = getClusterPolicyReport(rClient, appsubNs, appsubName, clusterPolicyReportNs, true)
		if err != nil {
			klog.Errorf("Error getting cluster policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
			return err
		}
	}

	// Update result in policy report
	prResultFoundIndex := -1
	prResultSource := appsubNs + "/" + appsubName
	for i, result := range policyReport.Results {
		if result.Source == prResultSource && result.Policy == "APPSUB_FAILURE" {
			prResultFoundIndex = i
			break
		}
	}
	klog.V(1).Infof("Update policy report: %v/%v, resultIndex:%v", policyReport.Namespace, policyReport.Name, prResultFoundIndex)

	// deploy failed and found result, or deploy success and not found result
	if (prResultFoundIndex >= 0) == deployFailed {
		// Only for standalone successful deployments that needs "pass" count updated in the summary
		// No update needed for all others
		if !standalone || deployFailed || policyReport.Summary.Pass == 1 {
			return nil
		}
	}

	if prResultFoundIndex < 0 && deployFailed {
		// Deploy failed but result not in policy report - add it
		klog.V(1).Infof("Add result (source:%v) to policy report", prResultSource)

		prFailedResult := &policyReportV1alpha2.PolicyReportResult{
			Source:    prResultSource,
			Policy:    "APPSUB_FAILURE",
			Result:    "fail",
			Timestamp: metaV1.Timestamp{Seconds: time.Now().Unix()},
		}
		policyReport.Results = append(policyReport.Results, prFailedResult)
	} else if prResultFoundIndex >= 0 && !deployFailed {
		// Deploy success but result found policy report - remove it
		klog.V(1).Infof("Delete result (source:%v) from policy report", prResultSource)

		policyReport.Results = append(policyReport.Results[:prResultFoundIndex], policyReport.Results[prResultFoundIndex+1:]...)
	}

	// Update summary for app policy report only
	if standalone {
		if deployFailed {
			policyReport.Summary.Fail = 1
			policyReport.Summary.Pass = 0
		} else {
			policyReport.Summary.Fail = 0
			policyReport.Summary.Pass = 1
		}
	}

	if err := rClient.Update(context.TODO(), policyReport); err != nil {
		klog.Errorf("Error in updating on hub, policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
		return err
	}

	return nil
}

func deletePolicyReportResult(rClient client.Client, appsubNs, appsubName, clusterPolicyReportNs string, standalone bool, appsubUnitStatus []SubscriptionUnitStatus) error {
	source := appsubNs + "/" + appsubName
	klog.V(1).Infof("Delete policy report result, Namespace:%v, source:%v", clusterPolicyReportNs, source)

	// For managed clusters, get cluster policy reports, for standalone get app policy report
	var policyReport *policyReportV1alpha2.PolicyReport
	var err error
	if standalone {
		policyReport, err = getAppPolicyReport(rClient, appsubNs, appsubName, appsubUnitStatus)
		if err != nil {
			klog.Errorf("Error getting app policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
			return err
		}
	} else {
		policyReport, err = getClusterPolicyReport(rClient, appsubNs, appsubName, clusterPolicyReportNs, false)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Infof("Cluster policyReport not found:%v/%v, skip deleting policy report result source:%v", policyReport.Namespace, policyReport.Name, source)
				return nil
			} else {
				klog.Errorf("Error getting cluster policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
				return err
			}
		}
	}

	// Find the corresponding result from the policy report and remove it if it exists
	prResultFoundIndex := -1
	for i, result := range policyReport.Results {
		if result.Source == source && result.Policy == "APPSUB_FAILURE" {
			prResultFoundIndex = i
			break
		}
	}
	klog.V(1).Infof("Update policy report: %v/%v, resultIndex:%v", policyReport.Namespace, policyReport.Name, prResultFoundIndex)

	if prResultFoundIndex >= 0 {
		policyReport.Results = append(policyReport.Results[:prResultFoundIndex], policyReport.Results[prResultFoundIndex+1:]...)
		if err := rClient.Update(context.TODO(), policyReport); err != nil {
			klog.Errorf("Error in updating on hub, policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
			return err
		}
	} else {
		klog.V(2).Infof("result (source:%v) not found in policyReport, no update required to cluster policy", source)
	}

	return nil
}

func getClusterPolicyReport(rClient client.Client, appsubNs, appsubName, clusterPolicyReportNs string, create bool) (*policyReportV1alpha2.PolicyReport, error) {
	policyReport := &policyReportV1alpha2.PolicyReport{}
	policyReport.Namespace = clusterPolicyReportNs
	policyReport.Name = "policyreport-appsub-status"
	klog.V(1).Infof("Get cluster policy report: %v/%v", policyReport.Namespace, policyReport.Name)

	if err := rClient.Get(context.TODO(),
		client.ObjectKey{Name: policyReport.Name, Namespace: policyReport.Namespace}, policyReport); err != nil {
		if errors.IsNotFound(err) {
			if create {
				klog.V(1).Infof("Policy report: %v/%v not found, create it.", policyReport.Namespace, policyReport.Name)

				labels := map[string]string{
					"apps.open-cluster-management.io/cluster": "true",
				}
				policyReport.Labels = labels

				if err := rClient.Create(context.TODO(), policyReport); err != nil {
					klog.Errorf("Error in creating on hub, policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
					return policyReport, err
				}
			} else {
				return policyReport, err
			}
		} else {
			klog.Errorf("Error getting policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
			return policyReport, err
		}
	}

	return policyReport, nil
}

func getAppPolicyReport(rClient client.Client, appsubNs, appsubName string, appsubUnitStatus []SubscriptionUnitStatus) (*policyReportV1alpha2.PolicyReport, error) {
	policyReport := &policyReportV1alpha2.PolicyReport{}
	policyReport.Name = "policyreport-app-" + appsubName
	policyReport.Namespace = appsubNs

	policyReportFound := true
	if err := rClient.Get(context.TODO(),
		client.ObjectKey{Name: policyReport.Name, Namespace: policyReport.Namespace}, policyReport); err != nil {
		if errors.IsNotFound(err) {
			policyReportFound = false
		} else {
			klog.Errorf("Error getting policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
			return policyReport, err
		}
	}

	if !policyReportFound {
		klog.V(1).Infof("App policy report: %v/%v not found, create it.", policyReport.Namespace, policyReport.Name)

		policyReport.Labels = map[string]string{
			"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appsubNs+"."+appsubName),
		}

		// Add resource list
		resources := []*v1.ObjectReference{}
		for _, sus := range appsubUnitStatus {
			resource := &v1.ObjectReference{
				Kind:       sus.Kind,
				Namespace:  sus.Namespace,
				Name:       sus.Name,
				APIVersion: sus.ApiVersion,
			}
			resources = append(resources, resource)
		}

		results := []*policyReportV1alpha2.PolicyReportResult{}
		result := &policyReportV1alpha2.PolicyReportResult{
			Source:    appsubNs + "/" + appsubName,
			Policy:    "APPSUB_RESOURCE_LIST",
			Timestamp: metaV1.Timestamp{Seconds: time.Now().Unix()},
			Result:    "pass",
			Subjects:  resources,
		}
		results = append(results, result)
		policyReport.Results = results

		if err := rClient.Create(context.TODO(), policyReport); err != nil {
			klog.Errorf("Error in creating app policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
			return policyReport, err
		}
	}

	return policyReport, nil
}
