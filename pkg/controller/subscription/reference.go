// Copyright 2021 The Kubernetes Authors.
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
	"k8s.io/apimachinery/pkg/runtime/schema"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	subutil "open-cluster-management.io/multicloud-operators-subscription/pkg/utils"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
)

var SecretKindStr = "Secret"
var ConfigMapKindStr = "ConfigMap"
var SubscriptionGVK = schema.GroupVersionKind{
	Group:   appv1.SchemeGroupVersion.Group,
	Kind:    "Subscription",
	Version: appv1.SchemeGroupVersion.Version}

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

func (r *ReconcileSubscription) ListAndDeployReferredObject(instance *appv1.Subscription, gvk schema.GroupVersionKind, refObj referredObject) error {
	return subutil.ListAndDeployReferredObject(r.Client, instance, gvk, refObj)
}

func (r *ReconcileSubscription) DeleteReferredObjects(rq types.NamespacedName, gvk schema.GroupVersionKind) error {
	return subutil.DeleteReferredObjects(r.Client, rq, gvk)
}
