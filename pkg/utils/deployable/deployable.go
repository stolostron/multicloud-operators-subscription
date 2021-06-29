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

package deployable

import (
	"context"
	"encoding/json"
	"errors"

	"reflect"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
)

// DeployablePredicateFunc defines predicate function for deployable watch in deployable controller
var DeployablePredicateFunc = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newdpl := e.ObjectNew.(*appv1alpha1.Deployable)
		olddpl := e.ObjectOld.(*appv1alpha1.Deployable)

		if len(newdpl.GetFinalizers()) > 0 {
			return true
		}

		hosting := GetHostDeployableFromObject(newdpl)
		if hosting != nil {
			// reconcile its parent for status
			return true
		}

		if !reflect.DeepEqual(newdpl.GetAnnotations(), olddpl.GetAnnotations()) {
			return true
		}

		if !reflect.DeepEqual(newdpl.GetLabels(), olddpl.GetLabels()) {
			return true
		}

		oldtmpl := &unstructured.Unstructured{}
		newtmpl := &unstructured.Unstructured{}

		if olddpl.Spec.Template == nil || olddpl.Spec.Template.Raw == nil {
			return true
		}
		err := json.Unmarshal(olddpl.Spec.Template.Raw, oldtmpl)
		if err != nil {
			return true
		}
		if newdpl.Spec.Template.Raw == nil {
			return true
		}
		err = json.Unmarshal(newdpl.Spec.Template.Raw, newtmpl)
		if err != nil {
			return true
		}

		if !reflect.DeepEqual(newtmpl, oldtmpl) {
			return true
		}

		olddpl.Spec.Template = newdpl.Spec.Template.DeepCopy()
		return !reflect.DeepEqual(olddpl.Spec, newdpl.Spec)
	},
}

// CompareDeployable compare two deployables and return true if they are equal.
func CompareDeployable(olddpl *appv1alpha1.Deployable, newdpl *appv1alpha1.Deployable) bool {
	if !reflect.DeepEqual(newdpl.GetAnnotations(), olddpl.GetAnnotations()) {
		klog.V(5).Infof("old annotation: %#v, new annotation: %#v", olddpl.GetAnnotations(), newdpl.GetAnnotations())
		return false
	}

	if !reflect.DeepEqual(newdpl.GetLabels(), olddpl.GetLabels()) {
		klog.V(5).Infof("old lael: %#v, new label: %#v", olddpl.GetLabels(), newdpl.GetLabels())
		return false
	}

	oldtmpl := &unstructured.Unstructured{}
	newtmpl := &unstructured.Unstructured{}

	if olddpl.Spec.Template == nil || olddpl.Spec.Template.Raw == nil {
		return false
	}

	err := json.Unmarshal(olddpl.Spec.Template.Raw, oldtmpl)
	if err != nil {
		return false
	}

	if newdpl.Spec.Template.Raw == nil {
		return false
	}

	err = json.Unmarshal(newdpl.Spec.Template.Raw, newtmpl)
	if err != nil {
		return false
	}

	if !reflect.DeepEqual(newtmpl, oldtmpl) {
		klog.V(5).Infof("old template: %#v, new template: %#v", oldtmpl, newtmpl)
		return false
	}

	tmpdpl := olddpl.DeepCopy()

	tmpdpl.Spec.Template = newdpl.Spec.Template.DeepCopy()

	if !reflect.DeepEqual(tmpdpl.Spec, newdpl.Spec) {
		klog.V(5).Infof("old spec: %#v, new spec: %#v", tmpdpl.Spec, newdpl.Spec)
		return false
	}

	return true
}

// PrepareInstance prepares the deployable instane for later actions
func PrepareInstance(instance *appv1alpha1.Deployable) bool {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	original := instance.DeepCopy()

	annotations := instance.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	// set it to false as default
	if annotations[appv1alpha1.AnnotationLocal] == "" {
		annotations[appv1alpha1.AnnotationLocal] = "false"
	}

	if annotations[appv1alpha1.AnnotationManagedCluster] == "" {
		annotations[appv1alpha1.AnnotationManagedCluster] = client.ObjectKey{}.String()
	}

	instance.SetAnnotations(annotations)

	updated := !reflect.DeepEqual(original.GetAnnotations(), instance.GetAnnotations()) || !reflect.DeepEqual(original.GetLabels(), instance.GetLabels())

	labels := instance.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	instance.SetLabels(labels)

	return updated
}

// GetUnstructuredTemplateFromDeployable return error if needed
func GetUnstructuredTemplateFromDeployable(instance *appv1alpha1.Deployable) (*unstructured.Unstructured, error) {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	template := &unstructured.Unstructured{}

	if instance.Spec.Template == nil {
		return nil, errors.New("Processing local deployable without template:" + instance.GetName())
	}

	err := json.Unmarshal(instance.Spec.Template.Raw, template)
	klog.V(10).Info("Processing Local with template:", template)

	if err != nil {
		klog.Error("Failed to unmashal template with error: ", err)

		return nil, err
	}

	if template.GetKind() == "" {
		return nil, errors.New("Failed to update template with empty kind. gvk:" + template.GetObjectKind().GroupVersionKind().String())
	}

	return template, nil
}

// GetClusterFromResourceObject return nil if no host is found
func GetClusterFromResourceObject(obj metav1.Object) *types.NamespacedName {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	if obj == nil {
		return nil
	}

	annotations := obj.GetAnnotations()

	if annotations == nil {
		return nil
	}

	clstr := annotations[appv1alpha1.AnnotationManagedCluster]

	if clstr == "" {
		return nil
	}

	parsedstr := strings.Split(clstr, "/")

	if len(parsedstr) != 2 {
		return nil
	}

	host := &types.NamespacedName{Name: parsedstr[1], Namespace: parsedstr[0]}

	return host
}

