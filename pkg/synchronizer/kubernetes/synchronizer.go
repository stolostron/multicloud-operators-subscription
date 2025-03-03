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
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	jsonpatch "k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/metrics"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

func (sync *KubeSynchronizer) getGVRfromGVK(group, version, kind string) (schema.GroupVersionResource, bool, error) {
	pkgGK := schema.GroupKind{
		Kind:  kind,
		Group: group,
	}

	mapping, err := sync.RestMapper.RESTMapping(pkgGK, version)
	if err != nil {
		return schema.GroupVersionResource{}, false, fmt.Errorf("failed to get GVR from restmapping: %w", err)
	}

	var isNamespaced = true

	if mapping.Scope.Name() != "namespace" {
		isNamespaced = false
	}

	klog.Infof("scope: %#v", mapping.Scope)

	return mapping.Resource, isNamespaced, nil
}

// DeleteSingleSubscribedResource delete a subcribed resource from a appsub.
func (sync *KubeSynchronizer) DeleteSingleSubscribedResource(hostSub types.NamespacedName,
	pkgStatus appSubStatusV1alpha1.SubscriptionUnitStatus) error {
	pkgGroup, pkgVersion := utils.ParseAPIVersion(pkgStatus.APIVersion)

	if pkgGroup == "" && pkgVersion == "" {
		if pkgStatus.Phase == "Failed" {
			klog.Info("phase of resource is failed with no apiVersion info, nothing to delete")

			return nil
		}

		klog.Infof("invalid apiversion pkgStatus: %v", pkgStatus)

		return fmt.Errorf("invalid apiversion")
	}

	pkgGVR, isNamespaced, err := sync.getGVRfromGVK(pkgGroup, pkgVersion, pkgStatus.Kind)

	if err != nil {
		klog.Infof("Failed to get GVR from restmapping: %v", err)

		return err
	}

	nri := sync.DynamicClient.Resource(pkgGVR)

	var ri dynamic.ResourceInterface

	if isNamespaced {
		ri = nri.Namespace(pkgStatus.Namespace)
	} else {
		ri = nri
	}

	pkgObj, err := ri.Get(context.TODO(), pkgStatus.Name, metav1.GetOptions{})

	if err != nil {
		klog.Infof("Failed to get the package, no need to delete. err: %v, ", err)

		return nil
	}

	annotations := pkgObj.GetAnnotations()

	// If the resource has a do-not-delete: "true" annotation, skip the deletion of this resource
	if annotations[appv1alpha1.AnnotationResourceDoNotDeleteOption] == "true" {
		klog.Infof("pkgName: %v, pkgNamespace: %v has do-not-delete annotation, skip deleting", pkgStatus.Name, pkgStatus.Namespace)
		return nil
	}

	// The resource might not be owned by the subscription if you deployed the susbcription
	// with subscription-admin role and merge option. In this case, do not delete the resource on subscription deletion.
	if annotations[appv1alpha1.AnnotationHosting] != (hostSub.Namespace+"/"+hostSub.Name) &&
		annotations[appv1alpha1.AnnotationHosting] != (hostSub.Namespace+"/"+hostSub.Name+"-local") {
		klog.Infof("appsub: %v, pkgName: %v, pkgNamespace: %v, is not owned by the subscription. Skip deleting.",
			hostSub, pkgStatus.Name, pkgStatus.Namespace)

		return nil
	}

	deletepolicy := metav1.DeletePropagationBackground
	err = ri.Delete(context.TODO(), pkgObj.GetName(), metav1.DeleteOptions{PropagationPolicy: &deletepolicy})

	if err != nil && !errors.IsNotFound(err) {
		klog.Errorf("Failed to delete package, appsub: %v, pkgName: %v, pkgNamespace: %v, err: %v",
			hostSub, pkgStatus.Name, pkgStatus.Namespace, err)

		return err
	}

	return nil
}

