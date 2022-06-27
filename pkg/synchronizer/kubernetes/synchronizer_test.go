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

package kubernetes

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
)

var (
	host = types.NamespacedName{
		Name:      "cluster1",
		Namespace: "cluster1",
	}

	hostSub = types.NamespacedName{
		Name:      "appsub-1",
		Namespace: "appsub-ns-1",
	}

	workload1Configmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configmap1",
			Namespace: "default",
		},
	}

	selector = map[string]string{"a": "b"}

	workload2Deployment = apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment1",
			Namespace: "default",
		},
		Spec: apps.DeploymentSpec{
			Replicas: func() *int32 { i := int32(1); return &i }(),
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selector,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "foobar",
							Image: "foo/bar",
						},
					},
				},
			},
		},
	}
)

var _ = Describe("test Delete Single Subscribed Resource", func() {
	It("should delete the confimap and deployment without failure", func() {
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		workload1 := workload1Configmap.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		pkgStatus := appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "configmap1",
			Namespace:  "default",
			APIVersion: "v1",
			Kind:       "ConfigMap",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).NotTo(HaveOccurred())

		workload2 := workload2Deployment.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload2)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload2)

		pkgStatus = appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "deployment1",
			Namespace:  "default",
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should have an invalid apiversion pkgStatus", func() {
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		workload1 := workload1Configmap.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		pkgStatus := appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "configmap1",
			Namespace:  "default",
			APIVersion: "",
			Kind:       "ConfigMap",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("test getHostingAppSub", func() {
	It("should fail", func() {
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		subscription, err := sync.getHostingAppSub(hostSub)
		Expect(err).To(HaveOccurred())
		Expect(subscription).To(BeNil())
	})
})
