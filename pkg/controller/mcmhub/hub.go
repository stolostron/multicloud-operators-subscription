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
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	chnv1alpha1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	"github.com/ghodss/yaml"
	releasev1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appsubreportv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	awsutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/aws"
)

// doMCMHubReconcile process Subscription on hub - distribute it via manifestWork
func (r *ReconcileSubscription) doMCMHubReconcile(sub *appv1alpha1.Subscription) error {
	substr := fmt.Sprintf("%v/%v", sub.GetNamespace(), sub.GetName())
	klog.V(1).Infof("entry doMCMHubReconcile %v", substr)

	defer klog.V(1).Infof("exit doMCMHubReconcile %v", substr)

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

	// Check and add cluster-admin annotation for multi-namepsace application
	isAdmin := r.AddClusterAdminAnnotation(sub)

	var resources []*v1.ObjectReference

	switch tp := strings.ToLower(string(primaryChannel.Spec.Type)); tp {
	case chnv1alpha1.ChannelTypeGit, chnv1alpha1.ChannelTypeGitHub:
		resources, err = r.GetGitResources(sub, isAdmin)
	case chnv1alpha1.ChannelTypeHelmRepo:
		resources, err = getHelmTopoResources(r.Client, r.cfg, primaryChannel, secondaryChannel, sub, isAdmin)
	case chnv1alpha1.ChannelTypeObjectBucket:
		resources, err = r.getObjectBucketResources(sub, primaryChannel, secondaryChannel, objectBucketParent, isAdmin)
	}

	if err != nil {
		klog.Error(err, "Error creating resource list")

		return err
	}

	// get all managed clusters
	clusters, err := r.getClustersByPlacement(sub)

	if err != nil {
		klog.Error("Error in getting clusters:", err)

		if err := r.createAppAppsubReport(sub, resources, 1, 1); err != nil {
			klog.Error(err, "Error creating app appsubReport")
		}

		return err
	}

	if err := r.createAppAppsubReport(sub, resources, 0, len(clusters)); err != nil {
		klog.Error(err, "Error creating app appsubReport")

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

func (r *ReconcileSubscription) createAppAppsubReport(sub *appv1alpha1.Subscription, resources []*v1.ObjectReference,
	propagationFailedCount, clusterCount int) error {
	appsubReport := &appsubreportv1alpha1.SubscriptionReport{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      sub.Name,
			Namespace: sub.Namespace,
		},
		Resources:  resources,
		ReportType: "Application",
	}

	appsubReportFound := true

	if err := r.Get(context.TODO(),
		client.ObjectKey{Name: appsubReport.Name, Namespace: appsubReport.Namespace}, appsubReport); err != nil {
		if apierrors.IsNotFound(err) {
			appsubReportFound = false
		} else {
			klog.Errorf("Error getting AppsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)

			return err
		}
	}

	if !appsubReportFound {
		klog.V(1).Infof("App appsubReport: %v/%v not found, create it.", appsubReport.Namespace, appsubReport.Name)

		appsubReport.Labels = map[string]string{
			"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", sub.Namespace+"."+sub.Name),
		}

		results := []*appsubreportv1alpha1.SubscriptionReportResult{}
		result := &appsubreportv1alpha1.SubscriptionReportResult{
			Source:    sub.Namespace + "/" + sub.Name,
			Timestamp: metav1.Timestamp{Seconds: time.Now().Unix()},
			Result:    "deployed",
		}
		results = append(results, result)
		appsubReport.Results = results

		//initialize placementrule cluster count as the pass count
		appsubReport.Summary.Deployed = 0
		appsubReport.Summary.Failed = 0
		appsubReport.Summary.PropagationFailed = propagationFailedCount
		appsubReport.Summary.Clusters = clusterCount

		if propagationFailedCount > 0 {
			appsubReport.Summary.InProgress = 0
		} else {
			appsubReport.Summary.InProgress = clusterCount
		}

		appsubReport.SetOwnerReferences([]metav1.OwnerReference{
			*metav1.NewControllerRef(sub, schema.GroupVersionKind{Group: "apps.open-cluster-management.io", Version: "v1", Kind: "Subscription"})})

		if err := r.Create(context.TODO(), appsubReport); err != nil {
			klog.Errorf("Error in creating app AppsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)

			return err
		}
	} else {
		klog.V(1).Infof("App appsubReport found: %v/%v, update it.", appsubReport.Namespace, appsubReport.Name)

		// Update resource list
		resourceList := appsubReport.Resources
		if resourceList != nil && reflect.DeepEqual(resourceList, resources) {
			klog.V(1).Infof("App appsubReport(%v/%v) resource list unchanged.", appsubReport.Namespace, appsubReport.Name)

			return nil
		}

		appsubReport.Resources = resources

		//reset placementrule cluster count as the pass count
		if propagationFailedCount > 0 {
			appsubReport.Summary.InProgress = 0
		} else {
			appsubReport.Summary.InProgress = clusterCount
		}

		appsubReport.Summary.PropagationFailed = propagationFailedCount
		appsubReport.Summary.Clusters = clusterCount

		if err := r.Update(context.TODO(), appsubReport); err != nil {
			klog.Errorf("Error in updating app AppsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)

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

func (r *ReconcileSubscription) getObjectBucketResources(sub *appv1alpha1.Subscription, channel, secondaryChannel *chnv1alpha1.Channel,
	parentType string, isAdmin bool) ([]*v1.ObjectReference, error) {
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
	var errMsgs []string
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

		errs := validation.IsDNS1123Subdomain(template.GetName())
		if len(errs) > 0 {
			errs = append([]string{fmt.Sprintf("Invalid %s name '%s'", template.GetKind(), template.GetName())}, errs...)
			errMsgs = append(errMsgs, strings.Join(errs, ","))
		}

		resource := &v1.ObjectReference{
			Kind:       template.GetKind(),
			Namespace:  template.GetNamespace(),
			Name:       template.GetName(),
			APIVersion: template.GetAPIVersion(),
		}

		// No need to save the namespace object to the resource list of the appsub
		if resource.Kind == "Namespace" {
			continue
		}

		// respect object customized namespace if the appsub user is subscription admin, or apply it to appsub namespace
		if isAdmin {
			if resource.Namespace == "" {
				resource.Namespace = sub.Namespace
			}
		} else {
			resource.Namespace = sub.Namespace
		}

		resources = append(resources, resource)
	}

	if len(errMsgs) > 0 {
		return resources, errors.New(strings.Join(errMsgs, ","))
	}

	return resources, nil
}
