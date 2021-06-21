// Copyright 2020 The Kubernetes Authors.
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
	"time"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

var (
	agentLabel = map[string]string{"app": "application-manager"}
)

// LeaseReconciler reconciles a Secret object
type LeaseReconciler struct {
	HubKubeClient        kubernetes.Interface
	KubeClient           kubernetes.Interface
	ClusterName          string
	LeaseName            string
	LeaseDurationSeconds int32
	componentNamespace   string
}

func (r *LeaseReconciler) Reconcile(ctx context.Context) {
	if len(r.componentNamespace) == 0 {
		componentNamespace, err := utils.GetComponentNamespace()
		if err != nil {
			klog.Errorf("failed to get pod namespace use. error:%v", err)
		}

		r.componentNamespace = componentNamespace
	}

	// Create/update lease on managed cluster first. If it fails, it could mean lease resource kind
	// is not supported on the managed cluster. Create/update lease on the hub then.
	err := r.updateLease(ctx, r.componentNamespace, r.KubeClient)

	if err != nil {
		klog.Errorf("Failed to update lease %s/%s: %v on managed cluster", r.LeaseName, r.componentNamespace, err)

		// Try to create or update the lease on in the managed cluster's namespace on the hub cluster.
		if r.HubKubeClient != nil {
			klog.Errorf("Trying to update lease on the hub")

			if err := r.updateLease(ctx, r.ClusterName, r.HubKubeClient); err != nil {
				klog.Errorf("Failed to update lease %s/%s: %v on hub cluster", r.LeaseName, r.ClusterName, err)
			}
		}
	}
}

func (r *LeaseReconciler) updateLease(ctx context.Context, namespace string, client kubernetes.Interface) error {
	klog.Infof("trying to update lease %q/%q", namespace, r.LeaseName)

	lease, err := client.CoordinationV1().Leases(namespace).Get(ctx, r.LeaseName, metav1.GetOptions{})

	switch {
	case errors.IsNotFound(err):
		// create lease
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.LeaseName,
				Namespace: namespace,
			},
			Spec: coordinationv1.LeaseSpec{
				LeaseDurationSeconds: &r.LeaseDurationSeconds,
				RenewTime: &metav1.MicroTime{
					Time: time.Now(),
				},
			},
		}
		if _, err := client.CoordinationV1().Leases(namespace).Create(ctx, lease, metav1.CreateOptions{}); err != nil {
			klog.Errorf("unable to create addon lease %q/%q . error:%v", namespace, r.LeaseName, err)

			return err
		}

		klog.Infof("addon lease %q/%q created", namespace, r.LeaseName)

		return nil
	case err != nil:
		klog.Errorf("unable to get addon lease %q/%q . error:%v", namespace, r.LeaseName, err)

		return err
	default:
		// update lease
		lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now()}
		if _, err = client.CoordinationV1().Leases(namespace).Update(ctx, lease, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("unable to update cluster lease %q/%q . error:%v", namespace, r.LeaseName, err)

			return err
		}

		klog.Infof("addon lease %q/%q updated", namespace, r.LeaseName)

		return nil
	}
}
