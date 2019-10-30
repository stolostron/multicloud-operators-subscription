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

package helmrepo

import (
	"encoding/json"
	"errors"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//ListAndRegistSecrets is used to list and manage the referred secert for a subscription
func (hrsi *SubscriberItem) ListAndRegistSecrets() error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	chSrt := hrsi.ChannelSecret
	//should list from the channel to see if the secret is there or not

	if chSrt == nil {
		emsg := "referred secert is not set up correctly or it's nil"
		return errors.New(emsg)
	}

	cleanedSrt := utils.CleanUpObject(*chSrt)

	dplSrt := utils.PackageSecert(cleanedSrt)
	hrsi.RegisterToResourceMap(dplSrt)

	return nil
}

//RegisterToResourceMap leverage the synchronizer to handle the sercet lifecycle management
func (hrsi *SubscriberItem) RegisterToResourceMap(dpl *dplv1alpha1.Deployable) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	subscription := hrsi.Subscription

	hostkey := types.NamespacedName{Name: subscription.Name, Namespace: subscription.Namespace}
	syncsource := "subscription-" + hostkey.String()
	// subscribed k8s resource
	kvalid := hrsi.synchronizer.CreateValiadtor(syncsource)

	template := &unstructured.Unstructured{}

	if dpl.Spec.Template == nil {
		klog.Warning("Processing local deployable without template:", dpl)
	}

	err := json.Unmarshal(dpl.Spec.Template.Raw, template)
	if err != nil {
		klog.Warning("Processing local deployable with error template:", dpl, err)
	}

	err = controllerutil.SetControllerReference(subscription, template, hrsi.scheme)

	if err != nil {
		klog.Warning("Adding owner reference to template, got error:", err)
	}

	kubesync := hrsi.synchronizer

	orggvk := template.GetObjectKind().GroupVersionKind()
	validgvk := kubesync.GetValidatedGVK(orggvk)

	if kubesync.KubeResources[*validgvk].Namespaced {
		template.SetNamespace(subscription.Namespace)
	}

	dpltosync := dpl.DeepCopy()
	dpltosync.Spec.Template.Raw, err = json.Marshal(template)

	if err != nil {
		klog.Warning("Mashaling template, got error:", err)
	}

	annotations := dpltosync.GetAnnotations()

	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[dplv1alpha1.AnnotationLocal] = "true"
	dpltosync.SetAnnotations(annotations)

	err = kubesync.RegisterTemplate(hostkey, dpltosync, syncsource)

	dplkey := types.NamespacedName{
		Name:      dpltosync.Name,
		Namespace: dpltosync.Namespace,
	}

	kvalid.AddValidResource(*validgvk, hostkey, dplkey)

	klog.V(10).Info("Finished Register ", *validgvk, hostkey, dplkey, " with err:", err)

	hrsi.synchronizer.ApplyValiadtor(kvalid)
}
