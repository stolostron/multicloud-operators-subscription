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

package v1

import (
	"testing"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

var (
	key = types.NamespacedName{
		Name:      "foo",
		Namespace: "default",
	}

	payload = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
	}

	pov = PackageOverride{
		RawExtension: runtime.RawExtension{
			Object: payload,
		},
	}

	pkgovr = &Overrides{
		PackageName:      "p1",
		PackageOverrides: []PackageOverride{pov},
	}

	sub = &Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: SubscriptionSpec{
			PackageFilter: &PackageFilter{
				Version: "1.0.0",
			},
			PackageOverrides: []*Overrides{
				pkgovr,
			},
		},
	}
)

func TestStorageSubscription(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Create
	fetched := &Subscription{}

	created := sub.DeepCopy()
	g.Expect(c.Create(context.TODO(), created)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), key, fetched)).NotTo(gomega.HaveOccurred())

	// cleanup object of rawextension for comparison
	created.Spec.PackageOverrides[0].PackageOverrides[0].RawExtension.Object = nil

	g.Expect(fetched).To(gomega.Equal(created))

	// Test Updating the Labels
	updated := fetched.DeepCopy()
	updated.Labels = map[string]string{"hello": "world"}

	g.Expect(c.Update(context.TODO(), updated)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), key, fetched)).NotTo(gomega.HaveOccurred())
	g.Expect(fetched).To(gomega.Equal(updated))

	// Test Delete
	g.Expect(c.Delete(context.TODO(), fetched)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), key, fetched)).To(gomega.HaveOccurred())
}
