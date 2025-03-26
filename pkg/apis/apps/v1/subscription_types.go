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

package v1

import (
	"crypto/tls"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	chnv1alpha1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	plrv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
)

var (
	// AnnotationSyncSource target deployable to rolling update to
	AnnotationSyncSource = SchemeGroupVersion.Group + "/sync-source"
	// AnnotationRollingUpdateTarget target deployable to rolling update to
	AnnotationRollingUpdateTarget = SchemeGroupVersion.Group + "/rollingupdate-target"
	// AnnotationRollingUpdateMaxUnavailable defines max un available clusters during rolling update
	AnnotationRollingUpdateMaxUnavailable = SchemeGroupVersion.Group + "/rollingupdate-maxunavaialble"
	// AnnotationDeployables defines all deployables subscribed by the subscription
	AnnotationDeployables = SchemeGroupVersion.Group + "/deployables"
	// AnnotationTopo list all resources will create by the subscription
	AnnotationTopo = SchemeGroupVersion.Group + "/topo"
	// AnnotationHosting defines the subscription hosting the resource
	AnnotationHosting = SchemeGroupVersion.Group + "/hosting-subscription"
	// AnnotationChannelGeneration defines the channel generation
	AnnotationChannelGeneration = SchemeGroupVersion.Group + "/channel-generation"
	// AnnotationWebhookEnabled indicates webhook event notification is enabled
	AnnotationWebhookEnabled = SchemeGroupVersion.Group + "/webhook-enabled"
	// AnnotationWebhookEventCount gets incremented by an incoming webhook event notification
	AnnotationWebhookEventCount = SchemeGroupVersion.Group + "/webhook-event-count"
	// AnnotationWebhookSecret defines webhook secret
	AnnotationWebhookSecret = SchemeGroupVersion.Group + "/webhook-secret"
	// AnnotationGithubPath defines webhook secret
	AnnotationGithubPath = SchemeGroupVersion.Group + "/github-path"
	// AnnotationGithubBranch defines webhook secret
	AnnotationGithubBranch = SchemeGroupVersion.Group + "/github-branch"
	// AnnotationGithubCommit defines Git repo commit ID
	AnnotationGithubCommit = SchemeGroupVersion.Group + "/github-commit"
	// AnnotationGitPath defines webhook secret
	AnnotationGitPath = SchemeGroupVersion.Group + "/git-path"
	// AnnotationGitBranch defines webhook secret
	AnnotationGitBranch = SchemeGroupVersion.Group + "/git-branch"
	// AnnotationGitCommit defines currently deployed Git repo commit ID
	AnnotationGitCommit = SchemeGroupVersion.Group + "/git-current-commit"
	// AnnotationGitCloneDepth defines Git repo clone depth to be able to check out previous commits
	AnnotationGitCloneDepth = SchemeGroupVersion.Group + "/git-clone-depth"
	// AnnotationGitTargetCommit defines Git repo commit to be deployed
	AnnotationGitTargetCommit = SchemeGroupVersion.Group + "/git-desired-commit"
	// AnnotationGitTag defines Git repo revision tag
	AnnotationGitTag = SchemeGroupVersion.Group + "/git-tag"
	// AnnotationClusterAdmin indicates the subscription has cluster admin access
	AnnotationClusterAdmin = SchemeGroupVersion.Group + "/cluster-admin"
	// AnnotationChannelType indicates the channel type for subscription
	AnnotationChannelType = SchemeGroupVersion.Group + "/channel-type"
	// AnnotationUserGroup is subscription user group
	AnnotationUserGroup = "open-cluster-management.io/user-group"
	// AnnotationUserIdentity is subscription user id
	AnnotationUserIdentity = "open-cluster-management.io/user-identity"
	// AnnotationResourceReconcileOption is for reconciling existing resource
	AnnotationResourceReconcileOption   = SchemeGroupVersion.Group + "/reconcile-option"
	AnnotationResourceDoNotDeleteOption = SchemeGroupVersion.Group + "/do-not-delete"
	// AnnotationResourceReconcileLevel is for resource reconciliation frequency
	AnnotationResourceReconcileLevel = SchemeGroupVersion.Group + "/reconcile-rate"
	// AnnotationManualReconcileTime is the time user triggers a manual resource reconcile
	AnnotationManualReconcileTime = SchemeGroupVersion.Group + "/manual-refresh-time"
	//LabelSubscriptionPause sits in subscription label to identify if the subscription is paused or not
	LabelSubscriptionPause = "subscription-pause"
	//LabelSubscriptionName is the subscription name
	LabelSubscriptionName = SchemeGroupVersion.Group + "/subscription"
	// AnnotationHookType defines ansible hook job type - prehook/posthook
	AnnotationHookType = SchemeGroupVersion.Group + "/hook-type"
	// AnnotationHookTemplate defines ansible hook job template namespaced name
	AnnotationHookTemplate = SchemeGroupVersion.Group + "/hook-template"
	// AnnotationBucketPath defines s3 object bucket subfolder path
	AnnotationBucketPath = SchemeGroupVersion.Group + "/bucket-path"
	// AnnotationManagedCluster identifies this is a deployable for managed cluster
	AnnotationManagedCluster = SchemeGroupVersion.Group + "/managed-cluster"
	// AnnotationHostingDeployable sits in templated resource, gives name of hosting deployable, legacy annotation
	AnnotationHostingDeployable = SchemeGroupVersion.Group + "/hosting-deployable"
	// AnnotationCurrentNamespaceScoped specifies to deloy resources into subscription namespace
	AnnotationCurrentNamespaceScoped = SchemeGroupVersion.Group + "/current-namespace-scoped"
	// AnnotationSkipHubValidation indicates the hub subscription should skip the "dry-run" validations and proceed to propagation phase
	AnnotationSkipHubValidation = SchemeGroupVersion.Group + "/skip-hub-validation"
)

