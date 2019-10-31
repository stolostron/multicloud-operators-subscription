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

package kubernetes

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime/debug"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

// TemplateUnit defines the basic unit of Template and whether it should be updated or not
type TemplateUnit struct {
	*unstructured.Unstructured
	Source          string
	ResourceUpdated bool
	StatusUpdated   bool
}

// ResourceMap is a registry for all resources
type ResourceMap struct {
	GroupVersionResource schema.GroupVersionResource
	Namespaced           bool
	ServerUpdated        bool
	TemplateMap          map[string]*TemplateUnit
}

// KubeSynchronizer handles resources to a kube endpoint
type KubeSynchronizer struct {
	Interval       int
	LocalClient    client.Client
	RemoteClient   client.Client
	localConfig    *rest.Config
	DynamicClient  dynamic.Interface
	KubeResources  map[schema.GroupVersionKind]*ResourceMap
	SynchronizerID *types.NamespacedName
	Extension      Extension

	signal <-chan struct{}

	dynamicFactory dynamicinformer.DynamicSharedInformerFactory
}

var (
	deployableResource = schema.GroupVersionResource{
		Group:    dplv1alpha1.SchemeGroupVersion.Group,
		Resource: "deployables",
		Version:  dplv1alpha1.SchemeGroupVersion.Version,
	}

	crdresource = "customresourcedefinitions"
)

var defaultSynchronizer *KubeSynchronizer

// Add creates the default syncrhonizer and add the start function as runnable into manager
func Add(mgr manager.Manager, hubconfig *rest.Config, syncid *types.NamespacedName, interval int) error {
	var err error
	defaultSynchronizer, err = CreateSynchronizer(mgr.GetConfig(), hubconfig, syncid, interval, defaultExtension)

	if err != nil {
		klog.Error("Failed to create synchronizer with error: ", err)
		return err
	}

	return mgr.Add(defaultSynchronizer)
}

// GetDefaultSynchronizer - return the default kubernetse synchronizer
func GetDefaultSynchronizer() *KubeSynchronizer {
	return defaultSynchronizer
}

// CreateSynchronizer createa an instance of synchrizer with give api-server config
func CreateSynchronizer(config, remoteConfig *rest.Config, syncid *types.NamespacedName, interval int, ext Extension) (*KubeSynchronizer, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	var err error

	dynamicClient := dynamic.NewForConfigOrDie(config)

	s := &KubeSynchronizer{
		Interval:       interval,
		SynchronizerID: syncid,
		DynamicClient:  dynamicClient,
		localConfig:    config,
		KubeResources:  make(map[schema.GroupVersionKind]*ResourceMap),
		Extension:      ext,
	}

	s.LocalClient, err = client.New(remoteConfig, client.Options{})

	if err != nil {
		klog.Error("Failed to initialize client to update local status. err: ", err)
		return nil, err
	}

	s.RemoteClient = s.LocalClient
	if remoteConfig != nil {
		s.RemoteClient, err = client.New(remoteConfig, client.Options{})

		if err != nil {
			klog.Error("Failed to initialize client to update remote status. err: ", err)
			return nil, err
		}
	}

	defaultExtension.localClient = s.LocalClient
	defaultExtension.remoteClient = s.RemoteClient

	if ext == nil {
		s.Extension = defaultExtension
	}

	err = s.discoverResources()

	if err != nil {
		klog.Error("Failed to discover resources with error", err)
		return nil, err
	}

	return s, nil
}

// Start the discovery and start caches
func (sync *KubeSynchronizer) Start(s <-chan struct{}) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	sync.signal = s

	sync.dynamicFactory.Start(s)

	go wait.Until(func() {
		err := sync.houseKeeping()
		if err != nil {
			klog.Error("Housekeeping with error", err)
		}
	}, time.Duration(sync.Interval)*time.Second, sync.signal)

	<-sync.signal

	return nil
}

