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
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	kubesynchronizer "github.com/IBM/multicloud-operators-subscription/pkg/synchronizer/kubernetes"

	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
	awsutils "github.com/IBM/multicloud-operators-subscription/pkg/utils/aws"
)

var SubscriptionGVK = schema.GroupVersionKind{Group: "app.ibm.com", Kind: "Subscription", Version: "v1alpha1"}

// SubscriberItem - defines the unit of namespace subscription
type SubscriberItem struct {
	appv1alpha1.SubscriberItem

	bucket      string
	objectStore awsutils.ObjectStore
	stopch      chan struct{}

	syncinterval int
	synchronizer *kubesynchronizer.KubeSynchronizer
}

// SubscribeItem subscribes a subscriber item with namespace channel
func (obsi *SubscriberItem) Start() {
	if err := obsi.initObjectStore(); err != nil {
		klog.Error("Unable to initialize object store connection for subscription ", obsi.Subscription.Name, " channel ", obsi.Channel.Name)
		return
	}

	// do nothing if already started
	if obsi.stopch != nil {
		return
	}

	obsi.stopch = make(chan struct{})

	go wait.Until(func() {
		tw := obsi.SubscriberItem.Subscription.Spec.TimeWindow
		if tw != nil {
			nextRun := utils.NextStartPoint(tw, time.Now())
			if nextRun > time.Duration(0) {
				klog.V(1).Infof("Subcription %v/%v will de deploy after %v",
					obsi.SubscriberItem.Subscription.GetNamespace(),
					obsi.SubscriberItem.Subscription.GetName(), nextRun)
				return
			}
		}

		err := obsi.doSubscription()

		if err != nil {
			klog.Error("Object Bucket ", obsi.Subscription.Namespace, "/", obsi.Subscription.Name, "housekeeping failed with error: ", err)
		}
	}, time.Duration(obsi.syncinterval)*time.Second, obsi.stopch)
}

// Stop the subscriber
func (obsi *SubscriberItem) Stop() {
	if obsi.stopch != nil {
		close(obsi.stopch)
		obsi.stopch = nil
	}
}

func (obsi *SubscriberItem) initObjectStore() error {
	var err error

	awshandler := &awsutils.Handler{}

	pathName := obsi.Channel.Spec.PathName

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
	}

	klog.V(2).Info("Trying to connect to aws ", endpoint, "|", obsi.bucket)

	if err := awshandler.InitObjectStoreConnection(endpoint, accessKeyID, secretAccessKey); err != nil {
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

func (obsi *SubscriberItem) doSubscription() error {
	var dpls []*dplv1alpha1.Deployable

	keys, err := obsi.objectStore.List(obsi.bucket)
	klog.V(5).Infof("object keys: %v", keys)

	if err != nil {
		klog.Info("Failed to list objects in bucket ", obsi.bucket)
	}
	//converting template from obeject store to DPL
	for _, key := range keys {
		tplb, err := obsi.objectStore.Get(obsi.bucket, key)
		if err != nil {
			klog.Info("Failed to get object ", key, " in bucket ", obsi.bucket)
			return err
		}

		dpl := &dplv1alpha1.Deployable{}
		dpl.Name = key
		dpl.Namespace = obsi.bucket
		dpl.Spec.Template = &runtime.RawExtension{}
		err = yaml.Unmarshal(tplb, dpl.Spec.Template)

		if err != nil {
			klog.Error("Failed to unmashall ", obsi.bucket, "/", key, " err:", err)
			continue
		}

		klog.V(5).Infof("Retived Dpl: %v", dpl)
		dpls = append(dpls, dpl)
	}

	hostkey := types.NamespacedName{Name: obsi.Subscription.Name, Namespace: obsi.Subscription.Namespace}
	syncsource := objectbucketsyncsource + hostkey.String()
	// subscribed k8s resource
	kvalid := obsi.synchronizer.CreateValiadtor(syncsource)
	pkgMap := make(map[string]bool)

	var vsub = ""

	if obsi.Subscription.Spec.PackageFilter != nil {
		vsub = obsi.Subscription.Spec.PackageFilter.Version
	}

	versionMap := utils.GenerateVersionSet(dpls, vsub)

	for _, dpl := range dpls {
		dpltosync, validgvk, err := obsi.doSubscribeDeployable(dpl.DeepCopy(), versionMap, pkgMap)

		if err != nil {
			continue
		}

		err = obsi.synchronizer.RegisterTemplate(hostkey, dpltosync, syncsource)

		if err != nil {
			klog.Info("eror in registering :", err)
			err = utils.SetInClusterPackageStatus(&(obsi.Subscription.Status), dpltosync.GetName(), err, nil)

			if err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}

			pkgMap[dpltosync.GetName()] = true

			continue
		}

		dplkey := types.NamespacedName{
			Name:      dpltosync.Name,
			Namespace: dpltosync.Namespace,
		}
		kvalid.AddValidResource(*validgvk, hostkey, dplkey)

		pkgMap[dplkey.Name] = true

		klog.V(5).Info("Finished Register ", *validgvk, hostkey, dplkey, " with err:", err)
	}

	obsi.synchronizer.ApplyValiadtor(kvalid)

	if utils.ValidatePackagesInSubscriptionStatus(obsi.synchronizer.LocalClient, obsi.Subscription, pkgMap) != nil {
		err = obsi.synchronizer.LocalClient.Get(context.TODO(), hostkey, obsi.Subscription)
		if err != nil {
			klog.Error("Failed to get and subscription resource with error:", err)
		}

		err = utils.ValidatePackagesInSubscriptionStatus(obsi.synchronizer.LocalClient, obsi.Subscription, pkgMap)
	}

	return err
}

