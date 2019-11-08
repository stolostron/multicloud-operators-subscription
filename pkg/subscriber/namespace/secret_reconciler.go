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
	"time"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//SecretRecondiler defined a info collection for query secret resource
type SecretReconciler struct {
	Subscriber *Subscriber
	Clt        client.Client
	Schema     *runtime.Scheme
	Itemkey    types.NamespacedName
}

func newSecertReconciler(subscriber *Subscriber, mgr manager.Manager, subItemKey types.NamespacedName) reconcile.Reconciler {
	rec := &SecretReconciler{
		Subscriber: subscriber,
		Clt:        mgr.GetClient(),
		Schema:     mgr.GetScheme(),
		Itemkey:    subItemKey,
	}

	return rec
}

//Reconcile handle the main logic to deploy the secret coming from the channel namespace
func (s *SecretReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()\n request %v, secret fur subitem %v", fnName, request.NamespacedName, s.Itemkey)

		defer klog.Infof("Exiting: %v()\n request %v, secret for subitem %v", fnName, request.NamespacedName, s.Itemkey)
	}

	tw := s.Subscriber.itemmap[s.Itemkey].Subscription.Spec.TimeWindow
	if tw != nil {
		nextRun := utils.NextStartPoint(tw, time.Now())
		if nextRun > time.Duration(0) {
			klog.V(1).Infof("Subcription %v will run after %v", request.NamespacedName.String(), nextRun)
			return reconcile.Result{RequeueAfter: nextRun}, nil
		}
	}

	klog.V(1).Info("Reconciling: ", request.NamespacedName, " sercet for subitem ", s.Itemkey)

	srt, err := s.GetSecret(request.NamespacedName)

	var dpls []*dplv1alpha1.Deployable

	// handle the NotFound case and other can't list case
	if err != nil {
		// update the synchronizer to make sure the sercet is also delete from synchronizer
		s.RegisterToResourceMap(dpls)
		return reconcile.Result{}, err
	}

	// only move forward if the sercet belongs to the subscription's channel
	if srt.GetNamespace() != s.Subscriber.itemmap[s.Itemkey].Channel.GetNamespace() {
		return reconcile.Result{}, err
	}

	//filter out the secret by deployable annotations
	if !isSecretAnnoatedAsDeployable(*srt) {
		return reconcile.Result{}, err
	}

	klog.Infof("reconciler %v, got secret %v to process,  with error %v", request.NamespacedName, srt, err)

	srtNew, ok := utils.ApplyFilters(*srt, s.Subscriber.itemmap[s.Itemkey].Subscription)
	if ok {
		dpls = append(dpls, utils.PackageSecert(srtNew))
	}

	s.RegisterToResourceMap(dpls)

	return reconcile.Result{}, nil
}

//GetSecret get the Secert from all the suscribed channel
func (s *SecretReconciler) GetSecret(srtKey types.NamespacedName) (*v1.Secret, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	srt := &v1.Secret{}
	err := s.Clt.Get(context.TODO(), srtKey, srt)

	if err != nil {
		return nil, err
	}

	return srt, nil
}

func isSecretAnnoatedAsDeployable(srt v1.Secret) bool {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	secretsAnno := srt.GetAnnotations()

	if secretsAnno == nil {
		return false
	}

	if _, ok := secretsAnno[appv1alpha1.AnnotationDeployables]; !ok {
		return false
	}

	return true
}

//RegisterToResourceMap leverage the synchronizer to handle the sercet lifecycle management
func (s *SecretReconciler) RegisterToResourceMap(dpls []*dplv1alpha1.Deployable) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	subscription := s.Subscriber.itemmap[s.Itemkey].Subscription

	hostkey := types.NamespacedName{Name: subscription.Name, Namespace: subscription.Namespace}
	syncsource := secretsyncsource + hostkey.String()
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
