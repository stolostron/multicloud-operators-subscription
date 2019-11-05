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

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

var SecretKindStr = "Secret"
var ConfigMapKindStr = "ConfigMap"

type referredObject interface {
	GetObjectKind() schema.ObjectKind
	DeepCopyObject() runtime.Object

	GetName() string
	SetName(name string)
	SetNamespace(namespace string)
	SetUID(uid types.UID)
	SetResourceVersion(version string)
	GetLabels() map[string]string
	SetLabels(labels map[string]string)
	// GetAnnotations() map[string]string
	SetAnnotations(annotations map[string]string)
}

//ListAndDeployReferredSecrets handles the create/update reconciler request
// the idea is, first it will try to get the referred secret from the subscription namespace
// if it can't find it,
////it could be it's a brand new secret request or it's trying to use a differenet one.
//// to address these, we will try to list the sercert within the subscription namespace with the subscription label.
//// if we are seeing these secret, we will delete the label of the reconciled subscription.
///// then we will create a new secret and label it
// if we can find a secret at the subscription namespace, it means there must be some other subscription is
// using it. In this case, we will just add an extra label to it

// refObj *unstructured.Unstructured,

func (r *ReconcileSubscription) ListAndDeployReferredObject(instance *appv1alpha1.Subscription, refObj referredObject) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	instanceKey := types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}

	// list secret within the sub ns given the secret name
	localObj, err := r.ListReferredObjectByName(instance, refObj)

	if err != nil {
		klog.Errorf("When trying to list the be created object from the subsription namespace having error %v", err)
		return err
	}

	// list the used secret marked with the subscription
	objList, err := r.ListReferredObjectByLabel(instanceKey)

	if err != nil {
		klog.Errorf("When trying to list all referred object of subscription%v, had error %v", instanceKey.String(), err)
		return err
	}
	// if the listed secret is used by myself, and it's not the newOne, then delete it, otherwise,
	err = r.UpdateLabelsOnOldReferredObject(instance, refObj, objList)

	if err != nil {
		klog.Errorf("Had error %v, while deleting labels for subscription %v", err, instanceKey.String())
		return err
	}

	if localObj == nil {
		//delete old lalbels
		err = r.DeployReferredObject(instance, refObj)
		if err != nil {
			klog.Errorf("Had error %v, while creating object %v for subscription %v", err, refObj.GetName(), instanceKey.String())
			return err
		}

		return nil
	}

	// we already have the referred secret in the subscription namespace

	if refObj.GetName() != "" {
		lb := refObj.GetLabels()
		lb[instance.GetName()] = "true"
		lb[utils.SercertReferredMarker] = "true"

		updateObj := CleanUpReferredObject(refObj)
		updateObj.SetLabels(lb)
		err = r.Client.Update(context.TODO(), updateObj)

		if err != nil {
			klog.Errorf("Had error %v, while updating object %v for subscription %v", err, refObj.GetName(), instanceKey.String())
			return err
		}
	}

	return err
}

//list all objects, then process if the selected object is the target kind
// if it's a right kind, then we will see if there's a new target name, if so, add label
// if not, create object

//when doing delete, we can narrow down the select via a label selector

//ListReferredObjectByName lists object within the subscription namespace and having label <subscription.name>:"true"
func (r *ReconcileSubscription) ListReferredObjectByName(instance *appv1alpha1.Subscription, refObj referredObject) (referredObject, error) {
	objKey := types.NamespacedName{Namespace: instance.GetNamespace(), Name: refObj.GetName()}

	tObj := refObj

	err := r.Client.Get(context.TODO(), objKey, tObj)

	if k8serrors.IsNotFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return tObj, nil
}

// ListReferredObjectByLabel lists object within the subscription namespace and having label <subscription.name>:"true"
func (r *ReconcileSubscription) ListReferredObjectByLabel(rq types.NamespacedName) (*unstructured.UnstructuredList, error) {
	listOptions := &client.ListOptions{Namespace: rq.Namespace}
	ls, err := metav1.LabelSelectorAsSelector(utils.GetLabelSelectorOfSubscription(rq.Name))

	if err != nil {
		klog.Errorf("Can't parse the sercert label selector due to %v", err)
	}

	listOptions.LabelSelector = ls

	objlist := &unstructured.UnstructuredList{}

	err = r.Client.List(context.TODO(), listOptions, objlist)

	if err != nil {
		return nil, err
	}

	return objlist, nil
}

////UpdateLabelsOnOldRefSecret check up if the secret was owned by subscriptions
////Checke up all the secret labeled with the reconciled subscription name,
////if the secret only labeled with one subscription,
////// then we will delete it as long as the secret is not having the same name as the serRef
/////// skipping the same named secret is due to the fact that later on we will update the label for this case
////if the secret labeled with more than one subscription,
////// then we will only update the label removing the reconciled subscription
func (r *ReconcileSubscription) UpdateLabelsOnOldReferredObject(
	instance *appv1alpha1.Subscription,
	refObj referredObject,
	objList *unstructured.UnstructuredList) error {

	if len(objList.Items) == 0 {
		return nil
	}

	//having a referenced secret, label with the subscription name

	for _, obj := range objList.Items {
		tmp := obj.DeepCopy()
		if refObj.GetName() != tmp.GetName() {
			lb := tmp.GetLabels()
			if len(lb) > 2 {
				delete(lb, instance.GetName())
				tmp.SetLabels(lb)
				err := r.Client.Update(context.TODO(), tmp)

				if err != nil {
					return err
				}
			} else {
				err := r.Client.Delete(context.TODO(), tmp)

				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

//
////DeployReferredSecret deply the referred secret to the subscription namespace, also it set up the label and ownerReference for the secret
func (r *ReconcileSubscription) DeployReferredObject(instance *appv1alpha1.Subscription, newObj referredObject) error {
	objLabel := map[string]string{utils.SercertReferredMarker: "true", instance.GetName(): "true"}

	ns := instance.GetNamespace()

	cleanObj := CleanUpReferredObject(newObj)

	cleanObj.SetLabels(objLabel)
	cleanObj.SetNamespace(ns)

	err := r.Client.Create(context.TODO(), cleanObj)

	if err != nil {
		return err
	}

	return nil
}

//
////ListSubscriptionOwnedSrtAndDelete check up if secrets are owned by the subscription or not,
////if the reconciled subscription is the only label subscription for the secret, then we will delete the referred sercert
////otherwise we will remove the subscription label from the secret labels
func (r *ReconcileSubscription) ListSubscriptionOwnedObjectsAndDelete(rq types.NamespacedName) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	objList, err := r.ListReferredObjectByLabel(rq)
	if err != nil {
		return err
	}

	obj := objList.Items[0]

	if len(obj.GetLabels()) == 2 {
		err = r.Client.Delete(context.TODO(), &obj)
		if err != nil {
			return err
		}
	} else {
		lb := obj.GetLabels()
		delete(lb, rq.Name)

		obj.SetLabels(lb)
		if err != nil {
			return err
		}
		err = r.Client.Update(context.TODO(), &obj)
		if err != nil {
			return err
		}
	}

	return nil
}

//CleanUpConfigMapObject is used to reset the sercet fields in order to put the secret into deployable template
func CleanUpReferredObject(obj referredObject) referredObject {

	t := types.UID("")

	obj.SetUID(t)
	obj.SetResourceVersion("")

	return obj
}