func (obsi *SubscriberItem) doSubscribeDeployable(dpl *dplv1alpha1.Deployable,
	versionMap map[string]utils.VersionRep, pkgMap map[string]bool) (*dplv1alpha1.Deployable, *schema.GroupVersionKind, error) {
	var annotations map[string]string

	template := &unstructured.Unstructured{}

	if dpl.Spec.Template == nil {
		errmsg := "Processing local deployable without template " + dpl.Name
		klog.Warning(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	err := json.Unmarshal(dpl.Spec.Template.Raw, template)
	if err != nil {
		errmsg := "Processing local deployable " + dpl.Name + " with error template, err: " + err.Error()
		klog.Warning(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	if obsi.Subscription.Spec.PackageFilter != nil {
		if obsi.Subscription.Spec.Package != "" && obsi.Subscription.Spec.Package != dpl.Name {
			errmsg := "Name does not match, skiping:" + obsi.Subscription.Spec.Package + "|" + dpl.Name
			klog.V(3).Info(errmsg)

			return nil, nil, errors.New(errmsg)
		}

		if !utils.LabelChecker(obsi.Subscription.Spec.PackageFilter.LabelSelector, template.GetLabels()) {
			errmsg := "Failed to pass label check to deployable " + dpl.Name
			klog.V(3).Info(errmsg)

			return nil, nil, errors.New(errmsg)
		}

		klog.V(5).Info("checking annotations filter:", annotations)

		annotations = obsi.Subscription.Spec.PackageFilter.Annotations
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
				errmsg := "Failed to pass annotation check to deployable " + dpl.Name
				klog.V(3).Info(errmsg)

				return nil, nil, errors.New(errmsg)
			}
		}
	}

	if !utils.IsDeployableInVersionSet(versionMap, dpl) {
		errmsg := "Failed to pass version check to deployable " + dpl.Name
		klog.V(3).Info(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	template, err = utils.OverrideResourceBySubscription(template, dpl.GetName(), obsi.Subscription)
	if err != nil {
		pkgMap[dpl.GetName()] = true
		errmsg := "Failed override package " + dpl.Name + " with error: " + err.Error()
		err = utils.SetInClusterPackageStatus(&(obsi.Subscription.Status), dpl.GetName(), err, nil)

		if err != nil {
			errmsg += " and failed to set in cluster package status with error: " + err.Error()
		}

		klog.V(2).Info(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	template.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: SubscriptionGVK.Version,
		Kind:       SubscriptionGVK.Kind,
		Name:       obsi.Subscription.Name,
		UID:        obsi.Subscription.UID,
	}})

	orggvk := template.GetObjectKind().GroupVersionKind()
	validgvk := obsi.synchronizer.GetValidatedGVK(orggvk)

	if validgvk == nil {
		pkgMap[dpl.GetName()] = true
		errmsg := "Resource " + orggvk.String() + " is not supported"
		gvkerr := errors.New(errmsg)
		err = utils.SetInClusterPackageStatus(&(obsi.Subscription.Status), dpl.GetName(), gvkerr, nil)

		if err != nil {
			errmsg += " and failed to set in cluster package status with error: " + err.Error()
		}

		klog.V(2).Info(errmsg)

		return nil, nil, errors.New(errmsg)
	}

	dpl.Spec.Template.Raw, err = json.Marshal(template)

	if err != nil {
		klog.Warning("Mashaling template, got error:", err)

		return nil, nil, err
	}

	//the registered dpl template will be deployed to the subscription namespace
	dpl.Namespace = obsi.Subscription.Namespace

	annotations = make(map[string]string)
	annotations[dplv1alpha1.AnnotationLocal] = "true"

	dpl.SetAnnotations(annotations)

	return dpl, validgvk, nil
}
