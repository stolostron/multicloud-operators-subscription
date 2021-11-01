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
	corev1 "k8s.io/api/core/v1"
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
	APIVersion     string       `json:"apiVersion,omitempty"`
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

type SubscriptionReportSummary struct {

	// Deployed provides the count of subscriptions that deployed successfully
	// +optional
	Deployed string `json:"deployed"`

	// InProgress provides the count of subscriptions that are in the process of being deployed
	// +optional
	InProgress string `json:"inProgress"`

	// Failed provides the count of subscriptions that failed to deploy
	// +optional
	Failed string `json:"failed"`

	// PropagationFailed provides the count of subscriptions that failed to propagate to a managed cluster
	// +optional
	PropagationFailed string `json:"propagationFailed"`

	// Clusters provides the count of all managed clusters the subscription is deployed to
	// +optional
	Clusters string `json:"clusters"`
}

// SubscriptionResult has one of the following values:
//   - deployed: the subscription deployed successfully
//   - failed: the subscription failed to deploy
//   - propagationFailed: the subscription failed to propagate
//
// +kubebuilder:validation:Enum=deployed;failed;propagationFailed
type SubscriptionResult string

// SubscriptionReportResult provides the result for an individual subscription
type SubscriptionReportResult struct {

	// Source is an identifier for the subscription
	// +optional
	Source string `json:"source"`

	// Timestamp indicates the time the result was found
	Timestamp metav1.Timestamp `json:"timestamp,omitempty"`

	// Result indicates the outcome of the subscription deployment
	Result SubscriptionResult `json:"result,omitempty"`
}

// SubscriptionReportType has one of the following values:
//   - Application: an appsub across all managed clusters
//   - Cluster: all appsubs on a managed cluster
//
// +kubebuilder:validation:Enum=Application;Cluster
type SubscriptionReportType string

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="ReportType",type=string,JSONPath=`.reportType`
// +kubebuilder:printcolumn:name="Deployed",type=string,JSONPath=`.summary.deployed`
// +kubebuilder:printcolumn:name="InProgress",type=string,JSONPath=`.summary.inProgress`
// +kubebuilder:printcolumn:name="Failed",type=string,JSONPath=`.summary.failed`
// +kubebuilder:printcolumn:name="PropagationFailed",type=string,JSONPath=`.summary.propagationFailed`
// +kubebuilder:printcolumn:name="Clusters",type=string,JSONPath=`.summary.clusters`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=appsubreport

// SubscriptionReport is the Schema for the subscriptionreports API
type SubscriptionReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// ReportType is the name or identifier of the type of report
	ReportType SubscriptionReportType `json:"reportType"`

	// SubscriptionReportSummary provides a summary of results
	// +optional
	Summary SubscriptionReportSummary `json:"summary,omitempty"`

	// SubscriptionReportResult provides result details
	// +optional
	Results []*SubscriptionReportResult `json:"results,omitempty"`

	// Resources is an optional reference to the subscription resources
	// +optional
	Resources []*corev1.ObjectReference `json:"resources,omitempty"`
}

// SubscriptionReportList contains a list of SubscriptionReport
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SubscriptionReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SubscriptionReport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SubscriptionStatus{}, &SubscriptionStatusList{})
	SchemeBuilder.Register(&SubscriptionReport{}, &SubscriptionReportList{})
}
