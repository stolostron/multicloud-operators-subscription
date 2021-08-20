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

	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	klog.V(1).Infof("cluster: %v, appsub: %v/%v, action: %v\n", appsubClusterStatus.Cluster,
		appsubClusterStatus.AppSub.Namespace, appsubClusterStatus.AppSub.Name, appsubClusterStatus.Action)

	// Get existing appsubpackagestatus on managed cluster, if it exists
	isLocalCluster := appsubClusterStatus.Cluster == "local-cluster"

	appsubName := appsubClusterStatus.AppSub.Name
	pkgstatusName := appsubClusterStatus.AppSub.Name
	pkgstatusNs := appsubClusterStatus.AppSub.Namespace

	if isLocalCluster {
		if strings.HasSuffix(pkgstatusName, "-local") {
			appsubName = pkgstatusName[:len(pkgstatusName)-6]
			pkgstatusName = appsubName
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

			// Skip creation of appsubpackagestatus on appSub NS if on local-cluster
			if !isLocalCluster {
				if err := sync.LocalClient.Create(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error in creating on managed cluster, appsubpackagestatus:%v/%v, err:%v", appsubClusterStatus.AppSub.Namespace, pkgstatusName, err)
					return err
				}
			}

			// Sync to hub
			klog.Infof("Sync appSubPackageStatus:%v/%v to hub", pkgstatus.Namespace, pkgstatus.Name)
			hubPkgstatus := pkgstatus.DeepCopy()
			hubPkgstatus.Namespace = appsubClusterStatus.Cluster
			hubPkgstatus.ResourceVersion = ""

			if err := sync.RemoteClient.Create(context.TODO(), hubPkgstatus); err != nil {
				klog.Errorf("Error in creating on hub, appsubpackagestatus:%v/%v, err:%v", appsubClusterStatus.Cluster, pkgstatusName, err)
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
				klog.V(2).Infof("Delete subscription unit status:%v/%v", resource.Namespace, resource.Name)

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
				// No appsubpackagestatus on appSub NS on local-cluster - skip
				if !isLocalCluster {
					klog.V(2).Infof("Delete from managed cluster, appsubpackagestatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
					if err := sync.LocalClient.Delete(context.TODO(), pkgstatus); err != nil {
						klog.Errorf("Error delete from managed cluster, appsubpackagestatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatus.Name, err)
						return err
					}
				}

				if err := deleteAppSubPackageStatusOnHub(sync.RemoteClient, pkgstatusName, appsubClusterStatus.Cluster); err != nil {
					return err
				}

				return nil
			}

			// No appsubpackagestatus on appSub NS on local-cluster - skip
			if !isLocalCluster {
				pkgstatus.Statuses.SubscriptionPackageStatus = newUnitStatus
				if err := sync.LocalClient.Update(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error in updating on managed cluster, appsubpackagestatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatusName, err)
					return err
				}
			}

			// Update appsubpackagestatus from hub
			if err := updateAppSubPackageStatusOnHub(sync.RemoteClient, pkgstatusName, appsubClusterStatus.Cluster,
				appsubName, appsubClusterStatus.AppSub.Namespace, newUnitStatus); err != nil {
				return err
			}
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

		if len(failedUnitStatuses) == 0 {
			if foundPkgStatus {
				// No appsubpackagestatus on appSub NS on local-cluster - skip
				if !isLocalCluster {
					klog.V(2).Infof("Delete from managed cluster, appsubpackagestatus:%v/%v", pkgstatus.Namespace, pkgstatus.Name)
					if err := sync.LocalClient.Delete(context.TODO(), pkgstatus); err != nil {
						klog.Errorf("Error delete from managed cluster, appsubpackagestatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatus.Name, err)
						return err
					}
				}

				// Delete appsubpackagestatus from hub
				if err := deleteAppSubPackageStatusOnHub(sync.RemoteClient, pkgstatusName, appsubClusterStatus.Cluster); err != nil {
					return err
				}
			}
		} else {
			klog.V(2).Infof("%v subscription unit statuses failed to delete", len(failedUnitStatuses))

			if !foundPkgStatus {
				// TODO: appsubpackagestatus doesn't exist - do we recreate it?
				klog.Infof("appSubPackageStatus already deleted, ignore update")
			} else {
				// No appsubpackagestatus on appSub NS on local-cluster - skip
				if !isLocalCluster {
					pkgstatus.Statuses.SubscriptionPackageStatus = failedUnitStatuses
					if err := sync.LocalClient.Update(context.TODO(), pkgstatus); err != nil {
						klog.Errorf("Error in updating on managed cluster, appsubpackagestatus:%v/%v, err:%v", pkgstatus.Namespace, pkgstatusName, err)
						return err
					}
				}

				// Update appsubpackagestatus from hub
				if err := updateAppSubPackageStatusOnHub(sync.RemoteClient, pkgstatusName, appsubClusterStatus.Cluster,
					appsubName, appsubClusterStatus.AppSub.Namespace, failedUnitStatuses); err != nil {
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
	pkgstatus.Name = statusName
	pkgstatus.Namespace = statusNs

	labels := map[string]string{
		"apps.open-cluster-management.io/cluster":              cluster,
		"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appsubNs+"."+appsubName),
	}
	pkgstatus.Labels = labels

	pkgstatus.Statuses.SubscriptionPackageStatus = unitStatuses
	return pkgstatus
}

func updateAppSubPackageStatusOnHub(rClient client.Client, pkgstatusName, pkgstatusNs, appsubName, appsubNs string, unitStatuses []v1alpha1.SubscriptionUnitStatus) error {
	klog.V(2).Infof("Update on hub, appsubpackagestatus:%v/%v", pkgstatusNs, pkgstatusName)

	hubPkgStatus := &v1alpha1.SubscriptionPackageStatus{}
	if err := rClient.Get(context.TODO(),
		client.ObjectKey{Name: pkgstatusName, Namespace: pkgstatusNs}, hubPkgStatus); err != nil {

		// appsubpackagestatus doesn't exist on the hub - create it
		if errors.IsNotFound(err) {
			klog.V(2).Infof("appsubpackagestatus:%v/%v not found on hub, creating...", pkgstatusNs, pkgstatusName)

			hubPkgStatus := buildAppSubPackageStatus(pkgstatusName, pkgstatusNs,
				appsubName, appsubNs, pkgstatusNs, unitStatuses)

			if err := rClient.Create(context.TODO(), hubPkgStatus); err != nil {
				klog.Errorf("Error in creating on hub, appsubpackagestatus:%v/%v, err:%v", pkgstatusNs, pkgstatusName, err)
				return err
			}

			return nil
		}

		return err
	}

	// Update appsubpackagestatus on the hub
	hubPkgStatus.Statuses.SubscriptionPackageStatus = unitStatuses
	if err := rClient.Update(context.TODO(), hubPkgStatus); err != nil {
		klog.Errorf("Error in updating on hub, appsubpackagestatus:%v/%v, err:%v", hubPkgStatus.Namespace, hubPkgStatus.Name, err)
		return err
	}

	return nil
}

func deleteAppSubPackageStatusOnHub(rClient client.Client, pkgstatusName, pkgstatusNs string) error {
	klog.V(2).Infof("Delete from hub, appsubpackagestatus:%v/%v", pkgstatusNs, pkgstatusName)

	hubPkgStatus := &v1alpha1.SubscriptionPackageStatus{}
	if err := rClient.Get(context.TODO(),
		client.ObjectKey{Name: pkgstatusName,
			Namespace: pkgstatusNs}, hubPkgStatus); err != nil {

		if errors.IsNotFound(err) {
			// hub appsubpackagestatus is already deleted - ignore
			klog.V(2).Infof("appsubpackagestatus:%v/%v not found on hub, may be deleted already", pkgstatusNs, pkgstatusName)
			return nil
		}

		return err
	}

	if err := rClient.Delete(context.TODO(), hubPkgStatus); err != nil {
		klog.Errorf("Error delete from hub, appsubpackagestatus:%v/%v, err:%v", pkgstatusNs, pkgstatusName, err)
		return err
	}

	return nil
}
