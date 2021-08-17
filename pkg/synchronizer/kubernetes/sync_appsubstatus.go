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

func (sync *KubeSynchronizer) SyncAppsubClusterStatus(appsubClusterStatus SubscriptionClusterStatus) error {
	klog.V(1).Infof("cluster: %v\n", appsubClusterStatus.Cluster)
	klog.V(1).Infof("appsub: %v\n", appsubClusterStatus.AppSub)
	klog.V(1).Infof("action: %v\n", appsubClusterStatus.Action)

	for _, resource := range appsubClusterStatus.SubscriptionPackageStatus {
		klog.V(1).Infof("resource status - Name: %v, Namespace: %v, Apiversion: %v, Kind: %v, Phase: %v, Message: %v\n",
			resource.Name, resource.Namespace, resource.ApiVersion, resource.Kind, resource.Phase, resource.Message)
	}

	// sync.LocalClient: go cached client for managed cluster
	// sync.RemoteClient: go cache client for hub cluster
	return nil
}