//HouseKeeping - Apply resources defined in sync.KubeResources
func (sync *KubeSynchronizer) houseKeeping() error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	err := sync.validateDeployables()
	if err != nil {
		klog.Error("Failed to validate deployables with err:", err)
		return err
	}

	crdUpdated := false
	// make sure the template map and the actual resource are aligned
	for gvk, res := range sync.KubeResources {
		var err error

		// klog.V(5).Infof("Applying templates in gvk: %#v, res: %#v", gvk, res)

		if res.ServerUpdated {
			err = sync.checkServerObjects(res)

			if res.GroupVersionResource.Resource == crdresource {
				klog.V(5).Info("CRD Updated! let's discover it!")

				crdUpdated = true
			}

			res.ServerUpdated = false
		}

		if err != nil {
			klog.Error("Error in checking server objects of gvk:", gvk, "error:", err, " skipping")
			continue
		}

		sync.applyKindTemplates(res)
	}

	if crdUpdated {
		err = sync.discoverResources()
		if err != nil {
			klog.Error("discover resource with error", err)
		}
	}

	return nil
}

// validate parent deployable exist or not
// only delete deployables, leave the templatemap to remote controller
// does not add template to templatemap
func (sync *KubeSynchronizer) validateDeployables() error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	// Only validate deployables for deployable synchronizer on hub (SynchronizerID = "/")
	if sync.SynchronizerID == nil || sync.SynchronizerID.String() != (client.ObjectKey{}).String() {

		klog.V(5).Info("Managed cluster controller does not need to validate deployables, even it sits on hub")

		return nil
	}

	deployablelist, err := sync.DynamicClient.Resource(deployableResource).List(metav1.ListOptions{})
	if err != nil {
		klog.Error("Failed to obtain deployable list")
	}

	// construct a map to make things easier
	deployableMap := make(map[string]*unstructured.Unstructured)
	for _, dpl := range deployablelist.Items {
		deployableMap[(types.NamespacedName{Name: dpl.GetName(), Namespace: dpl.GetNamespace()}).String()] = dpl.DeepCopy()
		klog.V(5).Info("validateDeployables() dpl: ", dpl.GetNamespace(), " ", dpl.GetName())
	}

	// check each deployable for parents
	for k, v := range deployableMap {
		done := false
		obj := v.DeepCopy()

		for !done {
			annotations := obj.GetAnnotations()
			if annotations == nil {
				// newly added not processed yet break this loop
				break
			}

			host := utils.GetHostDeployableFromObject(obj)
			if host == nil || host.String() == (client.ObjectKey{}).String() {
				// reached level 1, stays
				break
			}

			ok := false
			obj, ok = deployableMap[host.String()]

			if !ok {
				// parent is gone, delete the deployable from map and from kube
				klog.V(5).Infof("parent is gone, delete the deployable from map and from kube: host: %#v, k: %#v, v: %#v ", host, k, v)
				delete(deployableMap, k)

				deletepolicy := metav1.DeletePropagationBackground

				err = sync.DynamicClient.Resource(deployableResource).Namespace(v.GetNamespace()).Delete(
					v.GetName(), &metav1.DeleteOptions{PropagationPolicy: &deletepolicy})

				if err != nil {
					klog.Error("Failed to delete orphan deployable with error:", err)
				}

				done = true
			}
		}
	}

	return nil
}

func (sync *KubeSynchronizer) checkServerObjects(res *ResourceMap) error {
	klog.V(5).Info("Checking Server object:", res.GroupVersionResource)

	objlist, err := sync.DynamicClient.Resource(res.GroupVersionResource).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	var dl dynamic.ResourceInterface

	for _, obj := range objlist.Items {
		if !sync.Extension.IsObjectOwnedBySynchronizer(&obj, sync.SynchronizerID) {
			continue
		}

		host := sync.Extension.GetHostFromObject(&obj)
		dpl := utils.GetHostDeployableFromObject(&obj)

		if dpl == nil {
			continue
		}

		reskey := sync.generateResourceMapKey(*host, *dpl)

		tplunit, ok := res.TemplateMap[reskey]

		if res.Namespaced {
			dl = sync.DynamicClient.Resource(res.GroupVersionResource).Namespace(obj.GetNamespace())
		} else {
			dl = sync.DynamicClient.Resource(res.GroupVersionResource)
		}

		if !ok {
			klog.V(5).Infof("Deleting host: %#v, obj: %#v, TemplateMap: %#v", dpl, obj, res.TemplateMap)

			deletepolicy := metav1.DeletePropagationBackground
			err = dl.Delete(obj.GetName(), &metav1.DeleteOptions{PropagationPolicy: &deletepolicy})
		} else {
			status := obj.Object["status"]
			klog.V(5).Info("Found for ", dpl, ", tplunit:", tplunit, "Doing obj ", obj.GetNamespace(), "/", obj.GetName(), " with status:", status)
			delete(obj.Object, "status")

			err = sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, status)

			if err != nil {
				klog.Error("Failed to update host status with error:", err)
			}

			if tplunit.ResourceUpdated {
				continue
			}

			if !reflect.DeepEqual(obj, tplunit.Unstructured.Object) {
				newobj := tplunit.Unstructured.DeepCopy()
				newobj.SetResourceVersion(obj.GetResourceVersion())
				_, err = dl.Update(newobj, metav1.UpdateOptions{})
				klog.V(5).Info("Check - Updated existing Resource to", tplunit, " with err:", err)

				if err == nil {
					tplunit.ResourceUpdated = true
				}
			}
			// don't process the err of status update. leave it to next round house keeping
			if err != nil {
				return err
			}
			klog.V(10).Info("Updated template ", tplunit.Unstructured.GetName(), ":", tplunit.ResourceUpdated)
			res.TemplateMap[reskey] = tplunit
		}
	}

	return nil
}

