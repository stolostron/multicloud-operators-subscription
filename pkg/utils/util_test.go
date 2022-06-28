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
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestNamespacedNameFormat(t *testing.T) {
	n := "tname"
	ns := "tnamespace"
	nsn := types.NamespacedName{
		Name:      n,
		Namespace: ns,
	}

	fnsn := NamespacedNameFormat(nsn.String())
	if !reflect.DeepEqual(nsn, fnsn) {
		t.Errorf("Format NamespacedName string failed.\n\tExpect:%v\n\tResult:%v", nsn, fnsn)
	}

	fnsn = NamespacedNameFormat("incorrect format")
	if !reflect.DeepEqual(types.NamespacedName{}, fnsn) {
		t.Errorf("Format NamespacedName string failed.\n\tExpect:%v\n\tResult:%v", types.NamespacedName{}, fnsn)
	}
}

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

func TestValidateK8sLabel(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	APIServer := "_-.api.xili-aws-cluster-pool-tg2g4.dev06.red-chesterfield.com_-."
	expectedServerLabel := "api.xili-aws-cluster-pool-tg2g4.dev06.red-chesterfield.com"

	ServerLabel := ValidateK8sLabel(APIServer)

	g.Expect(ServerLabel).Should(gomega.Equal(expectedServerLabel))
}
