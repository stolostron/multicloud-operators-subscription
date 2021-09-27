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

package mcmhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	gerr "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ghodss/yaml"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	releasev1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	awsutils "github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils/aws"
	policyReportV1alpha2 "sigs.k8s.io/wg-policy-prototypes/policy-report/pkg/api/wgpolicyk8s.io/v1alpha2"
)

// doMCMHubReconcile process Subscription on hub - distribute it via manifestWork
func (r *ReconcileSubscription) doMCMHubReconcile(sub *appv1alpha1.Subscription) error {
	substr := fmt.Sprintf("%v/%v", sub.GetNamespace(), sub.GetName())
	klog.V(1).Infof("entry doMCMHubReconcile %v", substr)

	defer klog.V(1).Infof("exix doMCMHubReconcile %v", substr)

	// TO-DO: need to implement the new appsub rolling update with no deployable dependency

	primaryChannel, secondaryChannel, err := r.getChannel(sub)

	if err != nil {
		klog.Errorf("Failed to find a channel for subscription: %s", sub.GetName())

		return err
	}

	if (primaryChannel != nil && secondaryChannel != nil) &&
		(primaryChannel.Spec.Type != secondaryChannel.Spec.Type) {
		klog.Errorf("he type of primary and secondary channels is different. primary channel type: %s, secondary channel type: %s",
			primaryChannel.Spec.Type, secondaryChannel.Spec.Type)

		newError := fmt.Errorf("he type of primary and secondary channels is different. primary channel type: %s, secondary channel type: %s",
			primaryChannel.Spec.Type, secondaryChannel.Spec.Type)

		return newError
	}

	chnAnnotations := primaryChannel.GetAnnotations()

	if chnAnnotations[appv1.AnnotationResourceReconcileLevel] != "" {
		// When channel reconcile rate is changed, this label is used to trigger
		// managed cluster to pick up the channel change and adjust reconcile rate.
		sublabels := sub.GetLabels()

		if sublabels == nil {
			sublabels = make(map[string]string)
		}

		sublabels[appv1.AnnotationResourceReconcileLevel] = chnAnnotations[appv1.AnnotationResourceReconcileLevel]
		klog.Info("Adding subscription label ", appv1.AnnotationResourceReconcileLevel, ": ", chnAnnotations[appv1.AnnotationResourceReconcileLevel])
		sub.SetLabels(sublabels)
	}

	klog.Infof("subscription: %v/%v", sub.GetNamespace(), sub.GetName())

	var resources []*v1.ObjectReference

	switch tp := strings.ToLower(string(primaryChannel.Spec.Type)); tp {
	case chnv1alpha1.ChannelTypeGit, chnv1alpha1.ChannelTypeGitHub:
		resources, err = r.GetGitResources(sub)
	case chnv1alpha1.ChannelTypeHelmRepo:
		resources, err = getHelmTopoResources(r.Client, r.cfg, primaryChannel, secondaryChannel, sub)
	case chnv1alpha1.ChannelTypeObjectBucket:
		resources, err = r.getObjectBucketResources(sub, primaryChannel, secondaryChannel, objectBucketParent)
	}

	if err != nil {
		klog.Error(err, "Error creating resource list")

		return err
	}

	// get all managed clusters
	clusters, err := r.getClustersByPlacement(sub)

	if err != nil {
		klog.Error("Error in getting clusters:", err)

		return err
	}

	if err := r.createAppPolicyReport(sub, resources, len(clusters)); err != nil {
		klog.Error(err, "Error creating app policy report")

		return err
	}

	err = r.PropagateAppSubManifestWork(sub, clusters)

	return err
}

//GetChannelNamespaceType get the channel namespace and channel type by the given subscription
func (r *ReconcileSubscription) GetChannelNamespaceType(s *appv1alpha1.Subscription) (string, string, string) {
	chNameSpace := ""
	chName := ""
	chType := ""

	if s.Spec.Channel != "" {
		strs := strings.Split(s.Spec.Channel, "/")
		if len(strs) == 2 {
			chNameSpace = strs[0]
			chName = strs[1]
		} else {
			chNameSpace = s.Namespace
		}
	}

	chkey := types.NamespacedName{Name: chName, Namespace: chNameSpace}
	chobj := &chnv1alpha1.Channel{}
	err := r.Get(context.TODO(), chkey, chobj)

	if err == nil {
		chType = string(chobj.Spec.Type)
	}

	return chNameSpace, chName, chType
}