func (sync *KubeSynchronizer) createNewResourceByTemplateUnit(ri dynamic.ResourceInterface, tplunit *TemplateUnit) error {
	klog.V(5).Info("Apply - Creating New Resource ", tplunit)
	obj, err := ri.Create(tplunit.Unstructured, metav1.CreateOptions{})

	// Auto Create Namespace if not exist
	if err != nil && errors.IsNotFound(err) {
		ns := &corev1.Namespace{}
		ns.Name = tplunit.GetNamespace()
		nsus := &unstructured.Unstructured{}
		nsus.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(ns)

		if err == nil {
			_, err = sync.DynamicClient.Resource(schema.GroupVersionResource{
				Version:  "v1",
				Resource: "namespaces",
			}).Create(nsus, metav1.CreateOptions{})

			if err == nil {
				// try again
				obj, err = ri.Create(tplunit.Unstructured, metav1.CreateOptions{})
			}
		}
	}

	if err != nil {
		tplunit.ResourceUpdated = false

		klog.Error("Failed to apply resource with error: ", err)

		return err
	}

	tplunit.ResourceUpdated = true

	if obj != nil {
		err = sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, obj.Object["status"])
	} else {
		err = sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, nil)
	}

	if err != nil {
		klog.Error("Failed to update host status with error: ", err)
	}

	return err
}

func (sync *KubeSynchronizer) updateResourceByTemplateUnit(ri dynamic.ResourceInterface, obj *unstructured.Unstructured, tplunit *TemplateUnit) error {
	var err error

	tplown := sync.Extension.GetHostFromObject(tplunit)
	if tplown != nil && !sync.Extension.IsObjectOwnedByHost(obj, *tplown, sync.SynchronizerID) {
		errmsg := "Obj " + tplunit.GetNamespace() + "/" + tplunit.GetName() + " exists and owned by others, backoff"
		klog.Info(errmsg)

		tplunit.ResourceUpdated = false

		err = sync.Extension.UpdateHostStatus(errors.NewBadRequest(errmsg), tplunit.Unstructured, nil)

		if err != nil {
			klog.Error("Failed to update host status for existing resource with error:", err)
		}

		return err
	}

	newobj := tplunit.Unstructured.DeepCopy()
	newobj.SetResourceVersion(obj.GetResourceVersion())
	_, err = ri.Update(newobj, metav1.UpdateOptions{})
	klog.V(10).Info("Check - Updated existing Resource to", tplunit, " with err:", err)

	if err == nil {
		tplunit.ResourceUpdated = true
	}

	sterr := sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, obj.Object["status"])

	if sterr != nil {
		klog.Error("Failed to update host status with error:", err)
	}

	return nil
}

func (sync *KubeSynchronizer) applyKindTemplates(res *ResourceMap) {
	for k, tplunit := range res.TemplateMap {
		klog.V(2).Info("Applying (key:", k, ") template:", tplunit, tplunit.Unstructured, "updated:", tplunit.ResourceUpdated)

		var ri dynamic.ResourceInterface

		if res.Namespaced {
			klog.V(5).Info("Namespaced ri for ", res.GroupVersionResource)
			ri = sync.DynamicClient.Resource(res.GroupVersionResource).Namespace(tplunit.GetNamespace())
		} else {
			klog.V(5).Info("Clusterscpoped ri for ", res.GroupVersionResource)
			ri = sync.DynamicClient.Resource(res.GroupVersionResource)
		}

		obj, err := ri.Get(tplunit.GetName(), metav1.GetOptions{})

		if err != nil {
			if errors.IsNotFound(err) {
				err = sync.createNewResourceByTemplateUnit(ri, tplunit)
			}
		} else if !tplunit.ResourceUpdated {
			err = sync.updateResourceByTemplateUnit(ri, obj, tplunit)
			// don't process the err of status update. leave it to next round house keeping
		}
		// leave the sync the check routine, not this one
		if err != nil {
			klog.Info("Failed to apply kind template", tplunit.Unstructured, "with error:", err)
			continue
		}

		klog.V(5).Info("Applied Kind Template ", tplunit.Unstructured, " error:", err)
	}
}

