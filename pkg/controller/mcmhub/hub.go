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

package mcmhub

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	gerr "github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	"github.com/ghodss/yaml"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appsubreportv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	helmops "open-cluster-management.io/multicloud-operators-subscription/pkg/subscriber/helmrepo"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	awsutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/aws"
)

// doMCMHubReconcile process Subscription on hub - distribute it via manifestWork
func (r *ReconcileSubscription) doMCMHubReconcile(sub *appv1.Subscription) error {
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
		klog.Errorf("the type of primary and secondary channels is different. primary channel type: %s, secondary channel type: %s",
			primaryChannel.Spec.Type, secondaryChannel.Spec.Type)

		newError := fmt.Errorf("the type of primary and secondary channels is different. primary channel type: %s, secondary channel type: %s",
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

	klog.Infof("subscribing subscription: %v/%v", sub.GetNamespace(), sub.GetName())

	// Check and add cluster-admin annotation for multi-namepsace application
	isAdmin := r.AddClusterAdminAnnotation(sub)

	// Add or sync application labels
	r.AddAppLabels(sub)

	// Skip fetching resources and creating appsub report if skip hub validation is true
	if shouldSkipHubValidation(sub) {
		clusters, err := r.getClustersByPlacement(sub)
		if err != nil {
			klog.Error("Error in getting clusters:", err)
			return err
		}

		return r.PropagateAppSubManifestWork(sub, clusters)
	}

	var resources []*v1.ObjectReference

	switch tp := strings.ToLower(string(primaryChannel.Spec.Type)); tp {
	case chnv1.ChannelTypeGit, chnv1.ChannelTypeGitHub:
		resources, err = r.GetGitResources(sub, isAdmin)
	case chnv1.ChannelTypeHelmRepo:
		helmRls, err := helmops.GetSubscriptionChartsOnHub(r.Client, primaryChannel, secondaryChannel, sub)
		if err != nil {
			klog.Errorf("failed to get the chart index for helm subscription %v, err: %v", ObjectString(sub), err)

			return err
		}

		resources = getHelmTopoResources(helmRls, r.Client, r.cfg, r.restMapper, sub, isAdmin)
	case chnv1.ChannelTypeObjectBucket:
		resources, err = r.getObjectBucketResources(sub, primaryChannel, secondaryChannel, isAdmin)
	}

	if err != nil {
		klog.Error(err, "Error creating resource list")

		return err
	}

	// if app resource list is empty, we simply regard the appsub status as successful
	if len(resources) == 0 {
		klog.Infof("empty app resource list, appsub: %v/%v", sub.Namespace, sub.Name)

		return nil
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

func (r *ReconcileSubscription) AddAppLabels(s *appv1.Subscription) {
	labels := s.GetLabels()

	if labels == nil {
		labels = make(map[string]string)
	}

	if labels["app"] != "" { // if label "app" exists, sync with "app.kubernetes.io/part-of" label
		if labels["app.kubernetes.io/part-of"] != labels["app"] {
			labels["app.kubernetes.io/part-of"] = labels["app"]
		}
	} else { // if "app" label does not exist, set it and "app.kubernetes.io/part-of" label with the subscription name
		if labels["app.kubernetes.io/part-of"] != s.Name {
			labels["app.kubernetes.io/part-of"] = s.Name
		}

		if labels["app"] != s.Name {
			labels["app"] = s.Name
		}
	}

	s.SetLabels(labels)
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

func (r *ReconcileSubscription) getChannel(s *appv1.Subscription) (*chnv1.Channel, *chnv1.Channel, error) {
	return GetSubscriptionRefChannel(r.Client, s)
}

func (r *ReconcileSubscription) IsNamespacedResource(group, version, kind string) bool {
	pkgGK := schema.GroupKind{
		Kind:  kind,
		Group: group,
	}

	mapping, err := r.restMapper.RESTMapping(pkgGK, version)
	if err != nil {
		klog.Errorf("Failed to get GVR from restmapping, keep the original namespace: group: %v, version: %v, kind: %v, err:%v",
			group, version, kind, err)

		return true
	}

	var isNamespaced = true

	if mapping.Scope.Name() != "namespace" {
		isNamespaced = false
	}

	klog.Infof("group: %v, version: %v, kind: %v, scope: %#v, isNamespaced: %v",
		group, version, kind, mapping.Scope, isNamespaced)

	return isNamespaced
}

func (r *ReconcileSubscription) createAppAppsubReport(sub *appv1.Subscription, resources []*v1.ObjectReference,
	propagationFailedCount, clusterCount int) error {
	// remove resource.namespace if the resource is cluster scoped
	for _, resource := range resources {
		pkgGroup, pkgVersion := utils.ParseAPIVersion(resource.APIVersion)

		if pkgGroup == "" && pkgVersion == "" {
			klog.Infof("invalid apiversion: %v", resource)
		} else {
			isNamespaced := r.IsNamespacedResource(pkgGroup, pkgVersion, resource.Kind)

			if !isNamespaced {
				resource.Namespace = ""
			}
		}
	}

	appsubReport := &appsubreportv1alpha1.SubscriptionReport{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SubscriptionReport",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
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
		klog.Infof("App appsubReport: %v/%v not found, create it.", appsubReport.Namespace, appsubReport.Name)

		appsubReport.Labels = map[string]string{
			"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", sub.Namespace+"."+sub.Name),
		}

		//initialize placementrule cluster count as the pass count
		appsubReport.Summary.Deployed = "0"
		appsubReport.Summary.Failed = "0"
		appsubReport.Summary.PropagationFailed = strconv.Itoa(propagationFailedCount)
		appsubReport.Summary.Clusters = strconv.Itoa(clusterCount)

		if propagationFailedCount > 0 {
			appsubReport.Summary.InProgress = "0"
		} else {
			appsubReport.Summary.InProgress = strconv.Itoa(clusterCount)
		}

		appsubReport.SetOwnerReferences([]metav1.OwnerReference{
			*metav1.NewControllerRef(sub, schema.GroupVersionKind{Group: "apps.open-cluster-management.io", Version: "v1", Kind: "Subscription"})})

		if err := r.Create(context.TODO(), appsubReport); err != nil {
			klog.Errorf("Error in creating app AppsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)

			return err
		}
	} else {
		klog.Infof("App appsubReport found: %v/%v, update it.", appsubReport.Namespace, appsubReport.Name)

		if propagationFailedCount > 0 {
			klog.Infof("Failed to get clusters from placement, exit without updating appsubReport")

			return nil
		}

		// Always apply the new resources list
		appsubReport.Resources = resources

		// update counts
		deployed, err := strconv.Atoi(appsubReport.Summary.Deployed)
		if err != nil {
			deployed = 0
		}

		var failed int
		failed, err = strconv.Atoi(appsubReport.Summary.Failed)

		if err != nil {
			failed = 0
		}

		appsubReport.Summary.InProgress = strconv.Itoa(clusterCount - deployed - failed)
		appsubReport.Summary.PropagationFailed = strconv.Itoa(propagationFailedCount)
		appsubReport.Summary.Clusters = strconv.Itoa(clusterCount)

		if err := r.Update(context.TODO(), appsubReport); err != nil {
			klog.Errorf("Error in updating app AppsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)

			return err
		}
	}

	return nil
}

func (r *ReconcileSubscription) initObjectStore(channel *chnv1.Channel) (*awsutils.Handler, string, error) {
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
	objInsecureSkipVerify := "false"
	objCaCert := ""

	if channel.Spec.SecretRef != nil {
		channelSecret := &v1.Secret{}
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

	if channel.Spec.ConfigMapRef != nil {
		configMapRet := utils.GetChannelConfigMap(r.Client, channel)

		if configMapRet != nil {
			objCaCert = configMapRet.Data[appv1.ChannelCertificateData]
			if objCaCert != "" {
				r.logger.Info("ObjectStore channel config map with CA certs found")
			}
		}
	}

	if channel.Spec.InsecureSkipVerify {
		objInsecureSkipVerify = "true"
	}

	klog.V(1).Info("Trying to connect to object bucket ", endpoint, "|", bucket)

	if err := awshandler.InitObjectStoreConnection(
		endpoint, accessKeyID, secretAccessKey, region, objInsecureSkipVerify, objCaCert); err != nil {
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

func (r *ReconcileSubscription) getObjectBucketResources(sub *appv1.Subscription, channel, secondaryChannel *chnv1.Channel,
	isAdmin bool) ([]*v1.ObjectReference, error) {
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
