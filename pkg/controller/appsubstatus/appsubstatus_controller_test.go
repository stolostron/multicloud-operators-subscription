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

package appsubstatus

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
)

var c client.Client

var (
	subkey = types.NamespacedName{
		Name:      "test-pkgstatus",
		Namespace: "test-pkgstatus-namespace",
	}

	summarykey = types.NamespacedName{
		Name:      "test-pkgstatus.status",
		Namespace: "test-pkgstatus-namespace",
	}

	labels = map[string]string{"apps.open-cluster-management.io/hosting-subscription": subkey.Namespace + "." + subkey.Name}

	// Package status for cluster1
	pkgkeyCluster1 = types.NamespacedName{
		Name:      "test-pkgstatus",
		Namespace: "cluster1",
	}

	saStatusCluster1 = &v1alpha1.SubscriptionUnitStatus{
		Name:      "testsa",
		Kind:      "ServiceAccount",
		Namespace: pkgkeyCluster1.Namespace,
		Phase:     v1alpha1.PackageDeployed,
		Message:   "Deployed successfully",
		LastUpdateTime: metav1.Time{
			Time: time.Now(),
		},
	}

	pkgstatusCluster1 = &v1alpha1.SubscriptionPackageStatus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SubscriptionPackageStatus",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pkgkeyCluster1.Name,
			Namespace: pkgkeyCluster1.Namespace,
			Labels:    labels,
		}, Statuses: v1alpha1.SubscriptionClusterStatusMap{
			SubscriptionPackageStatus: []v1alpha1.SubscriptionUnitStatus{*saStatusCluster1},
		},
	}

	// Package status for cluster2
	pkgkeyCluster2 = types.NamespacedName{
		Name:      "test-pkgstatus",
		Namespace: "cluster2",
	}

	saStatusCluster2 = &v1alpha1.SubscriptionUnitStatus{
		Name:      "testsa",
		Kind:      "ServiceAccount",
		Namespace: pkgkeyCluster2.Namespace,
		Phase:     v1alpha1.PackageDeployFailed,
		Message:   "Failed to deploy",
		LastUpdateTime: metav1.Time{
			Time: time.Now(),
		},
	}

	pkgstatusCluster2 = &v1alpha1.SubscriptionPackageStatus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SubscriptionPackageStatus",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pkgkeyCluster2.Name,
			Namespace: pkgkeyCluster2.Namespace,
			Labels:    labels,
		}, Statuses: v1alpha1.SubscriptionClusterStatusMap{
			SubscriptionPackageStatus: []v1alpha1.SubscriptionUnitStatus{*saStatusCluster2},
		},
	}

	// Package status for cluster3
	pkgkeyCluster3 = types.NamespacedName{
		Name:      "test-pkgstatus",
		Namespace: "cluster3",
	}

	saStatusCluster3 = &v1alpha1.SubscriptionUnitStatus{
		Name:      "testsa",
		Kind:      "ServiceAccount",
		Namespace: pkgkeyCluster3.Namespace,
		Phase:     v1alpha1.PackagePropagationFailed,
		Message:   "Failed to propagate",
		LastUpdateTime: metav1.Time{
			Time: time.Now(),
		},
	}

	pkgstatusCluster3 = &v1alpha1.SubscriptionPackageStatus{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SubscriptionPackageStatus",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pkgkeyCluster3.Name,
			Namespace: pkgkeyCluster3.Namespace,
			Labels:    labels,
		}, Statuses: v1alpha1.SubscriptionClusterStatusMap{
			SubscriptionPackageStatus: []v1alpha1.SubscriptionUnitStatus{*saStatusCluster3},
		},
	}
)

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec := newReconciler(mgr).(*ReconcileAppSubStatus)
	recFn, _ := SetupTestReconcile(rec)

	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Create the appSubPackageStatus
	instanceCl1 := pkgstatusCluster1.DeepCopy()
	g.Expect(c.Create(context.TODO(), instanceCl1)).NotTo(gomega.HaveOccurred())

	time.Sleep(1 * time.Second)

	fetched := &v1alpha1.SubscriptionPackageStatus{}
	g.Expect(c.Get(context.TODO(), pkgkeyCluster1, fetched)).NotTo(gomega.HaveOccurred())

	// Generate appsubsummarystatus
	g.Expect(rec.generateAppSubSummary(subkey.Namespace, subkey.Name, instanceCl1)).NotTo(gomega.HaveOccurred())

	time.Sleep(1 * time.Second)

	// Verify appsubsummary status
	summaryStatusList := &v1alpha1.SubscriptionSummaryStatusList{}
	listopts := &client.ListOptions{}
	listopts.Namespace = subkey.Namespace
	g.Expect(c.List(context.TODO(), summaryStatusList, listopts)).NotTo(gomega.HaveOccurred())
	g.Expect(len(summaryStatusList.Items)).To(gomega.Equal(1))

	summaryStatus := &v1alpha1.SubscriptionSummaryStatus{}
	g.Expect(c.Get(context.TODO(), summarykey, summaryStatus)).NotTo(gomega.HaveOccurred())
	g.Expect(summaryStatus.Summary.DeployedSummary.Count).To(gomega.Equal(1))
	g.Expect(summaryStatus.Summary.DeployedSummary.Clusters[0]).To(gomega.Equal(pkgkeyCluster1.Namespace))
	g.Expect(summaryStatus.Summary.DeployFailedSummary.Count).To(gomega.Equal(0))
	g.Expect(summaryStatus.Summary.PropagationFailedSummary.Count).To(gomega.Equal(0))

	// Create package status for cluster 2
	instanceCl2 := pkgstatusCluster2.DeepCopy()
	g.Expect(c.Create(context.TODO(), instanceCl2)).NotTo(gomega.HaveOccurred())
	time.Sleep(1 * time.Second)

	g.Expect(c.Get(context.TODO(), pkgkeyCluster2, fetched)).NotTo(gomega.HaveOccurred())

	// Generate appsubsummarystatus
	g.Expect(rec.generateAppSubSummary(subkey.Namespace, subkey.Name, instanceCl1)).NotTo(gomega.HaveOccurred())
	time.Sleep(1 * time.Second)
	g.Expect(c.Get(context.TODO(), summarykey, summaryStatus)).NotTo(gomega.HaveOccurred())
	g.Expect(summaryStatus.Summary.DeployedSummary.Count).To(gomega.Equal(1))
	g.Expect(summaryStatus.Summary.DeployedSummary.Clusters[0]).To(gomega.Equal(pkgkeyCluster1.Namespace))
	g.Expect(summaryStatus.Summary.DeployFailedSummary.Count).To(gomega.Equal(1))
	g.Expect(summaryStatus.Summary.DeployFailedSummary.Clusters[0]).To(gomega.Equal(pkgkeyCluster2.Namespace))
	g.Expect(summaryStatus.Summary.PropagationFailedSummary.Count).To(gomega.Equal(0))

	// Create package status for cluster 3
	instanceCl3 := pkgstatusCluster3.DeepCopy()
	g.Expect(c.Create(context.TODO(), instanceCl3)).NotTo(gomega.HaveOccurred())
	time.Sleep(1 * time.Second)

	g.Expect(c.Get(context.TODO(), pkgkeyCluster3, fetched)).NotTo(gomega.HaveOccurred())

	// Generate appsubsummarystatus
	g.Expect(rec.generateAppSubSummary(subkey.Namespace, subkey.Name, instanceCl1)).NotTo(gomega.HaveOccurred())
	time.Sleep(1 * time.Second)
	g.Expect(c.Get(context.TODO(), summarykey, summaryStatus)).NotTo(gomega.HaveOccurred())
	g.Expect(summaryStatus.Summary.DeployedSummary.Count).To(gomega.Equal(1))
	g.Expect(summaryStatus.Summary.DeployedSummary.Clusters[0]).To(gomega.Equal(pkgkeyCluster1.Namespace))
	g.Expect(summaryStatus.Summary.DeployFailedSummary.Count).To(gomega.Equal(1))
	g.Expect(summaryStatus.Summary.DeployFailedSummary.Clusters[0]).To(gomega.Equal(pkgkeyCluster2.Namespace))
	g.Expect(summaryStatus.Summary.PropagationFailedSummary.Count).To(gomega.Equal(1))
	g.Expect(summaryStatus.Summary.PropagationFailedSummary.Clusters[0]).To(gomega.Equal(pkgkeyCluster3.Namespace))

	// Delete the appSubPackageStatus
	g.Expect(c.Delete(context.TODO(), instanceCl1)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Delete(context.TODO(), instanceCl2)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Delete(context.TODO(), instanceCl3)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Delete(context.TODO(), summaryStatus)).NotTo(gomega.HaveOccurred())
}
