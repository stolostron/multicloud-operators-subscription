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

package kubernetes

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appSubStatusV1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

	workloadConfigmap1 = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configmap1",
			Namespace: "default",
		},
	}
)

var _ = Describe("test Delete Single Subscribed Resource", func() {
	It("should delete the confimap without failure", func() {
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		workload1 := workloadConfigmap1.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		pkgStatus := appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "configmap1",
			Namespace:  "default",
			ApiVersion: "v1",
			Kind:       "ConfigMap",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).NotTo(HaveOccurred())
	})
})
