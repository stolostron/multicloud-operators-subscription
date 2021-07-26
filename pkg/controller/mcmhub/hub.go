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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ghodss/yaml"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	dplutils "github.com/open-cluster-management/multicloud-operators-deployable/pkg/utils"
	releasev1 "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	subutil "github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	awsutils "github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils/aws"
)

// doMCMHubReconcile process Subscription on hub - distribute it via manifestWork
func (r *ReconcileSubscription) doMCMHubReconcile(sub *appv1alpha1.Subscription) error {
	substr := fmt.Sprintf("%v/%v", sub.GetNamespace(), sub.GetName())
	klog.V(1).Infof("entry doMCMHubReconcile %v", substr)

	defer klog.V(1).Infof("exix doMCMHubReconcile %v", substr)

	// TO-DO: need to implement the new appsub rolling update with no deployable dependency

	channel, err := r.getChannel(sub)
	if err != nil {
		klog.Errorf("Failed to find a channel for subscription: %s", sub.GetName())

		return err
	}

	chnAnnotations := channel.GetAnnotations()

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

	// Check and add cluster-admin annotation for multi-namepsace application
	r.AddClusterAdminAnnotation(sub)

	klog.Infof("subscription: %v/%v", sub.GetNamespace(), sub.GetName())

	err = r.PropagateAppSubManifestWork(sub)

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

func GetSubscriptionRefChannel(clt client.Client, s *appv1.Subscription) (*chnv1.Channel, error) {
	chNameSpace := ""
	chName := ""

	if s.Spec.Channel != "" {
		strs := strings.Split(s.Spec.Channel, "/")
		if len(strs) == 2 {
			chNameSpace = strs[0]
			chName = strs[1]
		}
	}

	chkey := types.NamespacedName{Name: chName, Namespace: chNameSpace}
	chobj := &chnv1.Channel{}
	err := clt.Get(context.TODO(), chkey, chobj)

	if err != nil {
		if apierrors.IsNotFound(err) {
			err1 := fmt.Errorf("channel %s/%s not found for subscription %s/%s", chNameSpace, chName, s.GetNamespace(), s.GetName())
			err = err1
		}
	}

	return chobj, err
}

func (r *ReconcileSubscription) getChannel(s *appv1alpha1.Subscription) (*chnv1alpha1.Channel, error) {
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

func (r *ReconcileSubscription) updateSubscriptionStatus(sub *appv1alpha1.Subscription, found *dplv1alpha1.Deployable, chn *chnv1alpha1.Channel) error {
	r.logger.Info(fmt.Sprintf("entry doMCMHubReconcile:updateSubscriptionStatus %s", PrintHelper(sub)))
	defer r.logger.Info(fmt.Sprintf("exit doMCMHubReconcile:updateSubscriptionStatus %s", PrintHelper(sub)))

	newsubstatus := appv1alpha1.SubscriptionStatus{}

	newsubstatus.AnsibleJobsStatus = *sub.Status.AnsibleJobsStatus.DeepCopy()

	newsubstatus.Phase = appv1alpha1.SubscriptionPropagated
	newsubstatus.Message = ""
	newsubstatus.Reason = ""

	msg := ""

	if found.Status.Phase == dplv1alpha1.DeployableFailed {
		newsubstatus.Statuses = nil
	} else {
		newsubstatus.Statuses = make(map[string]*appv1alpha1.SubscriptionPerClusterStatus)

		for cluster, cstatus := range found.Status.PropagatedStatus {
			clusterSubStatus := &appv1alpha1.SubscriptionPerClusterStatus{}
			subPkgStatus := make(map[string]*appv1alpha1.SubscriptionUnitStatus)

			if cstatus.ResourceStatus != nil {
				mcsubstatus := &appv1alpha1.SubscriptionStatus{}
				err := json.Unmarshal(cstatus.ResourceStatus.Raw, mcsubstatus)
				if err != nil {
					klog.Infof("Failed to unmashall ResourceStatus from target cluster: %v, in deployable: %v/%v", cluster, found.GetNamespace(), found.GetName())
					return err
				}

				if msg == "" {
					msg = fmt.Sprintf("%s:%s", cluster, mcsubstatus.Message)
				} else {
					msg += fmt.Sprintf(",%s:%s", cluster, mcsubstatus.Message)
				}

				//get status per package if exist, for namespace/objectStore/helmRepo channel subscription status
				for _, lcStatus := range mcsubstatus.Statuses {
					for pkg, pkgStatus := range lcStatus.SubscriptionPackageStatus {
						subPkgStatus[pkg] = getStatusPerPackage(pkgStatus, chn)
					}
				}

				//if no status per package, apply status.<per cluster>.resourceStatus, for github channel subscription status
				if len(subPkgStatus) == 0 {
					subUnitStatus := &appv1alpha1.SubscriptionUnitStatus{}
					subUnitStatus.LastUpdateTime = mcsubstatus.LastUpdateTime
					subUnitStatus.Phase = mcsubstatus.Phase
					subUnitStatus.Message = mcsubstatus.Message
					subUnitStatus.Reason = mcsubstatus.Reason

					subPkgStatus["/"] = subUnitStatus
				}
			}

			clusterSubStatus.SubscriptionPackageStatus = subPkgStatus

			newsubstatus.Statuses[cluster] = clusterSubStatus
		}
	}

	newsubstatus.LastUpdateTime = sub.Status.LastUpdateTime
	klog.V(5).Info("Check status for ", sub.Namespace, "/", sub.Name, " with ", newsubstatus)
	newsubstatus.Message = msg

	if !utils.IsEqualSubScriptionStatus(&sub.Status, &newsubstatus) {
		klog.V(1).Infof("check subscription status sub: %v/%v, substatus: %#v, newsubstatus: %#v",
			sub.Namespace, sub.Name, sub.Status, newsubstatus)

		//perserve the Ansiblejob status
		newsubstatus.DeepCopyInto(&sub.Status)
	}

	return nil
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

func (r *ReconcileSubscription) getSubscriptionDeployables(sub *appv1alpha1.Subscription) map[string]*dplv1alpha1.Deployable {
	allDpls := make(map[string]*dplv1alpha1.Deployable)

	dplList := &dplv1alpha1.DeployableList{}

	chNameSpace, chName, chType := r.GetChannelNamespaceType(sub)

	dplNamespace := chNameSpace

	if utils.IsGitChannel(chType) {
		// If Git channel, deployables are created in the subscription namespace
		dplNamespace = sub.Namespace
	}

	dplListOptions := &client.ListOptions{Namespace: dplNamespace}

	if sub.Spec.PackageFilter != nil && sub.Spec.PackageFilter.LabelSelector != nil {
		matchLbls := sub.Spec.PackageFilter.LabelSelector.MatchLabels
		matchLbls[chnv1alpha1.KeyChannel] = chName
		matchLbls[chnv1alpha1.KeyChannelType] = chType
		clSelector, err := dplutils.ConvertLabels(sub.Spec.PackageFilter.LabelSelector)

		if err != nil {
			klog.Error("Failed to set label selector of subscrption:", sub.Spec.PackageFilter.LabelSelector, " err: ", err)
			return nil
		}

		dplListOptions.LabelSelector = clSelector
	} else {
		// Handle deployables from multiple channels in the same namespace
		subLabel := make(map[string]string)
		subLabel[chnv1alpha1.KeyChannel] = chName
		subLabel[chnv1alpha1.KeyChannelType] = chType

		if utils.IsGitChannel(chType) {
			subscriptionNameLabel := types.NamespacedName{
				Name:      sub.Name,
				Namespace: sub.Namespace,
			}
			subscriptionNameLabelStr := strings.ReplaceAll(subscriptionNameLabel.String(), "/", "-")

			subLabel[appv1alpha1.LabelSubscriptionName] = subscriptionNameLabelStr
		}

		labelSelector := &metav1.LabelSelector{
			MatchLabels: subLabel,
		}
		chSelector, err := dplutils.ConvertLabels(labelSelector)

		if err != nil {
			klog.Error("Failed to set label selector. err: ", chSelector, " err: ", err)
			return nil
		}

		dplListOptions.LabelSelector = chSelector
	}

	// Sleep so that all deployables are fully created
	time.Sleep(3 * time.Second)

	err := r.Client.List(context.TODO(), dplList, dplListOptions)

	if err != nil {
		klog.Error("Failed to list objects from sbuscription namespace ", sub.Namespace, " err: ", err)
		return nil
	}

	klog.V(1).Info("Hub Subscription found Deployables:", len(dplList.Items))

	for _, dpl := range dplList.Items {
		if !r.checkDeployableBySubcriptionPackageFilter(sub, dpl) {
			continue
		}

		dplkey := types.NamespacedName{Name: dpl.Name, Namespace: dpl.Namespace}.String()
		allDpls[dplkey] = dpl.DeepCopy()
	}

	return allDpls
}

func checkDplPackageName(sub *appv1alpha1.Subscription, dpl dplv1alpha1.Deployable) bool {
	dpltemplate := &unstructured.Unstructured{}

	if dpl.Spec.Template != nil {
		err := json.Unmarshal(dpl.Spec.Template.Raw, dpltemplate)
		if err == nil {
			dplPkgName := dpltemplate.GetName()
			if sub.Spec.Package != "" && sub.Spec.Package != dplPkgName {
				klog.Info("Name does not match, skiping:", sub.Spec.Package, "|", dplPkgName)
				return false
			}
		}
	}

	return true
}

func (r *ReconcileSubscription) checkDeployableBySubcriptionPackageFilter(sub *appv1alpha1.Subscription, dpl dplv1alpha1.Deployable) bool {
	dplanno := dpl.GetAnnotations()

	if sub.Spec.PackageFilter != nil {
		if dplanno == nil {
			dplanno = make(map[string]string)
		}

		if !checkDplPackageName(sub, dpl) {
			return false
		}

		annotations := sub.Spec.PackageFilter.Annotations

		//append deployable template annotations to deployable annotations only if they don't exist in the deployable annotations
		dpltemplate := &unstructured.Unstructured{}

		if dpl.Spec.Template != nil {
			err := json.Unmarshal(dpl.Spec.Template.Raw, dpltemplate)
			if err == nil {
				dplTemplateAnno := dpltemplate.GetAnnotations()
				for k, v := range dplTemplateAnno {
					if dplanno[k] == "" {
						dplanno[k] = v
					}
				}
			}
		}

		vdpl := dpl.GetAnnotations()[dplv1alpha1.AnnotationDeployableVersion]

		klog.V(5).Info("checking annotations package filter: ", annotations)

		if annotations != nil {
			matched := true

			for k, v := range annotations {
				if dplanno[k] != v {
					matched = false
					break
				}
			}

			if !matched {
				return false
			}
		}

		vsub := sub.Spec.PackageFilter.Version
		if vsub != "" {
			vmatch := subutil.SemverCheck(vsub, vdpl)
			klog.V(5).Infof("version check is %v; subscription version filter condition is %v, deployable version is: %v", vmatch, vsub, vdpl)

			if !vmatch {
				return false
			}
		}
	}

	return true
}

func (r *ReconcileSubscription) updateTargetSubscriptionDeployable(sub *appv1alpha1.Subscription, targetSubDpl *dplv1alpha1.Deployable) error {
	targetKey := types.NamespacedName{
		Namespace: targetSubDpl.Namespace,
		Name:      targetSubDpl.Name,
	}

	found := &dplv1alpha1.Deployable{}
	err := r.Get(context.TODO(), targetKey, found)

	if err != nil && apierrors.IsNotFound(err) {
		klog.Info("Creating target Deployable - ", "namespace: ", targetSubDpl.Namespace, ", name: ", targetSubDpl.Name)
		err = r.Create(context.TODO(), targetSubDpl)

		//record events
		addtionalMsg := "target Depolyable " + targetKey.String() + " created in the subscription namespace"
		r.eventRecorder.RecordEvent(sub, "Deploy", addtionalMsg, err)

		return err
	} else if err != nil {
		return err
	}

	orgTpl := &unstructured.Unstructured{}
	err = json.Unmarshal(targetSubDpl.Spec.Template.Raw, orgTpl)

	if err != nil {
		klog.V(5).Info("Error in unmarshall target subscription deployable template, err:", err, " |template: ", string(targetSubDpl.Spec.Template.Raw))
		return err
	}

	fndTpl := &unstructured.Unstructured{}
	err = json.Unmarshal(found.Spec.Template.Raw, fndTpl)

	if err != nil {
		klog.V(5).Info("Error in unmarshall target found subscription deployable template, err:", err, " |template: ", string(found.Spec.Template.Raw))
		return err
	}

	if !reflect.DeepEqual(orgTpl, fndTpl) || !reflect.DeepEqual(targetSubDpl.Spec.Overrides, found.Spec.Overrides) {
		klog.V(5).Infof("Updating target Deployable. orig: %#v, found: %#v", targetSubDpl, found)

		targetSubDpl.Spec.DeepCopyInto(&found.Spec)

		foundanno := found.GetAnnotations()
		if foundanno == nil {
			foundanno = make(map[string]string)
		}

		foundanno[dplv1alpha1.AnnotationIsGenerated] = "true"
		foundanno[dplv1alpha1.AnnotationLocal] = "false"
		found.SetAnnotations(foundanno)

		klog.V(5).Info("Updating Deployable - ", "namespace: ", targetSubDpl.Namespace, " ,name: ", targetSubDpl.Name)

		err = r.Update(context.TODO(), found)

		//record events
		addtionalMsg := "target Depolyable " + targetKey.String() + " updated in the subscription namespace"
		r.eventRecorder.RecordEvent(sub, "Deploy", addtionalMsg, err)

		if err != nil {
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

func generateDplNameFromKey(key string) string {
	return strings.ReplaceAll(key, "/", "-")
}
