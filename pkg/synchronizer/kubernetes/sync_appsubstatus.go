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
	klog.V(1).Infof("cluster: %v\n", appsubClusterStatus.Cluster)
	klog.V(1).Infof("appsub: %v\n", appsubClusterStatus.AppSub.Namespace)
	klog.V(1).Infof("action: %v\n", appsubClusterStatus.Action)

	// Get existing appsubpackagestatus on managed cluster, if it exists
	appsubName := appsubClusterStatus.AppSub.Namespace + "." + appsubClusterStatus.AppSub.Name
	pkgstatusName := appsubClusterStatus.AppSub.Name
	pkgstatus := &v1alpha1.SubscriptionPackageStatus{}

	foundPkgStatus := true
	if err := sync.LocalClient.Get(context.TODO(),
		client.ObjectKey{Name: pkgstatusName,
			Namespace: appsubClusterStatus.AppSub.Namespace}, pkgstatus); err != nil {

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
				Name:      resource.Name,
				Namespace: appsubClusterStatus.AppSub.Namespace,
				Phase:     v1alpha1.PackagePhase(resource.Phase),
				Message:   resource.Message,
				LastUpdateTime: metaV1.Time{
					Time: time.Now(),
				},
			}
			newUnitStatus = append(newUnitStatus, *uS)
			// sync.LocalClient: go cached client for managed cluster
			// sync.RemoteClient: go cache client for hub cluster
			// sync.SynchronizerID.Name:  managed cluster name, same as appsubClusterStatus.Cluster
			// appsubClusterStatus.AppSub: appsub namespaced name

		}
		klog.Infof("Subscription unit statuses:%v", newUnitStatus)

		if !foundPkgStatus {
			// Create new appsubpackagestatus
			pkgstatus.Name = pkgstatusName
			pkgstatus.Namespace = appsubClusterStatus.AppSub.Namespace

			klog.Infof("Creating new appsubpackagestatus: %v/%v", pkgstatus.Namespace, pkgstatus.Name)

			labels := map[string]string{
				"apps.open-cluster-management.io/cluster":              appsubClusterStatus.Cluster,
				"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appsubName),
			}
			pkgstatus.Labels = labels

			pkgstatus.Statuses.SubscriptionPackageStatus = newUnitStatus

			if err := sync.LocalClient.Create(context.TODO(), pkgstatus); err != nil {
				klog.Errorf("Error in creating appsubpackagestatus:%v for cluster:%v, err:%v", pkgstatusName, appsubClusterStatus.Cluster, err)
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
					Name:      appsubClusterStatus.AppSub.Name,
				}
				if err := sync.DeleteSingleSubscribedResource(hostSub, resource); err != nil {
					klog.Errorf("Error deleting subscription unit status:%v", err)

					failedUnitStatus := resource.DeepCopy()
					failedUnitStatus.Phase = v1alpha1.PackageDeployFailed
					failedUnitStatus.Message = err.Error()

					newUnitStatus = append(newUnitStatus, *failedUnitStatus)
				}
			}

			pkgstatus.Statuses.SubscriptionPackageStatus = newUnitStatus
			if err := sync.LocalClient.Update(context.TODO(), pkgstatus); err != nil {
				klog.Errorf("Error in updating appsubpackagestatus:%v for cluster:%v, err:%v", pkgstatusName, appsubClusterStatus.Cluster, err)
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
				if err := sync.LocalClient.Delete(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error delete package status:%v/%v from managed cluster:%v, err:%v", pkgstatus.Namespace, pkgstatus.Name, appsubClusterStatus.Cluster, err)
					return err
				}
			}
		} else {
			klog.V(2).Infof("%v subsription unit statuses failed to delete", len(failedUnitStatuses))

			if !foundPkgStatus {
				// TODO: appsubpackagestatus doesn't exist - do we recreate it?
				klog.Infof("appSubPackageStatus already deleted, ignore update")
			} else {
				pkgstatus.Statuses.SubscriptionPackageStatus = failedUnitStatuses
				if err := sync.LocalClient.Update(context.TODO(), pkgstatus); err != nil {
					klog.Errorf("Error in updating appsubpackagestatus:%v for cluster:%v, err:%v", pkgstatusName, appsubClusterStatus.Cluster, err)
					return err
				}
			}
		}
	}

	// TODO: Sync to hub

	return nil
}

func SyncAppsubClusterStatusToHub(remoteClient client.Client, appsubPackageStatus v1alpha1.SubscriptionPackageStatus, action string) error {
	klog.Infof("Sync appSubPackageStatus:%v/%v to hub with action:%v", appsubPackageStatus.Namespace, appsubPackageStatus.Name, action)

	pkgstatus := &v1alpha1.SubscriptionPackageStatus{}
	if err := remoteClient.Get(context.TODO(),
		client.ObjectKey{Name: appsubPackageStatus.Name,
			Namespace: appsubPackageStatus.Namespace}, pkgstatus); err != nil {

	}

	return nil
}
