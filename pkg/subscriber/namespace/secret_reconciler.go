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

	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"

	"github.com/pkg/errors"

	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"

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

//SecretReconciler defined a info collection for query secret resource
type SecretReconciler struct {
	*NsSubscriber
	Clt     client.Client
	Schema  *runtime.Scheme
	Itemkey types.NamespacedName
}

func newSecretReconciler(subscriber *NsSubscriber, mgr manager.Manager, subItemKey types.NamespacedName) *SecretReconciler {
	rec := &SecretReconciler{
		NsSubscriber: subscriber,
		Clt:          mgr.GetClient(),
		Schema:       mgr.GetScheme(),
		Itemkey:      subItemKey,
	}

	return rec
}

//Reconcile handle the main logic to deploy the secret coming from the channel namespace
func (s *SecretReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	if klog.V(1) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v() request %v, secret for subitem %v", fnName, request.NamespacedName, s.Itemkey)

		defer klog.Infof("Exiting: %v() request %v, secret for subitem %v", fnName, request.NamespacedName, s.Itemkey)
	}

	curSubItem := s.NsSubscriber.itemmap[s.Itemkey]

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
		nSrt.SetNamespace(curSubItem.Subscription.GetNamespace())

		if utils.CanPassPackageFilter(packageFilter, &nSrt) {
			dpls = append(dpls, packageSecertIntoDeployable(nSrt))
		}
	}

	//handle secret lifecycle by registering packaged secret to synchronizer
	kubesync := s.NsSubscriber.synchronizer

	return reconcile.Result{}, registerToResourceMap(s.Schema, s.NsSubscriber.itemmap[s.Itemkey].Subscription, kubesync, dpls)
}

//GetSecrets get the Secert from all the suscribed channel
func (s *SecretReconciler) getSecretsBySubLabel(srtNs string) (*v1.SecretList, error) {
	pfilter := s.NsSubscriber.itemmap[s.Itemkey].Subscription.Spec.PackageFilter
	opts := &client.ListOptions{Namespace: srtNs}

	if pfilter != nil && pfilter.LabelSelector != nil {
		clSelector, err := utils.ConvertLabels(pfilter.LabelSelector)
		if err != nil {
			return nil, err
		}

		opts.LabelSelector = clSelector
	}

	hubclient, err := client.New(s.config, client.Options{})
	if err != nil {
		return nil, err
	}

	srts := &v1.SecretList{}
	if err := hubclient.List(context.TODO(), srts, opts); err != nil {
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
func registerToResourceMap(sch *runtime.Scheme, pSubscription *appv1alpha1.Subscription,
	kubesync SyncSource, pDpls []*dplv1alpha1.Deployable) error {
	hostkey := types.NamespacedName{Name: pSubscription.Name, Namespace: pSubscription.Namespace}
	syncsource := secretsyncsource + hostkey.String()

	// create a validator when
	dplOrder := []kubesynchronizer.DplUnit{}

	for _, dpl := range pDpls {
		template := &unstructured.Unstructured{}

		if dpl.Spec.Template == nil {
			klog.Warning("Processing local deployable without template:", dpl)
			continue
		}

		if err := json.Unmarshal(dpl.Spec.Template.Raw, template); err != nil {
			klog.Warning("Processing local deployable with error template:", dpl, err)
			continue
		}

		template, err := utils.OverrideResourceBySubscription(template, dpl.GetName(), pSubscription)
		if err != nil {
			if err := utils.SetInClusterPackageStatus(&(pSubscription.Status), dpl.GetName(), err, nil); err != nil {
				klog.Info("error in overriding for package: ", err)
			}

			continue
		}

		if err := controllerutil.SetControllerReference(pSubscription, template, sch); err != nil {
			klog.Warning("Adding owner reference to template, got error:", err)
			continue
		}

		orggvk := template.GetObjectKind().GroupVersionKind()
		validgvk := kubesync.GetValidatedGVK(orggvk)

		if validgvk == nil {
			gvkerr := errors.New(fmt.Sprintf("resource %v is not supported", orggvk.String()))

			if err := utils.SetInClusterPackageStatus(&(pSubscription.Status), dpl.GetName(), gvkerr, nil); err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}

			continue
		}

		if kubesync.IsResourceNamespaced(*validgvk) {
			template.SetNamespace(pSubscription.Namespace)
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

		dplkey := types.NamespacedName{
			Name:      dpltosync.Name,
			Namespace: dpltosync.Namespace,
		}

		dUnit := kubesynchronizer.DplUnit{
			Dpl: dpltosync,
			Gvk: *validgvk,
		}

		dplOrder = append(dplOrder, dUnit)

		klog.V(10).Info("Finished Register ", *validgvk, hostkey, dplkey, " with err:", err)
	}

	return kubesync.AddTemplates(syncsource, hostkey, dplOrder)
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
