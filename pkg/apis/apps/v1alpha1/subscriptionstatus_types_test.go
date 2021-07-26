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

package v1alpha1

import (
	"testing"
	"time"

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

	saStatus = &SubscriptionUnitStatus{
		Name:      "testsa",
		Kind:      "ServiceAccount",
		Namespace: "ns-sub-1",
		Phase:     "Deployed",
		Message:   "Deployed successfully",
		LastUpdateTime: metav1.Time{
			Time: time.Now(),
		},
	}

	crStatus = &SubscriptionUnitStatus{
		Name:      "testcr",
		Kind:      "ClusterRole",
		Namespace: "ns-sub-1",
		Phase:     "Deployed",
		Message:   "Deployed successfully",
		LastUpdateTime: metav1.Time{
			Time: time.Now(),
		},
	}

	pkgStatus = &SubscriptionPackageStatus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SubscriptionPackageStatus",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pkgKey.Name,
			Namespace: pkgKey.Namespace,
		},
		Statuses: SubscriptionClusterStatusMap{
			SubscriptionPackageStatus: []SubscriptionUnitStatus{*saStatus},
		},
	}

	summaryKey = types.NamespacedName{
		Name:      "testsummarystatus",
		Namespace: "default",
	}

	summaryStatus = &SubscriptionSummaryStatus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SubscriptionSummaryStatus",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      summaryKey.Name,
			Namespace: summaryKey.Namespace,
		},
		Summary: SubscriptionSummary{
			DeployedSummary: ClusterSummary{
				Count:    1,
				Clusters: []string{"cluster1"},
			},
			DeployFailedSummary: ClusterSummary{
				Count:    1,
				Clusters: []string{"cluster2"},
			},
			PropagationFailedSummary: ClusterSummary{
				Count:    1,
				Clusters: []string{"cluster3"},
			},
		},
	}
)

func TestAppSubPackageStatus(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Create and Get
	fetched := &SubscriptionPackageStatus{}

	created := pkgStatus.DeepCopy()
	g.Expect(c.Create(context.TODO(), created)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), pkgKey, fetched)).NotTo(gomega.HaveOccurred())

	g.Expect(fetched).To(gomega.Equal(created))

	// Test Updating the Labels
	updated := fetched.DeepCopy()
	updated.Statuses.SubscriptionPackageStatus = append(updated.Statuses.SubscriptionPackageStatus, *crStatus)

	g.Expect(c.Update(context.TODO(), updated)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), pkgKey, fetched)).NotTo(gomega.HaveOccurred())
	g.Expect(fetched).To(gomega.Equal(updated))

	// Test Delete
	g.Expect(c.Delete(context.TODO(), fetched)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), pkgKey, fetched)).To(gomega.HaveOccurred())
}

func TestAppSubSummaryStatus(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Create and Get
	fetched := &SubscriptionSummaryStatus{}

	created := summaryStatus.DeepCopy()
	g.Expect(c.Create(context.TODO(), created)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), summaryKey, fetched)).NotTo(gomega.HaveOccurred())

	g.Expect(fetched).To(gomega.Equal(created))

	// Test Updating the Labels
	updated := fetched.DeepCopy()
	updated.Summary.DeployedSummary.Clusters = append(updated.Summary.DeployedSummary.Clusters, "cluster5")
	updated.Summary.DeployedSummary.Count = 2

	g.Expect(c.Update(context.TODO(), updated)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), summaryKey, fetched)).NotTo(gomega.HaveOccurred())
	g.Expect(fetched).To(gomega.Equal(updated))

	// Test Delete
	g.Expect(c.Delete(context.TODO(), fetched)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), summaryKey, fetched)).To(gomega.HaveOccurred())
}