// PurgeSubscribedResources purge all resources deployed by the appsub.
func (sync *KubeSynchronizer) PurgeAllSubscribedResources(appsub *appv1alpha1.Subscription) error {
	sync.kmtx.Lock()
	defer sync.kmtx.Unlock()

	hostSub := types.NamespacedName{
		Namespace: appsub.GetNamespace(),
		Name:      appsub.GetName(),
	}

	klog.Infof("Prepare to purge all resources deployed by the appsub: %v", hostSub.String())

	appSubStatus := &appSubStatusV1alpha1.SubscriptionStatus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SubscriptionStatus",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
	}

	appsubStatusName := hostSub.Name
	appsubStatusNs := hostSub.Namespace

	// Handle appsubstatus on local-cluster
	if sync.hub && !sync.standalone {
		appsubStatusName = strings.TrimSuffix(appsubStatusName, "-local")
	}

	appSubStatusKey := types.NamespacedName{
		Name:      appsubStatusName,
		Namespace: appsubStatusNs,
	}

	appSubUnitStatuses := []SubscriptionUnitStatus{}

	err := sync.LocalClient.Get(context.TODO(), appSubStatusKey, appSubStatus)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("appSubStatus not found, %s/%s", appSubStatusKey.Namespace, appSubStatusKey.Name)

			appsubClusterStatus := SubscriptionClusterStatus{
				Cluster:                   sync.SynchronizerID.Name,
				AppSub:                    hostSub,
				Action:                    "DELETE",
				SubscriptionPackageStatus: appSubUnitStatuses,
			}

			err := sync.SyncAppsubClusterStatus(appsub, appsubClusterStatus, nil, nil)
			if err != nil {
				klog.Warning("error while sync app sub cluster status: ", err)
			}

			return nil
		}

		klog.Infof("failed to get appSubStatus, appsubstatus: %s/%s, err: %v", appSubStatusKey.Namespace, appSubStatusKey.Name, err)

		return nil
	}

	if sync.SkipAppSubStatusResDel {
		klog.Info("SkipAppSubStatusResDel enabled for ", hostSub.Namespace, "/", hostSub.Name)
	} else {
		for _, pkgStatus := range appSubStatus.Statuses.SubscriptionPackageStatus {
			appSubUnitStatus := SubscriptionUnitStatus{}
			appSubUnitStatus.APIVersion = pkgStatus.APIVersion
			appSubUnitStatus.Kind = pkgStatus.Kind
			appSubUnitStatus.Name = pkgStatus.Name
			appSubUnitStatus.Namespace = pkgStatus.Namespace

			err := sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
			if err != nil {
				appSubUnitStatus.Phase = string(appSubStatusV1alpha1.PackageDeployFailed)
				appSubUnitStatus.Message = err.Error()
				appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)

				continue
			}

			appSubUnitStatus.Phase = string(appSubStatusV1alpha1.PackageDeployed)
			appSubUnitStatus.Message = ""
			appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)
		}

		legacyUnitStatuses := sync.getResourcesByLegacySubStatus(appsub)
		for _, legacyResource := range legacyUnitStatuses {
			appSubUnitStatus := SubscriptionUnitStatus{}
			appSubUnitStatus.Kind = legacyResource.Kind
			appSubUnitStatus.Name = legacyResource.Name
			appSubUnitStatus.Namespace = legacyResource.Namespace

			for _, appsub := range appSubUnitStatuses { // search for resources with same kind to find version
				if appSubUnitStatus.Kind == appsub.Kind {
					appSubUnitStatus.APIVersion = appsub.APIVersion
					break
				}
			}

			err := sync.DeleteSingleSubscribedResource(hostSub, legacyResource)
			if err != nil {
				appSubUnitStatus.Phase = string(appSubStatusV1alpha1.PackageDeployFailed)
				appSubUnitStatus.Message = err.Error()
				appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)

				continue
			}

			appSubUnitStatus.Phase = string(appSubStatusV1alpha1.PackageDeployed)
			appSubUnitStatus.Message = ""
			appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)
		}
	}

	appsubClusterStatus := SubscriptionClusterStatus{
		Cluster:                   sync.SynchronizerID.Name,
		AppSub:                    hostSub,
		Action:                    "DELETE",
		SubscriptionPackageStatus: appSubUnitStatuses,
	}

	err = sync.SyncAppsubClusterStatus(appsub, appsubClusterStatus, nil, nil)
	if err != nil {
		klog.Warning("error while sync app sub cluster status: ", err)
	}

	return nil
}

