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
	"io/ioutil"
	"reflect"
	"time"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

var (
	agentLabel = map[string]string{"app": "application-manager"}
)

// LeaseReconciler reconciles a Secret object
type LeaseReconciler struct {
	KubeFake             bool
	LeaseDurationSeconds int32
	KubeClient           kubernetes.Interface
	LeaseName            string
	LeaseNamespace       string
	HubKubeConfigPath    string
	cachedKubeConfig     []byte
	componentNamespace   string
	hubClient            kubernetes.Interface
}

func (r *LeaseReconciler) Reconcile(ctx context.Context) {
	curKubeConfig, err := ioutil.ReadFile(r.HubKubeConfigPath)
	if err != nil || len(curKubeConfig) == 0 {
		klog.Errorf("Failed to get hub kubeconfig. path: %v", r.HubKubeConfigPath)
		return
	}

	if !reflect.DeepEqual(r.cachedKubeConfig, curKubeConfig) {
		if len(r.componentNamespace) == 0 {
			r.componentNamespace, err = utils.GetComponentNamespace()
			if err != nil {
				klog.Errorf("use the open-cluster-management-agent-addon namespace. error:%v", err)
			}
		}

		if len(r.cachedKubeConfig) != 0 {
			//If kubeconfig changed, restart agent pods
			labelSelector := labels.FormatLabels(agentLabel)

			appmgrPods, err := r.KubeClient.CoreV1().Pods(r.componentNamespace).List(context.TODO(),
				metav1.ListOptions{LabelSelector: labelSelector})

			if err != nil {
				klog.Infof("failed to fetch appmgrPods, err: %v", err)
			}

			for _, appmgrPod := range appmgrPods.Items {
				err = r.KubeClient.CoreV1().Pods(r.componentNamespace).Delete(context.TODO(), appmgrPod.GetName(),
					metav1.DeleteOptions{})

				if err != nil {
					klog.Errorf("failed to delete the klusterlet-addon-appmgr pod. error:%v", err)
				}
			}

			return
		}

		// update cached kubeconfig to new hub client.
		// kubefake is true only when the reconcicle is triggerred by the lease controller unit test.
		if !r.KubeFake {
			r.hubClient, err = utils.BuildKubeClient(r.HubKubeConfigPath)
			if err != nil {
				klog.Errorf("failed to build hub client. error:%v", err)
				return
			}
		}

		r.cachedKubeConfig = curKubeConfig
	}

	lease, err := r.hubClient.CoordinationV1().Leases(r.LeaseNamespace).Get(context.TODO(), r.LeaseName, metav1.GetOptions{})

	switch {
	case errors.IsNotFound(err):
		//create lease
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.LeaseName,
				Namespace: r.LeaseNamespace,
			},
			Spec: coordinationv1.LeaseSpec{
				LeaseDurationSeconds: &r.LeaseDurationSeconds,
				RenewTime: &metav1.MicroTime{
					Time: time.Now(),
				},
			},
		}
		if _, err := r.hubClient.CoordinationV1().Leases(r.LeaseNamespace).Create(context.TODO(), lease, metav1.CreateOptions{}); err != nil {
			klog.Errorf("unable to create addon lease %q/%q on hub cluster. error:%v", r.LeaseNamespace, r.LeaseName, err)
		} else {
			klog.Infof("addon lease %q/%q on hub cluster created", r.LeaseNamespace, r.LeaseName)
		}

		return
	case err != nil:
		klog.Errorf("unable to get addon lease %q/%q on hub cluster. error:%v", r.LeaseNamespace, r.LeaseName, err)
		return
	default:
		//update lease
		lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now()}
		if _, err = r.hubClient.CoordinationV1().Leases(r.LeaseNamespace).Update(context.TODO(), lease, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("unable to update cluster lease %q/%q on hub cluster. error:%v", r.LeaseNamespace, r.LeaseName, err)
		} else {
			klog.Infof("addon lease %q/%q on hub cluster updated", r.LeaseNamespace, r.LeaseName)
		}

		return
	}
}
