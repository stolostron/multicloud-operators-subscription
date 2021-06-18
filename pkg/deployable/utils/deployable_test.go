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

package utils

import (
	"testing"

	"github.com/onsi/gomega"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var (
	oldDeployable = &appv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployable",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "sub-deployable",
			Labels: map[string]string{
				"deployable-label": "passed-in",
			},
		},
	}

	newDeployable = &appv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployable",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "sub-deployable",
			Labels: map[string]string{
				"deployable-label": "passed-out",
			},
		},
	}
)

func TestDeployablePredicate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test ClusterPredicateFunc
	instance := DeployablePredicateFunc

	updateEvt := event.UpdateEvent{
		ObjectOld: oldDeployable,
		MetaOld:   oldDeployable.GetObjectMeta(),
		ObjectNew: newDeployable,
		MetaNew:   newDeployable.GetObjectMeta(),
	}
	ret := instance.Update(updateEvt)
	g.Expect(ret).To(gomega.Equal(true))
}

func TestCompareDeployable(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	newDepl := d.DeepCopy()
	b := CompareDeployable(d, newDepl)
	g.Expect(b).To(gomega.Equal(true))

	newDepl.Spec.Channels = []string{"test"}
	b = CompareDeployable(d, newDepl)
	g.Expect(b).To(gomega.Equal(false))
}

func TestClusterFromResourceObjects(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	newDepl := d.DeepCopy()

	cl := GetClusterFromResourceObject(nil)
	g.Expect(cl).To(gomega.BeNil())

	cl = GetClusterFromResourceObject(newDepl)
	g.Expect(cl).To(gomega.BeNil())

	ann := make(map[string]string)
	ann[appv1alpha1.AnnotationManagedCluster] = dplns + "/" + dplname
	newDepl.Annotations = ann
	cl = GetClusterFromResourceObject(newDepl)
	g.Expect(cl).NotTo(gomega.BeNil())
	g.Expect(cl.Namespace).To(gomega.Equal(dplns))
	g.Expect(cl.Name).To(gomega.Equal(dplname))
}

func TestGetHostDeployableFromObject(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	newDepl := d.DeepCopy()

	hd := GetHostDeployableFromObject(nil)
	g.Expect(hd).To(gomega.BeNil())

	hd = GetHostDeployableFromObject(newDepl)
	g.Expect(hd).To(gomega.BeNil())

	ann := make(map[string]string)
	ann[appv1alpha1.AnnotationHosting] = dplns + "/" + dplname
	newDepl.Annotations = ann
	hd = GetHostDeployableFromObject(newDepl)
	g.Expect(hd).NotTo(gomega.BeNil())
	g.Expect(hd.Namespace).To(gomega.Equal(dplns))
	g.Expect(hd.Name).To(gomega.Equal(dplname))
}

func TestGetUnstructuredTemplateFromDeployable(t *testing.T) {
	g := gomega.NewWithT(t)

	newDepl := d.DeepCopy()

	tpl, er := GetUnstructuredTemplateFromDeployable(newDepl)
	g.Expect(er).NotTo(gomega.HaveOccurred())
	g.Expect(tpl).NotTo(gomega.BeNil())

	newDepl.Spec.Template = nil
	_, er = GetUnstructuredTemplateFromDeployable(newDepl)
	g.Expect(er).To(gomega.HaveOccurred())
}

func TestIsDependencyDeployable(t *testing.T) {
	g := gomega.NewWithT(t)

	newDepl := d.DeepCopy()

	b := IsDependencyDeployable(newDepl)
	g.Expect(b).To(gomega.Equal(false))

	ann := make(map[string]string)
	ann[appv1alpha1.AnnotationHosting] = dplns + "/" + dplname
	newDepl.Annotations = ann
	newDepl.GenerateName = "Test"
	b = IsDependencyDeployable(newDepl)
	g.Expect(b).To(gomega.Equal(true))
}

func TestPrepareInstance(t *testing.T) {
	g := gomega.NewWithT(t)

	newDepl := d.DeepCopy()

	b := PrepareInstance(newDepl)
	g.Expect(b).To(gomega.Equal(true))

	ann := make(map[string]string)
	ann[appv1alpha1.AnnotationLocal] = "false"
	ann[appv1alpha1.AnnotationManagedCluster] = dplns + "/" + dplname
	newDepl.Annotations = ann
	b = PrepareInstance(newDepl)
	g.Expect(b).To(gomega.Equal(false))
}