func (sync *KubeSynchronizer) ProcessSubResources(appsub *appv1alpha1.Subscription, resources []ResourceUnit,
	allowlist, denyList map[string]map[string]string, isAdmin, failOnStatusErr bool) error {
	hostSub := types.NamespacedName{
		Namespace: appsub.GetNamespace(),
		Name:      appsub.GetName(),
	}
	// meaning clean up all the resource from a source:host
	if len(resources) == 0 {
		return sync.PurgeAllSubscribedResources(appsub)
	}

	// handle orphan resource
	sync.kmtx.Lock()

	defer sync.kmtx.Unlock()

	appSubUnitStatuses := []SubscriptionUnitStatus{}
	gotDeployErrs := false
	startTime := time.Now().UnixMilli()

	for _, resource := range resources {
		appSubUnitStatus := SubscriptionUnitStatus{}

		template, err := sync.OverrideResource(hostSub, &resource)

		if err != nil {
			appSubUnitStatus.Phase = string(appSubStatusV1alpha1.PackageDeployFailed)
			appSubUnitStatus.Message = err.Error()
			appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)
			gotDeployErrs = true

			klog.Infof("Failed to override resource. err: %v", err)

			continue
		}

		resource.Resource = template

		appSubUnitStatus.APIVersion = resource.Resource.GetAPIVersion()
		appSubUnitStatus.Kind = resource.Resource.GetKind()
		appSubUnitStatus.Name = resource.Resource.GetName()

		pkgGVR, isNamespaced, err := sync.getGVRfromGVK(resource.Gvk.Group, resource.Gvk.Version, resource.Gvk.Kind)

		if isNamespaced {
			appSubUnitStatus.Namespace = resource.Resource.GetNamespace()
		}

		if err != nil {
			appSubUnitStatus.Namespace = resource.Resource.GetNamespace()
			appSubUnitStatus.Phase = string(appSubStatusV1alpha1.PackageDeployFailed)
			appSubUnitStatus.Message = err.Error()
			appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)
			gotDeployErrs = true

			klog.Infof("Failed to get GVR from restmapping: %v", err)

			continue
		}

		nri := sync.DynamicClient.Resource(pkgGVR)

		err = sync.applyTemplate(nri, isNamespaced, resource, isSpecialResource(pkgGVR), allowlist, denyList, isAdmin)

		if err != nil {
			appSubUnitStatus.Phase = string(appSubStatusV1alpha1.PackageDeployFailed)
			appSubUnitStatus.Message = err.Error()
			appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)
			gotDeployErrs = true

			klog.Errorf("Failed to apply kind template, pkg: %v/%v, error: %v ",
				appSubUnitStatus.Namespace, appSubUnitStatus.Name, err)

			continue
		}

		appSubUnitStatus.Phase = string(appSubStatusV1alpha1.PackageDeployed)
		appSubUnitStatus.Message = ""
		appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)
	}

	appsubClusterStatus := SubscriptionClusterStatus{
		Cluster:                   sync.SynchronizerID.Name,
		AppSub:                    hostSub,
		Action:                    "APPLY",
		SubscriptionPackageStatus: appSubUnitStatuses,
	}

	err := sync.SyncAppsubClusterStatus(appsub, appsubClusterStatus, nil, nil)
	endTime := time.Now().UnixMilli()

	if err != nil {
		klog.Error("error while sync app sub cluster status: ", err)
		metrics.LocalDeploymentFailedPullTime.
			WithLabelValues(appsub.Namespace, appsub.Name).
			Observe(float64(endTime - startTime))

		return err
	}

	if gotDeployErrs {
		metrics.LocalDeploymentFailedPullTime.
			WithLabelValues(appsub.Namespace, appsub.Name).
			Observe(float64(endTime - startTime))
	} else {
		metrics.LocalDeploymentSuccessfulPullTime.
			WithLabelValues(appsub.Namespace, appsub.Name).
			Observe(float64(endTime - startTime))
	}

	if failOnStatusErr {
		appsubstatus, err := GetAppsubReportStatus(sync.LocalClient, sync.hub, sync.standalone, hostSub.Namespace, hostSub.Name)
		if err != nil {
			klog.Infof("failed to get subscription status for %s/%s, err:%v", hostSub.Namespace, hostSub.Name, err.Error())

			return err
		}

		if appsubstatus.Statuses.SubscriptionStatus.Phase == appSubStatusV1alpha1.SubscriptionDeployFailed {
			klog.Info("subscription status has failed phase, return error")

			return fmt.Errorf("subscription status error: %v", appsubstatus.Statuses.SubscriptionStatus.Message)
		}
	}

	return nil
}