const (
	// DefaultRollingUpdateMaxUnavailablePercentage defines the percentage for rolling update
	DefaultRollingUpdateMaxUnavailablePercentage = 25
	// SubscriptionAdmin is used as RBAC resource name for multi-namespace app deployment
	SubscriptionAdmin = "open-cluster-management:subscription-admin"
	// AcmWebhook is the ACM foundation mutation webhook that adds user identity and group annotations
	AcmWebhook = "ocm-mutating-webhook"
	// MergeReconcile creates or updates fields in resources using kubernetes patch
	MergeReconcile = "merge"
	// ReplaceReconcile replaces fields in resources using kubernetes update
	ReplaceReconcile = "replace"
	// MergeAndOwnReconcile creates or updates fields in resources using kubernetes patch and take ownership of the resource
	MergeAndOwnReconcile = "mergeAndOwn"
	// SubscriptionNameSuffix is appended to the subscription name when propagated to managed clusters
	SubscriptionNameSuffix = ""
	// ChannelCertificateData is the configmap data spec field containing trust certificates
	ChannelCertificateData = "caCerts"
	// TLS minimum version as integer
	TLSMinVersionInt = tls.VersionTLS12
	// TLS minimum version as string
	TLSMinVersionString = "1.2"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PackageFilter defines various types of filters for selecting resources
type PackageFilter struct {
	// LabelSelector defines a type of filter for selecting resources by label selector
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// Annotations defines a type of filter for selecting resources by annotations
	Annotations map[string]string `json:"annotations,omitempty"`

	// Version defines a type of filter for selecting resources by version
	Version string `json:"version,omitempty"`

	// FilterRef defines a type of filter for selecting resources by another resource reference
	FilterRef *corev1.LocalObjectReference `json:"filterRef,omitempty"`
}

// PackageOverride provides the contents for overriding a package
type PackageOverride struct {
	runtime.RawExtension `json:",inline"`
}

// Overrides defines a list of contents that will be overridden to a given resource
type Overrides struct {
	// PackageAlias defines the alias of the package name that will be onverriden
	PackageAlias string `json:"packageAlias,omitempty"`

	// PackageName defines the package name that will be onverriden
	PackageName string `json:"packageName"`

	// PackageOverrides defines a list of content for override
	PackageOverrides []PackageOverride `json:"packageOverrides,omitempty"`
}

// AllowDenyItem defines a group of resources allowed or denied for deployment
type AllowDenyItem struct {
	// APIVersion specifies the API version for the group of resources
	APIVersion string `json:"apiVersion,omitempty"`

	// Kinds specifies a list of kinds under the same API version for the group of resources
	Kinds []string `json:"kinds,omitempty"`
}

// TimeWindow defines a time window for the subscription to run or be blocked
type TimeWindow struct {
	// Activiate time window or not. The subscription deployment will only be handled during these active windows
	// Valid values include: active,blocked,Active,Blocked
	// +kubebuilder:validation:Enum={active,blocked,Active,Blocked}
	WindowType string `json:"windowtype,omitempty"`

	// time zone location, refer to TZ identifier in https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
	Location string `json:"location,omitempty"`

	// A list of days of a week, valid values include: Sunday, Monday, Tuesday, Wednesday, Thursday, Friday, Saturday
	Daysofweek []string `json:"daysofweek,omitempty"`

	// A list of hour ranges
	Hours []HourRange `json:"hours,omitempty"`
}

// HourRange defines the time format, refer to https://golang.org/pkg/time/#pkg-constants
type HourRange struct {
	// Start time of the hour range
	Start string `json:"start,omitempty"`

	// End time of the hour range
	End string `json:"end,omitempty"`
}

// ClusterOverride defines the contents for override rules
type ClusterOverride struct {
	runtime.RawExtension `json:",inline"`
}

// ClusterOverrides defines a list of contents that will be overridden to a given cluster
type ClusterOverrides struct {
	// Cluster name
	ClusterName string `json:"clusterName"`

	// ClusterOverrides defines a list of content for override
	//+kubebuilder:validation:MinItems=1
	ClusterOverrides []ClusterOverride `json:"clusterOverrides"` // To be added
}

// SubscriptionSpec defines the desired state of Subscription
type SubscriptionSpec struct {
	// The primary channel namespaced name used by the subscription. Its format is "<channel NameSpace>/<channel Name>"
	Channel string `json:"channel"`
	// The secondary channel will be applied if the primary channel fails to connect
	SecondaryChannel string `json:"secondaryChannel,omitempty"`
	// Subscribe a package by its package name
	Package string `json:"name,omitempty"`
	// Subscribe packages by a package filter
	PackageFilter *PackageFilter `json:"packageFilter,omitempty"`
	// Override packages
	PackageOverrides []*Overrides `json:"packageOverrides,omitempty"`
	// Specify a placement reference for selecting clusters. Hub use only
	Placement *plrv1alpha1.Placement `json:"placement,omitempty"`
	// Specify overrides when applied to clusters. Hub use only
	Overrides []ClusterOverrides `json:"overrides,omitempty"`
	// Specify a time window to indicate when the subscription is handled
	TimeWindow *TimeWindow `json:"timewindow,omitempty"`

	// Specify a secret reference used in Ansible job integration authentication
	// +optional
	HookSecretRef *corev1.ObjectReference `json:"hooksecretref,omitempty"`

	// Specify a list of resources allowed for deployment
	Allow []*AllowDenyItem `json:"allow,omitempty"`

	// Specify a list of resources denied for deployment
	Deny []*AllowDenyItem `json:"deny,omitempty"`

	// WatchHelmNamespaceScopedResources is used to enable watching namespace scope Helm chart resources
	WatchHelmNamespaceScopedResources bool `json:"watchHelmNamespaceScopedResources,omitempty"`
}

// SubscriptionPhase defines the phasing of a Subscription
type SubscriptionPhase string

const (
	// SubscriptionUnknown means this subscription is the "parent" sitting in hub
	SubscriptionUnknown SubscriptionPhase = ""
	// SubscriptionPropagated means this subscription is the "parent" sitting in hub
	SubscriptionPropagated SubscriptionPhase = "Propagated"
	// SubscriptionSubscribed means this subscription is child sitting in managed cluster
	SubscriptionSubscribed SubscriptionPhase = "Subscribed"
	// SubscriptionFailed means this subscription is the "parent" sitting in hub
	SubscriptionFailed SubscriptionPhase = "Failed"
	// SubscriptionPropagationFailed means this subscription is the "parent" sitting in hub
	SubscriptionPropagationFailed SubscriptionPhase = "PropagationFailed"
	PreHookSucessful              SubscriptionPhase = "PreHookSucessful"
)

// SubscriptionUnitStatus defines status of each package in a subscription
type SubscriptionUnitStatus struct {
	// Phase of the deployment package (Propagated/Subscribed/Failed/PropagationFailed/PreHookSucessful).
	Phase SubscriptionPhase `json:"phase,omitempty"`

	// Informational message from the deployment of the package.
	Message string `json:"message,omitempty"`

	// additional error output from the deployment of the package.
	Reason string `json:"reason,omitempty"`

	// Timestamp of when the deployment package was last updated.
	LastUpdateTime metav1.Time `json:"lastUpdateTime"`

	// reserved for backward compatibility
	ResourceStatus *runtime.RawExtension `json:"resourceStatus,omitempty"`
}

// SubscriptionPerClusterStatus defines status of each subscription in a cluster, key is package name
type SubscriptionPerClusterStatus struct {
	SubscriptionPackageStatus map[string]*SubscriptionUnitStatus `json:"packages,omitempty"`
}

// SubscriptionClusterStatusMap defines status of each subscription per cluster, key is cluster name
type SubscriptionClusterStatusMap map[string]*SubscriptionPerClusterStatus

// AnsibleJobsStatus defines status of ansible jobs propagated by the subscription
type AnsibleJobsStatus struct {
	// The lastly propagated prehook job
	LastPrehookJob string `json:"lastprehookjob,omitempty"`

	// reserved for backward compatibility
	PrehookJobsHistory []string `json:"prehookjobshistory,omitempty"`

	// The lastly propagated posthook job
	LastPosthookJob string `json:"lastposthookjob,omitempty"`

	// reserved for backward compatibility
	PosthookJobsHistory []string `json:"posthookjobshistory,omitempty"`
}

// SubscriptionStatus defines the observed status of a subscription
type SubscriptionStatus struct {
	// Phase of the subscription deployment
	Phase SubscriptionPhase `json:"phase,omitempty"`

	// The CLI reference for getting the subscription status output
	AppstatusReference string `json:"appstatusReference,omitempty"`

	// Informational message of the subscription deployment
	Message string `json:"message,omitempty"`

	// additional error output of the subscription deployment
	Reason string `json:"reason,omitempty"`

	// Timestamp of when the subscription status was last updated.
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`

	// +optional
	AnsibleJobsStatus AnsibleJobsStatus `json:"ansiblejobs,omitempty"`

	Statuses SubscriptionClusterStatusMap `json:"statuses,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true

// Subscription is the Schema for the subscriptions API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="SubscriptionState",type="string",JSONPath=".status.phase",description="subscription state"
// +kubebuilder:printcolumn:name="AppstatusReference",type="string",JSONPath=".status.appstatusReference",description="subscription status reference"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Updated",type="date",JSONPath=".status.lastUpdateTime"
// +kubebuilder:printcolumn:name="Local placement",type="boolean",JSONPath=".spec.placement.local"
// +kubebuilder:printcolumn:name="Time window",type="string",JSONPath=".spec.timewindow.windowtype"
// +kubebuilder:resource:shortName=appsub
type Subscription struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubscriptionSpec   `json:"spec"`
	Status SubscriptionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SubscriptionList contains a list of Subscription
type SubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subscription `json:"items"`
}

// +k8s:deepcopy-gen:nonpointer-interfaces=true

// SubscriberItem defines subscriber item to share subscribers with different channel types
type SubscriberItem struct {
	Subscription              *Subscription
	SubscriptionConfigMap     *corev1.ConfigMap
	Channel                   *chnv1alpha1.Channel
	ChannelSecret             *corev1.Secret
	ChannelConfigMap          *corev1.ConfigMap
	SecondaryChannel          *chnv1alpha1.Channel
	SecondaryChannelSecret    *corev1.Secret
	SecondaryChannelConfigMap *corev1.ConfigMap
}

// Subscriber efines common interface of different channel types
// +kubebuilder:object:generate=false
type Subscriber interface {
	SubscribeItem(*SubscriberItem) error
	UnsubscribeItem(types.NamespacedName) error
}

func init() {
	SchemeBuilder.Register(&Subscription{}, &SubscriptionList{})
}
