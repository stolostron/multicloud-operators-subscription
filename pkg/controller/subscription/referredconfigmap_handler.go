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

package subscription

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

//SercertReferredMarker is used as a label key to filter out the secert coming from reference
var SercertReferredMarker = "IsReferredBySub"

func (r *ReconcileSubscription) ListAndDeployReferredConfigMap(refCfg *corev1.ConfigMap, instance *appv1alpha1.Subscription) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	// list secret within the sub ns given the secert name
	localCfg, err := r.ListReferredConfigMapByName(instance, refCfg)

	// list the used secert marked with the subscription
	instanceKey := types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}
	oldCfgs, err := r.ListReferredConfigMap(instanceKey)
	// if the listed secert is used by myself, and it's not the newOne, then delete it, otherwise,
	err = r.UpdateLabelsOnOldRefConfigMap(instance, refCfg, oldCfgs)

	if localCfg == nil {
		//delete old lalbels
		err = r.DeployReferredConfigMap(instance, refCfg)
		return err
	}

	// we already have the referred secert in the subscription namespace
	lb := localCfg.GetLabels()
	lb[instance.GetName()] = "true"
	lb[SercertReferredMarker] = "true"
	localCfg.SetLabels(lb)
	err = r.Client.Update(context.TODO(), localCfg)

	return err
}

//ListReferredSecret lists secert within the subscription namespace and having label <subscription.name>:"true"
func (r *ReconcileSubscription) ListReferredConfigMapByName(instance *appv1alpha1.Subscription, ref *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	refKey := types.NamespacedName{Namespace: instance.GetNamespace(), Name: ref.GetName()}
	localRef := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), refKey, localRef)

	if err != nil {
		return nil, err
	}

	return localRef, nil
}

func getLabelOfSubscription(subName string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{SercertReferredMarker: "true", subName: "true"}}
}

//ListReferredSecret lists secert within the subscription namespace and having label <subscription.name>:"true"
func (r *ReconcileSubscription) ListReferredConfigMap(rq types.NamespacedName) (*corev1.ConfigMapList, error) {
	listOptions := &client.ListOptions{Namespace: rq.Namespace}
	ls, err := metav1.LabelSelectorAsSelector(getLabelOfSubscription(rq.Name))
	if err != nil {
		klog.Errorf("Can't parse the sercert label selector due to %v", err)
	}
	listOptions.LabelSelector = ls
	localCfgs := &corev1.ConfigMapList{}
	err = r.Client.List(context.TODO(), listOptions, localCfgs)

	if err != nil {
		return nil, err
	}

	return localCfgs, nil
}

//ListSubscriptionOwnedSrtAndDeploy check up if the secert is owned by the subscription or not, if not deploy one, otherwise modify the owner relationship for the secret
func (r *ReconcileSubscription) UpdateLabelsOnOldRefConfigMap(instance *appv1alpha1.Subscription, newCfg *corev1.ConfigMap, cfgLists *corev1.ConfigMapList) error {
	if len(cfgLists.Items) == 0 {
		return nil
	}
	//having a referenced secert, label with the subscription name

	var err error

	for _, cfg := range cfgLists.Items {
		if cfg.GetName() != newCfg.GetName() {
			lb := cfg.GetLabels()
			if len(lb) > 2 {
				delete(lb, instance.GetName())
				cfg.SetLabels(lb)
				err = r.Client.Update(context.TODO(), &cfg)
			} else {
				err = r.Client.Delete(context.TODO(), &cfg)
			}
		}
	}

	return err
}

//DeployReferredSecret deply the referred secert to the subscription namespace, also it set up the lable and ownerReference for the secert
func (r *ReconcileSubscription) DeployReferredConfigMap(instance *appv1alpha1.Subscription, newConfig *corev1.ConfigMap) error {
	cleanCfg := CleanUpConfigMapObject(*newConfig)

	srtLabel := map[string]string{SercertReferredMarker: "true", instance.GetName(): "true"}
	cleanCfg.SetLabels(srtLabel)

	cleanCfg.SetNamespace(instance.GetNamespace())

	err := r.Client.Create(context.TODO(), &cleanCfg)

	if err != nil {
		errmsg := fmt.Sprintf("Failed to create secert %v, got error: %v", cleanCfg.GetName(), err.Error())
		klog.Error(errmsg)

		return errors.New(errmsg)
	}

	return nil
}

//ListSubscriptionOwnedSrtAndDelete check up if the secert is owned by the subscription or not, if not deploy one, otherwise modify the owner relationship for the secret
func (r *ReconcileSubscription) ListSubscriptionOwnedCfgAndDelete(rq types.NamespacedName) error {
	cfgLists, err := r.ListReferredSecret(rq)
	if len(cfgLists.Items) == 0 {
		return nil
	}

	//having a referenced secert, label with the subscription name
	cfg := cfgLists.Items[0]

	if len(cfg.GetLabels()) == 2 {
		err = r.Client.Delete(context.TODO(), &cfg)
	} else {
		ls := cfg.GetLabels()
		delete(ls, rq.Name)
		cfg.SetLabels(ls)
		err = r.Client.Update(context.TODO(), &cfg)
	}
	return err
}



//CleanUpObject is used to reset the sercet fields in order to put the secret into deployable template
func CleanUpConfigMapObject(c corev1.ConfigMap) corev1.ConfigMap {
	c.SetResourceVersion("")

	t := types.UID("")

	c.SetUID(t)

	c.SetSelfLink("")

	gvk := schema.GroupVersionKind{
		Kind:    "ConfigMap",
		Version: "v1",
	}
	c.SetGroupVersionKind(gvk)

	return c
}