// DeRegisterTemplate applies the resource in spec.template to given kube
func (sync *KubeSynchronizer) DeRegisterTemplate(host, dpl types.NamespacedName, source string) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}
	// check resource template map for deployables
	klog.V(5).Info("Deleting template ", dpl, "for syncid ", dpl)

	for _, resmap := range sync.KubeResources {
		// all templates are added with annotations, no need to check nil
		if len(resmap.TemplateMap) > 0 {
			klog.V(5).Info("Checking valid resource map: ", resmap.GroupVersionResource)
		}

		reskey := sync.generateResourceMapKey(host, dpl)

		tplunit, ok := resmap.TemplateMap[reskey]
		if !ok || tplunit.Source != source {
			if tplunit != nil {
				klog.V(5).Infof("Delete - skipping tplunit with other source, resmap source: %v, source: %v", tplunit.Source, source)
			}

			continue
		}
		klog.Infof("host %v, dpl %v, source %v", host, dpl, source)
		debug.PrintStack()
		delete(resmap.TemplateMap, reskey)

		klog.V(5).Info("Deleted template ", dpl, "in resource map ", resmap.GroupVersionResource)

		if !resmap.GroupVersionResource.Empty() {
			var dl dynamic.ResourceInterface
			if resmap.Namespaced {
				dl = sync.DynamicClient.Resource(resmap.GroupVersionResource).Namespace(tplunit.GetNamespace())
			} else {
				dl = sync.DynamicClient.Resource(resmap.GroupVersionResource)
			}

			// check resource ownership
			tgtobj, err := dl.Get(tplunit.GetName(), metav1.GetOptions{})
			if err == nil {
				if sync.Extension.IsObjectOwnedByHost(tgtobj, host, sync.SynchronizerID) {
					klog.V(5).Info("Resource is owned by ", host, "Deleting ", tplunit.Unstructured)

					deletepolicy := metav1.DeletePropagationBackground
					err = dl.Delete(tplunit.GetName(), &metav1.DeleteOptions{PropagationPolicy: &deletepolicy})

					if err != nil {
						klog.Error("Failed to delete tplunit in kubernetes, with error:", err)
					}

					sterr := sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, nil)

					if sterr != nil {
						klog.Error("Failed to update host status, with error:", err)
					}
				}
			}
		}

		klog.V(5).Info("Deleted resource ", dpl, "in k8s")
	}

	return nil
}

