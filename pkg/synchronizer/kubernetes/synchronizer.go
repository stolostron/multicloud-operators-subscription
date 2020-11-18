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
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	jsonpatch "k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"

	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

func (sync *KubeSynchronizer) checkServerObjects(gvk schema.GroupVersionKind, res *ResourceMap) error {
	if res == nil {
		errmsg := "Checking server objects with nil map"
		klog.Error(errmsg)

		return errors.NewBadRequest(errmsg)
	}

	klog.V(5).Info("Checking Server object:", res.GroupVersionResource)

	objlist, err := sync.DynamicClient.Resource(res.GroupVersionResource).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	var dl dynamic.ResourceInterface

	for _, obj := range objlist.Items {
		obj := obj
		if !sync.Extension.IsObjectOwnedBySynchronizer(&obj, sync.SynchronizerID) {
			continue
		}

		host := sync.Extension.GetHostFromObject(&obj)
		dpl := utils.GetHostDeployableFromObject(&obj)
		source := utils.GetSourceFromObject(&obj)

		if dpl == nil || host == nil {
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
			if obj.GetDeletionTimestamp() == nil {
				klog.V(3).Infof("Havesting tplunit from cluster host: %#v, obj: %#v, TemplateMap: %#v", dpl, obj, res.TemplateMap)

				unit := &TemplateUnit{
					ResourceUpdated: false,
					StatusUpdated:   false,
					Unstructured:    obj.DeepCopy(),
					Source:          source,
				}
				unit.Unstructured.SetGroupVersionKind(gvk)
				res.TemplateMap[reskey] = unit
			}
		} else {
			if tplunit.Source != source {
				klog.V(3).Info("Havesting resource ", dpl.Namespace, "/", dpl.Name, " but owned by other source, skipping")
				continue
			}

			status := obj.Object["status"]
			klog.V(4).Info("Found for ", dpl, ", tplunit:", tplunit, "Doing obj ", obj.GetNamespace(), "/", obj.GetName(), " with status:", status)
			delete(obj.Object, "status")

			err = sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, status, false)

			if err != nil {
				klog.Error("Failed to update host status with error:", err)
			}

			if tplunit.ResourceUpdated {
				continue
			}

			if !reflect.DeepEqual(obj, tplunit.Unstructured.Object) {
				newobj := tplunit.Unstructured.DeepCopy()
				newobj.SetResourceVersion(obj.GetResourceVersion())
				_, err = dl.Update(context.TODO(), newobj, metav1.UpdateOptions{})
				klog.V(5).Info("Check - Updated existing Resource to", tplunit, " with err:", err)

				sync.eventrecorder.RecordEvent(tplunit.Unstructured, "UpdateResource",
					"Synchronizer updated resource "+tplunit.GetName()+"of gvk:"+tplunit.GroupVersionKind().String()+" for retry", err)

				if err == nil {
					tplunit.ResourceUpdated = true
				}
			}
			// don't process the err of status update. leave it to next round house keeping
			if err != nil {
				return err
			}
			klog.V(5).Info("Updated template ", tplunit.Unstructured.GetName(), ":", tplunit.ResourceUpdated)
			res.TemplateMap[reskey] = tplunit
		}
	}

	return nil
}

