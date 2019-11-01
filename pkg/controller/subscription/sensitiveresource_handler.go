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
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

//list all the referred secert, old ones
// if reconciler is giving a new secret reference,
//// then if the ownerReference is only to current subscription, then delete the old one and deploy the new one
//// if there're more than one onwer, then delete the current subscription from the ownerReference, and deploy new secert
// if reconciler is having the old secert then update the secret

//ListAndRegistSecrets is used to list and manage the referred secert for a subscription
func (r *ReconcileSubscription) ListAndDeployReferredSecrets(refSrt *corev1.Secret, instance *appv1alpha1.Subscription) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	// list the seceret from the subscription namespace with label

	// if we can't find any, then deploy with label
	localSrts, err := r.ListReferredSecret(instance)
	if err != nil {
		klog.Errorf("Can't list referred secrets due to error %v", err)
		return err
	}

	err = r.ListSubscriptionOwnedSrtAndDeploy(instance, refSrt, localSrts)

	if err != nil {
		klog.Errorf("Can't list secret owned by %v due to error %v", instance.GetName(), err)
	}

	// set up owerreference

	return nil
}

func getLabelOfSubscription(instance *appv1alpha1.Subscription) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{instance.GetName(): "true"}}
}

func (r *ReconcileSubscription) ListReferredSecret(instance *appv1alpha1.Subscription) (*corev1.SecretList, error) {
	listOptions := &client.ListOptions{Namespace: instance.GetNamespace()}
	ls, err := metav1.LabelSelectorAsSelector(getLabelOfSubscription(instance))
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

func (r *ReconcileSubscription) ListSubscriptionOwnedSrtAndDeploy(instance *appv1alpha1.Subscription, newSrt *v1.Secret, srtList *v1.SecretList) error {
	if len(srtList.Items) == 0 {
		r.DeployReferredSecret(instance, newSrt)
		return nil
	}

	for _, srt := range srtList.Items {
		owners := srt.GetOwnerReferences()

		if len(owners) == 0 { // not owned by anyone, then we should collect it
			r.Client.Delete(context.TODO(), &srt)
		} else if len(owners) == 1 { // need to check if it's owned by this subscription if so, then we will need to delete it otherwise do nothing
			if srt.GetName() == newSrt.GetName() {
				r.Client.Update(context.TODO(), &srt)
			} else {
				r.Client.Delete(context.TODO(), &srt)
				err := r.DeployReferredSecret(instance, newSrt)
				if err != nil {
					return err
				}
			}

		} else { // owned by more than one subscription, then we need to remove the current subscription from its owner list
			tmp := []metav1.OwnerReference{}
			for _, owner := range owners {
				if owner.Name != instance.GetName() {
					tmp = append(tmp, owner)
				}
			}
			srt.SetOwnerReferences(tmp)
			r.Client.Update(context.TODO(), &srt)
		}
	}
	return nil
}

func (r *ReconcileSubscription) DeployReferredSecret(instance *appv1alpha1.Subscription, newSrt *v1.Secret) error {
	cleanSrt := utils.CleanUpObject(*newSrt)
	srtLabel := map[string]string{instance.GetName(): "true"}
	cleanSrt.SetLabels(srtLabel)
	err := controllerutil.SetControllerReference(instance, &cleanSrt, r.scheme)

	if err != nil {
		errmsg := fmt.Sprintf("Adding owner reference to secert %v, got error: %v", cleanSrt.GetName(), err.Error())
		klog.Error(errmsg)

		return errors.New(errmsg)
	}

	return nil
}
