/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:resource:shortName=appsubstatus
// SubscriptionStatus defines the status of package deployments
type SubscriptionStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Statuses represents all the resources deployed by the subscription per cluster
	Statuses SubscriptionClusterStatusMap `json:"statuses,omitempty"`
}

// +kubebuilder:object:root=true
// SubscriptionStatusList contains a list of SubscriptionStatus.
type SubscriptionStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SubscriptionStatus `json:"items"`
}

// SubscriptionClusterStatusMap defines the status of packages in a cluster.
type SubscriptionClusterStatusMap struct {
	SubscriptionStatus []SubscriptionUnitStatus `json:"packages,omitempty"`
}

// SubscriptionUnitStatus defines status of a package deployment.
type SubscriptionUnitStatus struct {
	Name           string       `json:"name,omitempty"`
	ApiVersion     string       `json:"apiVersion,omitempty"`
	Kind           string       `json:"kind,omitempty"`
	Namespace      string       `json:"namespace,omitempty"`
	Phase          PackagePhase `json:"phase,omitempty"`
	Message        string       `json:"message,omitempty"`
	LastUpdateTime metav1.Time  `json:"lastUpdateTime"`
}

// PackagePhase defines the phasing of a Package
type PackagePhase string

const (
	// PackageUnknown means the status of the package is unknown
	PackageUnknown PackagePhase = ""
	// PackageDeployed means this packaged is deployed on the manage cluster
	PackageDeployed PackagePhase = "Deployed"
	// PackageDeployFailed means this package failed to deploy on the manage cluster
	PackageDeployFailed PackagePhase = "Failed"
	// PackagePropagationFailed means this package failed to propagate to the manage cluster
	PackagePropagationFailed PackagePhase = "PropagationFailed"
)

func init() {
	SchemeBuilder.Register(&SubscriptionStatus{}, &SubscriptionStatusList{})
}