func (sync *KubeSynchronizer) createNewResourceByTemplateUnit(ri dynamic.ResourceInterface, tplunit *TemplateUnit) error {
	klog.V(5).Info("Apply - Creating New Resource ", tplunit)

	tplunit.Unstructured.SetResourceVersion("")
	obj, err := ri.Create(context.TODO(), tplunit.Unstructured, metav1.CreateOptions{})

	// Auto Create Namespace if not exist
	if err != nil && errors.IsNotFound(err) {
		ns := &corev1.Namespace{}
		ns.Name = tplunit.GetNamespace()

		tplanno := tplunit.GetAnnotations()
		if tplanno == nil {
			tplanno = make(map[string]string)
		}

		nsanno := ns.GetAnnotations()
		if nsanno == nil {
			nsanno = make(map[string]string)
		}

		if tplanno[appv1alpha1.AnnotationHosting] > "" {
			nsanno[appv1alpha1.AnnotationHosting] = tplanno[appv1alpha1.AnnotationHosting]
			nsanno[appv1alpha1.AnnotationSyncSource] = "subnsdpl-" + tplanno[appv1alpha1.AnnotationHosting]
		}

		if tplanno[appv1alpha1.AnnotationClusterAdmin] > "" {
			// Do this so that nested children subscriptions inherit the cluster-admin role elevation as well.
			nsanno[appv1alpha1.AnnotationClusterAdmin] = tplanno[appv1alpha1.AnnotationClusterAdmin]
		}

		ns.SetAnnotations(nsanno)

		klog.V(1).Infof("Apply - Creating New Namespace: %#v", ns)

		nsus := &unstructured.Unstructured{}
		nsus.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(ns)

		if err == nil {
			nsus.SetGroupVersionKind(schema.GroupVersionKind{
				Version: "v1",
				Kind:    "Namespace",
			})
			sync.eventrecorder.RecordEvent(nsus, "CreateNamespace",
				"Synchronizer created namespace "+ns.Name+" for resource "+tplunit.GetName(), err)

			_, err = sync.DynamicClient.Resource(schema.GroupVersionResource{
				Version:  "v1",
				Resource: "namespaces",
			}).Create(context.TODO(), nsus, metav1.CreateOptions{})

			if err == nil {
				// try again
				obj, err = ri.Create(context.TODO(), tplunit.Unstructured, metav1.CreateOptions{})
			}
		}
	}

	if err != nil {
		tplunit.ResourceUpdated = false

		klog.Error("Failed to apply resource with error: ", err)

		return err
	}

	obj.SetGroupVersionKind(tplunit.GroupVersionKind())
	sync.eventrecorder.RecordEvent(obj, "CreateResource",
		"Synchronizer created resource "+tplunit.GetName()+" of gvk:"+obj.GroupVersionKind().String(), err)

	tplunit.ResourceUpdated = true

	if obj != nil {
		err = sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, obj.Object["status"], false)
	} else {
		err = sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, nil, false)
	}

	if err != nil {
		klog.Error("Failed to update host status with error: ", err)
	}

	return err
}

//updateResourceByTemplateUnit will have a NamespaceableResourceInterface,
//when calling, the ri will have the namespace and GVR information already.
//ri gets GVR from applyKindTemplates func
//ri gets namespace info from applyTemplate func
//
//updateResourceByTemplateUnit will then update,patch the obj given tplunit.
func (sync *KubeSynchronizer) updateResourceByTemplateUnit(ri dynamic.ResourceInterface,
	obj *unstructured.Unstructured, tplunit *TemplateUnit, isService bool) error {
	var err error

	overwrite := false
	merge := false
	tplown := sync.Extension.GetHostFromObject(tplunit)

	tmplAnnotations := tplunit.GetAnnotations()

	if tplown != nil && !sync.Extension.IsObjectOwnedByHost(obj, *tplown, sync.SynchronizerID) {
		// If the subscription is created by a subscription admin and reconcile option exists,
		// we can update the resource even if it is not owned by this subscription.
		// These subscription annotations are passed down to deployable payload by the subscribers.
		// When we update other owner's resources, make sure these annnotations along with other
		// subscription specific annotations are removed.
		if strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationClusterAdmin], "true") &&
			(strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption], appv1alpha1.MergeReconcile) ||
				strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption], appv1alpha1.ReplaceReconcile)) {
			klog.Infof("Resource %s/%s will be updated with reconcile option: %s.",
				tplunit.GetNamespace(),
				tplunit.GetName(),
				tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption])

			overwrite = true
		} else {
			errmsg := "Obj " + tplunit.GetNamespace() + "/" + tplunit.GetName() + " exists and owned by others, backoff"
			klog.Info(errmsg)

			tplunit.ResourceUpdated = false

			err = sync.Extension.UpdateHostStatus(errors.NewBadRequest(errmsg), tplunit.Unstructured, nil, false)

			if err != nil {
				klog.Error("Failed to update host status for existing resource with error:", err)
			}

			return err
		}
	}

	if strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption], appv1alpha1.MergeReconcile) {
		merge = true
	}

	newobj := tplunit.Unstructured.DeepCopy()
	newobj.SetResourceVersion(obj.GetResourceVersion())

	// If subscription-admin chooses merge option, remove the typical annotations we add. This will avoid the resources being
	// deleted when the subscription is removed.
	// If subscription-admin chooses replace option, keep the typical annotations we add. Subscription takes over the resources.
	// When the subscription is removed, the resources will be removed too.
	if overwrite && merge {
		// If overwriting someone else's resource, remove annotations like hosting subscription, hostring deployables... etc
		newobj = utils.RemoveSubAnnotations(newobj)
		newobj = utils.RemoveSubOwnerRef(newobj)
	}

	if merge || isService {
		var objb, tplb, pb []byte
		objb, err = obj.MarshalJSON()

		if err != nil {
			klog.Error("Failed to marshall obj with error:", err)
			return err
		}

		tplb, err = newobj.MarshalJSON()

		if err != nil {
			klog.Error("Failed to marshall tplunit with error:", err)
			return err
		}

		pb, err = jsonpatch.CreateThreeWayJSONMergePatch(tplb, tplb, objb)
		if err != nil {
			klog.Error("Failed to make patch with error:", err)
			return err
		}

		klog.Info("Patch object. obj: " + obj.GroupVersionKind().String())
		klog.V(5).Info("Generating Patch for service update.\nObjb:", string(objb), "\ntplb:", string(tplb), "\nPatch:", string(pb))
		_, err = ri.Patch(context.TODO(), obj.GetName(), types.MergePatchType, pb, metav1.PatchOptions{})
	} else {
		klog.Info("Update non-service object. newobj: " + newobj.GroupVersionKind().String())
		klog.V(5).Infof("Update non-service object. newobj: %#v", newobj)
		_, err = ri.Update(context.TODO(), newobj, metav1.UpdateOptions{})

		// Some kubernetes resources are immutable after creation. Log and ignore update errors.
		if errors.IsForbidden(err) {
			klog.Info(err.Error())
			return nil
		} else if errors.IsInvalid(err) {
			klog.Info(err.Error())
			return nil
		}
	}

	sync.eventrecorder.RecordEvent(tplunit.Unstructured, "UpdateResource",
		"Synchronizer updated resource for template "+tplunit.GetName()+" of gvk:"+tplunit.GroupVersionKind().String(), err)

	klog.V(5).Info("Check - Updated existing Resource to", tplunit, " with err:", err)

	if err == nil {
		tplunit.ResourceUpdated = true
	} else {
		klog.Error("Failed to update resource with error:", err)
	}

	sterr := sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, obj.Object["status"], false)

	if sterr != nil {
		klog.Error("Failed to update host status with error:", err)
	}

	return nil
}

