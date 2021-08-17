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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"

	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	awsutils "github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils/aws"
)

var SubscriptionGVK = schema.GroupVersionKind{Group: "apps.open-cluster-management.io", Kind: "Subscription", Version: "v1"}

// SubscriberItem - defines the unit of namespace subscription.
type SubscriberItem struct {
	appv1.SubscriberItem

	bucket       string
	objectStore  awsutils.ObjectStore
	stopch       chan struct{}
	successful   bool
	syncinterval int
	synchronizer SyncSource
}

// SubscribeItem subscribes a subscriber item with namespace channel.
func (obsi *SubscriberItem) Start() error {
	err := obsi.initObjectStore()

	// If the new object store connection status (successful or failed) is different, return for updating the appsub status
	// This will trigger another reconcile.
	if !obsi.CompareOjbectStoreStatus(err) {
		return err
	}

	// If the object bucket connection fails, stop the object bucket subscription
	// At this stage, there is no app status phase change, no new reconcile will happen.
	if err != nil {
		klog.Errorf("Unable to initialize object store connection for subscription. sub: %v, channel: %v, err: %v ", obsi.Subscription.Name, obsi.Channel.Name, err)

		return err
	}

	// do nothing if already started
	if obsi.stopch != nil {
		return nil
	}

	obsi.stopch = make(chan struct{})

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

		if !obsi.successful {
			err := obsi.doSubscription()

			if err != nil {
				klog.Error("Object Bucket ", obsi.Subscription.Namespace, "/", obsi.Subscription.Name, "housekeeping failed with error: ", err)
			} else {
				obsi.successful = true
			}
		}
	}, time.Duration(obsi.syncinterval)*time.Second, obsi.stopch)

	return nil
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

func (obsi *SubscriberItem) initObjectStore() error {
	var err error

	awshandler := &awsutils.Handler{}

	pathName := obsi.Channel.Spec.Pathname

	if pathName == "" {
		errmsg := "Empty Pathname in channel " + obsi.Channel.Name
		klog.Error(errmsg)

		return errors.New(errmsg)
	}

	if strings.HasSuffix(pathName, "/") {
		last := len(pathName) - 1
		pathName = pathName[:last]
	}

	loc := strings.LastIndex(pathName, "/")
	endpoint := pathName[:loc]
	obsi.bucket = pathName[loc+1:]

	accessKeyID := ""
	secretAccessKey := ""
	region := ""

	if obsi.ChannelSecret != nil {
		err = yaml.Unmarshal(obsi.ChannelSecret.Data[awsutils.SecretMapKeyAccessKeyID], &accessKeyID)
		if err != nil {
			klog.Error("Failed to unmashall accessKey from secret with error:", err)

			return err
		}

		err = yaml.Unmarshal(obsi.ChannelSecret.Data[awsutils.SecretMapKeySecretAccessKey], &secretAccessKey)
		if err != nil {
			klog.Error("Failed to unmashall secretaccessKey from secret with error:", err)

			return err
		}

		regionData := obsi.ChannelSecret.Data[awsutils.SecretMapKeyRegion]

		if len(regionData) > 0 {
			err = yaml.Unmarshal(regionData, &region)
			if err != nil {
				klog.Error("Failed to unmashall region from secret with error:", err)

				return err
			}
		}
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

// In aws s3 bucket, key could contain folder name. e.g. subfolder1/configmap3.yaml
// As a result, the hosting deployable annotation (NamespacedName) will be <namespace>/subfolder1/configmap3.yaml
// The invalid hosting deployable annotation will break the synchronizer

func generateDplNameFromKey(key string) string {
	return strings.ReplaceAll(key, "/", "-")
}

func (obsi *SubscriberItem) doSubscription() error {
	var folderName *string

	annotations := obsi.Subscription.GetAnnotations()
	bucketPath := annotations[appv1.AnnotationBucketPath]

	if bucketPath != "" {
		folderName = &bucketPath
	}

	keys, err := obsi.objectStore.List(obsi.bucket, folderName)
	klog.V(5).Infof("object keys: %v", keys)

	if err != nil {
		klog.Error("Failed to list objects in bucket ", obsi.bucket)

		return err
	}

	tpls := []unstructured.Unstructured{}

	// converting template from obeject store to DPL
	for _, key := range keys {
		tplb, err := obsi.objectStore.Get(obsi.bucket, key)
		if err != nil {
			klog.Error("Failed to get object ", key, " in bucket ", obsi.bucket)

			return err
		}

		// skip empty body object store
		if len(tplb.Content) == 0 {
			continue
		}

		tpl := &unstructured.Unstructured{}
		err = yaml.Unmarshal(tplb.Content, tpl)

		if err != nil {
			klog.Error("Failed to unmashall ", obsi.bucket, "/", key, " err:", err)

			return err
		}

		tpls = append(tpls, *tpl)
	}

	hostkey := types.NamespacedName{Name: obsi.Subscription.Name, Namespace: obsi.Subscription.Namespace}

	resources := make([]kubesynchronizer.ResourceUnit, 0)

	// track if there's any error when doSubscribeDeployable, if there's any, then we should retry this
	var doErr error

	for _, tpl := range tpls {
		resource, err := obsi.doSubscribeDeployable(&tpl)

		if err != nil {
			klog.Errorf("object bucket failed to package deployable, err: %v", err)

			doErr = err

			continue
		}

		resources = append(resources, *resource)
	}

	if err := obsi.synchronizer.ProcessSubResources(hostkey, resources); err != nil {
		klog.Error(err)

		return err
	}

	return doErr
}

func (obsi *SubscriberItem) doSubscribeDeployable(template *unstructured.Unstructured) (*kubesynchronizer.ResourceUnit, error) {
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

	template.SetNamespace(obsi.Subscription.Namespace)

	resource := &kubesynchronizer.ResourceUnit{Resource: template, Gvk: validgvk}

	return resource, nil
}
