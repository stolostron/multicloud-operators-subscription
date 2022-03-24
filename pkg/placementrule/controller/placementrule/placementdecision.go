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

package placementrule

import (
	"context"
	"fmt"

	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	clusterapi "open-cluster-management.io/api/cluster/v1beta1"
	placementruleapi "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	placementRuleLabel       = "cluster.open-cluster-management.io/placementrule"
	maxNumOfClusterDecisions = 100
)

// syncPlacementDecisions create/update/delete placementdecisions based on PlacementRule's status.decisions
// based on https://github.com/open-cluster-management-io/placement/blob/v0.2.0/pkg/controllers/scheduling/scheduling_controller.go#L339
func (r *ReconcilePlacementRuleStatus) syncPlacementDecisions(ctx context.Context,
	placementRule placementruleapi.PlacementRule) error {
	klog.Info("syncPlacementDecisions placementrule ", placementRule.Namespace, "/", placementRule.Name)

	var prDecisions []placementruleapi.PlacementDecision

	if placementRule.Status.Decisions != nil && len(placementRule.Status.Decisions) > 0 {
		prDecisions = placementRule.Status.Decisions
	}

	// no sorting necessary because it's already sorted by placementRule

	var clusterDecisions []clusterapi.ClusterDecision

	for _, prDecision := range prDecisions {
		clusterDecisions = append(clusterDecisions, clusterapi.ClusterDecision{ClusterName: prDecision.ClusterName})
	}

	// split the cluster decisions into slices, the size of each slice cannot exceed
	// maxNumOfClusterDecisions.
	decisionSlices := [][]clusterapi.ClusterDecision{}
	remainingDecisions := clusterDecisions

	for index := 0; len(remainingDecisions) > 0; index++ {
		var decisionSlice []clusterapi.ClusterDecision

		switch {
		case len(remainingDecisions) > maxNumOfClusterDecisions:
			decisionSlice = remainingDecisions[0:maxNumOfClusterDecisions]
			remainingDecisions = remainingDecisions[maxNumOfClusterDecisions:]
		default:
			decisionSlice = remainingDecisions
			remainingDecisions = nil
		}

		decisionSlices = append(decisionSlices, decisionSlice)
	}

	// bind cluster decision slices to placementdecisions.
	errs := []error{}

	placementDecisionNames := sets.NewString()

	for index, decisionSlice := range decisionSlices {
		placementDecisionName := fmt.Sprintf("%s-decision-%d", placementRule.Name, index+1)
		placementDecisionNames.Insert(placementDecisionName)
		err := r.createOrUpdatePlacementDecision(
			ctx, placementRule, placementDecisionName, decisionSlice)

		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return errorhelpers.NewMultiLineAggregate(errs)
	}

	// query all placementdecisions of the placement
	requirement, err := labels.NewRequirement(placementRuleLabel, selection.Equals, []string{placementRule.Name})
	if err != nil {
		return err
	}

	labelSelector := labels.NewSelector().Add(*requirement)
	placementDecisions := &clusterapi.PlacementDecisionList{}
	listopts := &client.ListOptions{}
	listopts.LabelSelector = labelSelector
	listopts.Namespace = placementRule.Namespace

	err = r.List(ctx, placementDecisions, listopts)
	if err != nil {
		return err
	}

	// delete redundant placementdecisions
	errs = []error{}

	for _, placementDecision := range placementDecisions.Items {
		if placementDecisionNames.Has(placementDecision.Name) {
			continue
		}

		err := r.Delete(ctx, &placementDecision)
		if errors.IsNotFound(err) {
			continue
		}

		if err != nil {
			errs = append(errs, err)
		}

		klog.Infof("Decision %s is deleted with placement %s in namespace %s", placementDecision.Name, placementRule.Name, placementRule.Namespace)
	}

	return errorhelpers.NewMultiLineAggregate(errs)
}

// createOrUpdatePlacementDecision updates or creates a new PlacementDecision if it does not exist
// based on https://github.com/open-cluster-management-io/placement/blob/v0.2.0/pkg/controllers/scheduling/scheduling_controller.go#L419
func (r *ReconcilePlacementRuleStatus) createOrUpdatePlacementDecision(
	ctx context.Context,
	placementRule placementruleapi.PlacementRule,
	placementDecisionName string,
	clusterDecisions []clusterapi.ClusterDecision,
) error {
	if len(clusterDecisions) > maxNumOfClusterDecisions {
		return fmt.Errorf("the number of clusterdecisions %q exceeds the max limitation %q", len(clusterDecisions), maxNumOfClusterDecisions)
	}

	pdNsn := types.NamespacedName{Namespace: placementRule.Namespace, Name: placementDecisionName}

	placementDecision := &clusterapi.PlacementDecision{}

	err := r.Get(ctx, pdNsn, placementDecision)

	switch {
	case errors.IsNotFound(err):
		// create the placementdecision if not exists
		owner := metav1.NewControllerRef(&placementRule, placementruleapi.SchemeGroupVersion.WithKind("PlacementRule"))
		placementDecision = &clusterapi.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      placementDecisionName,
				Namespace: placementRule.Namespace,
				Labels: map[string]string{
					placementRuleLabel: placementRule.Name,
				},
				OwnerReferences: []metav1.OwnerReference{*owner},
			},
		}

		var err error

		err = r.Create(ctx, placementDecision)
		if err != nil {
			return err
		}

		klog.Infof("Decision %s is created with placement %s in namespace %s", placementDecision.Name, placementRule.Name, placementRule.Namespace)
	case err != nil:
		return err
	}

	// update the status of the placementdecision if decisions change
	if apiequality.Semantic.DeepEqual(placementDecision.Status.Decisions, clusterDecisions) {
		return nil
	}

	newPlacementDecision := placementDecision.DeepCopy()

	newPlacementDecision.Status.Decisions = clusterDecisions

	err = r.Status().Update(ctx, newPlacementDecision)
	if err != nil {
		return err
	}

	klog.Infof("Decision %s is updated with placement %s in namespace %s", placementDecision.Name, placementRule.Name, placementRule.Namespace)

	return nil
}
