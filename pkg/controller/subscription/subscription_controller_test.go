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

package subscription

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	workv1 "open-cluster-management.io/api/work/v1"
	chnv1alpha1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	plv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var c client.Client

var (
	chnkey = types.NamespacedName{
		Name:      "test-chn",
		Namespace: "test-chn-namespace",
	}

	chnRef = &corev1.ObjectReference{
		Name: chnkey.Name,
	}

	channel = &chnv1alpha1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      chnkey.Name,
			Namespace: chnkey.Namespace,
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type:         chnv1alpha1.ChannelTypeNamespace,
			ConfigMapRef: chnRef,
			SecretRef:    chnRef,
		},
	}

	chnsec = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      chnkey.Name,
			Namespace: chnkey.Namespace,
		},
	}

	chncfg = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      chnkey.Name,
			Namespace: chnkey.Namespace,
		},
	}
)

var (
	subkey = types.NamespacedName{
		Name:      "test-sub",
		Namespace: "test-sub-namespace",
	}

	subRef = &corev1.LocalObjectReference{
		Name: subkey.Name,
	}

	subcfg = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subkey.Name,
			Namespace: subkey.Namespace,
		},
	}

	subscription = &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subkey.Name,
			Namespace: subkey.Namespace,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: chnkey.String(),
			PackageFilter: &appv1alpha1.PackageFilter{
				FilterRef: subRef,
			},
		},
	}
)

// used for referred rescource tests

var expectedRequest = reconcile.Request{NamespacedName: subkey}

const timeout = time.Second * 2

func TestReconcileWithoutTimeWindowStatusFlow(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec := newReconciler(mgr, mgr.GetClient(), nil, false)
	recFn, requests := SetupTestReconcile(rec)

	g.Expect(add(mgr, recFn, false)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	chn := channel.DeepCopy()
	chn.Spec.SecretRef = nil
	chn.Spec.ConfigMapRef = nil
	g.Expect(c.Create(context.TODO(), chn)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), chn)

	// Create the Subscription object and expect the Reconcile and Deployment to be created
	instance := subscription.DeepCopy()
	instance.Spec.PackageFilter = nil
	g.Expect(c.Create(context.TODO(), instance)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), instance)

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
}

func TestDoReconcileIncludingErrorPaths(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	instance := subscription.DeepCopy()

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	rec := newReconciler(mgr, mgr.GetClient(), nil, false).(*ReconcileSubscription)

	// no channel
	g.Expect(c.Create(context.TODO(), instance)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), instance)

	g.Expect(rec.doReconcile(instance)).To(gomega.HaveOccurred())

	// no sub filter ref
	chn := channel.DeepCopy()
	g.Expect(c.Create(context.TODO(), chn)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), chn)

	time.Sleep(1 * time.Second)
	g.Expect(rec.doReconcile(instance)).To(gomega.HaveOccurred())

	// has sub filter, no chn sec
	sf := subcfg.DeepCopy()
	g.Expect(c.Create(context.TODO(), sf)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), sf)

	time.Sleep(1 * time.Second)
	g.Expect(rec.doReconcile(instance)).To(gomega.HaveOccurred())

	// has chn sec, no chn cfg
	chsc := chnsec.DeepCopy()
	g.Expect(c.Create(context.TODO(), chsc)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), chsc)

	time.Sleep(1 * time.Second)
	g.Expect(rec.doReconcile(instance)).To(gomega.HaveOccurred())

	// success
	chcf := chncfg.DeepCopy()
	g.Expect(c.Create(context.TODO(), chcf)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), chcf)

	time.Sleep(1 * time.Second)
	g.Expect(rec.doReconcile(instance)).NotTo(gomega.HaveOccurred())

	// switch type
	chn.Spec.Type = chnv1alpha1.ChannelTypeObjectBucket
	g.Expect(c.Update(context.TODO(), chn)).NotTo(gomega.HaveOccurred())

	g.Expect(rec.doReconcile(instance)).NotTo(gomega.HaveOccurred())
}

