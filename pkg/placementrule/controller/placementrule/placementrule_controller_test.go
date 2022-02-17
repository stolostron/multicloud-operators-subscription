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

package placementrule

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterapi "open-cluster-management.io/api/cluster/v1alpha1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
)

var c client.Client

var (
	prulename = "foo-prule"
	prulens   = "default"
	prulekey  = types.NamespacedName{
		Name:      prulename,
		Namespace: prulens,
	}
	pdkey = types.NamespacedName{
		Name:      prulename + "-decision-1",
		Namespace: prulens,
	}
)

var expectedRequest = reconcile.Request{NamespacedName: prulekey}

const timeout = time.Second * 5

var (
	clusteralpha = &spokeClusterV1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "clusteralpha",
			Labels: map[string]string{
				"name": "clusteralpha",
				"key1": "value1",
				"key2": "value",
			},
		},
	}
	clusterbeta = &spokeClusterV1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "clusterbeta",
			Labels: map[string]string{
				"name": "clusterbeta",
				"key1": "value2",
				"key2": "value",
			},
		},
	}

	clusters = []*spokeClusterV1.ManagedCluster{clusteralpha, clusterbeta}
)

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Create the PlacementRule object and expect the Reconcile
	instance := &appv1alpha1.PlacementRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prulename,
			Namespace: prulens,
		},
	}
	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), instance)

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
}

func TestClusterNames(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	for _, cl := range clusters {
		clinstance := cl.DeepCopy()

		err = c.Create(context.TODO(), clinstance)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		defer c.Delete(context.TODO(), clinstance)
	}

	cl1 := appv1alpha1.GenericClusterReference{Name: clusteralpha.GetName()}
	instance := &appv1alpha1.PlacementRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prulename,
			Namespace: prulens,
		},
		Spec: appv1alpha1.PlacementRuleSpec{
			GenericPlacementFields: appv1alpha1.GenericPlacementFields{
				Clusters: []appv1alpha1.GenericClusterReference{cl1},
			},
		},
	}

	err = c.Create(context.TODO(), instance)
	defer c.Delete(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(1 * time.Second)

	result := &appv1alpha1.PlacementRule{}
	err = c.Get(context.TODO(), prulekey, result)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(result.Status.Decisions) != 1 || result.Status.Decisions[0].ClusterName != clusters[0].Name {
		t.Errorf("Failed to get cluster by name, placementrule: %v", result)
	}

	decision := &clusterapi.PlacementDecision{}

	err = c.Get(ctx, pdkey, decision)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(decision.Status.Decisions) != 1 || decision.Status.Decisions[0].ClusterName != clusters[0].Name {
		t.Errorf("Failed to get cluster by name, placementdecision: %v", decision)
	}
}

func TestClusterLabels(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	for _, cl := range clusters {
		clinstance := cl.DeepCopy()
		err = c.Create(context.TODO(), clinstance)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		defer c.Delete(context.TODO(), clinstance)
	}

	namereq := metav1.LabelSelectorRequirement{}
	namereq.Key = "key1"
	namereq.Operator = metav1.LabelSelectorOpIn

	namereq.Values = []string{"value2"}
	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{namereq},
	}

	instance := &appv1alpha1.PlacementRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prulename,
			Namespace: prulens,
		},
		Spec: appv1alpha1.PlacementRuleSpec{
			GenericPlacementFields: appv1alpha1.GenericPlacementFields{
				ClusterSelector: labelSelector,
			},
		},
	}

	err = c.Create(context.TODO(), instance)
	defer c.Delete(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(1 * time.Second)

	result := &appv1alpha1.PlacementRule{}
	err = c.Get(context.TODO(), prulekey, result)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(result.Status.Decisions) != 1 || result.Status.Decisions[0].ClusterName != clusters[1].Name {
		t.Errorf("Failed to get cluster by label, placementrule: %v", result)
	}

	decision := &clusterapi.PlacementDecision{}

	err = c.Get(ctx, pdkey, decision)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(decision.Status.Decisions) != 1 || decision.Status.Decisions[0].ClusterName != clusters[1].Name {
		t.Errorf("Failed to get cluster by label, placementdecision: %v", result)
	}
}

