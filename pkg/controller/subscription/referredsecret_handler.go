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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

func (r *ReconcileSubscription) ListAndDeployReferredSecrets(refSrt *corev1.Secret, instance *appv1alpha1.Subscription) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	// list secret within the sub ns given the secert name
	localSrt, err := r.ListReferredSecretByName(instance, refSrt)

	// list the used secert marked with the subscription
	instanceKey := types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}
	oldSrts, err := r.ListReferredSecret(instanceKey)
	// if the listed secert is used by myself, and it's not the newOne, then delete it, otherwise,
	err = r.UpdateLabelsOnOldRefSecret(instance, refSrt, oldSrts)

	if localSrt == nil {
		//delete old lalbels
		err = r.DeployReferredSecret(instance, refSrt)
		return err
	}

	// we already have the referred secert in the subscription namespace
	lb := localSrt.GetLabels()
	lb[instance.GetName()] = "true"
	lb[SercertReferredMarker] = "true"
	localSrt.SetLabels(lb)
	err = r.Client.Update(context.TODO(), localSrt)

	return err
}

//ListReferredSecret lists secert within the subscription namespace and having label <subscription.name>:"true"
func (r *ReconcileSubscription) ListReferredSecretByName(instance *appv1alpha1.Subscription, refSrt *corev1.Secret) (*corev1.Secret, error) {
	srtKey := types.NamespacedName{Namespace: instance.GetNamespace(), Name: refSrt.GetName()}
	localSrt := &corev1.Secret{}
	err := r.Client.Get(context.TODO(), srtKey, localSrt)

	if err != nil {
		return nil, err
	}

	return localSrt, nil
}

func getLabelOfSubscription(subName string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{SercertReferredMarker: "true", subName: "true"}}
}

//ListReferredSecret lists secert within the subscription namespace and having label <subscription.name>:"true"
func (r *ReconcileSubscription) ListReferredSecret(rq types.NamespacedName) (*corev1.SecretList, error) {
	listOptions := &client.ListOptions{Namespace: rq.Namespace}
	ls, err := metav1.LabelSelectorAsSelector(getLabelOfSubscription(rq.Name))
	if err != nil {
		klog.Errorf("Can't parse the sercert label selector due to %v", err)
	}
	listOptions.LabelSelector = ls
	localSrts := &corev1.SecretList{}
	err = r.Client.List(context.TODO(), listOptions, localSrts)

	if err != nil {
		return nil, err
	}

	return localSrts, nil
}

//ListSubscriptionOwnedSrtAndDeploy check up if the secert is owned by the subscription or not, if not deploy one, otherwise modify the owner relationship for the secret
func (r *ReconcileSubscription) UpdateLabelsOnOldRefSecret(instance *appv1alpha1.Subscription, newSrt *v1.Secret, srtList *v1.SecretList) error {
	if len(srtList.Items) == 0 {
		return nil
	}
	//having a referenced secert, label with the subscription name

	var err error

	for _, srt := range srtList.Items {
		if srt.GetName() != newSrt.GetName() {
			lb := srt.GetLabels()
			if len(lb) > 2 {
				delete(lb, instance.GetName())
				srt.SetLabels(lb)
				err = r.Client.Update(context.TODO(), &srt)
			} else {
				err = r.Client.Delete(context.TODO(), &srt)
			}
		}
	}

	return err
}

//DeployReferredSecret deply the referred secert to the subscription namespace, also it set up the lable and ownerReference for the secert
func (r *ReconcileSubscription) DeployReferredSecret(instance *appv1alpha1.Subscription, newSrt *v1.Secret) error {
	cleanSrt := utils.CleanUpObject(*newSrt)

	srtLabel := map[string]string{SercertReferredMarker: "true", instance.GetName(): "true"}
	cleanSrt.SetLabels(srtLabel)

	cleanSrt.SetNamespace(instance.GetNamespace())

	err := r.Client.Create(context.TODO(), &cleanSrt)

	if err != nil {
		errmsg := fmt.Sprintf("Failed to create secert %v, got error: %v", cleanSrt.GetName(), err.Error())
		klog.Error(errmsg)

		return errors.New(errmsg)
	}

	return nil
}

//ListSubscriptionOwnedSrtAndDelete check up if the secert is owned by the subscription or not, if not deploy one, otherwise modify the owner relationship for the secret
func (r *ReconcileSubscription) ListSubscriptionOwnedSrtAndDelete(rq types.NamespacedName) error {
	srtList, err := r.ListReferredSecret(rq)
	if len(srtList.Items) == 0 {
		return nil
	}

	//having a referenced secert, label with the subscription name
	srt := srtList.Items[0]

	if len(srt.GetLabels()) == 2 {
		err = r.Client.Delete(context.TODO(), &srt)
	} else {
		ls := srt.GetLabels()
		delete(ls, rq.Name)
		srt.SetLabels(ls)
		err = r.Client.Update(context.TODO(), &srt)
	}
	return err
}