// TestDoReconcileHelmRepoRechecksClusterAdmin covers the fix that added
// chnv1.ChannelTypeHelmRepo to the set of channel types whose cluster-admin
// annotation is re-verified via utils.IsClusterAdmin on every reconcile.
// Before the fix, Helm-repo subscriptions were excluded from this recheck, so
// a stale "cluster-admin: true" annotation (e.g. left over after the
// subscription is no longer considered hub-propagated) would never be
// de-escalated.
//
// It also covers the fix that gates the spoke-side annotation-trust branch of
// utils.IsClusterAdmin on isSubscriptionFromManifestWork: on a managed
// cluster (no ocm-mutating-webhook), the hosting-subscription and
// cluster-admin annotations are tenant-writable, so a namespace-admin could
// forge both on a locally-created Subscription. A Helm-repo subscription must
// only be trusted with cluster-admin when it is verifiably owned by a
// cluster-scoped AppliedManifestWork that lists it in
// status.appliedResources, proving it was actually propagated by the hub via
// ManifestWork rather than forged locally.
func TestDoReconcileHelmRepoRechecksClusterAdmin(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	rec := newReconciler(mgr, mgr.GetClient(), nil, false).(*ReconcileSubscription)

	helmChnKey := types.NamespacedName{Name: "test-helmrepo-chn", Namespace: chnkey.Namespace}
	helmChn := &chnv1alpha1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmChnKey.Name,
			Namespace: helmChnKey.Namespace,
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type:     chnv1alpha1.ChannelTypeHelmRepo,
			Pathname: "https://charts.example.com/",
		},
	}
	g.Expect(c.Create(context.TODO(), helmChn)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), helmChn)

	instance := &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-helmrepo-clusteradmin-recheck",
			Namespace: subkey.Namespace,
			Annotations: map[string]string{
				appv1alpha1.AnnotationHosting:      "hub-namespace/hub-sub",
				appv1alpha1.AnnotationClusterAdmin: "true",
			},
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: helmChnKey.String(),
		},
	}
	g.Expect(c.Create(context.TODO(), instance)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), instance)

	// The subscription is (simulated as) propagated from the hub and there is
	// no ACM mutating webhook in this test environment, but the
	// hosting-subscription/cluster-admin annotations are tenant-forgeable on
	// the spoke and there is no AppliedManifestWork ownerReference proving
	// this Subscription was actually applied by the work agent. IsClusterAdmin
	// must not trust the forged annotation, and doReconcile must strip it.
	g.Expect(rec.doReconcile(instance)).NotTo(gomega.HaveOccurred())
	g.Expect(instance.GetAnnotations()[appv1alpha1.AnnotationClusterAdmin]).NotTo(gomega.Equal("true"))

	// Simulate a legitimate hub propagation: the OCM work agent applies the
	// Subscription with an ownerReference to a cluster-scoped
	// AppliedManifestWork and records this Subscription in its
	// status.appliedResources. A namespace-admin tenant cannot create or
	// update cluster-scoped AppliedManifestWork resources, so this proves the
	// Subscription was legitimately delivered by the hub.
	amw := &workv1.AppliedManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fakehubhash-helmrepo-clusteradmin-recheck-work",
		},
		Spec: workv1.AppliedManifestWorkSpec{
			HubHash:          "fakehubhash",
			ManifestWorkName: "helmrepo-clusteradmin-recheck-work",
		},
	}
	g.Expect(c.Create(context.TODO(), amw)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), amw)

	// c is a cache-backed client (mgr.GetClient()), so wait for the newly
	// created AppliedManifestWork to appear in the informer cache before
	// attempting to update its status, otherwise the Get inside
	// Status().Update() can race the cache sync and return NotFound.
	amwKey := types.NamespacedName{Name: amw.GetName()}
	g.Eventually(func() error {
		return c.Get(context.TODO(), amwKey, &workv1.AppliedManifestWork{})
	}, timeout).Should(gomega.Succeed())

	amw.Status = workv1.AppliedManifestWorkStatus{
		AppliedResources: []workv1.AppliedManifestResourceMeta{
			{
				ResourceIdentifier: workv1.ResourceIdentifier{
					Group:     appv1alpha1.SchemeGroupVersion.Group,
					Resource:  "subscriptions",
					Namespace: instance.GetNamespace(),
					Name:      instance.GetName(),
				},
				Version: appv1alpha1.SchemeGroupVersion.Version,
			},
		},
	}
	g.Expect(c.Status().Update(context.TODO(), amw)).NotTo(gomega.HaveOccurred())

	// c is a cache-backed client, so wait for the status update to appear in
	// the informer cache before relying on it in doReconcile below, otherwise
	// the AppliedManifestWork ownership check can race the cache sync and
	// see a stale (empty) status.appliedResources.
	g.Eventually(func() []workv1.AppliedManifestResourceMeta {
		updated := &workv1.AppliedManifestWork{}

		if err := c.Get(context.TODO(), amwKey, updated); err != nil {
			return nil
		}

		return updated.Status.AppliedResources
	}, timeout).ShouldNot(gomega.BeEmpty())

	annotations := instance.GetAnnotations()
	annotations[appv1alpha1.AnnotationClusterAdmin] = "true"
	instance.SetAnnotations(annotations)
	instance.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: workv1.GroupVersion.String(),
		Kind:       "AppliedManifestWork",
		Name:       amw.GetName(),
		UID:        amw.GetUID(),
	}})

	// Now that the AppliedManifestWork ownerReference verifiably lists this
	// Subscription, the cluster-admin annotation is trusted and kept set.
	g.Expect(rec.doReconcile(instance)).NotTo(gomega.HaveOccurred())
	g.Expect(instance.GetAnnotations()[appv1alpha1.AnnotationClusterAdmin]).To(gomega.Equal("true"))

	// Simulate the subscription no longer being considered hub-propagated
	// (e.g. the hosting-subscription annotation is gone) while the
	// cluster-admin annotation is still stale/true from before. If Helm-repo
	// subscriptions are correctly rechecked on every reconcile, the stale
	// annotation must be removed.
	annotations = instance.GetAnnotations()
	delete(annotations, appv1alpha1.AnnotationHosting)
	instance.SetAnnotations(annotations)

	g.Expect(rec.doReconcile(instance)).NotTo(gomega.HaveOccurred())
	g.Expect(instance.GetAnnotations()[appv1alpha1.AnnotationClusterAdmin]).NotTo(gomega.Equal("true"))
}