func TestAllClusters(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	for _, cl := range clusters {
		clinstance := cl.DeepCopy()

		err = c.Create(context.TODO(), clinstance)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		defer c.Delete(context.TODO(), clinstance)
	}

	time.Sleep(6 * time.Second)

	cAlphaKey := types.NamespacedName{
		Name: "clusteralpha",
	}
	cAlpha := &spokeClusterV1.ManagedCluster{}
	err = c.Get(context.TODO(), cAlphaKey, cAlpha)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	cAlpha.Status = spokeClusterV1.ManagedClusterStatus{Conditions: []metav1.Condition{},
		Allocatable: spokeClusterV1.ResourceList{
			spokeClusterV1.ResourceCPU: resource.MustParse("10500m"),
		}}
	err = c.Status().Update(context.TODO(), cAlpha)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	cBetaKey := types.NamespacedName{
		Name: "clusterbeta",
	}
	cBeta := &spokeClusterV1.ManagedCluster{}
	err = c.Get(context.TODO(), cBetaKey, cBeta)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	cBeta.Status = spokeClusterV1.ManagedClusterStatus{Conditions: []metav1.Condition{},
		Allocatable: spokeClusterV1.ResourceList{
			spokeClusterV1.ResourceCPU: resource.MustParse("8"),
		}}
	err = c.Status().Update(context.TODO(), cBeta)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	instance := &appv1alpha1.PlacementRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prulename,
			Namespace: prulens,
		},
		Spec: appv1alpha1.PlacementRuleSpec{
			ResourceHint: &appv1alpha1.ResourceHint{
				Type:  appv1alpha1.ResourceTypeCPU,
				Order: appv1alpha1.SelectionOrderAsce,
			},
		},
	}

	err = c.Create(context.TODO(), instance)
	defer c.Delete(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(1 * time.Second)

	result := &appv1alpha1.PlacementRule{}
	err = c.Get(context.TODO(), prulekey, result)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(result.Status.Decisions) != 2 {
		t.Errorf("Failed to get all clusters, placementrule: %v", result)
	}

	// expect order of first clusterbeta "8" then second clusteralpha "10500m" for asc cpu sort
	if result.Status.Decisions[0].ClusterName == "clusteralpha" {
		t.Errorf("Failed to sort cluster properly, placementrule: %v", result)
	}

	decision := &clusterapi.PlacementDecision{}

	err = c.Get(ctx, pdkey, decision)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(decision.Status.Decisions) != 2 {
		t.Errorf("Failed to get all clusters, placementdecision: %v", result)
	}

	// expect order of first clusterbeta "8" then second clusteralpha "10500m" for asc cpu sort
	if decision.Status.Decisions[0].ClusterName == "clusteralpha" {
		t.Errorf("Failed to sort cluster properly, placementdecision: %v", result)
	}
}

func TestClusterReplica(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	for _, cl := range clusters {
		clinstance := cl.DeepCopy()

		err = c.Create(context.TODO(), clinstance)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		defer c.Delete(context.TODO(), clinstance)
	}

	var rpl int32 = 1

	instance := &appv1alpha1.PlacementRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prulename,
			Namespace: prulens,
		},
		Spec: appv1alpha1.PlacementRuleSpec{
			ClusterReplicas: &rpl,
		},
	}

	err = c.Create(context.TODO(), instance)
	defer c.Delete(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(1 * time.Second)

	result := &appv1alpha1.PlacementRule{}
	err = c.Get(context.TODO(), prulekey, result)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(result.Status.Decisions) != 1 {
		t.Errorf("Failed to get 1 from all clusters, placementrule: %v", result)
	}

	decision := &clusterapi.PlacementDecision{}

	err = c.Get(ctx, pdkey, decision)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(decision.Status.Decisions) != 1 {
		t.Errorf("Failed to get 1 from all clusters, placementdecision: %v", result)
	}
}

func TestClusterChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	clinstance := clusters[0].DeepCopy()
	err = c.Create(context.TODO(), clinstance)

	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), clinstance)

	instance := &appv1alpha1.PlacementRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prulename,
			Namespace: prulens,
		},
	}

	err = c.Create(context.TODO(), instance)
	defer c.Delete(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(5 * time.Second)

	result := &appv1alpha1.PlacementRule{}
	err = c.Get(context.TODO(), prulekey, result)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(result.Status.Decisions) != 1 {
		t.Errorf("Failed to get all(1) clusters, placementrule: %v", result)
	}

	decision := &clusterapi.PlacementDecision{}

	err = c.Get(ctx, pdkey, decision)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(decision.Status.Decisions) != 1 {
		t.Errorf("Failed to get all(1) clusters, placementdecision: %v", result)
	}

	clinstance = clusters[1].DeepCopy()
	err = c.Create(context.TODO(), clinstance)

	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), clinstance)

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(8 * time.Second)

	err = c.Get(context.TODO(), prulekey, result)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(result.Status.Decisions) != 2 {
		t.Errorf("Failed to get all(2) clusters, placementrule: %v", result)
	}

	err = c.Get(ctx, pdkey, decision)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(decision.Status.Decisions) != 2 {
		t.Errorf("Failed to get all(2) clusters, placementdecision: %v", decision)
	}
}