func (sync *KubeSynchronizer) createNewResourceByTemplateUnit(ri dynamic.ResourceInterface, tplunit *unstructured.Unstructured) error {
	klog.Infof("Apply - Creating New Resource: %v/%v, kind: %v", tplunit.GetNamespace(), tplunit.GetName(), tplunit.GetKind())

	tplunit.SetResourceVersion("")
	obj, err := ri.Create(context.TODO(), tplunit, metav1.CreateOptions{})

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

		klog.Infof("Apply - Creating New Namespace: %#v", ns)

		nsus := &unstructured.Unstructured{}
		nsus.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(ns)

		if err == nil {
			nsus.SetGroupVersionKind(schema.GroupVersionKind{
				Version: "v1",
				Kind:    "Namespace",
			})

			_, err = sync.DynamicClient.Resource(schema.GroupVersionResource{
				Version:  "v1",
				Resource: "namespaces",
			}).Create(context.TODO(), nsus, metav1.CreateOptions{})

			if err == nil {
				// try again
				obj, err = ri.Create(context.TODO(), tplunit, metav1.CreateOptions{})
			}
		}
	}

	if err != nil {
		klog.Error("Failed to apply resource with error: ", err)

		return err
	}

	obj.SetGroupVersionKind(tplunit.GroupVersionKind())

	if err != nil {
		klog.Error("Failed to update host status with error: ", err)
	}

	return err
}

// updateResourceByTemplateUnit will have a NamespaceableResourceInterface,
// when calling, the ri will have the namespace and GVR information already.
// ri gets GVR from applyKindTemplates func
// ri gets namespace info from applyTemplate func
//
// updateResourceByTemplateUnit will then update,patch the obj given tplunit.
func (sync *KubeSynchronizer) updateResourceByTemplateUnit(ri dynamic.ResourceInterface,
	origUnit *unstructured.Unstructured, tplunit *unstructured.Unstructured, specialResource bool) error {
	var err error

	overwrite := false
	merge := true
	tplown := sync.Extension.GetHostFromObject(tplunit)
	isHelmRelease := strings.EqualFold(tplunit.GetAPIVersion(), "apps.open-cluster-management.io/v1") &&
		strings.EqualFold(tplunit.GetKind(), "HelmRelease")

	tmplAnnotations := tplunit.GetAnnotations()

	if tplown != nil && !sync.Extension.IsObjectOwnedByHost(origUnit, *tplown, sync.SynchronizerID) {
		// If the subscription is created by a subscription admin and reconcile option exists,
		// we can update the resource even if it is not owned by this subscription.
		// These subscription annotations are passed down payload by the subscribers.
		// When we update other owner's resources, make sure these annnotations along with other
		// subscription specific annotations are removed.
		if strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationClusterAdmin], "true") &&
			(strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption], appv1alpha1.MergeReconcile) ||
				strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption], appv1alpha1.ReplaceReconcile) ||
				strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption], appv1alpha1.MergeAndOwnReconcile)) {
			klog.Infof("Resource %s/%s will be updated with reconcile option: %s.",
				tplunit.GetNamespace(),
				tplunit.GetName(),
				tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption])

			overwrite = true
		} else {
			errmsg := "Obj " + tplunit.GetNamespace() + "/" + tplunit.GetName() + " exists and owned by others, backoff"
			klog.Info(errmsg)

			return errors.NewBadRequest("Obj " + tplunit.GetNamespace() + "/" + tplunit.GetName() + " exists and owned by others, backoff")
		}
	}

	if strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption], appv1alpha1.ReplaceReconcile) {
		merge = false
	}

	if strings.EqualFold(tplunit.GetKind(), "subscription") &&
		strings.EqualFold(tplunit.GetAPIVersion(), "apps.open-cluster-management.io/v1") {
		klog.Info("Always apply replace to appsub kind resource")

		merge = false
	}

	hasHostSubscription := tmplAnnotations[appv1alpha1.AnnotationHosting] != ""

	newobj := tplunit.DeepCopy()
	newobj.SetResourceVersion(origUnit.GetResourceVersion())

	// If subscription-admin chooses merge option, remove the typical annotations we add. This will avoid the resources being
	// deleted when the subscription is removed.
	// If subscription-admin chooses replace option, keep the typical annotations we add. Subscription takes over the resources.
	// When the subscription is removed, the resources will be removed too.
	// If mergeAndOwn, do not remove the annotations and ownerRef. We want to merge and also take ownership of the existing resource.
	if overwrite && merge && !strings.EqualFold(tmplAnnotations[appv1alpha1.AnnotationResourceReconcileOption], appv1alpha1.MergeAndOwnReconcile) {
		// If overwriting someone else's resource, remove annotations like hosting subscription... etc
		newobj = utils.RemoveSubAnnotations(newobj)
		newobj = utils.RemoveSubOwnerRef(newobj)
	}

	if (merge || specialResource) && !isHelmRelease {
		if specialResource {
			klog.Info("One of special resources requiring merge update")
		}

		var objb, tplb, pb []byte
		objb, err = origUnit.MarshalJSON()

		if err != nil {
			klog.Error("Failed to marshall obj with error:", err)

			return err
		}

		tplb, err = newobj.MarshalJSON()

		if err != nil {
			klog.Error("Failed to marshall tplunit with error:", err)

			return err
		}

		// Note: this 3-way merge patch doesn't work on deletion patch, we don't support delete patch yet.
		// replace is recommended for deleting fields
		pb, err = jsonpatch.CreateThreeWayJSONMergePatch(tplb, tplb, objb)
		if err != nil {
			klog.Error("Failed to make patch with error:", err)

			return err
		}

		klog.Infof("Patch object. obj: %s, %s, patch: %s", origUnit.GetName(), origUnit.GroupVersionKind().String(), string(pb))
		klog.V(1).Info("Generating Patch for service update.\nObjb:", string(objb), "\ntplb:", string(tplb), "\nPatch:", string(pb))

		_, err = ri.Patch(context.TODO(), origUnit.GetName(), types.MergePatchType, pb, metav1.PatchOptions{})
	} else {
		klog.Info("Apply object. newobj: " + newobj.GroupVersionKind().String())
		klog.V(1).Infof("Apply object. newobj: %#v", newobj)
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

	klog.Info("Check - Updated existing Resource to", tplunit, " with err:", err)

	if err != nil {
		klog.Error("Failed to update resource with error:", err)

		return err
	}

	if strings.EqualFold(tplunit.GetKind(), "subscription") && hasHostSubscription {
		klog.Info("this is propagated subscription resource. skip updating status")
	}

	return nil
}

