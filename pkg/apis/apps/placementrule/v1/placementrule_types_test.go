/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"testing"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	pkgKey = types.NamespacedName{
		Name:      "testpkgstatus",
		Namespace: "default",
	}

	pdAlpha = PlacementDecision{
		ClusterName:      "cluster-1",
		ClusterNamespace: "cluster1-ns",
	}

	pdBeta = PlacementDecision{
		ClusterName:      "cluster-2",
		ClusterNamespace: "cluster2-ns",
	}

	prStatus = PlacementRuleStatus{
		Decisions: []PlacementDecision{pdAlpha, pdBeta},
	}

	prClusterSelector = GenericPlacementFields{
		ClusterSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"name": "cluster-1"},
		},
	}

	prSpec = PlacementRuleSpec{
		GenericPlacementFields: prClusterSelector,
	}

	placementRule = &PlacementRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pkgKey.Name,
			Namespace: pkgKey.Namespace,
		},
		Spec:   prSpec,
		Status: prStatus,
	}
)

func TestPlacementRulev1(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Create and Get
	fetched := &PlacementRule{ObjectMeta: metav1.ObjectMeta{
		Name:      pkgKey.Name,
		Namespace: pkgKey.Namespace,
	},
		Spec:   prSpec,
		Status: prStatus}

	g.Expect(c.Create(context.TODO(), placementRule)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), pkgKey, fetched)).NotTo(gomega.HaveOccurred())

	// Test Delete
	g.Expect(c.Delete(context.TODO(), fetched)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), pkgKey, fetched)).To(gomega.HaveOccurred())
}
