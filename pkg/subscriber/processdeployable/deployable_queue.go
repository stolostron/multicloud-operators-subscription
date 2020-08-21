// Copyright 2019 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package processdeployable

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
)

type SyncSource interface {
	GetLocalClient() client.Client
	GetValidatedGVK(schema.GroupVersionKind) *schema.GroupVersionKind
	AddTemplates(string, types.NamespacedName, []kubesynchronizer.DplUnit) error
}

//PProcessDeployableUnits unify the deployable handle process between helm and objectbucket deployables
func Units(sub *subv1.Subscription, synchronizer SyncSource,
	hostkey types.NamespacedName, syncsource string,
	pkgMap map[string]bool, dplUnits []kubesynchronizer.DplUnit) error {
	if err := synchronizer.AddTemplates(syncsource, hostkey, dplUnits); err != nil {
		klog.Error("error in registering :", err)

		if serr := utils.SetInClusterPackageStatus(&(sub.Status), sub.GetName(), err, nil); serr != nil {
			klog.Error("error in setting in cluster package status :", serr)
		}
		// as long as add to template fail we should return error for retry
		return err
	}

	return nil
}
