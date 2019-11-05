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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

//ListAndDeployReferredSecrets handles the create/update reconciler request
// the idea is, first it will try to get the referred secret from the subscription namespace
// if it can't find it,
////it could be it's a brand new secret request or it's trying to use a differenet one.
//// to address these, we will try to list the sercert within the subscription namespace with the subscription label.
//// if we are seeing these secret, we will delete the label of the reconciled subscription.
///// then we will create a new secret and label it
// if we can find a secret at the subscription namespace, it means there must be some other subscription is
// using it. In this case, we will just add an extra label to it
func (r *ReconcileSubscription) ListAndDeployReferredObject(
	instance *appv1alpha1.Subscription,
	gvk schema.GroupVersionKind,
	refObj runtime.Object,
	objName string) error {

	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	instanceKey := types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}

	// list secret within the sub ns given the secret name
	localObj, err := r.ListReferredObjectByName(instance, refObj, objName)
	if err != nil {
		klog.Errorf("When trying to list the be created object from the subsription namespace having error %v", err)
		return err
	}

	// list the used secret marked with the subscription
	objList, err := r.ListReferredObjectByLabel(instanceKey, gvk)

	if err != nil {
		klog.Errorf("When trying to list all referred object of subscription%v, had error %v", instanceKey.String(), err)
		return err
	}
	// if the listed secret is used by myself, and it's not the newOne, then delete it, otherwise,
	err = r.UpdateLabelsOnOldReferredObject(instance, gvk, objName, objList)

	if err != nil {
		klog.Errorf("Had error %v, while deleting labels for subscription %v", err, instanceKey.String())
		return err
	}

	if localObj == nil {
		//delete old lalbels
		err = r.DeployReferredObject(instance, gvk, refObj)
		if err != nil {
			klog.Errorf("Had error %v, while creating object %v for subscription %v", err, objName, instanceKey.String())
			return err
		}

		return nil
	}

	// we already have the referred secret in the subscription namespace
	mObj, _ := meta.Accessor(localObj)
	if mObj.GetName() != "" {
		lb := mObj.GetLabels()
		lb[instance.GetName()] = "true"
		lb[utils.SercertReferredMarker] = "true"

		updateObj, _ := CleanUpObjectAndSetLabelsWithNamespace(instance.GetNamespace(), gvk, refObj, lb)
		err = r.Client.Update(context.TODO(), updateObj)

		if err != nil {
			klog.Errorf("Had error %v, while updating object %v for subscription %v", err, objName, instanceKey.String())
			return err
		}
	}
	return err
}

//ListReferredObjectByName lists object within the subscription namespace and having label <subscription.name>:"true"
func (r *ReconcileSubscription) ListReferredObjectByName(instance *appv1alpha1.Subscription, refObj runtime.Object, objName string) (runtime.Object, error) {
	objKey := types.NamespacedName{Namespace: instance.GetNamespace(), Name: objName}
	localObj := refObj.DeepCopyObject()
	err := r.Client.Get(context.TODO(), objKey, localObj)

	if errors.IsNotFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return localObj, nil
}

// ListReferredObjectByLabel lists object within the subscription namespace and having label <subscription.name>:"true"
func (r *ReconcileSubscription) ListReferredObjectByLabel(rq types.NamespacedName, gvk schema.GroupVersionKind) (runtime.Object, error) {
	listOptions := &client.ListOptions{Namespace: rq.Namespace}
	ls, err := metav1.LabelSelectorAsSelector(utils.GetLabelSelectorOfSubscription(rq.Name))

	if err != nil {
		klog.Errorf("Can't parse the sercert label selector due to %v", err)
	}

	listOptions.LabelSelector = ls

	switch k := gvk.Kind; k {
	case SecretKindStr:
		localObjs := &corev1.SecretList{}
		err = r.Client.List(context.TODO(), listOptions, localObjs)

		if err != nil {
			fmt.Println(err)
			return nil, err
		}

		return localObjs, nil
	case ConfigMapKindStr:
		localObjs := &corev1.ConfigMapList{}
		err = r.Client.List(context.TODO(), listOptions, localObjs)

		if err != nil {
			fmt.Println(err)
			return nil, err
		}

		return localObjs, nil
	default:

		return nil, nil
	}

}