func GetSubscriptionRefChannel(clt client.Client, s *appv1.Subscription) (*chnv1.Channel, *chnv1.Channel, error) {
	primaryChannel, err := parseGetChannel(clt, s.Spec.Channel)

	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.Errorf("primary channel %s not found for subscription %s/%s", s.Spec.Channel, s.GetNamespace(), s.GetName())

			return nil, nil, err
		}
	}

	secondaryChannel, err := parseGetChannel(clt, s.Spec.SecondaryChannel)

	if err != nil {
		klog.Errorf("secondary channel %s not found for subscription %s/%s", s.Spec.SecondaryChannel, s.GetNamespace(), s.GetName())

		return nil, nil, err
	}

	return primaryChannel, secondaryChannel, err
}

func parseGetChannel(clt client.Client, channelName string) (*chnv1.Channel, error) {
	if channelName == "" {
		return nil, nil
	}

	chNameSpace := ""
	chName := ""
	strs := strings.Split(channelName, "/")

	if len(strs) == 2 {
		chNameSpace = strs[0]
		chName = strs[1]
	}

	chkey := types.NamespacedName{Name: chName, Namespace: chNameSpace}
	channel := &chnv1.Channel{}
	err := clt.Get(context.TODO(), chkey, channel)

	if err != nil {
		return nil, err
	}

	return channel, nil
}

func (r *ReconcileSubscription) getChannel(s *appv1alpha1.Subscription) (*chnv1alpha1.Channel, *chnv1alpha1.Channel, error) {
	return GetSubscriptionRefChannel(r.Client, s)
}

// GetChannelGeneration get the channel generation
func (r *ReconcileSubscription) GetChannelGeneration(s *appv1alpha1.Subscription) (string, error) {
	chNameSpace := ""
	chName := ""

	if s.Spec.Channel != "" {
		strs := strings.Split(s.Spec.Channel, "/")
		if len(strs) == 2 {
			chNameSpace = strs[0]
			chName = strs[1]
		} else {
			chNameSpace = s.Namespace
		}
	}

	chkey := types.NamespacedName{Name: chName, Namespace: chNameSpace}
	chobj := &chnv1alpha1.Channel{}
	err := r.Get(context.TODO(), chkey, chobj)

	if err != nil {
		return "", err
	}

	return strconv.FormatInt(chobj.Generation, 10), nil
}

func getStatusPerPackage(pkgStatus *appv1alpha1.SubscriptionUnitStatus, chn *chnv1alpha1.Channel) *appv1alpha1.SubscriptionUnitStatus {
	subUnitStatus := &appv1alpha1.SubscriptionUnitStatus{}

	switch chn.Spec.Type {
	case "HelmRepo":
		subUnitStatus.LastUpdateTime = pkgStatus.LastUpdateTime

		if pkgStatus.ResourceStatus != nil {
			setHelmSubUnitStatus(pkgStatus.ResourceStatus, subUnitStatus)
		} else {
			subUnitStatus.Phase = pkgStatus.Phase
			subUnitStatus.Message = pkgStatus.Message
			subUnitStatus.Reason = pkgStatus.Reason
		}
	default:
		subUnitStatus = pkgStatus
	}

	return subUnitStatus
}

func setHelmSubUnitStatus(pkgResourceStatus *runtime.RawExtension, subUnitStatus *appv1alpha1.SubscriptionUnitStatus) {
	if pkgResourceStatus == nil || subUnitStatus == nil {
		klog.Errorf("failed to setHelmSubUnitStatus due to pkgResourceStatus %v or subUnitStatus %v is nil", pkgResourceStatus, subUnitStatus)
		return
	}

	helmAppStatus := &releasev1.HelmAppStatus{}
	err := json.Unmarshal(pkgResourceStatus.Raw, helmAppStatus)

	if err != nil {
		klog.Error("Failed to unmashall pkgResourceStatus to helm condition. err: ", err)
	}

	subUnitStatus.Phase = "Subscribed"
	subUnitStatus.Message = ""
	subUnitStatus.Reason = ""

	messages := []string{}
	reasons := []string{}

	for _, condition := range helmAppStatus.Conditions {
		if strings.Contains(string(condition.Reason), "Error") {
			subUnitStatus.Phase = "Failed"

			messages = append(messages, condition.Message)

			reasons = append(reasons, string(condition.Reason))
		}
	}

	if len(messages) > 0 {
		subUnitStatus.Message = strings.Join(messages, ", ")
	}

	if len(reasons) > 0 {
		subUnitStatus.Reason = strings.Join(reasons, ", ")
	}
}

