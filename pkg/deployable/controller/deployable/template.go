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
	"encoding/json"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/deployable/utils"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
)

func (r *ReconcileDeployable) propagateDeployables(clusters []types.NamespacedName, instance *appv1alpha1.Deployable,
	familymap map[string]*appv1alpha1.Deployable) (map[string]*appv1alpha1.Deployable, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	var err error
	// generate the deploaybles
	hosting := types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}

	for _, cluster := range clusters {
		familymap, err = r.createManagedDeployable(cluster, hosting, instance, familymap)
		if err != nil {
			klog.Error("Error in propagating ", cluster)
			return familymap, err
		}
	}

	return familymap, nil
}

func (r *ReconcileDeployable) createManagedDeployable(cluster types.NamespacedName, hosting types.NamespacedName,
	instance *appv1alpha1.Deployable, familymap map[string]*appv1alpha1.Deployable) (map[string]*appv1alpha1.Deployable, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	var err error

	namespace := cluster.Namespace
	// remove active clusters from expiration list
	klog.V(5).Info("Creating Managed deployable:", instance, " in cluster:", cluster)

	// create or update child deployable
	truekey := types.NamespacedName{Name: instance.GetName() + "-", Namespace: namespace}.String()

	var existingdeployable *appv1alpha1.Deployable
	existingdeployable, ok := familymap[truekey]

	if !ok {
		existingdeployable = &appv1alpha1.Deployable{}
	}

	original := existingdeployable.DeepCopy()
	existingdeployable = r.setLocalDeployable(&cluster, hosting, instance, existingdeployable)
	ifRecordEvent := false

	if !ok {
		klog.V(5).Info("Creating new local deployable:", existingdeployable)
		err = r.Create(context.TODO(), existingdeployable)

		if instance.Status.PropagatedStatus == nil {
			instance.Status.PropagatedStatus = make(map[string]*appv1alpha1.ResourceUnitStatus)
		}

		instance.Status.PropagatedStatus[cluster.Name] = &appv1alpha1.ResourceUnitStatus{}
		ifRecordEvent = true
	} else {
		if !utils.CompareDeployable(original, existingdeployable) {
			klog.Info("Updating existing local deployable: ", existingdeployable.GetName())
			err = r.Update(context.TODO(), existingdeployable)
			if err == nil {
				newDpl := existingdeployable.DeepCopy()
				newDpl.Status.Phase = ""
				newDpl.Status.Message = ""
				newDpl.Status.Reason = ""
				newDpl.Status.ResourceStatus = nil
				now := metav1.Now()
				newDpl.Status.LastUpdateTime = &now
				err = r.Status().Update(context.TODO(), newDpl)
			}

			instance.Status.PropagatedStatus[cluster.Name] = &appv1alpha1.ResourceUnitStatus{}
			ifRecordEvent = true
		} else {
			klog.V(5).Info("Same existing local deployable, no need to update. instance: ",
				string(original.Spec.Template.Raw), " vs existing: ", string(existingdeployable.Spec.Template.Raw))
		}
	}

	if ifRecordEvent {
		//record events
		hostDeployable := &appv1alpha1.Deployable{}
		error1 := r.Get(context.TODO(), hosting, hostDeployable)

		if error1 != nil {
			klog.V(5).Info("hosting deployable not found, unable to record events to it. ", hosting)
		} else {
			dplkey := types.NamespacedName{Name: existingdeployable.GetName(), Namespace: existingdeployable.GetNamespace()}
			eventObj := ""
			addtionalMsg := ""

			if utils.IsDependencyDeployable(existingdeployable) {
				eventObj = "Dependency Deployable"
			} else {
				eventObj = "Deployable"
			}

			addtionalMsg = "Propagate " + eventObj + " " + dplkey.String() + " for cluster " + cluster.String()
			r.eventRecorder.RecordEvent(hostDeployable, "Deploy", addtionalMsg, err)
		}
	}

	if err != nil {
		// return error is something is wrong
		klog.Error("Failed in processing local deployable with error:", err)

		return nil, err
	}

	// remove it from to be deleted map
	klog.V(5).Info("Removing ", truekey, " from ", familymap)
	delete(familymap, truekey)

	return r.createManagedDependencies(cluster, instance, familymap)
}

