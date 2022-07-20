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

	. "github.com/onsi/gomega"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestPrepareOverrides(t *testing.T) {
	g := NewGomegaWithT(t)

	var cluster = types.NamespacedName{
		Name:      "test",
		Namespace: "default",
	}
	var appsub = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
		},
	}
	appsub.Spec.Overrides = append(appsub.Spec.Overrides, appv1.ClusterOverrides{ClusterName: "test", ClusterOverrides: nil})

	// find matching override
	var overrides []appv1.ClusterOverride

	overrideMap, err := PrepareOverrides(cluster, appsub)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(overrideMap).To(Equal(overrides))

	// nil appsub
	overrideMap, err = PrepareOverrides(cluster, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(overrideMap).To(BeNil())
}
