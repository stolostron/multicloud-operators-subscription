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
// +kubebuilder:resource:shortName=appsubsummarystatus
// SubscriptionPackageStatus defines a summary of the status of package deployments on the clusters
type SubscriptionSummaryStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Summary SubscriptionSummary `json:"summary,omitempty"`
}

// +kubebuilder:object:root=true
// SubscriptionSummaryStatusList contains a list of SubscriptionSummaryStatus.
type SubscriptionSummaryStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SubscriptionSummaryStatus `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:resource:shortName=appsubpackagestatus
// SubscriptionPackageStatus defines the status of package deployments
type SubscriptionPackageStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Statuses represents all the resources deployed by the subscription per cluster
	Statuses SubscriptionClusterStatusMap `json:"statuses,omitempty"`
}

// +kubebuilder:object:root=true
// SubscriptionPackagetatusList contains a list of SubscriptionPackageStatus.
type SubscriptionPackageStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SubscriptionPackageStatus `json:"items"`
}

//SubscriptionSummary contains a ClusterSummary of packages that deployed successfully,
// failed to deploy, or failed to propagate
type SubscriptionSummary struct {
	// A ClusterSummary of packages that deployed successfully
	DeployedSummary ClusterSummary `json:"deployed,omitempty"`
	// A ClusterSummary of packages that failed to deployed
	DeployFailedSummary ClusterSummary `json:"deployFailed,omitempty"`
	// A ClusterSummary of packages that failed to propagate
	PropagationFailedSummary ClusterSummary `json:"propagationFailed,omitempty"`
}

// ClusterSummary defines status of a package deployment.
type ClusterSummary struct {
	Count    int      `json:"count,omitempty"`
	Clusters []string `json:"clusters,omitempty"`
}

// SubscriptionClusterStatusMap defines the status of packages in a cluster.
type SubscriptionClusterStatusMap struct {
	SubscriptionPackageStatus []SubscriptionUnitStatus `json:"packages,omitempty"`
}

// SubscriptionUnitStatus defines status of a package deployment.
type SubscriptionUnitStatus struct {
	Name           string       `json:"name,omitempty"`
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
	SchemeBuilder.Register(&SubscriptionSummaryStatus{}, &SubscriptionSummaryStatusList{},
		&SubscriptionPackageStatus{}, &SubscriptionPackageStatusList{})
}
