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
	"k8s.io/klog"
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

	for _, resource := range appsubClusterStatus.SubscriptionPackageStatus {
		klog.V(1).Infof("resource status - Name: %v, Namespace: %v, Apiversion: %v, Kind: %v, Phase: %v, Message: %v\n",
			resource.Name, resource.Namespace, resource.ApiVersion, resource.Kind, resource.Phase, resource.Message)

		// sync.LocalClient: go cached client for managed cluster
		// sync.RemoteClient: go cache client for hub cluster
		// sync.SynchronizerID.Name:  managed cluster name, same as appsubClusterStatus.Cluster
		// appsubClusterStatus.AppSub: appsub namespaced name

	}

	return nil
}