func (r *ReconcileSubscription) createAppPolicyReport(sub *appv1alpha1.Subscription, resources []*v1.ObjectReference,
	clusterCount int) error {
	policyReport := &policyReportV1alpha2.PolicyReport{}
	policyReport.Name = sub.Name + "-policyreport-appsub-status"
	policyReport.Namespace = sub.Namespace

	policyReportFound := true

	if err := r.Get(context.TODO(),
		client.ObjectKey{Name: policyReport.Name, Namespace: policyReport.Namespace}, policyReport); err != nil {
		if apierrors.IsNotFound(err) {
			policyReportFound = false
		} else {
			klog.Errorf("Error getting policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)

			return err
		}
	}

	if !policyReportFound {
		klog.V(1).Infof("App policy report: %v/%v not found, create it.", policyReport.Namespace, policyReport.Name)

		policyReport.Labels = map[string]string{
			"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", sub.Namespace+"."+sub.Name),
		}

		results := []*policyReportV1alpha2.PolicyReportResult{}
		result := &policyReportV1alpha2.PolicyReportResult{
			Source:    sub.Namespace + "/" + sub.Name,
			Policy:    "APPSUB_RESOURCE_LIST",
			Timestamp: metav1.Timestamp{Seconds: time.Now().Unix()},
			Result:    "pass",
			Subjects:  resources,
		}
		results = append(results, result)
		policyReport.Results = results

		//initialize placementrule cluster count as the pass count
		policyReport.Summary.Pass = clusterCount
		policyReport.Summary.Fail = 0

		policyReport.SetOwnerReferences([]metav1.OwnerReference{
			*metav1.NewControllerRef(sub, schema.GroupVersionKind{Group: "apps.open-cluster-management.io", Version: "v1", Kind: "Subscription"})})

		if err := r.Create(context.TODO(), policyReport); err != nil {
			klog.Errorf("Error in creating app policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)

			return err
		}
	} else if resources != nil {
		klog.V(1).Infof("App policy report found: %v/%v, update it.", policyReport.Namespace, policyReport.Name)

		var resourceListResult *policyReportV1alpha2.PolicyReportResult
		for _, result := range policyReport.Results {
			if result.Policy == "APPSUB_RESOURCE_LIST" {
				resourceListResult = result

				break
			}
		}

		// Update resource list
		if resourceListResult != nil && reflect.DeepEqual(resourceListResult, resources) {
			klog.V(1).Infof("App policy report(%v/%v) resource list unchanged.", policyReport.Namespace, policyReport.Name)

			return nil
		}

		if resourceListResult == nil {
			if policyReport.Results == nil {
				policyReport.Results = []*policyReportV1alpha2.PolicyReportResult{}
			}

			result := &policyReportV1alpha2.PolicyReportResult{
				Source:    sub.Namespace + "/" + sub.Name,
				Policy:    "APPSUB_RESOURCE_LIST",
				Timestamp: metav1.Timestamp{Seconds: time.Now().Unix()},
				Result:    "pass",
				Subjects:  resources,
			}
			policyReport.Results = append(policyReport.Results, result)
		} else {
			resourceListResult.Subjects = resources
		}

		//reset placementrule cluster count as the pass count
		policyReport.Summary.Pass = clusterCount
		policyReport.Summary.Fail = 0

		if err := r.Update(context.TODO(), policyReport); err != nil {
			klog.Errorf("Error in updating app policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)

			return err
		}
	}

	return nil
}

func (r *ReconcileSubscription) initObjectStore(channel *chnv1alpha1.Channel) (*awsutils.Handler, string, error) {
	var err error

	awshandler := &awsutils.Handler{}

	pathName := channel.Spec.Pathname

	if pathName == "" {
		errmsg := "Empty Pathname in channel " + channel.Spec.Pathname
		klog.Error(errmsg)

		return nil, "", errors.New(errmsg)
	}

	if strings.HasSuffix(pathName, "/") {
		last := len(pathName) - 1
		pathName = pathName[:last]
	}

	loc := strings.LastIndex(pathName, "/")
	endpoint := pathName[:loc]
	bucket := pathName[loc+1:]

	accessKeyID := ""
	secretAccessKey := ""
	region := ""

	if channel.Spec.SecretRef != nil {
		channelSecret := &corev1.Secret{}
		chnseckey := types.NamespacedName{
			Name:      channel.Spec.SecretRef.Name,
			Namespace: channel.Namespace,
		}

		if err := r.Get(context.TODO(), chnseckey, channelSecret); err != nil {
			return nil, "", gerr.Wrap(err, "failed to get reference secret from channel")
		}

		err = yaml.Unmarshal(channelSecret.Data[awsutils.SecretMapKeyAccessKeyID], &accessKeyID)
		if err != nil {
			klog.Error("Failed to unmashall accessKey from secret with error:", err)

			return nil, "", err
		}

		err = yaml.Unmarshal(channelSecret.Data[awsutils.SecretMapKeySecretAccessKey], &secretAccessKey)
		if err != nil {
			klog.Error("Failed to unmashall secretaccessKey from secret with error:", err)

			return nil, "", err
		}

		regionData := channelSecret.Data[awsutils.SecretMapKeyRegion]

		if len(regionData) > 0 {
			err = yaml.Unmarshal(regionData, &region)
			if err != nil {
				klog.Error("Failed to unmashall region from secret with error:", err)

				return nil, "", err
			}
		}
	}

	klog.V(1).Info("Trying to connect to object bucket ", endpoint, "|", bucket)

	if err := awshandler.InitObjectStoreConnection(endpoint, accessKeyID, secretAccessKey, region); err != nil {
		klog.Error(err, "unable initialize object store settings")

		return nil, "", err
	}
	// Check whether the connection is setup successfully
	if err := awshandler.Exists(bucket); err != nil {
		klog.Error(err, "Unable to access object store bucket ", bucket, " for channel ", channel.Name)

		return nil, "", err
	}

	return awshandler, bucket, nil
}

func (r *ReconcileSubscription) getObjectBucketResources(
	sub *appv1alpha1.Subscription, channel, secondaryChannel *chnv1alpha1.Channel, parentType string) ([]*v1.ObjectReference, error) {
	awsHandler, bucket, err := r.initObjectStore(channel)
	if err != nil {
		klog.Error(err, "Unable to access object store: ")

		if secondaryChannel != nil {
			klog.Infof("trying the secondary channel %s", secondaryChannel.Name)
			// Try with secondary channel
			awsHandler, bucket, err = r.initObjectStore(secondaryChannel)

			if err != nil {
				klog.Error(err, "Unable to access object store with channel ", channel.Name)

				return nil, err
			}
		} else {
			klog.Error(err, "Unable to access object store with channel ", channel.Name)

			return nil, err
		}
	}

	var folderName *string

	annotations := sub.GetAnnotations()
	bucketPath := annotations[appv1.AnnotationBucketPath]

	if bucketPath != "" {
		folderName = &bucketPath
	}

	keys, err := awsHandler.List(bucket, folderName)
	klog.V(5).Infof("object keys: %v", keys)

	if err != nil {
		klog.Error("Failed to list objects in bucket ", bucket)

		return nil, err
	}

	// converting template from object store to resource
	resources := []*v1.ObjectReference{}
	for _, key := range keys {
		tplb, err := awsHandler.Get(bucket, key)
		if err != nil {
			klog.Error("Failed to get object ", key, " in bucket ", bucket)

			return nil, err
		}

		// skip empty body object store
		if len(tplb.Content) == 0 {
			continue
		}

		template := &unstructured.Unstructured{}
		err = yaml.Unmarshal(tplb.Content, template)

		if err != nil {
			klog.V(5).Infof("Error in unmarshall template, err:%v |template: %v", err, string(tplb.Content))
			continue
		}

		resource := &v1.ObjectReference{
			Kind:       template.GetKind(),
			Namespace:  template.GetNamespace(),
			Name:       template.GetName(),
			APIVersion: template.GetAPIVersion(),
		}
		resources = append(resources, resource)
	}

	return resources, nil
}
