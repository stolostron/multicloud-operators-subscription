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
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
)

var _ = Describe("benchmark the sync channel", func() {
	var (
		hostKey = types.NamespacedName{
			Name:      "test-sub",
			Namespace: "default",
		}

		payload = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hostKey.Name,
				Namespace: hostKey.Namespace,
			},
			Data: map[string]string{
				"path": "test-path",
			},
		}

		pGvk = schema.GroupVersionKind{
			Group:   payload.GroupVersionKind().Group,
			Version: payload.GroupVersionKind().Version,
			Kind:    payload.GroupVersionKind().Kind,
		}

		dplinstance = dplv1alpha1.Deployable{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hostKey.Name,
				Namespace: hostKey.Namespace,
				Annotations: map[string]string{
					dplv1alpha1.AnnotationLocal: "true",
				},
			},
			Spec: dplv1alpha1.DeployableSpec{
				Template: &runtime.RawExtension{
					Object: payload,
				},
			},
		}

		syncsource = "test-sync"
		sync       = &KubeSynchronizer{}
	)

	BeforeEach(func() {
		sync, _ = CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
	}, 60)

	PMeasure("measure the client side", func(b Benchmarker) {
		//defer close(sync.stopCh)
		dplinstance.SetName(strconv.Itoa(rand.Intn(200)))
		payload.SetName(strconv.Itoa(rand.Intn(200)))
		dplinstance.Spec.Template.Object = payload
		go sync.AddTemplates(syncsource, hostKey, []DplUnit{{Dpl: &dplinstance, Gvk: pGvk}})

		tt := b.Time("timing the adding template", func() {
			sync.processOrder(<-sync.tplCh)
		})

		By("took", func() {
			fmt.Println(tt.Seconds())
		})
	}, 100)
})
