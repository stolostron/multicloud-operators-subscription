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
	KubeClient           kubernetes.Interface
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

	lease, err := r.KubeClient.CoordinationV1().Leases(r.componentNamespace).Get(context.TODO(), r.LeaseName, metav1.GetOptions{})

	switch {
	case errors.IsNotFound(err):
		// create lease
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.LeaseName,
				Namespace: r.componentNamespace,
			},
			Spec: coordinationv1.LeaseSpec{
				LeaseDurationSeconds: &r.LeaseDurationSeconds,
				RenewTime: &metav1.MicroTime{
					Time: time.Now(),
				},
			},
		}
		if _, err := r.KubeClient.CoordinationV1().Leases(r.componentNamespace).Create(context.TODO(), lease, metav1.CreateOptions{}); err != nil {
			klog.Errorf("unable to create addon lease %q/%q on local managed cluster. error:%v", r.componentNamespace, r.LeaseName, err)
		} else {
			klog.Infof("addon lease %q/%q on local managed cluster created", r.componentNamespace, r.LeaseName)
		}

		return
	case err != nil:
		klog.Errorf("unable to get addon lease %q/%q on local managed cluster. error:%v", r.componentNamespace, r.LeaseName, err)

		return
	default:
		// update lease
		lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now()}
		if _, err = r.KubeClient.CoordinationV1().Leases(r.componentNamespace).Update(context.TODO(), lease, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("unable to update cluster lease %q/%q on local managed cluster. error:%v", r.componentNamespace, r.LeaseName, err)
		} else {
			klog.Infof("addon lease %q/%q on local managed cluster updated", r.componentNamespace, r.LeaseName)
		}

		return
	}
}
