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

package kubernetes

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

// Validator is used to validate resources in synchronizer
type Validator struct {
	*KubeSynchronizer
	Store      map[schema.GroupVersionKind]map[string]bool
	syncsource string
}

// CreateValiadtor returns initialized validator struct
func (sync *KubeSynchronizer) CreateValiadtor(syncsource string) *Validator {
	return &Validator{
		KubeSynchronizer: sync,
		Store:            make(map[schema.GroupVersionKind]map[string]bool),
		syncsource:       syncsource,
	}
}

// ApplyValiadtor use validator to check resources in synchronizer
func (sync *KubeSynchronizer) ApplyValiadtor(v *Validator) {
	var err error

	for resgvk, resmap := range sync.KubeResources {
		for reskey, tplunit := range resmap.TemplateMap {
			if v.Store[resgvk] == nil || !v.Store[resgvk][reskey] {
				// will ignore non-syncsource templates
				tplhost := sync.Extension.GetHostFromObject(tplunit)
				tpldpl := utils.GetHostDeployableFromObject(tplunit)

				klog.V(10).Infof("Start DeRegister, with resgvk: %v, reskey: %s", resgvk, reskey)

				err = sync.DeRegisterTemplate(*tplhost, *tpldpl, v.syncsource)

				if err != nil {
					klog.Error("Failed to deregister template for applying validator with error: ", err)
				}
			}
		}
	}
}

// AddValidResource adds resource into validator
func (v *Validator) AddValidResource(gvk schema.GroupVersionKind, host, dpl types.NamespacedName) {
	if _, ok := v.Store[gvk]; !ok {
		v.Store[gvk] = make(map[string]bool)
	}

	reskey := v.generateResourceMapKey(host, dpl)

	v.Store[gvk][reskey] = true
}
