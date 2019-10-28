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

package utils

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"

	nssub "github.com/IBM/multicloud-operators-subscription/pkg/subscriber/namespace"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// list resource
//ListSecrets list all the secret resouces at the suscribed channel
func ListSecrets(sub *appv1alpha1.Subscription, kubeclient client.Client) *v1.SecretList {

	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)
		defer klog.Infof("Exiting: %v()", fnName)
	}
	klog.V(10).Infof("Processing subscriptions: %v/%v ", sub.GetNamespace(), sub.GetName())

	secretList := &v1.SecretList{}

	targetChNamespace := ""

	if sub.Spec.CatalogSourceNamespace != "" {
		targetChNamespace = sub.Spec.CatalogSourceNamespace
	} else if sub.Spec.Channel != "" {
		strs := strings.Split(sub.Spec.Channel, "/")
		if len(strs) == 2 {
			targetChNamespace = strs[0]
		} else {
			targetChNamespace = "default"
		}
	}

	listOptions := &client.ListOptions{Namespace: targetChNamespace}
	if sub.Spec.PackageFilter != nil && sub.Spec.PackageFilter.LabelSelector != nil {
		clSelector, err := ConvertLabels(sub.Spec.PackageFilter.LabelSelector)
		if err != nil {
			klog.Error("Failed to set label selector of subscrption:", sub.Spec.PackageFilter.LabelSelector, " err:", err)
		}
		listOptions.LabelSelector = clSelector
	}

	err := kubeclient.List(context.TODO(), listOptions, secretList)

	if err != nil {
		klog.Error("Failed to list objecrts from namespace ", targetChNamespace, " err:", err)
	}
	return secretList
}

// put the resource into template

func PackageSecert(s v1.Secret) *dplv1alpha1.Deployable {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)
		defer klog.Infof("Exiting: %v()", fnName)
	}

	dpl := &dplv1alpha1.Deployable{}
	dpl.Name = s.GetName()
	dpl.Namespace = s.GetNamespace()
	dpl.Spec.Template = &runtime.RawExtension{}

	sRaw, err := json.Marshal(s)
	dpl.Spec.Template.Raw = sRaw
	if err != nil {
		klog.Error("Failed to unmashall ", s.GetNamespace(), "/", s.GetName(), " err:", err)
	}
	klog.V(10).Infof("Retived Dpl: %v", dpl)

	return dpl
}

func isSecretAnnoatedAsDeployable(srt v1.Secret) bool {
	secretsAnno := srt.GetAnnotations()

	if secretsAnno == nil {
		return false
	}

	if _, ok := secretsAnno[appv1alpha1.AnnotationDeployables]; !ok {
		return false
	}
	return true

}

// process template, such as annotation check

func ApplyFilters(secret v1.Secret, sub *appv1alpha1.Subscription) (v1.Secret, bool) {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)
		defer klog.Infof("Exiting: %v()", fnName)
	}

	secret = CleanUpObject(secret)

	// adding this to avoid the JSON marshall error caused by the embedded fileds, typeMeta

	if sub.Spec.PackageFilter != nil {
		if sub.Spec.Package != "" && sub.Spec.Package != secret.GetName() {
			klog.Info("Name does not match, skiping:", sub.Spec.Package, "|", secret.GetName())
			return secret, false
		}
		subAnno := sub.GetAnnotations()
		klog.V(10).Info("checking annotations filter:", subAnno)
		if subAnno != nil {
			secretsAnno := secret.GetAnnotations()
			for k, v := range subAnno {
				if secretsAnno[k] != v {
					klog.Info("Annotation filter does not match:", k, "|", v, "|", secretsAnno[k])
					return secret, false
				}
			}
		}
	}
	// TODO need to figure out the version filter for this one
	// if !utils.IsDeployableInVersionSet(versionMap, dpl) {
	// continue
	// }
	return secret, true
}

func CleanUpObject(s v1.Secret) v1.Secret {

	s.SetResourceVersion("")
	t := types.UID("")
	s.SetUID(t)
	// s.SetCreationTimestamp()
	s.SetSelfLink("")

	gvk := schema.GroupVersionKind{
		Kind:    "Secret",
		Version: "v1",
	}
	s.SetGroupVersionKind(gvk)

	return s

}


