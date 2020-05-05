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

package namespace

import (
	"time"

	. "github.com/onsi/ginkgo"

	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
)

var id = types.NamespacedName{
	Name:      "tend",
	Namespace: "tch",
}

var (
	workloadkey = types.NamespacedName{
		Name:      "testworkload",
		Namespace: "tch",
	}

	workloadconfigmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadkey.Name,
			Namespace: workloadkey.Namespace,
		},
	}

	chdpl = &dplv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployable",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "chdpl",
			Namespace: id.Namespace,
		},
		Spec: dplv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: &workloadconfigmap,
			},
		},
	}
)

var (
	defaultworkloadkey = types.NamespacedName{
		Name:      "testworkload",
		Namespace: "default",
	}

	defaultworkloadconfigmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultworkloadkey.Name,
			Namespace: defaultworkloadkey.Namespace,
		},
	}

	defaultchdpl = &dplv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployable",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dftdpl",
			Namespace: id.Namespace,
		},
		Spec: dplv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: &defaultworkloadconfigmap,
			},
		},
	}
)

var (
	chns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      id.Namespace,
			Namespace: id.Namespace,
		},
	}
)

var _ = Describe("default deployable should be reconciled", func() {
	It("should reconcile on deployable add/update/delete event", func() {
		// prepare default channel
		ns := chns.DeepCopy()
		dpl := chdpl.DeepCopy()
		dpldft := defaultchdpl.DeepCopy()
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.

		Expect(k8sClient.Create(context.TODO(), ns)).NotTo(HaveOccurred())
		Expect(k8sClient.Create(context.TODO(), dpl)).NotTo(HaveOccurred())
		Expect(k8sClient.Create(context.TODO(), dpldft)).NotTo(HaveOccurred())

		cfgmap := &corev1.ConfigMap{}
		tdpl := &dplv1alpha1.Deployable{}

		time.Sleep(k8swait)
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: dpl.GetName(), Namespace: dpl.GetNamespace()}, tdpl)).NotTo(HaveOccurred())
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: dpldft.GetName(), Namespace: dpldft.GetNamespace()}, tdpl)).NotTo(HaveOccurred())
		Expect(k8sClient.Get(context.TODO(), workloadkey, cfgmap)).To(HaveOccurred())
		Expect(k8sClient.Get(context.TODO(), defaultworkloadkey, cfgmap)).To(HaveOccurred())

		// clean up
		Expect(k8sClient.Delete(context.TODO(), dpl)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), dpldft)).Should(Succeed())

		time.Sleep(k8swait)
		Expect(k8sClient.Get(context.TODO(), workloadkey, cfgmap)).To(HaveOccurred())
		Expect(k8sClient.Get(context.TODO(), defaultworkloadkey, cfgmap)).To(HaveOccurred())

		Expect(k8sClient.Delete(context.TODO(), ns)).Should(Succeed())
	})
})
