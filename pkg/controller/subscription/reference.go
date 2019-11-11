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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

var SecretKindStr = "Secret"
var ConfigMapKindStr = "ConfigMap"
var SubscriptionGVK = schema.GroupVersionKind{Group: "app.ibm.com", Kind: "Subscription", Version: "v1alpha1"}

//SercertReferredMarker is used as a label key to filter out the secert coming from reference
var SercertReferredMarker = "IsReferredBySub-"

type referredObject interface {
	runtime.Object
	metav1.Object
}

//ListAndDeployReferredObject handles the create/update reconciler request
// the idea is, first it will try to get the referred secret from the subscription namespace
// if it can't find it,
////it could be it's a brand new secret request or it's trying to use a differenet one.
//// to address these, we will try to list the sercert within the subscription namespace with the subscription label.
//// if we are seeing these secret, we will delete the label of the reconciled subscription.
///// then we will create a new secret and label it
// if we can find a secret at the subscription namespace, it means there must be some other subscription is
// using it. In this case, we will just add an extra label to it

func (r *ReconcileSubscription) ListAndDeployReferredObject(instance *appv1alpha1.Subscription, gvk schema.GroupVersionKind, refObj referredObject) error {
	insName := instance.GetName()
	insNs := instance.GetNamespace()
	uObjList := &unstructured.UnstructuredList{}

	uObjList.SetGroupVersionKind(gvk)

	opts := &client.ListOptions{Namespace: insNs}
	err := r.Client.List(context.TODO(), uObjList, opts)

	if err != nil && !errors.IsNotFound(err) {
		klog.Errorf("Failed to list referred objects with error %v ", err)
		return err
	}

	found := false
	referLabel := SercertReferredMarker + insName

	for _, obj := range uObjList.Items {
		u := obj.DeepCopy()
		lb := u.GetLabels()

		if len(lb) == 0 {
			lb = make(map[string]string)
		}

		if u.GetName() == refObj.GetName() {
			found = true

			lb[referLabel] = "true"
			u.SetLabels(lb)
			newOwers := addObjectOwnedBySub(u, instance)
			u.SetOwnerReferences(newOwers)

			err := r.Client.Update(context.TODO(), u)
			if err != nil {
				return err
			}

			continue
		}

		if lb[referLabel] == "true" {
			delete(lb, referLabel)

			owners := u.GetOwnerReferences()
			if len(owners) > 1 {
				u.SetLabels(lb)
				newOwers := deleteSubFromObjectOwnersByName(u, insName)
				u.SetOwnerReferences(newOwers)

				err := r.Client.Update(context.TODO(), u)
				if err != nil {
					return err
				}
			} else {
				err := r.Client.Delete(context.TODO(), u)
				if err != nil {
					return err
				}
			}
		}
	}

	if !found {
		lb := refObj.GetLabels()

		if len(lb) == 0 {
			lb = make(map[string]string)
		}

		t := types.UID("")

		lb[referLabel] = "true"
		refObj.SetLabels(lb)
		refObj.SetNamespace(insNs)
		refObj.SetResourceVersion("")
		refObj.SetUID(t)

		newOwers := addObjectOwnedBySub(refObj, instance)

		refObj.SetOwnerReferences(newOwers)
		err := r.Client.Create(context.TODO(), refObj)

		if err != nil {
			klog.Errorf("Got error %v, while creating referred object %v for subscription %v", err, refObj.GetName(), insName)
		}
	}

	return nil
}

func (r *ReconcileSubscription) DeleteReferredObjects(rq types.NamespacedName, gvk schema.GroupVersionKind) error {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{SercertReferredMarker + rq.Name: "true"}}
	ls, _ := metav1.LabelSelectorAsSelector(selector)
	opts := &client.ListOptions{
		Namespace:     rq.Namespace,
		LabelSelector: ls,
	}
	uObjList := &unstructured.UnstructuredList{}

	uObjList.SetGroupVersionKind(gvk)

	err := r.Client.List(context.TODO(), uObjList, opts)

	if err != nil {
		return err
	}

	if len(uObjList.Items) == 0 {
		return nil
	}

	referLabel := SercertReferredMarker + rq.Name

	for _, obj := range uObjList.Items {
		u := obj.DeepCopy()
		lb := u.GetLabels()

		delete(lb, referLabel)

		owners := obj.GetOwnerReferences()
		if len(owners) == 1 { // leave to the k8s handle it
			continue
		} else {
			u.SetLabels(lb)
			newOwers := deleteSubFromObjectOwnersByName(u, rq.Name)

			u.SetOwnerReferences(newOwers)
			err := r.Client.Update(context.TODO(), u)
			if err != nil {
				return nil
			}
		}
	}

	return nil
}

func isObjectOwnedBySub(obj referredObject, subname string) bool {
	owers := obj.GetOwnerReferences()

	if len(owers) == 0 {
		return false
	}

	for _, ower := range owers {
		if ower.Name == subname {
			return true
		}
	}

	return false
}

func addObjectOwnedBySub(obj referredObject, sub *appv1alpha1.Subscription) []metav1.OwnerReference {
	if isObjectOwnedBySub(obj, sub.GetName()) {
		return obj.GetOwnerReferences()
	}

	owers := obj.GetOwnerReferences()
	newOwer := metav1.OwnerReference{
		APIVersion: SubscriptionGVK.Version,
		Name:       sub.GetName(),
		Kind:       SubscriptionGVK.Kind,
		UID:        sub.GetUID(),
	}
	owers = append(owers, newOwer)

	return owers
}

func deleteSubFromObjectOwnersByName(obj referredObject, subname string) []metav1.OwnerReference {
	if !isObjectOwnedBySub(obj, subname) {
		return obj.GetOwnerReferences()
	}

	owners := obj.GetOwnerReferences()

	if len(owners) == 0 {
		return owners
	}

	newOwners := make([]metav1.OwnerReference, 0)

	for _, owner := range owners {
		if owner.Name != subname {
			newOwners = append(newOwners, owner)
		}
	}

	return newOwners
}
