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

package mcmhub

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	clusterapi "open-cluster-management.io/api/cluster/v1beta1"
	appSubV1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	placementutils "open-cluster-management.io/multicloud-operators-subscription/pkg/placementrule/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	placementRuleLabel = "cluster.open-cluster-management.io/placementrule"
	placementLabel     = "cluster.open-cluster-management.io/placement"
)

type ManageClusters struct {
	Cluster        string
	IsLocalCluster bool
}

// Top priority: placementRef, ignore others
// Next priority: clusterNames, ignore selector
// Bottomline: Use label selector.
func (r *ReconcileSubscription) getClustersByPlacement(instance *appSubV1.Subscription) ([]ManageClusters, error) {
	var clusters []ManageClusters

	var err error

	if instance.Spec.Placement == nil {
		return clusters, nil
	}

	// Top priority: placementRef, ignore others
	// Next priority: clusterNames, ignore selector
	// Bottomline: Use label selector
	if instance.Spec.Placement.PlacementRef != nil {
		clusters, err = r.getClustersFromPlacementRef(instance)
	} else {
		clustermap, err := placementutils.PlaceByGenericPlacmentFields(r.Client, instance.Spec.Placement.GenericPlacementFields, instance)
		if err != nil {
			klog.Error("Failed to get clusters from generic fields with error: ", err)
			return nil, err
		}
		for _, cl := range clustermap {
			clusters = append(clusters,
				ManageClusters{
					Cluster:        cl.Name,
					IsLocalCluster: isLocalClusterByLabels(cl.GetLabels()),
				})
		}
	}

	if err != nil {
		klog.Error("Failed in finding cluster namespaces. error: ", err)
		return nil, err
	}

	klog.Info("Deploying to clusters", clusters)

	return clusters, nil
}

func getDecisionsFromPlacementRef(pref *corev1.ObjectReference, namespace string, kubeClient client.Client) ([]string, error) {
	klog.V(1).Info("Preparing cluster names from ", pref.Name)

	label := placementRuleLabel

	if strings.EqualFold(pref.Kind, "Placement") {
		label = placementLabel
	}

	// query all placementdecisions of the placement
	requirement, err := labels.NewRequirement(label, selection.Equals, []string{pref.Name})
	if err != nil {
		return nil, err
	}

	labelSelector := labels.NewSelector().Add(*requirement)
	placementDecisions := &clusterapi.PlacementDecisionList{}
	listopts := &client.ListOptions{}
	listopts.LabelSelector = labelSelector
	listopts.Namespace = namespace

	err = kubeClient.List(context.TODO(), placementDecisions, listopts)
	if err != nil {
		return nil, err
	}

	var clusterNames []string

	for _, placementDecision := range placementDecisions.Items {
		if placementDecision.Status.Decisions != nil {
			for _, decision := range placementDecision.Status.Decisions {
				clusterNames = append(clusterNames, decision.ClusterName)
			}
		}
	}

	return clusterNames, nil
}

func (r *ReconcileSubscription) getClustersFromPlacementRef(instance *appSubV1.Subscription) ([]ManageClusters, error) {
	var clusters []ManageClusters

	pref := instance.Spec.Placement.PlacementRef

	if (len(pref.Kind) > 0 && pref.Kind != "PlacementRule" && pref.Kind != "Placement") ||
		(len(pref.APIVersion) > 0 &&
			pref.APIVersion != "apps.open-cluster-management.io/v1" &&
			pref.APIVersion != "cluster.open-cluster-management.io/v1alpha1" &&
			pref.APIVersion != "cluster.open-cluster-management.io/v1beta1") {
		klog.Warning("Unsupported placement reference:", instance.Spec.Placement.PlacementRef)

		return nil, nil
	}

	klog.Info("Referencing Placement: ", pref, " in ", instance.GetNamespace())

	ns := instance.GetNamespace() // only allow to pick up the placementRule on the same namespace as appsub

	clusterNames, err := getDecisionsFromPlacementRef(pref, ns, r.Client)
	if err != nil {
		klog.Error("Failed to get decisions from placement reference: ", pref.Name)

		return nil, err
	}

	for _, clusterName := range clusterNames {
		cluster := ManageClusters{Cluster: clusterName, IsLocalCluster: r.isLocalCluster(clusterName)}
		clusters = append(clusters, cluster)
	}

	return clusters, nil
}

func (r *ReconcileSubscription) isLocalCluster(clusterName string) bool {
	managedCluster := &spokeClusterV1.ManagedCluster{}
	managedClusterKey := types.NamespacedName{
		Name: clusterName,
	}
	err := r.Get(context.TODO(), managedClusterKey, managedCluster)

	if err != nil {
		klog.Errorf("Failed to find managed cluster: %v, error: %v ", clusterName, err)
		return false
	}

	labels := managedCluster.GetLabels()

	if labels == nil {
		labels = make(map[string]string)
	}

	if strings.EqualFold(labels["local-cluster"], "true") {
		klog.Infof("This is local-cluster: %v", clusterName)
		return true
	}

	return false
}

func isLocalClusterByLabels(clusterLabels map[string]string) bool {
	if clusterLabels == nil {
		return false
	}

	if strings.EqualFold(clusterLabels["local-cluster"], "true") {
		klog.Infof("There is local-cluster label: %v", clusterLabels)
		return true
	}

	return false
}