type testClock struct {
	timestamp string
}

func (c *testClock) now() time.Time {
	t, err := time.Parse(time.UnixDate, c.timestamp)
	if err != nil {
		time.Now()
	}

	return t
}

func TestReconcileWithTimeWindowStatusFlow(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	g.Expect(mgr.GetCache().WaitForCacheSync(ctx)).Should(gomega.BeTrue())

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	chn := channel.DeepCopy()
	chn.Spec.SecretRef = nil
	chn.Spec.ConfigMapRef = nil

	g.Expect(c.Create(context.TODO(), chn)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), chn)

	// Create the Subscription object and expect the Reconcile and Deployment to be created
	var tests = []struct {
		name                    string
		curTime                 string
		given                   *appv1alpha1.Subscription
		expectedReconcileResult Reconciliation
		expectedSubMsg          string
	}{
		{
			name: "without time window",
			given: &appv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subkey.Name,
					Namespace: subkey.Namespace,
				},
				Spec: appv1alpha1.SubscriptionSpec{
					Channel: chnkey.String(),
				},
			},
			expectedReconcileResult: Reconciliation{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      subkey.Name,
						Namespace: subkey.Namespace,
					},
				},
			},
			expectedSubMsg: subscriptionActive,
		},
		{
			name:    "within time window",
			curTime: "Sun Nov  3 12:00:00 UTC 2019",
			given: &appv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subkey.Name,
					Namespace: subkey.Namespace,
				},
				Spec: appv1alpha1.SubscriptionSpec{
					Channel: chnkey.String(),
					TimeWindow: &appv1alpha1.TimeWindow{
						WindowType: "active",
						Daysofweek: []string{},
						Hours: []appv1alpha1.HourRange{
							{Start: "10:00AM", End: "5:00PM"},
						},
					},
				},
			},
			expectedReconcileResult: Reconciliation{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      subkey.Name,
						Namespace: subkey.Namespace,
					},
				},
				result: reconcile.Result{
					RequeueAfter: 5*time.Hour + 1*time.Minute,
				},
			},
			expectedSubMsg: subscriptionActive,
		},
		{
			name:    "outside time window",
			curTime: "Sun Nov  3 09:00:00 UTC 2019",
			given: &appv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subkey.Name,
					Namespace: subkey.Namespace,
				},
				Spec: appv1alpha1.SubscriptionSpec{
					Channel: chnkey.String(),
					TimeWindow: &appv1alpha1.TimeWindow{
						WindowType: "active",
						Daysofweek: []string{},
						Hours: []appv1alpha1.HourRange{
							{Start: "10:00AM", End: "5:00PM"},
						},
					},
				},
			},
			expectedReconcileResult: Reconciliation{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      subkey.Name,
						Namespace: subkey.Namespace,
					},
				},
				result: reconcile.Result{
					RequeueAfter: 1*time.Hour + 1*time.Minute,
				},
			},
			expectedSubMsg: subscriptionBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run the reconciler in standalone mode
			rec := spyReconciler(mgr, mgr.GetClient(), nil, (&testClock{tt.curTime}).now, true)
			recFn, reconciliation := ReconcilerSpy(rec)

			g.Expect(add(mgr, recFn, false)).NotTo(gomega.HaveOccurred())

			// Set the subscription placement to be local so that it is reconciled.
			pl := &plv1.Placement{}
			l := true
			pl.Local = &l
			tt.given.Spec.Placement = pl
			g.Expect(c.Create(context.TODO(), tt.given)).NotTo(gomega.HaveOccurred())

			g.Eventually(reconciliation, timeout).Should(gomega.Receive(gomega.Equal(tt.expectedReconcileResult)))

			got := &appv1alpha1.Subscription{}
			givenObjKey := types.NamespacedName{Name: tt.given.GetName(), Namespace: tt.given.GetNamespace()}

			g.Expect(c.Get(context.TODO(), givenObjKey, got)).NotTo(gomega.HaveOccurred())
			gotMsg := got.Status.Message

			if gotMsg != tt.expectedSubMsg {
				// Changed Errorf to Logf for now
				t.Logf("(%v): expected %s, actual %s", tt.given, tt.expectedSubMsg, gotMsg)
			}

			c.Delete(context.TODO(), tt.given)

			time.Sleep(time.Second * 2)
		})
	}
}
