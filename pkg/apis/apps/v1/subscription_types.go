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

// Important:
//  - Run "make api" after modifying this file to regenerate the OpenAPI go file.
//  - JSON tags are required for struct fields to be serializable;
//  - Run "make" to regenerate code after modifying this file.

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	plrv1alpha1 "github.com/open-cluster-management/multicloud-operators-placementrule/pkg/apis/apps/v1"
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

	//LabelSubscriptionPause sits in subscription label to identify if the subscription is paused or not
	LabelSubscriptionPause = "subscription-pause"
)

const (
	// DefaultRollingUpdateMaxUnavailablePercentage defines the percentage for rolling update
	DefaultRollingUpdateMaxUnavailablePercentage = 25
)

// PackageFilter defines the reference to Channel
type PackageFilter struct {
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	Annotations   map[string]string     `json:"annotations,omitempty"`

	// +kubebuilder:validation:Pattern=([0-9]+)((\.[0-9]+)(\.[0-9]+)|(\.[0-9]+)?(\.[xX]))$
	Version   string                       `json:"version,omitempty"`

	FilterRef *corev1.LocalObjectReference `json:"filterRef,omitempty"`
}

// PackageOverride describes rules for override
type PackageOverride struct {
	runtime.RawExtension `json:",inline"`
}

// Overrides field in deployable
type Overrides struct {
	// The name of package. Required.
	PackageName      string            `json:"packageName"`

	// Optional string providing an alias for the package.
	PackageAlias     string            `json:"packageAlias,omitempty"`

	// A list of raw JSON data providing the settings to override for this
	// package.
	PackageOverrides []PackageOverride `json:"packageOverrides,omitempty"`
}

// TimeWindow defines a time window for subscription to run or be blocked
type TimeWindow struct {
	// Type of time window. Valid values include "Active" and "Blocked".
	// Deploy only happens during an active time window.
	// <kubebuilder:validation:Enum={active,blocked,Active,Blocked}>
	WindowType string `json:"windowtype,omitempty"`

	// Timezone definition as defined in
	// https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
	// +optional
	Location string `json:"location,omitempty"`

	// TODO: Rename this to "daysOfWeek"
	// The day of the week for this time window. Refer to https://golang.org/pkg/time/#Weekday
	// +optional
	Daysofweek []string    `json:"daysofweek,omitempty"`

	// The range of hours in a day.
	// +optional
	Hours      []HourRange `json:"hours,omitempty"`
}

// HourRange time format for each time will be Kitchen format, defined at https://golang.org/pkg/time/#pkg-constants
type HourRange struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// SubscriptionSpec defines the desired state of Subscription
type SubscriptionSpec struct {
	// Name of the channel subscribed.
	Channel string `json:"channel"`

	// The targeted package in a Channel.
	// +optional
	Package string `json:"name,omitempty"`

	// The targeted collection of packages in a Channel.
	// +optional
	PackageFilter *PackageFilter `json:"packageFilter,omitempty"`

	// To provide flexibility to override package in channel with local input
	// +optional
	PackageOverrides []*Overrides `json:"packageOverrides,omitempty"`

	// Placement rule that determines the target clusters.
	// This is only used for the hub cluster.
	// +optional
	Placement *plrv1alpha1.Placement `json:"placement,omitempty"`

	// The Overrides to apply when the subscription is applied to clusters.
	// This is only used for the hub cluster.
	// +optional
	Overrides []dplv1alpha1.Overrides `json:"overrides,omitempty"`

	// A time window during which the subscription takes affect.
	// +optional
	TimeWindow *TimeWindow `json:"timewindow,omitempty"`
}

// SubscriptionPhase defines the phasing of a Subscription
type SubscriptionPhase string

const (
	// This subscription is the "parent" one sitting in the hub cluster.
	SubscriptionUnknown SubscriptionPhase = ""

	// This subscription is the "parent" one sitting in the hub cluster and
	// it has been successfully propagated to managed clusters.
	SubscriptionPropagated SubscriptionPhase = "Propagated"

	// This subscription is a "child" one sitting in a managed cluster
	SubscriptionSubscribed SubscriptionPhase = "Subscribed"

	// This subscription is the "parent" one sitting in the hub cluster.
	SubscriptionFailed SubscriptionPhase = "Failed"
)

// SubscriptionUnitStatus defines status of a unit (subscription or package)
type SubscriptionUnitStatus struct {
	// Phase are Propagated if it is in the hub cluster or Subscribed if it
	// is in a managed cluster.
	// +optional
	Phase          SubscriptionPhase `json:"phase,omitempty"`

	// Human reaable message describing the current status.
	// +optional
	Message        string            `json:"message,omitempty"`

	// A reason string that explains the reason that brought the
	// Subscription to its current status.
	// +optional
	Reason         string            `json:"reason,omitempty"`

	// The time when the last transition of status for the Subscription.
	LastUpdateTime metav1.Time       `json:"lastUpdateTime"`

	// Raw JSON data describing the detailed status of the unit.
	// +optional
	ResourceStatus *runtime.RawExtension `json:"resourceStatus,omitempty"`
}

// SubscriptionPerClusterStatus defines the status for a Subscription in a
// managed cluster. The key is the package name.
type SubscriptionPerClusterStatus struct {
	SubscriptionPackageStatus map[string]*SubscriptionUnitStatus `json:"packages,omitempty"`
}

// SubscriptionClusterStatusMap defines the per-cluster status where the key is
// the cluster name.
type SubscriptionClusterStatusMap map[string]*SubscriptionPerClusterStatus

// SubscriptionStatus defines the observed state of a Subscription
type SubscriptionStatus struct {
	// Phase of the Subscription. Valid values can be an empty string (""),
	// meaning that the Subscription is in the hub cluster; "Propagated",
	// meaning that the Subscription has been propagated to managed
	// clusters; "Subscribed", meaning that the Subscription has been
	// subscribed and it is a copy sitting in a managed cluster; or
	// "Failed", meaning that the Subscription has some issues to solve.
	Phase          SubscriptionPhase `json:"phase,omitempty"`

	// Human reaable message describing the current status.
	Message        string            `json:"message,omitempty"`

	// A reason string that explains the reason that brought the
	// Subscription to its current status.
	Reason         string            `json:"reason,omitempty"`

	// The time when the last transition of status for the Subscription.
	LastUpdateTime metav1.Time       `json:"lastUpdateTime,omitempty"`

	// For endpoint, it is the status of subscription, key is package name,
	// For hub, it aggregates all status from managed clusters, with the
	// keys being cluster names.
	Statuses SubscriptionClusterStatusMap `json:"statuses,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Subscription is the Schema for the subscriptions API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="subscription status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=appsub
type Subscription struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification for the Subscription.
	Spec   SubscriptionSpec   `json:"spec,omitempty"`
	// The most recent observed status of the Subscription.
	Status SubscriptionStatus `json:"status,omitempty"`
}


// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SubscriptionList contains a list of Subscription
type SubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// A list of Subscription objects.
	Items           []Subscription `json:"items"`
}

// SubscriberItem defines subscriber item to share subscribers with different channel types
type SubscriberItem struct {

	Subscription          *Subscription

	SubscriptionConfigMap *corev1.ConfigMap

	Channel               *chnv1alpha1.Channel

	ChannelSecret         *corev1.Secret

	ChannelConfigMap      *corev1.ConfigMap
}

// Subscriber efines common interface of different channel types
type Subscriber interface {
	SubscribeItem(*SubscriberItem) error
	UnsubscribeItem(types.NamespacedName) error
}

func init() {
	SchemeBuilder.Register(&Subscription{}, &SubscriptionList{})
}