// GetHostDeployableFromObject return nil if no host is found
func GetHostDeployableFromObject(obj metav1.Object) *types.NamespacedName {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	if obj == nil {
		return nil
	}

	annotations := obj.GetAnnotations()

	if annotations == nil {
		return nil
	}

	hosttr := annotations[appv1alpha1.AnnotationHosting]

	if hosttr == "" {
		return nil
	}

	parsedstr := strings.Split(hosttr, "/")

	if len(parsedstr) != 2 {
		return nil
	}

	host := &types.NamespacedName{Name: parsedstr[1], Namespace: parsedstr[0]}

	return host
}

// IsDependencyDeployable return true the deploable is dependent depolyable
func IsDependencyDeployable(instance *appv1alpha1.Deployable) bool {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	if instance == nil {
		return false
	}

	annotations := instance.GetAnnotations()

	if annotations == nil {
		return false
	}

	hosttr := annotations[appv1alpha1.AnnotationHosting]

	if hosttr == "" {
		return false
	}

	parsedstr := strings.Split(hosttr, "/")

	if len(parsedstr) != 2 {
		return false
	}

	hostDeployable := parsedstr[1]

	if hostDeployable == "" {
		return false
	}

	genHhostDeployable := strings.TrimSuffix(instance.GetGenerateName(), "-")

	if genHhostDeployable == "" {
		return false
	}

	klog.V(10).Info("hostDeployable:", hostDeployable, ", genHhostDeployable:", genHhostDeployable)

	return hostDeployable != genHhostDeployable
}

// UpdateDeployableStatus based on error message, and propagate resource status
// - nil:  success
// - others: failed, with error message in reason
func UpdateDeployableStatus(statusClient client.Client, templateerr error, tplunit metav1.Object, status interface{}) error {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	dpl := &appv1alpha1.Deployable{}
	host := GetHostDeployableFromObject(tplunit)

	var err error

	if host == nil {
		klog.Info("Failed to find hosting deployable for ", tplunit)
	} else {
		err = statusClient.Get(context.TODO(), *host, dpl)

		if err != nil {
			// for all errors including not found return
			return err
		}
	}

	klog.V(10).Info("Trying to update deployable status:", host, templateerr)

	dpl.Status.PropagatedStatus = nil
	if templateerr == nil {
		dpl.Status.Phase = appv1alpha1.DeployableDeployed
		dpl.Status.Reason = ""
	} else {
		dpl.Status.Phase = appv1alpha1.DeployableFailed
		dpl.Status.Reason = templateerr.Error()
	}

	if status != nil {
		if dpl.Status.ResourceStatus == nil {
			dpl.Status.ResourceStatus = &runtime.RawExtension{}
		}

		dpl.Status.ResourceStatus.Raw, err = json.Marshal(status)

		if err != nil {
			klog.Info("Failed to mashall status for ", host, status, " with err:", err)
		}
	}

	now := metav1.Now()
	dpl.Status.LastUpdateTime = &now
	err = statusClient.Status().Update(context.Background(), dpl)
	// want to print out the error log before leave
	if err != nil {
		klog.Error("Failed to update status of deployable ", dpl)
	}

	return err
}

// ContainsName check whether the namespacedName array a contains string x
func ContainsName(a []types.NamespacedName, x string) bool {
	for _, n := range a {
		if x == n.Name {
			return true
		}
	}

	return false
}

// PrintPropagatedStatus output Propagated Status for each cluster
func PrintPropagatedStatus(r map[string]*appv1alpha1.ResourceUnitStatus, msg string) {
	for cluster, unitStatus := range r {
		klog.Infof("%v - cluster: %v, unit status: %#v", msg, cluster, unitStatus)
	}
}

func InstanceDeepCopy(a, b interface{}) error {
	byt, err := json.Marshal(a)

	if err == nil {
		err = json.Unmarshal(byt, b)
	}

	return err
}

// GetPauseLabel check if the subscription-pause label exists
func GetPauseLabel(instance *appv1alpha1.Deployable) bool {
	labels := instance.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	if labels[appv1alpha1.LabelSubscriptionPause] != "" && strings.EqualFold(labels[appv1alpha1.LabelSubscriptionPause], "true") {
		return true
	}

	return false
}

// SetPauseLabelDplSubTpl set the subscription-pause label to a deployable containing a subscription template
func SetPauseLabelDplSubTpl(instance, targetdpl *appv1alpha1.Deployable) error {
	targetTpl := &unstructured.Unstructured{}

	err := json.Unmarshal(targetdpl.Spec.Template.Raw, targetTpl)
	if err != nil {
		klog.Error("Failed to unmashal target deployable subscription template with error: ", err)
		return err
	}

	if targetTpl.GetKind() != "Subscription" {
		return nil
	}

	targetTplLabels := targetTpl.GetLabels()
	if targetTplLabels == nil {
		targetTplLabels = make(map[string]string)
	}

	if GetPauseLabel(instance) {
		targetTplLabels[appv1alpha1.LabelSubscriptionPause] = "true"
	} else {
		targetTplLabels[appv1alpha1.LabelSubscriptionPause] = "false"
	}

	targetTpl.SetLabels(targetTplLabels)

	targetdpl.Spec.Template.Raw, err = json.Marshal(targetTpl)
	if err != nil {
		klog.Error("Error in mashalling targetTpl obj")
		return err
	}

	return nil
}
