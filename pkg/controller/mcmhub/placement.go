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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	placementv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	appSubV1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	placementutils "open-cluster-management.io/multicloud-operators-subscription/pkg/placementrule/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		clustermap, err := placementutils.PlaceByGenericPlacmentFields(r.Client, instance.Spec.Placement.GenericPlacementFields, nil, instance)
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

func (r *ReconcileSubscription) getClustersFromPlacementRef(instance *appSubV1.Subscription) ([]ManageClusters, error) {
	var clusters []ManageClusters
	// only support mcm placementpolicy now
	pp := &placementv1alpha1.PlacementRule{}
	pref := instance.Spec.Placement.PlacementRef

	if len(pref.Kind) > 0 && pref.Kind != "PlacementRule" || len(pref.APIVersion) > 0 && pref.APIVersion != "apps.open-cluster-management.io/v1" {
		klog.Warning("Unsupported placement reference:", instance.Spec.Placement.PlacementRef)

		return nil, nil
	}

	klog.Info("Referencing existing PlacementRule:", instance.Spec.Placement.PlacementRef, " in ", instance.GetNamespace())

	// get placementpolicy resource
	err := r.Get(context.TODO(), client.ObjectKey{Name: instance.Spec.Placement.PlacementRef.Name, Namespace: instance.GetNamespace()}, pp)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Warning("Failed to locate placement reference", instance.Spec.Placement.PlacementRef)

			return nil, err
		}

		return nil, err
	}

	klog.V(1).Info("Preparing cluster namespaces from ", pp)

	for _, decision := range pp.Status.Decisions {
		cluster := ManageClusters{Cluster: decision.ClusterName, IsLocalCluster: r.isLocalCluster(decision.ClusterName)}
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