// RegisterTemplate applies the resource in spec.template to given kube
func (sync *KubeSynchronizer) RegisterTemplate(host types.NamespacedName, instance *dplv1alpha1.Deployable, source string) error {
	// Parse the resource in template
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	var err error

	template := &unstructured.Unstructured{}

	if instance.Spec.Template == nil {
		klog.Warning("Processing local deployable without template:", instance)
		return nil
	}

	if instance.Spec.Template.Object != nil {
		template.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(instance.Spec.Template.Object.DeepCopyObject())
	} else {
		err = json.Unmarshal(instance.Spec.Template.Raw, template)
		klog.V(10).Info("Processing Local with template:", template, ", syncid: ", sync.SynchronizerID, ", host: ", host)
	}

	if err != nil {
		klog.Error("Failed to unmashal template with error: ", err, " with ", string(instance.Spec.Template.Raw))
		return err
	}

	if template.GetKind() == "" {
		return errors.NewBadRequest("Failed to update template with empty kind. gvk:" + template.GetObjectKind().GroupVersionKind().String())
	}

	// set name to deployable name if not given
	if template.GetName() == "" {
		template.SetName(instance.GetName())
	}

	// carry/override with deployable labels
	tpllbls := template.GetLabels()
	if tpllbls == nil {
		tpllbls = make(map[string]string)
	}

	for k, v := range instance.GetLabels() {
		tpllbls[k] = v
	}

	template.SetLabels(tpllbls)

	tplgvk := template.GetObjectKind().GroupVersionKind()
	validgvk := sync.GetValidatedGVK(tplgvk)

	if validgvk == nil {
		return errors.NewBadRequest("GroupVersionKind of Template is not supported. " + tplgvk.String())
	}

	template.SetGroupVersionKind(*validgvk)

	resmap, ok := sync.KubeResources[*validgvk]

	if !ok {
		// register new kind
		resmap = &ResourceMap{
			GroupVersionResource: schema.GroupVersionResource{},
			TemplateMap:          make(map[string]*TemplateUnit),
			Namespaced:           true,
		}
		klog.V(10).Info("Adding new resource from registration. kind: ", template.GetKind(), " GroupVersionResource: ", resmap.GroupVersionResource)
	}

	if resmap.Namespaced && template.GetNamespace() == "" {
		template.SetNamespace(instance.GetNamespace())
	}

	dpl := types.NamespacedName{
		Name:      instance.GetName(),
		Namespace: instance.GetNamespace(),
	}

	reskey := sync.generateResourceMapKey(host, dpl)

	// Try to get template object, take error as not exist, will check again anyway.
	if len(instance.GetObjectMeta().GetFinalizers()) > 0 {
		// Deployable in being deleted, de-register template and return
		klog.V(10).Info("Deployable has finalizers, ready to delete object", instance)

		err = sync.DeRegisterTemplate(host, dpl, source)

		if err != nil {
			klog.Error("Failed to deregister template when there are finalizer(s) with error: ", err)
		}

		return nil
	}

	// step out if the target resource is not from this deployable
	existingTemplateUnit, ok := resmap.TemplateMap[reskey]

	if ok && !sync.Extension.IsObjectOwnedByHost(existingTemplateUnit.Unstructured, host, sync.SynchronizerID) {
		return errors.NewBadRequest(fmt.Sprintf("Resource owned by other owner: %s vs %s. Backing off.",
			sync.Extension.GetHostFromObject(existingTemplateUnit.Unstructured).String(), host.String()))
	}

	if !utils.IsLocalDeployable(instance) {
		klog.V(10).Info("Deployable is not (no longer) local, ready to delete object", instance)

		err = sync.DeRegisterTemplate(host, dpl, source)

		if err != nil {
			klog.Error("Failed to deregister template when the deployable is not local, with error:", err)
		}

		instance.Status.ResourceStatus = nil
		instance.Status.Message = ""
		instance.Status.Reason = ""

		return nil
	}

	// set to the resource annotation in template annotation
	err = sync.Extension.SetHostToObject(template, host, sync.SynchronizerID)
	if err != nil {
		klog.Error("Failed to set host to object with error:", err)
	}

	tplanno := template.GetAnnotations()
	tplanno[dplv1alpha1.AnnotationHosting] = instance.GetNamespace() + "/" + instance.GetName()
	template.SetAnnotations(tplanno)

	// apply override in template
	if sync.SynchronizerID != nil {
		ovmap, err := utils.PrepareOverrides(*sync.SynchronizerID, instance)
		if err != nil {
			klog.Error("Failed to prepare override for instance: ", instance)
			return err
		}

		template, err = utils.OverrideTemplate(template, ovmap)

		if err != nil {
			klog.Error("Failed to apply override for instance: ", instance)
			return err
		}
	}

	klog.V(5).Info("overrided template: ", template)
	// skip no-op to template

	if existingTemplateUnit != nil && reflect.DeepEqual(existingTemplateUnit.Unstructured, template) {
		klog.V(5).Info("Skipping.. template in registry is the same ", existingTemplateUnit)
		return nil
	}

	templateUnit := &TemplateUnit{
		ResourceUpdated: false,
		StatusUpdated:   false,
		Unstructured:    template.DeepCopy(),
		Source:          source,
	}
	resmap.TemplateMap[reskey] = templateUnit
	sync.KubeResources[template.GetObjectKind().GroupVersionKind()] = resmap

	klog.V(1).Info("Registered template ", template, "to KubeResource map:", template.GetObjectKind().GroupVersionKind())

	return nil
}

func (sync *KubeSynchronizer) generateResourceMapKey(host, dpl types.NamespacedName) string {
	return host.String() + "/" + dpl.String()
}