func (r *ReconcileDeployable) setLocalDeployable(cluster *client.ObjectKey, hosting types.NamespacedName,
	instance, localdeployable *appv1alpha1.Deployable) *appv1alpha1.Deployable {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	localdeployable.SetGenerateName(instance.GetName() + "-")
	localdeployable.SetNamespace(cluster.Namespace)

	localdeployable.Spec.Template = instance.Spec.Template.DeepCopy()

	managedCluster := &spokeClusterV1.ManagedCluster{}
	managedClusterKey := types.NamespacedName{
		Name: cluster.Name,
	}
	err := r.Get(context.TODO(), managedClusterKey, managedCluster)

	if err != nil {
		klog.Error("Failed to find managed cluster " + cluster.Name)
	} else {
		labels := managedCluster.GetLabels()

		if strings.EqualFold(labels["local-cluster"], "true") {
			klog.Info("This is local-cluster")
			klog.Info("Appending -local to the subscription name")
			// append -local to the local subscription name to avoid subscription name collision in the same namespace.
			sub := &unstructured.Unstructured{}
			err := json.Unmarshal(localdeployable.Spec.Template.Raw, sub)

			if err != nil {
				klog.Info("Error in unmarshall, err:", err, " |template: ", string(localdeployable.Spec.Template.Raw))
			} else {
				sub.SetName(sub.GetName() + "-local")
			}

			localdeployable.Spec.Template.Raw, err = json.Marshal(sub)

			if err != nil {
				klog.Info("Error in mashalling obj ", sub, err)
			}
		}
	}

	localdeployable.Spec.Dependencies = instance.Spec.Dependencies
	localdeployable.Spec.Overrides = nil
	localdeployable.Spec.Channels = nil

	localAnnotations := localdeployable.GetAnnotations()
	if localAnnotations == nil {
		localAnnotations = make(map[string]string)
	}

	for k, v := range instance.GetAnnotations() {
		localAnnotations[k] = v
	}

	localAnnotations[appv1alpha1.AnnotationLocal] = "true"
	localAnnotations[appv1alpha1.AnnotationManagedCluster] = cluster.String()
	localAnnotations[appv1alpha1.AnnotationIsGenerated] = "true"
	realhosting := &hosting

	if localAnnotations[appv1alpha1.AnnotationShared] == "true" {
		realhosting = &types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}
	}

	localAnnotations[appv1alpha1.AnnotationHosting] = realhosting.String()

	//delete rollingupdate target annotation anyway. it is not required to be deployed to managed clusters.
	delete(localAnnotations, appv1alpha1.AnnotationRollingUpdateTarget)

	localdeployable.SetAnnotations(localAnnotations)

	localLabels := localdeployable.GetLabels()

	if localLabels == nil {
		localLabels = make(map[string]string)
	}

	for k, v := range instance.GetLabels() {
		localLabels[k] = v
	}

	localLabels[appv1alpha1.PropertyHostingDeployableName] = realhosting.Name

	// propagate subscription-pause label
	if utils.GetPauseLabel(instance) {
		localLabels[appv1alpha1.LabelSubscriptionPause] = "true"
	} else {
		localLabels[appv1alpha1.LabelSubscriptionPause] = "false"
	}

	// propagate subscription-pause label to new local deployable subscription template
	err = utils.SetPauseLabelDplSubTpl(instance, localdeployable)
	if err != nil {
		klog.Info("Failed to propagate pause label to new local deployable subscription template. err:", err)
	}

	localdeployable.SetLabels(localLabels)

	covs, _ := utils.PrepareOverrides(*cluster, instance)
	if covs != nil {
		tplobj := &unstructured.Unstructured{}
		err := json.Unmarshal(localdeployable.Spec.Template.Raw, tplobj)

		if err != nil {
			klog.Info("Error in unmarshall template ", string(localdeployable.Spec.Template.Raw))
			return localdeployable
		}

		tplobj, err = utils.OverrideTemplate(tplobj, covs)
		if err != nil {
			klog.Info("Error in overriding obj ", tplobj)
			return localdeployable
		}

		localdeployable.Spec.Template.Raw, err = json.Marshal(tplobj)
		if err != nil {
			klog.Info("Error in mashalling obj ", tplobj)
		}
	}

	klog.V(5).Info("Local deployable:", localdeployable)

	return localdeployable
}
