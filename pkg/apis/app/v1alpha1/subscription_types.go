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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	chnv1alpha1 "github.com/IBM/multicloud-operators-channel/pkg/apis/app/v1alpha1"
	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	plrv1alpha1 "github.com/IBM/multicloud-operators-placementrule/pkg/apis/app/v1alpha1"
)

var (
	// AnnotationSyncSource target deployable to rolling update to
	AnnotationSyncSource = SchemeGroupVersion.Group + "/sync-source"
	// AnnotationRollingUpdateTarget target deployable to rolling update to
	AnnotationRollingUpdateTarget = SchemeGroupVersion.Group + "/rollingupdate-target"
	// AnnotationDeployables defines all deployables subscribed by the subscription
	AnnotationDeployables = SchemeGroupVersion.Group + "/deployables"
	// AnnotationHosting defines the subscription hosting the resource
	AnnotationHosting = SchemeGroupVersion.Group + "/hosting-subscription"
	// AnnotationChannelGeneration defines the channel generation
	AnnotationChannelGeneration = SchemeGroupVersion.Group + "/channel-generation"
)

const (
	// DefaultRollingUpdateMaxUnavailablePercentage defines the percentage for rolling update
	DefaultRollingUpdateMaxUnavailablePercentage = 25
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

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
	PackageName string `json:"packageName"`
	// +kubebuilder:validation:MinItems=1
	PackageOverrides []PackageOverride `json:"packageOverrides"` // To be added
}

// TimeWindow defines a time window for subscription to run or be blocked
type TimeWindow struct {
	// active time window or not, if timewindow is active, then deploy will only applies during these windows
	WindowType string `json:"windowtype,omitempty"`
	// https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
	Location string `json:"location,omitempty"`
	// weekdays defined the day of the week for this time window https://golang.org/pkg/time/#Weekday
	Weekdays []string    `json:"weekdays,omitempty"`
	Hours    []HourRange `json:"hours,omitempty"`
}

//Time format for each time will be Kitchen format, defined at https://golang.org/pkg/time/#pkg-constants
type HourRange struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// SubscriptionSpec defines the desired state of Subscription
type SubscriptionSpec struct {
	Channel string `json:"channel"`
	// To specify 1 package in channel
	Package string `json:"name,omitempty"`
	// To specify more than 1 package in channel
	PackageFilter *PackageFilter `json:"packageFilter,omitempty"`
	// To provide flexibility to override package in channel with local input
	PackageOverrides []*Overrides `json:"packageOverrides,omitempty"`
	// For hub use only, to specify which clusters to go to
	Placement *plrv1alpha1.Placement `json:"placement,omitempty"`
	// for hub use only to specify the overrides when apply to clusters
	Overrides []dplv1alpha1.Overrides `json:"overrides,omitempty"`
	// help user control when the subscription will take affect
	TimeWindow *TimeWindow `json:"timewindow,omitempty"`
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
)

// SubscriptionUnitStatus defines status of a unit (subscription or package)
type SubscriptionUnitStatus struct {
	// Phase are Propagated if it is in hub or Subscribed if it is in endpoint
	Phase          SubscriptionPhase `json:"phase,omitempty"`
	Message        string            `json:"message,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	LastUpdateTime metav1.Time       `json:"lastUpdateTime"`

	ResourceStatus *runtime.RawExtension `json:"resourceStatus,omitempty"`
}

// SubscriptionPerClusterStatus defines status for subscription in each cluster, key is package name
type SubscriptionPerClusterStatus struct {
	SubscriptionPackageStatus map[string]*SubscriptionUnitStatus `json:"packages,omitempty"`
}

// SubscriptionClusterStatusMap defines per cluster status, key is cluster name
type SubscriptionClusterStatusMap map[string]*SubscriptionPerClusterStatus

// SubscriptionStatus defines the observed state of Subscription
// Examples - status of a subscription on hub
//Status:
// 	phase: Propagated
// 	statuses:
// 	  washdc:
// 		packages:
// 		  nginx:
// 			phase: Subscribed
// 		  mongodb:
// 			phase: Failed
// 			Reason: "not authorized"
// 			Message: "user xxx does not have permission to start pod"
//			resourceStatus: {}
//    toronto:
// 		packages:
// 		  nginx:
// 			phase: Subscribed
// 		  mongodb:
// 			phase: Subscribed
//Status of a subscription on managed cluster will only have 1 cluster in the map.
type SubscriptionStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase          SubscriptionPhase `json:"phase,omitempty"`
	Message        string            `json:"message,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	LastUpdateTime metav1.Time       `json:"lastUpdateTime"`

	// For endpoint, it is the status of subscription, key is packagename,
	// For hub, it aggregates all status, key is cluster name
	Statuses SubscriptionClusterStatusMap `json:"statuses,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Subscription is the Schema for the subscriptions API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="subscription status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Namespaced
type Subscription struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubscriptionSpec   `json:"spec,omitempty"`
	Status SubscriptionStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SubscriptionList contains a list of Subscription
type SubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subscription `json:"items"`
}

// +k8s:deepcopy-gen:nonpointer-interfaces=true
// SubsriberItem defines subscriber item to share subscribers with different channel types
type SubscriberItem struct {
	Subscription          *Subscription
	SubscriptionConfigMap *corev1.ConfigMap
	Channel               *chnv1alpha1.Channel
	ChannelSecret         *corev1.Secret
	ChannelConfigMap      *corev1.ConfigMap
}

// Subsriber defines common interface of different channel types
type Subscriber interface {
	SubscribeItem(*SubscriberItem) error
	UnsubscribeItem(types.NamespacedName) error
}

func init() {
	SchemeBuilder.Register(&Subscription{}, &SubscriptionList{})
}
