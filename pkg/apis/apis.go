// Copyright 2021 The Kubernetes Authors.
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

package apis

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	placement "open-cluster-management.io/api/cluster/v1beta1"
	workV1 "open-cluster-management.io/api/work/v1"
	authv1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	chnapis "open-cluster-management.io/multicloud-operators-channel/pkg/apis"
)

// AddToSchemes may be used to add all resources defined in the project to a Scheme
var AddToSchemes runtime.SchemeBuilder

// AddToScheme adds all Resources to the Scheme
func AddToScheme(s *runtime.Scheme) error {
	err := chnapis.AddToSchemes.AddToScheme(s)
	if err != nil {
		klog.Error("Failed to add channel to scheme ")
		return err
	}

	err = spokeClusterV1.AddToScheme(s)
	if err != nil {
		return err
	}

	err = workV1.AddToScheme(s)
	if err != nil {
		return err
	}

	err = placement.AddToScheme(s)
	if err != nil {
		return err
	}

	err = authv1beta1.AddToScheme(s)
	if err != nil {
		return err
	}

	return AddToSchemes.AddToScheme(s)
}
