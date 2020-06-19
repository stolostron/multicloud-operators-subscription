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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

var (
	informerFactoryPeriod = 10 * time.Minute
	resourcePredicate     = discovery.SupportsAllVerbs{Verbs: []string{"create", "update", "delete", "list", "watch"}}
)

var (
	deploymentkeygvk = schema.GroupVersionKind{
		Group:   "extensions",
		Kind:    "Deployment",
		Version: "v1beta1",
	}
	deploymentvalgvk = schema.GroupVersionKind{
		Group:   "apps",
		Kind:    "Deployment",
		Version: "v1",
	}

	internalReplacedGroupVersionKind = map[schema.GroupVersionKind]*schema.GroupVersionKind{
		deploymentkeygvk: &deploymentvalgvk,
	}
)
var (
	rsextgk = schema.GroupKind{
		Group: "extensions",
		Kind:  "ReplicaSet",
	}

	rsappgk = schema.GroupKind{
		Group: "apps",
		Kind:  "ReplicaSet",
	}

	deployextgk = schema.GroupKind{
		Group: "extensions",
		Kind:  "Deployment",
	}

	dplgk = schema.GroupKind{
		Group: "apps.open-cluster-management.io",
		Kind:  "Deployable",
	}
	internalIgnoredGroupKind = map[schema.GroupKind]bool{
		rsextgk:     true,
		rsappgk:     true,
		deployextgk: true,
		dplgk:       true,
	}
)

var crdKind = "CustomResourceDefinition"

func (sync *KubeSynchronizer) stopDynamicClientCaching() {
	sync.dmtx.Lock()
	close(sync.stopCh)
	sync.dynamicFactory = nil
	sync.dmtx.Unlock()
}

func (sync *KubeSynchronizer) startDynamicClientCache() {
	sync.dmtx.Lock()
	sync.stopCh = make(chan struct{})
	sync.dynamicFactory.Start(sync.stopCh)
	sync.dmtx.Unlock()
	klog.Info("Synchronizer cache (re)started")
}

func (sync *KubeSynchronizer) rediscoverResource() {
	sync.stopDynamicClientCaching()
	sync.discoverResourcesOnce()
	sync.startDynamicClientCache()
}

func (sync *KubeSynchronizer) discoverResourcesOnce() {
	klog.V(1).Info("Discovering cluster resources")
	defer klog.V(1).Info("Discovered cluster resources, done")

	sync.dmtx.Lock()
	if sync.dynamicFactory == nil {
		sync.dynamicFactory = dynamicinformer.NewDynamicSharedInformerFactory(sync.DynamicClient, informerFactoryPeriod)
	}
	sync.dmtx.Unlock()

	resources, err := discovery.NewDiscoveryClientForConfigOrDie(sync.localConfig).ServerPreferredResources()
	if err != nil {
		// do not return this error
		// some api server aggregation may cause this problem, but can still get return some resources.
		klog.Error("Failed to discover server resources. skipping err:", err)
	}

	filteredResources := discovery.FilteredBy(resourcePredicate, resources)
	klog.V(5).Info("Discovered resources: ", filteredResources)

	valid := make(map[schema.GroupVersionKind]bool)

	for _, rl := range filteredResources {
		sync.validateAPIResourceList(rl, valid)
	}

	klog.V(5).Info("valid resources remain:", valid)

	sync.kmtx.Lock()
	for k := range sync.KubeResources {
		if _, ok := valid[k]; !ok {
			delete(sync.KubeResources, k)
		}
	}
	sync.kmtx.Unlock()
}

func (sync *KubeSynchronizer) validateAPIResourceList(rl *metav1.APIResourceList, valid map[schema.GroupVersionKind]bool) {
	for _, res := range rl.APIResources {
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			klog.V(5).Info("Skipping ", rl.GroupVersion, " with error:", err)
			continue
		}

		gvk := schema.GroupVersionKind{
			Kind:    res.Kind,
			Group:   gv.Group,
			Version: gv.Version,
		}

		if internalIgnoredGroupKind[gvk.GroupKind()] {
			klog.V(5).Info("Skipping internal ignored resource:", gvk, "Categories:", res.Categories)
			continue
		}

		if sync.Extension.IsIgnoredGroupKind(gvk.GroupKind()) {
			klog.V(5).Info("Skipping ignored resource:", gvk, "Categories:", res.Categories)
			continue
		}

		sync.kmtx.Lock()
		resmap, ok := sync.KubeResources[gvk]
		sync.kmtx.Unlock()

		valid[gvk] = true

		if !ok {
			resmap = &ResourceMap{
				GroupVersionResource: schema.GroupVersionResource{},
				TemplateMap:          make(map[string]*TemplateUnit),
			}
		}

		if resmap.GroupVersionResource.Empty() {
			// kind added by registration, complete it with informer
			// create new dynamic factor if this is first new api found
			gvr := schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: res.Name,
			}

			resmap.GroupVersionResource = gvr
			resmap.Namespaced = res.Namespaced

			sync.kmtx.Lock()
			sync.KubeResources[gvk] = resmap
			sync.kmtx.Unlock()

			//telling cache that when action happens, call the handlerFuncs
			sync.dynamicFactory.ForResource(gvr).Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
				AddFunc: func(new interface{}) {
					obj := new.(*unstructured.Unstructured)
					if obj.GetKind() == crdKind || sync.Extension.IsObjectOwnedBySynchronizer(obj, sync.SynchronizerID) {
						sync.markServerUpdated(gvk)
					}
				},
				UpdateFunc: func(old, new interface{}) {
					obj := new.(*unstructured.Unstructured)
					if obj.GetKind() == crdKind || sync.Extension.IsObjectOwnedBySynchronizer(obj, sync.SynchronizerID) {
						sync.markServerUpdated(gvk)
					}
				},
				DeleteFunc: func(old interface{}) {
					obj := old.(*unstructured.Unstructured)
					if obj.GetKind() == crdKind {
						sync.rediscoverResource()
					}

					if obj.GetKind() == crdKind || sync.Extension.IsObjectOwnedBySynchronizer(obj, sync.SynchronizerID) {
						sync.markServerUpdated(gvk)
					}
				},
			})
			klog.V(5).Info("Start watching kind: ", res.Kind, ", resource: ", gvr, " objects in it: ", len(resmap.TemplateMap))
		}
	}
}

func kubeResourceAddVersionToGK(kubeResource map[schema.GroupVersionKind]*ResourceMap, regGvk schema.GroupVersionKind, newGvk schema.GroupVersionKind) {
	regResmap := kubeResource[regGvk]

	newResmap := &ResourceMap{
		GroupVersionResource: schema.GroupVersionResource{},
		TemplateMap:          make(map[string]*TemplateUnit),
	}

	newGvr := schema.GroupVersionResource{
		Group:    newGvk.Group,
		Version:  newGvk.Version,
		Resource: regResmap.GroupVersionResource.Resource,
	}

	newResmap.GroupVersionResource = newGvr
	newResmap.Namespaced = regResmap.Namespaced
	kubeResource[newGvk] = newResmap
}

func (sync *KubeSynchronizer) markServerUpdated(gvk schema.GroupVersionKind) {
	sync.kmtx.Lock()
	defer sync.kmtx.Unlock()

	if resmap, ok := sync.KubeResources[gvk]; ok {
		resmap.ServerUpdated = true
	}
}