var serviceGVR = schema.GroupVersionResource{
	Version:  "v1",
	Resource: "services",
}

var serviceAccountGVR = schema.GroupVersionResource{
	Version:  "v1",
	Resource: "serviceaccounts",
}

var namespaceGVR = schema.GroupVersionResource{
	Version:  "v1",
	Resource: "namespaces",
}

func isSpecialResource(gvr schema.GroupVersionResource) bool {
	return gvr == serviceGVR || gvr == serviceAccountGVR || gvr == namespaceGVR
}

func (sync *KubeSynchronizer) applyTemplate(nri dynamic.NamespaceableResourceInterface, namespaced bool,
	resource ResourceUnit, specialResource bool, allowlist, denyList map[string]map[string]string, isAdmin bool) error {
	tplunit := resource.Resource
	klog.Infof("Applying template: %v/%v, kind: %v", tplunit.GetNamespace(), tplunit.GetName(), tplunit.GetKind())

	var ri dynamic.ResourceInterface
	if namespaced {
		ri = nri.Namespace(tplunit.GetNamespace())
	} else {
		ri = nri
	}

	if !utils.AllowApplyTemplate(sync.LocalClient, tplunit) {
		klog.Infof("Applying template is paused: %v/%v, kind: %v", tplunit.GetNamespace(), tplunit.GetName(), tplunit.GetKind())

		return nil
	}

	if utils.IsResourceDenied(*tplunit, denyList, isAdmin) {
		denyError := fmt.Errorf("the resource apiVersion: %s kind: %s is on the deny list. Not deployed",
			tplunit.GetAPIVersion(), tplunit.GetKind())

		klog.Info(denyError.Error())

		return denyError
	}

	if !utils.IsResourceAllowed(*tplunit, allowlist, isAdmin) {
		denyError := fmt.Errorf("the resource apiVersion: %s kind: %s is not on the allow list. Not deployed",
			tplunit.GetAPIVersion(), tplunit.GetKind())

		if !isAdmin {
			denyError = fmt.Errorf("not deployed by a subscription admin. the resource apiVersion: %s kind: %s is not deployed",
				tplunit.GetAPIVersion(), tplunit.GetKind())
		}

		klog.Info(denyError.Error())

		return denyError
	}

	origUnit, err := ri.Get(context.TODO(), tplunit.GetName(), metav1.GetOptions{})

	if err != nil {
		if errors.IsNotFound(err) {
			err = sync.createNewResourceByTemplateUnit(ri, tplunit)
		} else {
			klog.Error("Failed to apply resource with error:", err)
		}
	} else {
		err = sync.updateResourceByTemplateUnit(ri, origUnit, tplunit, specialResource)
	}

	klog.Infof("Applied Kind Template: %v/%v, err: %v ", tplunit.GetNamespace(), tplunit.GetName(), err)

	return err
}

