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

package apis

import (
	ansiblejob "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/ansible/v1alpha1"
	helmrelease "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	placementrule "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	v1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes, v1.SchemeBuilder.AddToScheme,
		ansiblejob.SchemeBuilder.AddToScheme,
		helmrelease.SchemeBuilder.AddToScheme,
		placementrule.SchemeBuilder.AddToScheme,
	)
}
