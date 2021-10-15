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

package objectbucket

import (
	"errors"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"

	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	awsutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/aws"
)

var SubscriptionGVK = schema.GroupVersionKind{Group: "apps.open-cluster-management.io", Kind: "Subscription", Version: "v1"}

// SubscriberItem - defines the unit of namespace subscription.
type SubscriberItem struct {
	appv1.SubscriberItem

	count         int
	reconcileRate string
	syncTime      string
	bucket        string
	objectStore   awsutils.ObjectStore
	stopch        chan struct{}
	successful    bool
	clusterAdmin  bool
	syncinterval  int
	synchronizer  SyncSource
}

// SubscribeItem subscribes a subscriber item with namespace channel.
func (obsi *SubscriberItem) Start(restart bool) {
	// do nothing if already started
	if obsi.stopch != nil {
		if restart {
			// restart this goroutine
			klog.Info("Stopping object SubscriberItem: ", obsi.Subscription.Name)
			obsi.Stop()
		} else {
			klog.Info("object SubscriberItem already started: ", obsi.Subscription.Name)

			return
		}
	}

	obsi.stopch = make(chan struct{})

	loopPeriod, retryInterval, retries := utils.GetReconcileInterval(obsi.reconcileRate, chnv1.ChannelTypeObjectBucket)
	klog.Infof("reconcileRate: %v, loopPeriod: %v, retryInterval: %v, retries: %v", obsi.reconcileRate, loopPeriod, retryInterval, retries)

	if strings.EqualFold(obsi.reconcileRate, "off") {
		klog.Infof("auto-reconcile is OFF")

		obsi.doSubscriptionWithRetries(retryInterval, retries)

		return
	}

	go wait.Until(func() {
		tw := obsi.SubscriberItem.Subscription.Spec.TimeWindow
		if tw != nil {
			nextRun := utils.NextStartPoint(tw, time.Now())
			if nextRun > time.Duration(0) {
				klog.Infof("Subscription is currently blocked by the time window. It %v/%v will be deployed after %v",
					obsi.SubscriberItem.Subscription.GetNamespace(),
					obsi.SubscriberItem.Subscription.GetName(), nextRun)

				return
			}
		}

		// if the subscription pause lable is true, stop subscription here.
		if utils.GetPauseLabel(obsi.SubscriberItem.Subscription) {
			klog.Infof("Object bucket Subscription %v/%v is paused.", obsi.SubscriberItem.Subscription.GetNamespace(), obsi.SubscriberItem.Subscription.GetName())

			return
		}

		obsi.doSubscriptionWithRetries(retryInterval, retries)
	}, loopPeriod, obsi.stopch)
}

// Stop the subscriber.
func (obsi *SubscriberItem) Stop() {
	if obsi.stopch != nil {
		close(obsi.stopch)
		obsi.stopch = nil
	}
}

// Compare current object store subscription error  with the new object store initialization error.
func (obsi *SubscriberItem) CompareOjbectStoreStatus(initObjectStoreErr error) bool {
	if initObjectStoreErr == nil {
		if obsi.Subscription.Status.Reason == "" {
			return true
		}
	} else {
		if strings.EqualFold(obsi.Subscription.Status.Reason, initObjectStoreErr.Error()) {
			return true
		}
	}

	return false
}

