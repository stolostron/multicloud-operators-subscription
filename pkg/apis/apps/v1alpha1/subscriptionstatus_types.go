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

// SubscriptionStatus provides detailed status for all the resources that are deployed by the application in a cluster.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:resource:shortName=appsubstatus
type SubscriptionStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Statuses SubscriptionClusterStatusMap `json:"statuses,omitempty"`
}

// SubscriptionStatusList provides a list of SubscriptionStatus.
// +kubebuilder:object:root=true
type SubscriptionStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SubscriptionStatus `json:"items"`
}

// SubscriptionClusterStatusMap contains the status of deployment packages in a cluster.
type SubscriptionClusterStatusMap struct {
	SubscriptionPackageStatus []SubscriptionUnitStatus `json:"packages,omitempty"`

	SubscriptionStatus SubscriptionOverallStatus `json:"subscription,omitempty"`
}

// SubscriptionUnitStatus provides the status of a single deployment package.
type SubscriptionUnitStatus struct {
	// Name of the deployment package.
	Name string `json:"name,omitempty"`

	// API version of the deployment package.
	APIVersion string `json:"apiVersion,omitempty"`

	// Kind of the deployment package.
	Kind string `json:"kind,omitempty"`

	// Namespace where the deployment package is deployed.
	Namespace string `json:"namespace,omitempty"`

	// Phase of the deployment package (unknown/deployed/failed/propagationFailed).
	Phase PackagePhase `json:"phase,omitempty"`

	// Informational message or error output from the deployment of the package.
	Message string `json:"message,omitempty"`

	// Timestamp of when the deployment package was last updated.
	LastUpdateTime metav1.Time `json:"lastUpdateTime"`
}

// SubscriptionOverallStatus provides the overall status of the subscription. It is computed using the status of
// all the deployment packages in the subscription.
type SubscriptionOverallStatus struct {
	// Phase of the overall subscription status (unknown/deployed/failed).
	Phase SubscriptionPhase `json:"phase,omitempty"`

	// Informational message or error output from the overall subscription status.
	Message string `json:"message,omitempty"`

	// Timestamp of when the overall subscription status was last updated.
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
}

// PackagePhase defines the phase of a deployment package. The supported phases are "", "Deployed", "Failed", and
// "PropagationFailed".
type PackagePhase string

const (
	// PackageUnknown represents the status of a package that is unknown
	PackageUnknown PackagePhase = ""

	// PackageDeployed represents the status of a package that is deployed on the managed cluster
	PackageDeployed PackagePhase = "Deployed"

	// PackageDeployFailed represents the status of a package that failed to deployed on the managed cluster
	PackageDeployFailed PackagePhase = "Failed"

	// PackagePropagationFailed represents the status of a package that failed to propagate to the managed cluster
	PackagePropagationFailed PackagePhase = "PropagationFailed"
)

// SubscriptionPhase defines the phase of the overall subscription. The supported phases are "", "Deployed", and "Failed".
type SubscriptionPhase string

const (
	// SubscriptionUnknown represents the status of a subscription that is unknown.
	SubscriptionUnknown SubscriptionPhase = ""

	// SubscriptionDeployed represents the status of a subscription that is deployed on the manage cluster.
	SubscriptionDeployed SubscriptionPhase = "Deployed"

	// SubscriptionDeployFailed represents the status of a subscription that failed to deploy on the manage cluster.
	SubscriptionDeployFailed SubscriptionPhase = "Failed"
)

// SubscriptionReportSummary provides a summary of the status of the subscription on all the managed clusters.
// It provides a count of the number of clusters, where the subscription is deployed to, that has the status of
// "deployed", "inProgress", "failed", and "propagationFailed".
type SubscriptionReportSummary struct {

	// Deployed provides the count of subscriptions that deployed successfully
	// +optional
	Deployed string `json:"deployed"`

	// InProgress provides the count of subscriptions that are in the process of being deployed.
	// +optional
	InProgress string `json:"inProgress"`

	// Failed provides the count of subscriptions that failed to deploy.
	// +optional
	Failed string `json:"failed"`

	// PropagationFailed provides the count of subscriptions that failed to propagate to a managed cluster.
	// +optional
	PropagationFailed string `json:"propagationFailed"`

	// Clusters provides the count of all managed clusters the subscription is deployed to.
	// +optional
	Clusters string `json:"clusters"`
}

// SubscriptionResult indicates the outcome of a subscription deployment. It could have one of the following values:
//   - deployed: the subscription deployed successfully
//   - failed: the subscription failed to deploy
//   - propagationFailed: the subscription failed to propagate
//
// +kubebuilder:validation:Enum=deployed;failed;propagationFailed
type SubscriptionResult string

// SubscriptionReportResult provides the result for an individual subscription. For application type reports, the
// details include the status of the subscription from all the managed clusters. For cluster type reports, the details
// include the status of all the subscriptions on a managed cluster.
type SubscriptionReportResult struct {

	// Source is an identifier of the subscription or managed cluster, depending on the type of the report.
	// +optional
	Source string `json:"source"`

	// Timestamp indicates the time when the result was found.
	Timestamp metav1.Timestamp `json:"timestamp,omitempty"`

	// Result indicates the outcome (deployed/failed/propagationFailed) of the subscription deployment.
	Result SubscriptionResult `json:"result,omitempty"`
}

// SubscriptionReportType indicates the type of the subscription report. It could have one of the following values:
//   - Application: a report for a particular subscription and its status on all the managed clusters
//   - Cluster: a report of all the subscriptions on a particular managed cluster
//
// +kubebuilder:validation:Enum=Application;Cluster
type SubscriptionReportType string

// SubscriptionReport provides a report of the status of the subscriptions on the managed clusters. There are two
// types of subscriptions reports: Application and Cluster. Application type reports provide the status of a particular
// subscription on all the managed clusters. Cluster type reports provide the status of all the subscriptions on a
// particular managed cluster.
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
type SubscriptionReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// ReportType identifies the type of subscription report.
	ReportType SubscriptionReportType `json:"reportType"`

	// +optional
	Summary SubscriptionReportSummary `json:"summary,omitempty"`

	// +optional
	Results []*SubscriptionReportResult `json:"results,omitempty"`

	// Resources is an optional reference to the subscription resources.
	// +optional
	Resources []*corev1.ObjectReference `json:"resources,omitempty"`
}

// SubscriptionReportList contains a list of SubscriptionReports.
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
