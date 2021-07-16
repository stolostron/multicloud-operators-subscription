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

type SubscriptionSummary struct {
	DeployedSummary ClusterSummary `json:"deployed,omitempty"`
	FailedSummary   ClusterSummary `json:"failed,omitempty"`
}

// ClusterSummary defines status of a package deployment.
type ClusterSummary struct {
	Count    int      `json:"count,omitempty"`
	Clusters []string `json:"clusters,omitempty"`
}

// SubscriptionClusterStatusMap defines per cluster, per package status, key is package name.
type SubscriptionClusterStatusMap struct {
	SubscriptionPackageStatus []SubscriptionUnitStatus `json:"packages,omitempty"`
}

// SubscriptionUnitStatus defines status of a package deployment.
type SubscriptionUnitStatus struct {
	Name           string      `json:"name,omitempty"`
	Kind           string      `json:"kind,omitempty"`
	Namespace      string      `json:"namespace,omitempty"`
	Phase          string      `json:"phase,omitempty"`
	Message        string      `json:"message,omitempty"`
	LastUpdateTime metav1.Time `json:"lastUpdateTime"`
}

func init() {
	SchemeBuilder.Register(&SubscriptionSummaryStatus{}, &SubscriptionSummaryStatusList{},
		&SubscriptionPackageStatus{}, &SubscriptionPackageStatusList{})
}