////UpdateLabelsOnOldRefSecret check up if the secret was owned by subscriptions
////Checke up all the secret labeled with the reconciled subscription name,
////if the secret only labeled with one subscription,
////// then we will delete it as long as the secret is not having the same name as the serRef
/////// skipping the same named secret is due to the fact that later on we will update the label for this case
////if the secret labeled with more than one subscription,
////// then we will only update the label removing the reconciled subscription
func (r *ReconcileSubscription) UpdateLabelsOnOldReferredObject(instance *appv1alpha1.Subscription, gvk schema.GroupVersionKind, newObjName string, objList runtime.Object) error {

	mObjList, err := meta.ExtractList(objList)
	if err != nil {
		return err
	}

	if len(mObjList) == 0 {
		return nil
	}

	//having a referenced secret, label with the subscription name

	for _, obj := range mObjList {
		mObj, err := meta.Accessor(obj)

		if err != nil {
			return err
		}

		if mObj.GetName() != newObjName {
			lb := mObj.GetLabels()
			if len(lb) > 2 {
				delete(lb, instance.GetName())

				updatedObj, err := CleanUpObjectAndSetLabelsWithNamespace(instance.GetNamespace(), gvk, obj, lb)

				if err != nil {
					return err
				}

				err = r.Client.Update(context.TODO(), updatedObj)

				if err != nil {
					return err
				}
			} else {
				err = r.Client.Delete(context.TODO(), obj)

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
func (r *ReconcileSubscription) DeployReferredObject(instance *appv1alpha1.Subscription, objGvk schema.GroupVersionKind, newObj runtime.Object) error {

	srtLabel := map[string]string{utils.SercertReferredMarker: "true", instance.GetName(): "true"}

	ns := instance.GetNamespace()

	cleanSrt, err := CleanUpObjectAndSetLabelsWithNamespace(ns, objGvk, newObj, srtLabel)
	if err != nil {
		return err
	}
	err = r.Client.Create(context.TODO(), cleanSrt)

	if err != nil {
		return err
	}

	return nil
}

//
////ListSubscriptionOwnedSrtAndDelete check up if secrets are owned by the subscription or not,
////if the reconciled subscription is the only label subscription for the secret, then we will delete the referred sercert
////otherwise we will remove the subscription label from the secret labels
func (r *ReconcileSubscription) ListSubscriptionOwnedObjectsAndDelete(rq types.NamespacedName, gvk schema.GroupVersionKind) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	objList, err := r.ListReferredObjectByLabel(rq, gvk)
	if err != nil {
		return err
	}

	mObjList, err := meta.ExtractList(objList)
	if err != nil {
		return err
	}

	if len(mObjList) == 0 {
		return nil
	}

	//having a referenced secret, label with the subscription name
	obj := mObjList[0]

	mObj, _ := meta.Accessor(obj)

	if len(mObj.GetLabels()) == 2 {
		err = r.Client.Delete(context.TODO(), obj)
	} else {
		lb := mObj.GetLabels()
		delete(lb, rq.Name)

		updatedObj, err := CleanUpObjectAndSetLabelsWithNamespace(rq.Namespace, gvk, obj, lb)
		if err != nil {
			return err
		}
		err = r.Client.Update(context.TODO(), updatedObj)
	}

	return err
}

//CleanUpConfigMapObject is used to reset the sercet fields in order to put the secret into deployable template
func CleanUpObjectAndSetLabelsWithNamespace(ns string, objGvk schema.GroupVersionKind, obj runtime.Object, objLabel map[string]string) (runtime.Object, error) {

	// srtGvk := schema.GroupVersionKind{
	// 	Kind:    "Secret",
	// 	Version: "v1",
	// }

	mObj, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}

	t := types.UID("")

	switch k := objGvk.Kind; k {
	case SecretKindStr:
		srt := corev1.Secret{}
		srt.SetName(mObj.GetName())
		srt.SetResourceVersion("")
		srt.SetUID(t)
		srt.SetSelfLink("")
		srt.SetGroupVersionKind(objGvk)
		srt.SetLabels(objLabel)
		srt.SetNamespace(ns)

		return &srt, nil
	case ConfigMapKindStr:

		cfg := corev1.ConfigMap{}
		cfg.SetName(mObj.GetName())
		cfg.SetResourceVersion("")
		cfg.SetUID(t)
		cfg.SetSelfLink("")
		cfg.SetGroupVersionKind(objGvk)
		cfg.SetLabels(objLabel)
		cfg.SetNamespace(ns)

		return &cfg, nil
	default:
		return nil, nil
	}
}