// OverrideResource updates resource based on the hosting appsub before the resource is deployed.
func (sync *KubeSynchronizer) OverrideResource(hostSub types.NamespacedName, resource *ResourceUnit) (*unstructured.Unstructured, error) {
	// Parse the resource in template
	if klog.V(utils.QuiteLogLel).Enabled() {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	var err error

	appsub, err := sync.getHostingAppSub(hostSub)
	if err != nil {
		return nil, err
	}

	template := resource.Resource.DeepCopy()

	if template.GetKind() == "" {
		return nil, errors.NewBadRequest("Failed to update template with empty kind. gvk:" + template.GetObjectKind().GroupVersionKind().String())
	}

	// set name to resource name if not given
	if template.GetName() == "" {
		template.SetName(hostSub.Name)
	}

	// carry/override with appsub labels
	tpllbls := template.GetLabels()
	if tpllbls == nil {
		tpllbls = make(map[string]string)
	}

	klog.V(1).Infof("pre template lables : %v", tpllbls)

	for k, v := range appsub.GetLabels() {
		if _, ok := tpllbls[k]; ok {
			continue
		}

		tpllbls[k] = v
	}

	klog.V(1).Infof("template lables combinded with appsub labels: %v", tpllbls)

	template.SetLabels(tpllbls)

	err = sync.Extension.SetHostToObject(template, hostSub, sync.SynchronizerID)
	if err != nil {
		klog.Error("Failed to set host to object with error:", err)
	}

	// apply override in template
	if sync.SynchronizerID != nil {
		ovmap, err := utils.PrepareOverrides(*sync.SynchronizerID, appsub)
		if err != nil {
			klog.Errorf("Failed to prepare override for instance: %v/%v", appsub.Namespace, appsub.Name)

			return nil, err
		}

		template, err = utils.OverrideTemplate(template, ovmap)

		if err != nil {
			klog.Errorf("Failed to apply override for instance: %v/%v", appsub.Namespace, appsub.Name)

			return nil, err
		}

		if template.GetNamespace() != appsub.Namespace {
			template = utils.RemoveSubOwnerRef(template)
		}
	}

	klog.Infof("overrode template: %v/%v, kind: %v", template.GetNamespace(), template.GetName(), template.GetKind())

	return template, nil
}

func (sync *KubeSynchronizer) IsResourceNamespaced(rsc *unstructured.Unstructured) bool {
	pkgGroup := rsc.GroupVersionKind().Group
	pkgVersion := rsc.GroupVersionKind().Version
	pkgKind := rsc.GroupVersionKind().Kind

	_, isNamespaced, err := sync.getGVRfromGVK(pkgGroup, pkgVersion, pkgKind)

	if err != nil {
		klog.Infof("Failed to get GVR from restmapping: %v", err)

		return false
	}

	return isNamespaced
}

func (sync *KubeSynchronizer) getHostingAppSub(hostSub types.NamespacedName) (*appv1alpha1.Subscription, error) {
	appsub := &appv1alpha1.Subscription{}

	if err := sync.LocalClient.Get(context.TODO(), hostSub, appsub); err != nil {
		klog.Errorf("failed to get hosting appsub: %v, error: %v ", hostSub.String(), err)

		return nil, err
	}

	return appsub, nil
}