func (obsi *SubscriberItem) getChannelConfig(primary bool) (endpoint, accessKeyID, secretAccessKey, region string, err error) {
	channel := obsi.Channel

	if !primary {
		channel = obsi.SecondaryChannel
	}

	pathName := channel.Spec.Pathname

	if pathName == "" {
		errmsg := "Empty Pathname in channel " + channel.Name
		klog.Error(errmsg)

		return "", "", "", "", errors.New(errmsg)
	}

	if strings.HasSuffix(pathName, "/") {
		last := len(pathName) - 1
		pathName = pathName[:last]
	}

	loc := strings.LastIndex(pathName, "/")
	endpoint = pathName[:loc]
	obsi.bucket = pathName[loc+1:]
	secret := obsi.ChannelSecret

	if !primary {
		secret = obsi.SecondaryChannelSecret
	}

	if secret != nil {
		err = yaml.Unmarshal(secret.Data[awsutils.SecretMapKeyAccessKeyID], &accessKeyID)
		if err != nil {
			klog.Error("Failed to unmashall accessKey from secret with error:", err)

			return "", "", "", "", err
		}

		err = yaml.Unmarshal(secret.Data[awsutils.SecretMapKeySecretAccessKey], &secretAccessKey)
		if err != nil {
			klog.Error("Failed to unmashall secretaccessKey from secret with error:", err)

			return "", "", "", "", err
		}

		regionData := secret.Data[awsutils.SecretMapKeyRegion]

		if len(regionData) > 0 {
			err = yaml.Unmarshal(regionData, &region)
			if err != nil {
				klog.Error("Failed to unmashall region from secret with error:", err)

				return "", "", "", "", err
			}
		}
	}

	return endpoint, accessKeyID, secretAccessKey, region, nil
}

func (obsi *SubscriberItem) getAwsHandler(primary bool) error {
	awshandler := &awsutils.Handler{}

	endpoint, accessKeyID, secretAccessKey, region, err := obsi.getChannelConfig(primary)

	if err != nil {
		return err
	}

	klog.V(1).Info("Trying to connect to object bucket ", endpoint, "|", obsi.bucket)

	if err := awshandler.InitObjectStoreConnection(endpoint, accessKeyID, secretAccessKey, region); err != nil {
		klog.Error(err, "unable initialize object store settings")
		return err
	}
	// Check whether the connection is setup successfully
	if err := awshandler.Exists(obsi.bucket); err != nil {
		klog.Error(err, "Unable to access object store bucket ", obsi.bucket, " for channel ", obsi.Channel.Name)
		return err
	}

	obsi.objectStore = awshandler

	return nil
}

func (obsi *SubscriberItem) initObjectStore() error {
	// Get AWS handler with the primary channel first
	err := obsi.getAwsHandler(true)

	if err != nil {
		if obsi.SecondaryChannel == nil {
			return err
		}

		klog.Warning("failed to connect with the primary channel, err: " + err.Error())
		klog.Info("trying with the secondary channel")

		err2 := obsi.getAwsHandler(false)

		if err2 != nil {
			klog.Error("failed to connect with the secondary channel, err: " + err2.Error())
			return err2
		}
	}

	return nil
}

// In aws s3 bucket, key could contain folder name. e.g. subfolder1/configmap3.yaml
// As a result, the hosting deployable annotation (NamespacedName) will be <namespace>/subfolder1/configmap3.yaml
// The invalid hosting deployable annotation will break the synchronizer

func generateDplNameFromKey(key string) string {
	return strings.ReplaceAll(key, "/", "-")
}

func (obsi *SubscriberItem) doSubscriptionWithRetries(retryInterval time.Duration, retries int) {
	obsi.doSubscription()

	// If the initial subscription fails, retry.
	n := 0

	for n < retries {
		if !obsi.successful {
			time.Sleep(retryInterval)
			klog.Infof("Re-try #%d: subcribing to the object bucket: %v", n+1, obsi.bucket)
			obsi.doSubscription()
			n++
		} else {
			break
		}
	}
}

