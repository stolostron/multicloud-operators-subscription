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

package appsubsummary

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	appsubReportV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	managedClusterView "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/view/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	appsubReport1Failed = appsubReportV1alpha1.SubscriptionReport{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
			Kind:       "SubscriptionReport",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"apps.open-cluster-management.io/cluster": "true",
			},
			Name:      "cluster1",
			Namespace: "cluster1",
		},
		ReportType: "Cluster",
		Results: []*appsubReportV1alpha1.SubscriptionReportResult{
			{
				Source: "app1-ns/app1",
				Result: appsubReportV1alpha1.SubscriptionResult("failed"),
			},
		},
	}

	appsubReport1Deployed = appsubReportV1alpha1.SubscriptionReport{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
			Kind:       "SubscriptionReport",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"apps.open-cluster-management.io/cluster": "true",
			},
			Name:      "cluster1",
			Namespace: "cluster1",
		},
		ReportType: "Cluster",
		Results: []*appsubReportV1alpha1.SubscriptionReportResult{
			{
				Source: "app1-ns/app1",
				Result: appsubReportV1alpha1.SubscriptionResult("deployed"),
			},
		},
	}

	appsubReport2 = appsubReportV1alpha1.SubscriptionReport{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
			Kind:       "SubscriptionReport",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"apps.open-cluster-management.io/cluster": "true",
			},
			Name:      "cluster2",
			Namespace: "cluster2",
		},
		ReportType: "Cluster",
		Results: []*appsubReportV1alpha1.SubscriptionReportResult{
			{
				Source: "app1-ns/app1",
				Result: appsubReportV1alpha1.SubscriptionResult("failed"),
			},
		},
	}

	view1Key = types.NamespacedName{
		Name:      "app1-ns-app1",
		Namespace: "cluster1",
	}

	view2Key = types.NamespacedName{
		Name:      "app1-ns-app1",
		Namespace: "cluster2",
	}
)

func TestRefreshManagedClusterViews(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	Add(mgr, 5)

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Test 1: create appsubReport1Failed, expect app1 managedClusterView is created in the cluster1 NS
	g.Expect(c.Create(context.TODO(), &appsubReport1Failed)).NotTo(gomega.HaveOccurred())

	time.Sleep(time.Second * 10)

	view1 := &managedClusterView.ManagedClusterView{}
	err = c.Get(context.TODO(), view1Key, view1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test 1.1: delete appsubReport1Failed, create appsubReport1Deployed, expect app1 managedClusterView is removed from the cluster1 NS
	g.Expect(c.Delete(context.TODO(), &appsubReport1Failed)).NotTo(gomega.HaveOccurred())

	g.Expect(c.Create(context.TODO(), &appsubReport1Deployed)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), &appsubReport1Deployed)

	time.Sleep(time.Second * 10)

	view11 := &managedClusterView.ManagedClusterView{}
	err = c.Get(context.TODO(), view1Key, view11)
	g.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

	// Test 2: create appsubReport2, expect app1 managedClusterView is created in the cluster2 NS
	g.Expect(c.Create(context.TODO(), &appsubReport2)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), &appsubReport2)

	time.Sleep(time.Second * 10)

	view2 := &managedClusterView.ManagedClusterView{}
	err = c.Get(context.TODO(), view2Key, view2)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	view1 = &managedClusterView.ManagedClusterView{}
	err = c.Get(context.TODO(), view1Key, view1)
	g.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
}
