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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	jsonpatch "k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
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

	s.LocalClient, err = client.New(config, client.Options{})

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

	s.discoverResources()

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

	time.Sleep(time.Duration(sync.Interval) * time.Second)

	sync.dynamicFactory.Start(s)

	go wait.Until(func() {
		sync.houseKeeping()
	}, time.Duration(sync.Interval)*time.Second, sync.signal)

	<-sync.signal

	return nil
}

//HouseKeeping - Apply resources defined in sync.KubeResources
func (sync *KubeSynchronizer) houseKeeping() {
	crdUpdated := false
	// make sure the template map and the actual resource are aligned
	for gvk, res := range sync.KubeResources {
		var err error

		klog.V(5).Infof("Applying templates in gvk: %#v, res: %#v", gvk, res)

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
		sync.discoverResources()
	}
}

func (sync *KubeSynchronizer) checkServerObjects(res *ResourceMap) error {
	if res == nil {
		errmsg := "Checking server objects with nil map"
		klog.Error(errmsg)

		return errors.NewBadRequest(errmsg)
	}

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
		source := utils.GetSourceFromObject(&obj)

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
			// Harvest from system
			klog.V(3).Infof("Havesting tplunit from cluster host: %#v, obj: %#v, TemplateMap: %#v", dpl, obj, res.TemplateMap)

			unit := &TemplateUnit{
				ResourceUpdated: false,
				StatusUpdated:   false,
				Unstructured:    obj.DeepCopy(),
				Source:          source,
			}
			res.TemplateMap[reskey] = unit
		} else {
			if tplunit.Source != source {
				klog.V(3).Info("Havesting resource ", dpl.Namespace, "/", dpl.Name, " but owned by other source, skipping")
				continue
			}

			status := obj.Object["status"]
			klog.V(4).Info("Found for ", dpl, ", tplunit:", tplunit, "Doing obj ", obj.GetNamespace(), "/", obj.GetName(), " with status:", status)
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

func (sync *KubeSynchronizer) updateResourceByTemplateUnit(ri dynamic.ResourceInterface,
	obj *unstructured.Unstructured, tplunit *TemplateUnit, isService bool) error {
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

	if isService {
		var objb, tplb, pb []byte
		objb, err = obj.MarshalJSON()

		if err != nil {
			klog.Error("Failed to marshall obj with error:", err)
			return err
		}

		tplb, err = tplunit.MarshalJSON()

		if err != nil {
			klog.Error("Failed to marshall tplunit with error:", err)
			return err
		}

		pb, err = jsonpatch.CreateThreeWayJSONMergePatch(tplb, tplb, objb)
		if err != nil {
			klog.Error("Failed to make patch with error:", err)
			return err
		}

		klog.V(4).Info("Generating Patch for service update.\nObjb:", string(objb), "\ntplb:", string(tplb), "\nPatch:", string(pb))

		_, err = ri.Patch(obj.GetName(), types.MergePatchType, pb, metav1.PatchOptions{})
	} else {
		_, err = ri.Update(newobj, metav1.UpdateOptions{})
	}

	klog.V(5).Info("Check - Updated existing Resource to", tplunit, " with err:", err)

	if err == nil {
		tplunit.ResourceUpdated = true
	} else {
		klog.Error("Failed to update resource with error:", err)
	}

	sterr := sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, obj.Object["status"])

	if sterr != nil {
		klog.Error("Failed to update host status with error:", err)
	}

	return nil
}

var serviceGVR = schema.GroupVersionResource{
	Version:  "v1",
	Resource: "Service",
}

func (sync *KubeSynchronizer) applyKindTemplates(res *ResourceMap) {
	nri := sync.DynamicClient.Resource(res.GroupVersionResource)

	for k, tplunit := range res.TemplateMap {
		err := sync.applyTemplate(nri, res.Namespaced, k, tplunit, (res.GroupVersionResource == serviceGVR))
		if err != nil {
			klog.Error("Failed to apply kind template", tplunit.Unstructured, "with error:", err)
		}
	}
}

func (sync *KubeSynchronizer) applyTemplate(nri dynamic.NamespaceableResourceInterface, namespaced bool,
	k string, tplunit *TemplateUnit, isService bool) error {
	klog.V(3).Info("Applying (key:", k, ") template:", tplunit, tplunit.Unstructured, "updated:", tplunit.ResourceUpdated)

	var ri dynamic.ResourceInterface
	if namespaced {
		ri = nri.Namespace(tplunit.GetNamespace())
	} else {
		ri = nri
	}

	obj, err := ri.Get(tplunit.GetName(), metav1.GetOptions{})

	if err != nil {
		if errors.IsNotFound(err) {
			err = sync.createNewResourceByTemplateUnit(ri, tplunit)
		} else {
			klog.Error("Failed to apply resource with error:", err)
		}
	} else if !tplunit.ResourceUpdated {
		err = sync.updateResourceByTemplateUnit(ri, obj, tplunit, isService)
		// don't process the err of status update. leave it to next round house keeping
	}
	// leave the sync the check routine, not this one

	klog.V(3).Info("Applied Kind Template ", tplunit.Unstructured, " error:", err)

	return err
}

// DeRegisterTemplate applies the resource in spec.template to given kube
func (sync *KubeSynchronizer) DeRegisterTemplate(host, dpl types.NamespacedName, source string) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}
	// check resource template map for deployables
	klog.V(2).Info("Deleting template ", dpl, "for source:", source)

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
		klog.V(3).Info("Processing Local with template:", template, ", syncid: ", sync.SynchronizerID, ", host: ", host)
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
		klog.V(5).Info("Adding new resource from registration. kind: ", template.GetKind(), " GroupVersionResource: ", resmap.GroupVersionResource)
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
		klog.V(5).Info("Deployable has finalizers, ready to delete object", instance)

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
		klog.V(5).Info("Deployable is not (no longer) local, ready to delete object", instance)

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
	tplanno[appv1alpha1.AnnotationSyncSource] = source
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

	klog.V(4).Info("overrided template: ", template)
	// skip no-op to template

	if existingTemplateUnit != nil && reflect.DeepEqual(existingTemplateUnit.Unstructured, template) {
		klog.V(2).Info("Skipping.. template in registry is the same ", existingTemplateUnit)
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

	klog.V(2).Info("Registered template ", template, "to KubeResource map:", template.GetObjectKind().GroupVersionKind(), "for source: ", source)

	return nil
}

func (sync *KubeSynchronizer) generateResourceMapKey(host, dpl types.NamespacedName) string {
	return host.String() + "/" + dpl.String()
}
