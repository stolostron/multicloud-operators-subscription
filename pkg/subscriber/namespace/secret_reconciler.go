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
	"fmt"
	"time"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	synckube "github.com/IBM/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"

	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type spySync interface {
	CreateValiadtor(string) *synckube.Validator
	ApplyValiadtor(*synckube.Validator)
	GetValidatedGVK(schema.GroupVersionKind) *schema.GroupVersionKind
	RegisterTemplate(types.NamespacedName, *dplv1alpha1.Deployable, string) error
	IsResourceNamespaced(schema.GroupVersionKind) bool
}

//SecretReconciler defined a info collection for query secret resource
type SecretReconciler struct {
	Subscriber *Subscriber
	Clt        client.Client
	Schema     *runtime.Scheme
	Itemkey    types.NamespacedName
	sSync      spySync
}

func newSecretReconciler(subscriber *Subscriber, mgr manager.Manager, subItemKey types.NamespacedName, sSync spySync) reconcile.Reconciler {
	rec := &SecretReconciler{
		Subscriber: subscriber,
		Clt:        mgr.GetClient(),
		Schema:     mgr.GetScheme(),
		Itemkey:    subItemKey,
		sSync:      sSync,
	}

	return rec
}

//Reconcile handle the main logic to deploy the secret coming from the channel namespace
func (s *SecretReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v() request %v, secret fur subitem %v", fnName, request.NamespacedName, s.Itemkey)

		defer klog.Infof("Exiting: %v() request %v, secret for subitem %v", fnName, request.NamespacedName, s.Itemkey)
	}
	curSubItem := s.Subscriber.itemmap[s.Itemkey]
	tw := curSubItem.Subscription.Spec.TimeWindow
	if tw != nil {
		nextRun := utils.NextStartPoint(tw, time.Now())
		if nextRun > time.Duration(0) {
			klog.V(1).Infof("Subcription %v will run after %v", request.NamespacedName.String(), nextRun)
			return reconcile.Result{RequeueAfter: nextRun}, nil
		}
	}

	// we only care the secret changes of the target namespace
	if request.Namespace != curSubItem.Channel.GetNamespace() {
		return reconcile.Result{}, errors.New("failed to reconcile due to untargeted namespace")
	}

	klog.V(1).Info("Reconciling: ", request.NamespacedName, " sercet for subitem ", s.Itemkey)

	// list by label filter
	srts, err := s.getSecretsBySubLabel(request.NamespacedName.Namespace)

	if err != nil {
		klog.Error(err)
		return reconcile.Result{}, err
	}

	var dpls []*dplv1alpha1.Deployable

	for _, srt := range srts.Items {
		//filter out the secret by deployable annotations
		if !isSecretAnnoatedAsDeployable(srt) {
			continue
		}

		packageFilter := curSubItem.Subscription.Spec.PackageFilter
		nSrt := cleanUpSecretObject(srt)
		if utils.CanPassPackageFilter(packageFilter, &nSrt) {
			dpls = append(dpls, packageSecertIntoDeployable(nSrt))
		}
	}

	s.registerToResourceMap(dpls)

	return reconcile.Result{}, nil
}

//GetSecrets get the Secert from all the suscribed channel
func (s *SecretReconciler) getSecretsBySubLabel(srtNs string) (*v1.SecretList, error) {
	pfilter := s.Subscriber.itemmap[s.Itemkey].Subscription.Spec.PackageFilter
	opts := &client.ListOptions{Namespace: srtNs}

	if pfilter != nil && pfilter.LabelSelector != nil {
		clSelector, err := utils.ConvertLabels(pfilter.LabelSelector)
		if err != nil {
			return nil, err
		}

		opts.LabelSelector = clSelector
	}

	srts := &v1.SecretList{}
	if err := s.Clt.List(context.TODO(), srts, opts); err != nil {
		return nil, err
	}

	return srts, nil
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

//registerToResourceMap leverage the synchronizer to handle the sercet lifecycle management
func (s *SecretReconciler) registerToResourceMap(dpls []*dplv1alpha1.Deployable) {
	subscription := s.Subscriber.itemmap[s.Itemkey].Subscription

	hostkey := types.NamespacedName{Name: subscription.Name, Namespace: subscription.Namespace}
	syncsource := secretsyncsource + hostkey.String()
	// subscribed k8s resource

	kubesync := s.sSync
	if s.sSync == nil {
		kubesync = s.Subscriber.synchronizer
	}
	kvalid := kubesync.CreateValiadtor(syncsource)

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

		orggvk := template.GetObjectKind().GroupVersionKind()
		validgvk := kubesync.GetValidatedGVK(orggvk)

		if validgvk == nil {
			gvkerr := errors.New(fmt.Sprintf("resource %v is not supported", orggvk.String()))
			err = utils.SetInClusterPackageStatus(&(subscription.Status), dpl.GetName(), gvkerr, nil)

			if err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}

			pkgMap[dpl.GetName()] = true

			continue
		}

		if kubesync.IsResourceNamespaced(*validgvk) {
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

	kubesync.ApplyValiadtor(kvalid)
}

//PackageSecert put the secret to the deployable template
func packageSecertIntoDeployable(s v1.Secret) *dplv1alpha1.Deployable {
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

//CleanUpObject is used to reset the sercet fields in order to put the secret into deployable template
func cleanUpSecretObject(s v1.Secret) v1.Secret {
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
