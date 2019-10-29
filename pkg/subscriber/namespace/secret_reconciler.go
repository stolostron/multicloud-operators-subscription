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

package namespace

import (
	"context"
	"encoding/json"
	"errors"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//SecretRecondiler defined a info collection for query secret resource
type SecretRecondiler struct {
	Subscriber *Subscriber
	Clt        client.Client
	Schema     *runtime.Scheme
	Itemkey    types.NamespacedName
}

//Reconcile handle the main logic to deploy the secret coming from the channel namespace
func (s *SecretRecondiler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	klog.Info("Reconciling: ", request.NamespacedName, " sercet for subitem ", s.Itemkey)


	sl, err := s.ListSecrets()
	if err != nil {
		return reconcile.Result{}, err
	}

	var dpls []*dplv1alpha1.Deployable
	//apply the subscription package filter to the correct annotated secret
	for _, srt := range sl.Items {
		if !isSecretAnnoatedAsDeployable(srt) {
			continue
		}

		srt, ok := ApplyFilters(srt, s.Subscriber.itemmap[s.Itemkey].Subscription)
		if ok {
			dpls = append(dpls, PackageSecert(srt))
		}
	}

	s.RegisterToResourceMap(dpls)


	return reconcile.Result{}, nil
}

//ListSecrets list all the secret resouces at the suscribed channel
func (s *SecretRecondiler) ListSecrets() (*v1.SecretList, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	subitem, ok := s.Subscriber.itemmap[s.Itemkey]
	if !ok {
		errmsg := "Failed to locate subscription item " + s.Itemkey.String() + " in existing map"

		klog.Error(errmsg)

		return nil, errors.New(errmsg)
	}

	sub := subitem.Subscription
	klog.V(10).Infof("Processing subscriptions: %v/%v ", sub.GetNamespace(), sub.GetName())

	secretList := &v1.SecretList{}

	targetChNamespace := subitem.Channel.Spec.PathName
	if targetChNamespace == "" {
		errmsg := "channel namespace should not be empty in channel resource of subitem " + sub.GetName()
		klog.Error(errmsg)

		return nil, errors.New(errmsg)
	}

	listOptions := &client.ListOptions{Namespace: targetChNamespace}

	if sub.Spec.PackageFilter != nil && sub.Spec.PackageFilter.LabelSelector != nil {
		clSelector, err := utils.ConvertLabels(sub.Spec.PackageFilter.LabelSelector)
		if err != nil {
			klog.Error("Failed to set label selector of subscrption:", sub.Spec.PackageFilter.LabelSelector, " err:", err)
		}

		listOptions.LabelSelector = clSelector
	}

	err := s.Clt.List(context.TODO(), listOptions, secretList)

	if err != nil {
		klog.Error("Failed to list objecrts from namespace ", targetChNamespace, " err:", err)
		return nil, err
	}

	return secretList, nil
}

//PackageSecert put the secret to the deployable template
func PackageSecert(s v1.Secret) *dplv1alpha1.Deployable {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
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

//ApplyFilters will apply the subscription level filters to the secret
func ApplyFilters(secret v1.Secret, sub *appv1alpha1.Subscription) (v1.Secret, bool) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
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

	return secret, true
}

//CleanUpObject is used to reset the sercet fields in order to put the secret into deployable template
func CleanUpObject(s v1.Secret) v1.Secret {
	s.SetResourceVersion("")

	t := types.UID("")

	s.SetUID(t)


	s.SetSelfLink("")

	gvk := schema.GroupVersionKind{
		Kind:    "Secret",
		Version: "v1",
	}
	s.SetGroupVersionKind(gvk)

	return s
}

//RegisterToResourceMap leverage the synchronizer to handle the sercet lifecycle management
func (s *SecretRecondiler) RegisterToResourceMap(dpls []*dplv1alpha1.Deployable) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}


	subscription := s.Subscriber.itemmap[s.Itemkey].Subscription

	hostkey := types.NamespacedName{Name: subscription.Name, Namespace: subscription.Namespace}
	syncsource := "subscription-" + hostkey.String()
	// subscribed k8s resource
	kvalid := s.Subscriber.synchronizer.CreateValiadtor(syncsource)
	pkgMap := make(map[string]bool)

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

		template, err = utils.OverrideResourceBySubscription(template, dpl.GetName(), subscription)
		if err != nil {
			err = utils.SetInClusterPackageStatus(&(subscription.Status), dpl.GetName(), err, nil)
			if err != nil {
				klog.Info("error in overriding for package: ", err)
			}

			pkgMap[dpl.GetName()] = true

			continue
		}

		err = controllerutil.SetControllerReference(subscription, template, s.Schema)

		if err != nil {
			klog.Warning("Adding owner reference to template, got error:", err)
			continue
		}

		kubesync := s.Subscriber.synchronizer

		orggvk := template.GetObjectKind().GroupVersionKind()
		validgvk := kubesync.GetValidatedGVK(orggvk)

		if validgvk == nil {
			gvkerr := errors.New("Resource " + orggvk.String() + " is not supported")
			err = utils.SetInClusterPackageStatus(&(subscription.Status), dpl.GetName(), gvkerr, nil)

			if err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}

			pkgMap[dpl.GetName()] = true


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

		err = kubesync.RegisterTemplate(hostkey, dpltosync, syncsource)

		if err != nil {
			err = utils.SetInClusterPackageStatus(&(subscription.Status), dpltosync.GetName(), err, nil)
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

		klog.V(10).Info("Finished Register ", *validgvk, hostkey, dplkey, " with err:", err)
	}

	s.Subscriber.synchronizer.ApplyValiadtor(kvalid)
}