var serviceGVR = schema.GroupVersionResource{
	Version:  "v1",
	Resource: "services",
}

func (sync *KubeSynchronizer) applyKindTemplates(res *ResourceMap) {
	nri := sync.DynamicClient.Resource(res.GroupVersionResource)

	for k, tplunit := range res.TemplateMap {
		klog.V(1).Infof("k: %v, res.GroupVersionResource: %v", k, res.GroupVersionResource)
		err := sync.applyTemplate(nri, res.Namespaced, k, tplunit, (res.GroupVersionResource == serviceGVR))

		if err != nil {
			klog.Error("Failed to apply kind template", tplunit.Unstructured, "with error:", err)
		}
	}
}

func (sync *KubeSynchronizer) applyTemplate(nri dynamic.NamespaceableResourceInterface, namespaced bool,
	k string, tplunit *TemplateUnit, isService bool) error {
	klog.V(1).Info("Applying (key:", k, ") template:", tplunit, tplunit.Unstructured, "updated:", tplunit.ResourceUpdated)

	var ri dynamic.ResourceInterface
	if namespaced {
		ri = nri.Namespace(tplunit.GetNamespace())
	} else {
		ri = nri
	}

	if !utils.AllowApplyTemplate(sync.LocalClient, tplunit.Unstructured) {
		klog.Infof("Applying template is paused: %v/%v", tplunit.GetName(), tplunit.GetNamespace())
		return nil
	}

	obj, err := ri.Get(context.TODO(), tplunit.GetName(), metav1.GetOptions{})

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
			tgtobj, err := dl.Get(context.TODO(), tplunit.GetName(), metav1.GetOptions{})
			if err == nil {
				if sync.Extension.IsObjectOwnedByHost(tgtobj, host, sync.SynchronizerID) {
					klog.V(5).Info("Resource is owned by ", host, "Deleting ", tplunit.Unstructured)

					deletepolicy := metav1.DeletePropagationBackground
					err = dl.Delete(context.TODO(), tplunit.GetName(), metav1.DeleteOptions{PropagationPolicy: &deletepolicy})
					sync.eventrecorder.RecordEvent(tplunit.Unstructured, "DeleteResource",
						"Synchronizer deleted resource "+tplunit.GetName()+" of gvk:"+tplunit.GroupVersionKind().String()+" by deregister", err)

					if err != nil {
						klog.Error("Failed to delete tplunit in kubernetes, with error:", err)
					}

					sterr := sync.Extension.UpdateHostStatus(err, tplunit.Unstructured, nil, true)

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

	klog.V(4).Info("overrode template: ", template)
	// skip no-op to template

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
