// Copyright 2022 The Kubernetes Authors.
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

package mcmhub

import (
	"context"
	"fmt"

	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	deployableV1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
	appSubV1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *ReconcileSubscription) cleanupDeployables(appsub *appSubV1.Subscription) error {
	// Delete child deployables with labels: hosting-deployable-name: git-gb-subscription-4-deployable
	deployableList := &deployableV1.DeployableList{}
	listopts := &client.ListOptions{}

	deployableSelector := &metaV1.LabelSelector{
		MatchLabels: map[string]string{
			"hosting-deployable-name": fmt.Sprintf("%.63s", appsub.GetName()+"-deployable"),
		},
	}

	deployableLabels, err := utils.ConvertLabels(deployableSelector)
	if err != nil {
		klog.Error("Failed to convert deployable label selector, err:", err)

		return err
	}

	listopts.LabelSelector = deployableLabels

	err = r.List(context.TODO(), deployableList, listopts)
	if err != nil {
		klog.Errorf("Failed to list deployables by hosting label, err:%v", err)

		return err
	}

	for _, deployable := range deployableList.Items {
		curDeployable := deployable.DeepCopy()
		err := r.Delete(context.TODO(), curDeployable)

		if err != nil {
			klog.Warningf("Error in deleting deployable: %v/%v, err: %v ", curDeployable.GetNamespace(), curDeployable.GetName(), err)

			// Log and continue with other cleanup instead of failing
			continue
		}

		klog.Infof("Deployable deleted: %v/%v", curDeployable.GetNamespace(), curDeployable.GetName())
	}

	// Delete deployable in app NS
	listopts = &client.ListOptions{}
	listopts.Namespace = appsub.GetNamespace()

	err = r.List(context.TODO(), deployableList, listopts)
	if err != nil {
		klog.Errorf("Failed to list deployables in appsub NS, err:%v", err)

		return err
	}

	for _, deployable := range deployableList.Items {
		curDeployable := deployable.DeepCopy()
		err := r.Delete(context.TODO(), curDeployable)

		if err != nil {
			klog.Warningf("Error in deleting deployable: %v/%v, err: %v ", curDeployable.GetNamespace(), curDeployable.GetName(), err)

			// Log and continue with other cleanup instead of failing
			continue
		}

		klog.Infof("Deployable deleted: %v/%v", curDeployable.GetNamespace(), curDeployable.GetName())
	}

	return nil
}
