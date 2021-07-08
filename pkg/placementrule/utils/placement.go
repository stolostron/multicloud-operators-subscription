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

package utils

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
)

func ToPlaceLocal(placement *appv1alpha1.Placement) bool {
	if placement == nil || placement.Local == nil {
		return false
	}

	return *placement.Local
}

// PlaceByGenericPlacmentFields search with basic placement criteria
// Top priority: clusterNames, ignore selector
// Bottomline: Use label selector
func PlaceByGenericPlacmentFields(kubeclient client.Client, placement appv1alpha1.GenericPlacementFields,
	authclient kubernetes.Interface, object runtime.Object) (map[string]*spokeClusterV1.ManagedCluster, error) {
	clmap := make(map[string]*spokeClusterV1.ManagedCluster)

	var labelSelector *metav1.LabelSelector

	// MCM Assumption: clusters are always labeled with name
	if len(placement.Clusters) != 0 {
		namereq := metav1.LabelSelectorRequirement{}
		namereq.Key = "name"
		namereq.Operator = metav1.LabelSelectorOpIn

		for _, cl := range placement.Clusters {
			namereq.Values = append(namereq.Values, cl.Name)
		}

		labelSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{namereq},
		}
	} else {
		labelSelector = placement.ClusterSelector
	}

	clSelector, err := ConvertLabels(labelSelector)

	if err != nil {
		return nil, err
	}

	klog.V(1).Info("Using Cluster LabelSelector ", clSelector)

	cllist := &spokeClusterV1.ManagedClusterList{}

	err = kubeclient.List(context.TODO(), cllist, &client.ListOptions{LabelSelector: clSelector})

	if err != nil && !errors.IsNotFound(err) {
		klog.Error("Listing clusters and found error: ", err)

		return nil, err
	}

	for _, cl := range cllist.Items {
		// the cluster will not be returned if it is in terminating process
		if cl.DeletionTimestamp != nil && !cl.DeletionTimestamp.IsZero() {
			continue
		}

		cl.Namespace = cl.Name

		// reduce memory consumption by cleaning up ManagedFields, Annotations, Finalizers for each managed clustger
		cl.DeepCopy().SetManagedFields(nil)
		cl.DeepCopy().SetAnnotations(nil)
		cl.DeepCopy().SetFinalizers(nil)

		clmap[cl.Name] = cl.DeepCopy()
	}

	klog.Infof("listed clusters original count: %v", len(cllist.Items))

	return clmap, nil
}

func InstanceDeepCopy(a, b interface{}) error {
	byt, err := json.Marshal(a)

	if err == nil {
		err = json.Unmarshal(byt, b)
	}

	return err
}

// IsReadyACMClusterRegistry check if ACM Cluster API service is ready or not.
func IsReadyACMClusterRegistry(clReader client.Reader) bool {
	cllist := &spokeClusterV1.ManagedClusterList{}

	listopts := &client.ListOptions{}

	err := clReader.List(context.TODO(), cllist, listopts)

	if err == nil {
		klog.Info("Cluster API service ready")
		return true
	}

	klog.Error("Cluster API service NOT ready: ", err)

	return false
}

// DetectClusterRegistry - Detect the ACM cluster API service every 10 seconds. the controller will be exited when it is ready
// The controller will be auto restarted by the multicluster-operators-application deployment CR later.
func DetectClusterRegistry(clReader client.Reader, s <-chan struct{}) {
	if !IsReadyACMClusterRegistry(clReader) {
		go wait.Until(func() {
			if IsReadyACMClusterRegistry(clReader) {
				os.Exit(1)
			}
		}, time.Duration(10)*time.Second, s)
	}
}
