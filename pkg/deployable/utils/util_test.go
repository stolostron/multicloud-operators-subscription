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
	"errors"
	"testing"
	"time"

	"github.com/onsi/gomega"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestEventlog(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec, err := NewEventRecorder(cfg, mgr.GetScheme())
	g.Expect(err).NotTo(gomega.HaveOccurred())

	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}
	g.Expect(c.Create(context.TODO(), obj)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), obj)

	rec.RecordEvent(obj, "testreason", "testmsg", nil)
	rec.RecordEvent(obj, "testreason", "testmsg", errors.New("testeventerr"))

	time.Sleep(1 * time.Second)
}

func TestConvertLabel(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	namereq := metav1.LabelSelectorRequirement{}
	namereq.Key = "name"
	namereq.Operator = metav1.LabelSelectorOpIn
	namereq.Values = []string{"postrollingendpoint"}

	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{namereq},
	}

	clSelector, err := ConvertLabels(labelSelector)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(clSelector).NotTo(gomega.BeElementOf(nil, labels.Nothing()))

	clSelector, err = ConvertLabels(nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(clSelector).To(gomega.Equal(labels.Everything()))
}

func TestCheckAndInstallCRD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	err := CheckAndInstallCRD(cfg, "../../deploy/crds/v1beta1/apps.open-cluster-management.io_deployables_crd_v1beta1.yaml")
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestGenerateOverrides(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	srcD := d.DeepCopy()
	destD := d.DeepCopy()

	o := GenerateOverrides(srcD, destD)
	g.Expect(o).To(gomega.BeEmpty())

	destD.Spec.Template = &runtime.RawExtension{
		Object: &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind: "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "payload2",
			},
		},
	}
	o = GenerateOverrides(srcD, destD)
	g.Expect(o).To(gomega.HaveLen(1))
}

func TestOverrideTemplate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	tIn := &unstructured.Unstructured{}
	tOut, er := OverrideTemplate(tIn, nil)
	g.Expect(er).NotTo(gomega.HaveOccurred())
	g.Expect(tOut).NotTo(gomega.BeNil())
	g.Expect(tOut.Object).To(gomega.BeNil())

	override := appv1alpha1.ClusterOverride{
		RawExtension: runtime.RawExtension{
			Raw: []byte("{\"path\": \".\", \"value\": {\"foo\": \"bar\"}}"),
		},
	}
	tOut, er = OverrideTemplate(tIn, []appv1alpha1.ClusterOverride{override})
	g.Expect(er).NotTo(gomega.HaveOccurred())
	g.Expect(tOut).NotTo(gomega.BeNil())
	g.Expect(tOut.Object).NotTo(gomega.BeNil())
	g.Expect(tOut.Object["foo"]).To(gomega.Equal("bar"))
}