// need to use the light version of the do subscription to refactor this function
func RegisterToResourceMap(dpls []*dplv1alpha1.Deployable, subInfo *nssub.SubscriptionInfo) {

	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)
		defer klog.Infof("Exiting: %v()", fnName)
	}

	if len(dpls) == 0 {
		return
	}
	subscription := subInfo.SubItem.Subscription

	// hostkey := types.NamespacedName{Name: subscription.Name, Namespace: subscription.Namespace}
	syncsource := "subscription-" + subInfo.HostKey.String()
	// subscribed k8s resource

	for _, dpl := range dpls {
		template := &unstructured.Unstructured{}
		if dpl.Spec.Template == nil {
			klog.Warning("Processing local deployable without template:", dpl)
			continue
		}

		err := json.Unmarshal(dpl.Spec.Template.Raw, template)
		if err != nil {
			klog.Warning("Processing local deployable with error template:", dpl, err)
			continue
		}

		template, err = OverrideResourceBySubscription(template, dpl.GetName(), subscription)
		if err != nil {
			err = SetInClusterPackageStatus(&(subscription.Status), dpl.GetName(), err, nil)
			if err != nil {
				klog.Info("error in overriding for package: ", err)
			}
			(*subInfo.PkgMap)[dpl.GetName()] = true
			continue
		}

		err = controllerutil.SetControllerReference(subscription, template, subInfo.Schema)

		if err != nil {
			klog.Warning("Adding owner reference to template, got error:", err)
			continue
		}

		kvalid := subInfo.Kvalid
		kubesync := subInfo.DplSync

		orggvk := template.GetObjectKind().GroupVersionKind()
		validgvk := kubesync.GetValidatedGVK(orggvk)

		if validgvk == nil {
			gvkerr := errors.New("Resource " + orggvk.String() + " is not supported")
			err = SetInClusterPackageStatus(&(subscription.Status), dpl.GetName(), gvkerr, nil)
			if err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}
			(*subInfo.PkgMap)[dpl.GetName()] = true
			continue
		}

		if kubesync.KubeResources[*validgvk].Namespaced {
			template.SetNamespace(subscription.Namespace)
		}

		dpltosync := dpl.DeepCopy()
		dpltosync.Spec.Template.Raw, err = json.Marshal(template)
		if err != nil {
			klog.Warning("Mashaling template, got error:", err)
			continue
		}

		annotations := dpltosync.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[dplv1alpha1.AnnotationLocal] = "true"
		dpltosync.SetAnnotations(annotations)

		err = kubesync.RegisterTemplate(subInfo.HostKey, dpltosync, syncsource)
		if err != nil {
			err = SetInClusterPackageStatus(&(subscription.Status), dpltosync.GetName(), err, nil)
			if err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}
			(*subInfo.PkgMap)[dpltosync.GetName()] = true
			continue
		}

		dplkey := types.NamespacedName{
			Name:      dpltosync.Name,
			Namespace: dpltosync.Namespace,
		}
		kvalid.AddValidResource(*validgvk, subInfo.HostKey, dplkey)
		(*subInfo.PkgMap)[dplkey.Name] = true
		klog.V(10).Info("Finished Register ", *validgvk, subInfo.HostKey, dplkey, " with err:", err)

	}

}

// to these the new flow
// 1. able to list secret from the target namespace
// 2. package the secret
// 3. call to the deployable controller to handle the packaged secret

func DeploySecretFromSubscribedNamespace(subInfo nssub.SubscriptionInfo) {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)
		defer klog.Infof("Exiting: %v()", fnName)
	}

	sl := ListSecrets(subInfo.SubItem.Subscription, subInfo.Clt)
	if len(sl.Items) == 0 {
		return
	}

	var dpls []*dplv1alpha1.Deployable
	//apply the subscription package filter to the correct annotated secret
	for _, srt := range sl.Items {

		if !isSecretAnnoatedAsDeployable(srt) {
			continue
		}

		srt, ok := ApplyFilters(srt, subInfo.SubItem.Subscription)
		if ok {
			dpls = append(dpls, PackageSecert(srt))
		}
	}

	if len(dpls) > 0 {
		RegisterToResourceMap(dpls, subInfo)
	}

}

// func DeploySecretFromSubReference(kvalid *kubesync.Validator, pkgMap *map[string]bool, sh *SecretHandler, hostkey types.NamespacedName, subType string) {
// 	if klog.V(QuiteLogLel) {
// 		fnName := GetFnName()
// 		klog.Infof("Entering: %v()", fnName)
// 		defer klog.Infof("Exiting: %v()", fnName)
// 	}

// 	subscription := sh.Sub
// 	sl := ListSecrets(subscription, sh.Clt)
// 	if len(sl.Items) == 0 {
// 		return
// 	}

// 	var dpls []*dplv1alpha1.Deployable

// 	secretRef := sh.GetSecretRef()
// 	if secretRef == nil {
// 		return
// 	}
// 	for _, srt := range sl.Items {
// 		if srt.GetName() == secretRef.Name && srt.GetNamespace() == secretRef.Namespace {
// 			srt, ok := ApplyFilters(srt, subscription)
// 			if ok {
// 				dpls = append(dpls, PackageSecert(srt))
// 			}
// 		}
// 	}

// 	if len(dpls) > 0 {
// 		RegisterToResourceMap(dpls, kvalid, pkgMap, sh, hostkey)
// 	}

// }
