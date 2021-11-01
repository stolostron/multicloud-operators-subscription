// +build !ignore_autogenerated

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apisappsv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appsv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AllowDenyItem) DeepCopyInto(out *AllowDenyItem) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AllowDenyItem.
func (in *AllowDenyItem) DeepCopy() *AllowDenyItem {
	if in == nil {
		return nil
	}
	out := new(AllowDenyItem)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AnsibleJobsStatus) DeepCopyInto(out *AnsibleJobsStatus) {
	*out = *in
	if in.PrehookJobsHistory != nil {
		in, out := &in.PrehookJobsHistory, &out.PrehookJobsHistory
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.PosthookJobsHistory != nil {
		in, out := &in.PosthookJobsHistory, &out.PosthookJobsHistory
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AnsibleJobsStatus.
func (in *AnsibleJobsStatus) DeepCopy() *AnsibleJobsStatus {
	if in == nil {
		return nil
	}
	out := new(AnsibleJobsStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterOverride) DeepCopyInto(out *ClusterOverride) {
	*out = *in
	in.RawExtension.DeepCopyInto(&out.RawExtension)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterOverride.
func (in *ClusterOverride) DeepCopy() *ClusterOverride {
	if in == nil {
		return nil
	}
	out := new(ClusterOverride)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClusterOverrides) DeepCopyInto(out *ClusterOverrides) {
	*out = *in
	if in.ClusterOverrides != nil {
		in, out := &in.ClusterOverrides, &out.ClusterOverrides
		*out = make([]ClusterOverride, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClusterOverrides.
func (in *ClusterOverrides) DeepCopy() *ClusterOverrides {
	if in == nil {
		return nil
	}
	out := new(ClusterOverrides)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HourRange) DeepCopyInto(out *HourRange) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HourRange.
func (in *HourRange) DeepCopy() *HourRange {
	if in == nil {
		return nil
	}
	out := new(HourRange)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Overrides) DeepCopyInto(out *Overrides) {
	*out = *in
	if in.PackageOverrides != nil {
		in, out := &in.PackageOverrides, &out.PackageOverrides
		*out = make([]PackageOverride, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Overrides.
func (in *Overrides) DeepCopy() *Overrides {
	if in == nil {
		return nil
	}
	out := new(Overrides)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageFilter) DeepCopyInto(out *PackageFilter) {
	*out = *in
	if in.LabelSelector != nil {
		in, out := &in.LabelSelector, &out.LabelSelector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.FilterRef != nil {
		in, out := &in.FilterRef, &out.FilterRef
		*out = new(corev1.LocalObjectReference)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageFilter.
func (in *PackageFilter) DeepCopy() *PackageFilter {
	if in == nil {
		return nil
	}
	out := new(PackageFilter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PackageOverride) DeepCopyInto(out *PackageOverride) {
	*out = *in
	in.RawExtension.DeepCopyInto(&out.RawExtension)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PackageOverride.
func (in *PackageOverride) DeepCopy() *PackageOverride {
	if in == nil {
		return nil
	}
	out := new(PackageOverride)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriberItem) DeepCopyInto(out *SubscriberItem) {
	*out = *in
	if in.Subscription != nil {
		in, out := &in.Subscription, &out.Subscription
		*out = new(Subscription)
		(*in).DeepCopyInto(*out)
	}
	if in.SubscriptionConfigMap != nil {
		in, out := &in.SubscriptionConfigMap, &out.SubscriptionConfigMap
		*out = new(corev1.ConfigMap)
		(*in).DeepCopyInto(*out)
	}
	if in.Channel != nil {
		in, out := &in.Channel, &out.Channel
		*out = new(apisappsv1.Channel)
		(*in).DeepCopyInto(*out)
	}
	if in.ChannelSecret != nil {
		in, out := &in.ChannelSecret, &out.ChannelSecret
		*out = new(corev1.Secret)
		(*in).DeepCopyInto(*out)
	}
	if in.ChannelConfigMap != nil {
		in, out := &in.ChannelConfigMap, &out.ChannelConfigMap
		*out = new(corev1.ConfigMap)
		(*in).DeepCopyInto(*out)
	}
	if in.SecondaryChannel != nil {
		in, out := &in.SecondaryChannel, &out.SecondaryChannel
		*out = new(apisappsv1.Channel)
		(*in).DeepCopyInto(*out)
	}
	if in.SecondaryChannelSecret != nil {
		in, out := &in.SecondaryChannelSecret, &out.SecondaryChannelSecret
		*out = new(corev1.Secret)
		(*in).DeepCopyInto(*out)
	}
	if in.SecondaryChannelConfigMap != nil {
		in, out := &in.SecondaryChannelConfigMap, &out.SecondaryChannelConfigMap
		*out = new(corev1.ConfigMap)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriberItem.
func (in *SubscriberItem) DeepCopy() *SubscriberItem {
	if in == nil {
		return nil
	}
	out := new(SubscriberItem)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Subscription) DeepCopyInto(out *Subscription) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Subscription.
func (in *Subscription) DeepCopy() *Subscription {
	if in == nil {
		return nil
	}
	out := new(Subscription)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Subscription) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in SubscriptionClusterStatusMap) DeepCopyInto(out *SubscriptionClusterStatusMap) {
	{
		in := &in
		*out = make(SubscriptionClusterStatusMap, len(*in))
		for key, val := range *in {
			var outVal *SubscriptionPerClusterStatus
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = new(SubscriptionPerClusterStatus)
				(*in).DeepCopyInto(*out)
			}
			(*out)[key] = outVal
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionClusterStatusMap.
func (in SubscriptionClusterStatusMap) DeepCopy() SubscriptionClusterStatusMap {
	if in == nil {
		return nil
	}
	out := new(SubscriptionClusterStatusMap)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionList) DeepCopyInto(out *SubscriptionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Subscription, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionList.
func (in *SubscriptionList) DeepCopy() *SubscriptionList {
	if in == nil {
		return nil
	}
	out := new(SubscriptionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SubscriptionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionPerClusterStatus) DeepCopyInto(out *SubscriptionPerClusterStatus) {
	*out = *in
	if in.SubscriptionPackageStatus != nil {
		in, out := &in.SubscriptionPackageStatus, &out.SubscriptionPackageStatus
		*out = make(map[string]*SubscriptionUnitStatus, len(*in))
		for key, val := range *in {
			var outVal *SubscriptionUnitStatus
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = new(SubscriptionUnitStatus)
				(*in).DeepCopyInto(*out)
			}
			(*out)[key] = outVal
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionPerClusterStatus.
func (in *SubscriptionPerClusterStatus) DeepCopy() *SubscriptionPerClusterStatus {
	if in == nil {
		return nil
	}
	out := new(SubscriptionPerClusterStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionSpec) DeepCopyInto(out *SubscriptionSpec) {
	*out = *in
	if in.PackageFilter != nil {
		in, out := &in.PackageFilter, &out.PackageFilter
		*out = new(PackageFilter)
		(*in).DeepCopyInto(*out)
	}
	if in.PackageOverrides != nil {
		in, out := &in.PackageOverrides, &out.PackageOverrides
		*out = make([]*Overrides, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(Overrides)
				(*in).DeepCopyInto(*out)
			}
		}
	}
	if in.Placement != nil {
		in, out := &in.Placement, &out.Placement
		*out = new(appsv1.Placement)
		(*in).DeepCopyInto(*out)
	}
	if in.Overrides != nil {
		in, out := &in.Overrides, &out.Overrides
		*out = make([]ClusterOverrides, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.TimeWindow != nil {
		in, out := &in.TimeWindow, &out.TimeWindow
		*out = new(TimeWindow)
		(*in).DeepCopyInto(*out)
	}
	if in.HookSecretRef != nil {
		in, out := &in.HookSecretRef, &out.HookSecretRef
		*out = new(corev1.ObjectReference)
		**out = **in
	}
	if in.Allow != nil {
		in, out := &in.Allow, &out.Allow
		*out = make([]*AllowDenyItem, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(AllowDenyItem)
				(*in).DeepCopyInto(*out)
			}
		}
	}
	if in.Deny != nil {
		in, out := &in.Deny, &out.Deny
		*out = make([]*AllowDenyItem, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(AllowDenyItem)
				(*in).DeepCopyInto(*out)
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionSpec.
func (in *SubscriptionSpec) DeepCopy() *SubscriptionSpec {
	if in == nil {
		return nil
	}
	out := new(SubscriptionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionStatus) DeepCopyInto(out *SubscriptionStatus) {
	*out = *in
	in.LastUpdateTime.DeepCopyInto(&out.LastUpdateTime)
	in.AnsibleJobsStatus.DeepCopyInto(&out.AnsibleJobsStatus)
	if in.Statuses != nil {
		in, out := &in.Statuses, &out.Statuses
		*out = make(SubscriptionClusterStatusMap, len(*in))
		for key, val := range *in {
			var outVal *SubscriptionPerClusterStatus
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = new(SubscriptionPerClusterStatus)
				(*in).DeepCopyInto(*out)
			}
			(*out)[key] = outVal
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionStatus.
func (in *SubscriptionStatus) DeepCopy() *SubscriptionStatus {
	if in == nil {
		return nil
	}
	out := new(SubscriptionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionUnitStatus) DeepCopyInto(out *SubscriptionUnitStatus) {
	*out = *in
	in.LastUpdateTime.DeepCopyInto(&out.LastUpdateTime)
	if in.ResourceStatus != nil {
		in, out := &in.ResourceStatus, &out.ResourceStatus
		*out = new(runtime.RawExtension)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionUnitStatus.
func (in *SubscriptionUnitStatus) DeepCopy() *SubscriptionUnitStatus {
	if in == nil {
		return nil
	}
	out := new(SubscriptionUnitStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TimeWindow) DeepCopyInto(out *TimeWindow) {
	*out = *in
	if in.Daysofweek != nil {
		in, out := &in.Daysofweek, &out.Daysofweek
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Hours != nil {
		in, out := &in.Hours, &out.Hours
		*out = make([]HourRange, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TimeWindow.
func (in *TimeWindow) DeepCopy() *TimeWindow {
	if in == nil {
		return nil
	}
	out := new(TimeWindow)
	in.DeepCopyInto(out)
	return out
}