func (obsi *SubscriberItem) doSubscription() {
	var folderName *string

	//Update the secret and config map
	if obsi.Channel != nil {
		sec, cm := utils.FetchChannelReferences(obsi.synchronizer.GetRemoteNonCachedClient(), *obsi.Channel)
		if sec != nil {
			if err := utils.ListAndDeployReferredObject(obsi.synchronizer.GetLocalNonCachedClient(), obsi.Subscription,
				schema.GroupVersionKind{Group: "", Kind: "Secret", Version: "v1"}, sec); err != nil {
				klog.Warningf("can't deploy reference secret %v for subscription %v", obsi.ChannelSecret.GetName(), obsi.Subscription.GetName())
			}
		}

		if cm != nil {
			if err := utils.ListAndDeployReferredObject(obsi.synchronizer.GetLocalNonCachedClient(), obsi.Subscription,
				schema.GroupVersionKind{Group: "", Kind: "ConfigMap", Version: "v1"}, cm); err != nil {
				klog.Warningf("can't deploy reference configmap %v for subscription %v", obsi.ChannelConfigMap.GetName(), obsi.Subscription.GetName())
			}
		}

		sec, cm = utils.FetchChannelReferences(obsi.synchronizer.GetLocalNonCachedClient(), *obsi.Channel)
		if sec != nil {
			klog.V(1).Info("updated in memory channel secret for ", obsi.Subscription.Name)
			obsi.ChannelSecret = sec
		}

		if cm != nil {
			klog.V(1).Info("updated in memory channel configmap for ", obsi.Subscription.Name)
			obsi.ChannelConfigMap = cm
		}
	}

	if obsi.SecondaryChannel != nil {
		sec, cm := utils.FetchChannelReferences(obsi.synchronizer.GetRemoteNonCachedClient(), *obsi.SecondaryChannel)
		if sec != nil {
			if err := utils.ListAndDeployReferredObject(obsi.synchronizer.GetLocalNonCachedClient(), obsi.Subscription,
				schema.GroupVersionKind{Group: "", Kind: "Secret", Version: "v1"}, sec); err != nil {
				klog.Warningf("can't deploy reference secondary secret %v for subscription %v", obsi.SecondaryChannelSecret.GetName(), obsi.Subscription.GetName())
			}
		}

		if cm != nil {
			if err := utils.ListAndDeployReferredObject(obsi.synchronizer.GetLocalNonCachedClient(), obsi.Subscription,
				schema.GroupVersionKind{Group: "", Kind: "ConfigMap", Version: "v1"}, cm); err != nil {
				klog.Warningf("can't deploy reference secondary configmap %v for subscription %v", obsi.SecondaryChannelConfigMap.GetName(), obsi.Subscription.GetName())
			}
		}

		sec, cm = utils.FetchChannelReferences(obsi.synchronizer.GetLocalNonCachedClient(), *obsi.SecondaryChannel)
		if sec != nil {
			klog.Info("updated in memory secondary channel secret for ", obsi.Subscription.Name)
			obsi.SecondaryChannelSecret = sec
		}

		if cm != nil {
			klog.V(1).Info("updated in memory secondary channel configmap for ", obsi.Subscription.Name)
			obsi.SecondaryChannelConfigMap = cm
		}
	}

	err := obsi.initObjectStore()

	if err != nil {
		klog.Errorf("Unable to initialize object store connection for subscription. sub: %v, channel: %v, err: %v ", obsi.Subscription.Name, obsi.Channel.Name, err)
		obsi.successful = false

		return
	}

	annotations := obsi.Subscription.GetAnnotations()
	bucketPath := annotations[appv1.AnnotationBucketPath]

	if bucketPath != "" {
		folderName = &bucketPath
	}

	keys, err := obsi.objectStore.List(obsi.bucket, folderName)
	klog.Infof("object keys: %v", keys)

	if err != nil {
		klog.Error("Failed to list objects in bucket ", obsi.bucket)
		obsi.successful = false

		return
	}

	tpls := []unstructured.Unstructured{}

	// converting template from obeject store to DPL
	for _, key := range keys {
		tplb, err := obsi.objectStore.Get(obsi.bucket, key)
		if err != nil {
			klog.Error("Failed to get object ", key, " in bucket ", obsi.bucket)
			obsi.successful = false

			return
		}

		// skip empty body object store
		if len(tplb.Content) == 0 {
			continue
		}

		tpl := &unstructured.Unstructured{}
		err = yaml.Unmarshal(tplb.Content, tpl)

		if err != nil {
			klog.Error("Failed to unmashall ", obsi.bucket, "/", key, " err:", err)
			obsi.successful = false

			return
		}

		tpls = append(tpls, *tpl)
	}

	resources := make([]kubesynchronizer.ResourceUnit, 0)

	// track if there's any error when doSubscribeManifest, if there's any, then we should retry this
	var doErr error

	for _, tpl := range tpls {
		resource, err := obsi.doSubscribeManifest(&tpl)

		if err != nil {
			klog.Errorf("object bucket failed to package deployable, err: %v", err)

			doErr = err

			continue
		}

		resources = append(resources, *resource)
	}

	allowedGroupResources, deniedGroupResources := utils.GetAllowDenyLists(*obsi.Subscription)

	if err := obsi.synchronizer.ProcessSubResources(obsi.Subscription, resources, allowedGroupResources, deniedGroupResources, false); err != nil {
		klog.Error(err)

		obsi.successful = false

		return
	}

	if doErr != nil {
		obsi.successful = false

		return
	}

	obsi.successful = true
}

