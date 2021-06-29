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

package deployable

import (
	"context"
	"reflect"
	"strconv"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/deployable/utils"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
)

func (r *ReconcileDeployable) rollingUpdate(instance *appv1alpha1.Deployable) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	klog.V(1).Info("Rolling Updating ", instance.GetName())

	annotations := instance.GetAnnotations()

	if annotations == nil || annotations[appv1alpha1.AnnotationRollingUpdateTarget] == "" {
		klog.V(1).Info("Empty annotation or No rolling update target in annotations", annotations)
		return nil
	}

	if len(instance.Status.PropagatedStatus) == 0 {
		klog.V(1).Info(" No propagated clusters for rolling update", annotations)
		return nil
	}

	// maxunav is the actual updated number in every rolling update
	maxunav, err := strconv.Atoi(annotations[appv1alpha1.AnnotationRollingUpdateMaxUnavailable])
	if err != nil {
		maxunav = appv1alpha1.DefaultRollingUpdateMaxUnavailablePercentage
	}

	maxunav = (len(instance.Status.PropagatedStatus)*maxunav + 99) / 100
	klog.V(1).Info("ongoing rolling update to ", annotations[appv1alpha1.AnnotationRollingUpdateTarget], " with max ", maxunav, " unavaialble clusters")

	targetdpl := &appv1alpha1.Deployable{}
	err = r.Get(context.TODO(),
		types.NamespacedName{
			Name:      annotations[appv1alpha1.AnnotationRollingUpdateTarget],
			Namespace: instance.Namespace,
		}, targetdpl)

	if err != nil {
		klog.Info("Failed to find rolling update target", annotations[appv1alpha1.AnnotationRollingUpdateTarget])

		return err
	}

	// propagate subscription-pause label to rolling update target deployable subscription template
	err = utils.SetPauseLabelDplSubTpl(instance, targetdpl)
	if err != nil {
		klog.Info("Failed to propagate pause label to target deployable subscription template. err:", err)
		return err
	}

	//it is only triggered in the initial rolling update.
	if !reflect.DeepEqual(instance.Spec.Template, targetdpl.Spec.Template) {
		klog.V(1).Info("Initialize rolling update to ", annotations[appv1alpha1.AnnotationRollingUpdateTarget])

		ov := appv1alpha1.Overrides{}

		// target dpl becomes new instnace template for propagation.
		// So instance Overrides stores all clusters who are not eligible to update now.
		// When the whole rolling update is done, the instance Overrides array should be zero element.
		ov.ClusterOverrides = utils.GenerateOverrides(targetdpl, instance)

		covmap := make(map[string]appv1alpha1.Overrides)

		for n := range instance.Status.PropagatedStatus {
			cov := *(ov.DeepCopy())
			cov.ClusterName = n
			covmap[n] = cov
		}
		// existing overrides are rolling out anyway
		maxunav -= len(instance.Spec.Overrides)

		for _, ov := range targetdpl.Spec.Overrides {
			covmap[ov.ClusterName] = *(ov.DeepCopy())
		}

		maxunav -= len(targetdpl.Spec.Overrides)

		instance.Spec.Overrides = nil
		for _, ov := range covmap {
			instance.Spec.Overrides = append(instance.Spec.Overrides, *(ov.DeepCopy()))
		}

		targetdpl.Spec.Template.DeepCopyInto(instance.Spec.Template)
	}

	for _, cs := range instance.Status.PropagatedStatus {
		if cs.Phase != appv1alpha1.DeployableDeployed {
			maxunav--
		}
	}

	var targetovs []appv1alpha1.Overrides

	ovmap := make(map[string]*appv1alpha1.Overrides)

	for _, tov := range targetdpl.Spec.Overrides {
		ovmap[tov.ClusterName] = tov.DeepCopy()
	}

	for _, ov := range instance.Spec.Overrides {
		// ensure desired overrides are aligned
		if cov, ok := ovmap[ov.ClusterName]; ok {
			targetovs = append(targetovs, *cov)
		} else if maxunav > 0 {
			// roll 1 more
			maxunav--
		} else {
			// out of quota
			cov = &appv1alpha1.Overrides{}
			ov.DeepCopyInto(cov)
			targetovs = append(targetovs, *cov)
		}
	}

	instance.Spec.Overrides = nil

	for _, cov := range targetovs {
		instance.Spec.Overrides = append(instance.Spec.Overrides, *(cov.DeepCopy()))
	}

	klog.V(1).Info("Rolling update exit with overrides: ", instance.Spec.Overrides)

	return nil
}

func (r *ReconcileDeployable) validateOverridesForRollingUpdate(instance *appv1alpha1.Deployable) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	klog.V(1).Info("Rolling update validation started with overrides: ", instance.Spec.Overrides, "and status ", instance.Status.PropagatedStatus)

	var allov []appv1alpha1.Overrides

	for _, ov := range instance.Spec.Overrides {
		klog.V(5).Info("validating overrides: ", ov)

		if _, ok := instance.Status.PropagatedStatus[ov.ClusterName]; ok {
			allov = append(allov, *(ov.DeepCopy()))
		}
	}

	instance.Spec.Overrides = allov

	klog.V(5).Info("Rolling update validated overrides: ", instance.Spec.Overrides)
}
