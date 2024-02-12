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
	"os"
	"testing"
	"time"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

const (
	leaseName     = "application-manager"
	agentNs       = "open-cluster-management-agent"
	appmgrPodName = "klusterlet-addon-appmgr"
)

var (
	ns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentNs,
		},
	}

	pod = &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   appmgrPodName,
			Labels: agentLabel,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
)

func TestLeaseReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	s := scheme.Scheme
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Namespace{})

	addontNs, _ := utils.GetComponentNamespace()
	pod.SetNamespace(addontNs)

	tmpFile, err := os.CreateTemp("", "temptest")
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	_, err = tmpFile.WriteString("fake kubeconfig data")
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	hubKubeConfigCheckSum, err := utils.GetCheckSum(tmpFile.Name())
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	defer os.Remove(tmpFile.Name()) // clean up the temp fake kubeconfig file

	kubeClient := kubefake.NewSimpleClientset(ns, pod)

	leaseReconciler := &LeaseReconciler{
		KubeClient:            kubeClient,
		HubConfigFilePathName: tmpFile.Name(),
		HubConfigCheckSum:     hubKubeConfigCheckSum,
		LeaseName:             leaseName,
		LeaseDurationSeconds:  1,
		componentNamespace:    agentNs,
	}

	// test1: create lease
	leaseReconciler.Reconcile(context.TODO())
	time.Sleep(1 * time.Second)

	lease, err := kubeClient.CoordinationV1().Leases(agentNs).Get(context.TODO(), leaseName, metav1.GetOptions{})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	renewTime1 := lease.Spec.RenewTime.DeepCopy()

	// test 2: update lease after 5 seconds, make sure lease spec.renewTime is updated
	time.Sleep(5 * time.Second)
	leaseReconciler.Reconcile(context.TODO())
	time.Sleep(1 * time.Second)

	lease, err = kubeClient.CoordinationV1().Leases(agentNs).Get(context.TODO(), leaseName, metav1.GetOptions{})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	renewTime2 := lease.Spec.RenewTime.DeepCopy()

	g.Expect(renewTime1.Before(renewTime2)).Should(gomega.BeTrue())

	// test 3: don't change temp fake kubeconfig file, expect no error
	err = leaseReconciler.CheckHubKubeConfig(context.TODO())
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	// test 4: change temp fake kubeconfig file, expect to return error
	_, err = tmpFile.WriteString("fake kubeconfig data 2")
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	err = leaseReconciler.CheckHubKubeConfig(context.TODO())
	g.Expect(err).Should(gomega.HaveOccurred())
}