func (obsi *SubscriberItem) doSubscribeManifest(template *unstructured.Unstructured) (*kubesynchronizer.ResourceUnit, error) {
	tplName := template.GetName()
	// Set app label
	utils.SetPartOfLabel(obsi.SubscriberItem.Subscription, template)

	if obsi.Subscription.Spec.PackageFilter != nil {
		if obsi.Subscription.Spec.Package != "" && obsi.Subscription.Spec.Package != tplName {
			errmsg := "Name does not match, skiping:" + obsi.Subscription.Spec.Package + "|" + tplName
			klog.Info(errmsg)

			return nil, errors.New(errmsg)
		}

		if !utils.LabelChecker(obsi.Subscription.Spec.PackageFilter.LabelSelector, template.GetLabels()) {
			errmsg := "Failed to pass label check to deployable " + tplName
			klog.Info(errmsg)

			return nil, errors.New(errmsg)
		}

		annotations := obsi.Subscription.Spec.PackageFilter.Annotations
		if annotations != nil {
			dplanno := template.GetAnnotations()
			if dplanno == nil {
				dplanno = make(map[string]string)
			}

			matched := true

			for k, v := range annotations {
				if dplanno[k] != v {
					klog.Info("Annotation filter does not match:", k, "|", v, "|", dplanno[k])

					matched = false

					break
				}
			}

			if !matched {
				errmsg := "Failed to pass annotation check to deployable " + tplName
				klog.Info(errmsg)

				return nil, errors.New(errmsg)
			}
		}
	}

	template, err := utils.OverrideResourceBySubscription(template, tplName, obsi.Subscription)
	if err != nil {
		errmsg := "Failed override package " + tplName + " with error: " + err.Error()

		klog.Info(errmsg)

		return nil, errors.New(errmsg)
	}

	template.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: SubscriptionGVK.Version,
		Kind:       SubscriptionGVK.Kind,
		Name:       obsi.Subscription.Name,
		UID:        obsi.Subscription.UID,
	}})

	validgvk := template.GetObjectKind().GroupVersionKind()

	subAnnotations := obsi.Subscription.GetAnnotations()
	if subAnnotations != nil {
		rscAnnotations := template.GetAnnotations()
		if rscAnnotations == nil {
			rscAnnotations = make(map[string]string)
		}

		if strings.EqualFold(subAnnotations[appv1.AnnotationClusterAdmin], "true") {
			rscAnnotations[appv1.AnnotationClusterAdmin] = "true"
		}

		if subAnnotations[appv1.AnnotationResourceReconcileOption] != "" {
			rscAnnotations[appv1.AnnotationResourceReconcileOption] = subAnnotations[appv1.AnnotationResourceReconcileOption]
		}

		template.SetAnnotations(rscAnnotations)
	}

	if obsi.clusterAdmin {
		klog.Info("cluster-admin is true.")

		if template.GetNamespace() != "" {
			klog.Info("Using resource's original namespace. Resource namespace is " + template.GetNamespace())
		} else {
			klog.Info("Setting it to subscription namespace " + obsi.Subscription.Namespace)
			template.SetNamespace(obsi.Subscription.Namespace)
		}
	} else {
		klog.Info("No cluster-admin. Setting it to subscription namespace " + obsi.Subscription.Namespace)
		template.SetNamespace(obsi.Subscription.Namespace)
	}

	resource := &kubesynchronizer.ResourceUnit{Resource: template, Gvk: validgvk}

	return resource, nil
}
