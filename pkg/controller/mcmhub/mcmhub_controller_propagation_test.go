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

package mcmhub

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	promTestUtils "github.com/prometheus/client_golang/prometheus/testutil"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	channelV1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	placementv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	appsv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/metrics"
)

var _ = Describe("test propagation statuses set by the hub reconciler", func() {
	It("should fail for subscriptions with no placement configured", func() {
		mgr, mgrErr := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
		Expect(mgrErr).NotTo(HaveOccurred())

		sutPropagationTestClient := mgr.GetClient()
		sutPropagationTestReconciler := newReconciler(mgr)

		ctrlErr := add(mgr, sutPropagationTestReconciler)
		Expect(ctrlErr).NotTo(HaveOccurred())

		ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
		mgrStopped := StartTestManager(ctx, mgr, nil)

		defer func() {
			cancel()
			mgrStopped.Wait()
		}()

		metrics.PropagationFailedPullTime.Reset()
		metrics.PropagationSuccessfulPullTime.Reset()

		noPlacementSubscriptionKey := types.NamespacedName{
			Name:      "test-propagation-no-placement-sub",
			Namespace: "propagation-test-cases",
		}
		noPlacementSubscription := &appsv1.Subscription{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Subscription",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      noPlacementSubscriptionKey.Name,
				Namespace: noPlacementSubscriptionKey.Namespace,
				Labels: map[string]string{
					"app":                       noPlacementSubscriptionKey.Name,
					"app.kubernetes.io/part-of": noPlacementSubscriptionKey.Name,
				},
			},
			Spec: appsv1.SubscriptionSpec{
				Channel: "propagation-test-cases/non-existing-channel",
			},
		}

		Expect(sutPropagationTestClient.Create(context.TODO(), noPlacementSubscription)).NotTo(HaveOccurred())
		defer sutPropagationTestClient.Delete(context.TODO(), noPlacementSubscription)

		_, reconcileErr := sutPropagationTestReconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: noPlacementSubscriptionKey})
		Expect(reconcileErr).ToNot(HaveOccurred())

		time.Sleep(2 * time.Second)

		reconciledSubscription := &appsv1.Subscription{}
		Expect(sutPropagationTestClient.Get(context.TODO(), noPlacementSubscriptionKey, reconciledSubscription)).NotTo(HaveOccurred())

		Expect(reconciledSubscription.Status.Phase).To(Equal(appsv1.SubscriptionPropagationFailed))
		Expect(reconciledSubscription.Status.Reason).To(Equal("Placement must be specified"))

		Expect(promTestUtils.CollectAndCount(metrics.PropagationFailedPullTime)).To(Equal(1))
		Expect(promTestUtils.CollectAndCount(metrics.PropagationSuccessfulPullTime)).To(BeZero())
	})

	It("should fail for subscriptions configured for both local and remote placements", func() {
		mgr, mgrErr := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
		Expect(mgrErr).NotTo(HaveOccurred())

		sutPropagationTestClient := mgr.GetClient()
		sutPropagationTestReconciler := newReconciler(mgr)

		ctrlErr := add(mgr, sutPropagationTestReconciler)
		Expect(ctrlErr).NotTo(HaveOccurred())

		ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
		mgrStopped := StartTestManager(ctx, mgr, nil)

		defer func() {
			cancel()
			mgrStopped.Wait()
		}()

		metrics.PropagationFailedPullTime.Reset()
		metrics.PropagationSuccessfulPullTime.Reset()

		wrongPlacementSubscriptionKey := types.NamespacedName{
			Name:      "test-propagation-wrong-placement-sub",
			Namespace: "propagation-test-cases",
		}
		includeLocal := true
		wrongPlacementSubscription := &appsv1.Subscription{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Subscription",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      wrongPlacementSubscriptionKey.Name,
				Namespace: wrongPlacementSubscriptionKey.Namespace,
				Labels: map[string]string{
					"app":                       wrongPlacementSubscriptionKey.Name,
					"app.kubernetes.io/part-of": wrongPlacementSubscriptionKey.Name,
				},
			},
			Spec: appsv1.SubscriptionSpec{
				Channel: "propagation-test-cases/non-existing-channel",
				// placement contains both local and remote
				Placement: &placementv1.Placement{
					Local: &includeLocal,
					PlacementRef: &corev1.ObjectReference{
						Name: "placement-name-goes-here",
					},
				},
			},
		}

		Expect(sutPropagationTestClient.Create(context.TODO(), wrongPlacementSubscription)).NotTo(HaveOccurred())
		defer sutPropagationTestClient.Delete(context.TODO(), wrongPlacementSubscription)

		_, reconcileErr := sutPropagationTestReconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: wrongPlacementSubscriptionKey})
		Expect(reconcileErr).ToNot(HaveOccurred())

		time.Sleep(2 * time.Second)

		reconciledSubscription := &appsv1.Subscription{}
		Expect(sutPropagationTestClient.Get(context.TODO(), wrongPlacementSubscriptionKey, reconciledSubscription)).NotTo(HaveOccurred())

		Expect(reconciledSubscription.Status.Phase).To(Equal(appsv1.SubscriptionPropagationFailed))
		Expect(reconciledSubscription.Status.Reason).To(Equal("local placement and remote placement cannot be used together"))

		Expect(promTestUtils.CollectAndCount(metrics.PropagationFailedPullTime)).To(Equal(1))
		Expect(promTestUtils.CollectAndCount(metrics.PropagationSuccessfulPullTime)).To(BeZero())
	})

	It("should successfully propagate for subscriptions configured for local placement only", func() {
		mgr, mgrErr := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
		Expect(mgrErr).NotTo(HaveOccurred())

		sutPropagationTestClient := mgr.GetClient()
		sutPropagationTestReconciler := newReconciler(mgr)

		ctrlErr := add(mgr, sutPropagationTestReconciler)
		Expect(ctrlErr).NotTo(HaveOccurred())

		ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
		mgrStopped := StartTestManager(ctx, mgr, nil)

		defer func() {
			cancel()
			mgrStopped.Wait()
		}()

		metrics.PropagationFailedPullTime.Reset()
		metrics.PropagationSuccessfulPullTime.Reset()

		localPlacementSubscriptionKey := types.NamespacedName{
			Name:      "test-propagation-local-placement-sub",
			Namespace: "propagation-test-cases",
		}
		includeLocal := true
		localPlacementSubscription := &appsv1.Subscription{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Subscription",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      localPlacementSubscriptionKey.Name,
				Namespace: localPlacementSubscriptionKey.Namespace,
				Labels: map[string]string{
					"app":                       localPlacementSubscriptionKey.Name,
					"app.kubernetes.io/part-of": localPlacementSubscriptionKey.Name,
				},
			},
			Spec: appsv1.SubscriptionSpec{
				// no channel
				Placement: &placementv1.Placement{
					Local: &includeLocal,
				},
			},
		}

		Expect(sutPropagationTestClient.Create(context.TODO(), localPlacementSubscription)).NotTo(HaveOccurred())
		defer sutPropagationTestClient.Delete(context.TODO(), localPlacementSubscription)

		_, reconcileErr := sutPropagationTestReconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: localPlacementSubscriptionKey})
		Expect(reconcileErr).ToNot(HaveOccurred())

		time.Sleep(2 * time.Second)

		reconciledSubscription := &appsv1.Subscription{}
		Expect(sutPropagationTestClient.Get(context.TODO(), localPlacementSubscriptionKey, reconciledSubscription)).NotTo(HaveOccurred())

		Expect(reconciledSubscription.Status.Phase).To(BeEmpty())
		Expect(reconciledSubscription.Status.Reason).To(BeEmpty())

		Expect(promTestUtils.CollectAndCount(metrics.PropagationFailedPullTime)).To(BeZero())
		Expect(promTestUtils.CollectAndCount(metrics.PropagationSuccessfulPullTime)).To(Equal(1))
	})

	It("should fail for subscriptions with a remote placement and no channel", func() {
		mgr, mgrErr := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
		Expect(mgrErr).NotTo(HaveOccurred())

		sutPropagationTestClient := mgr.GetClient()
		sutPropagationTestReconciler := newReconciler(mgr)

		ctrlErr := add(mgr, sutPropagationTestReconciler)
		Expect(ctrlErr).NotTo(HaveOccurred())

		ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
		mgrStopped := StartTestManager(ctx, mgr, nil)

		defer func() {
			cancel()
			mgrStopped.Wait()
		}()

		metrics.PropagationFailedPullTime.Reset()
		metrics.PropagationSuccessfulPullTime.Reset()

		noChannelSubscriptionKey := types.NamespacedName{
			Name:      "test-propagation-no-channel-sub",
			Namespace: "propagation-test-cases",
		}
		noChannelSubscription := &appsv1.Subscription{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Subscription",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      noChannelSubscriptionKey.Name,
				Namespace: noChannelSubscriptionKey.Namespace,
				Labels: map[string]string{
					"app":                       noChannelSubscriptionKey.Name,
					"app.kubernetes.io/part-of": noChannelSubscriptionKey.Name,
				},
			},
			Spec: appsv1.SubscriptionSpec{
				Channel: "propagation-test-cases/non-existing-channel",
				Placement: &placementv1.Placement{
					PlacementRef: &corev1.ObjectReference{
						Name: "placement-name-goes-here",
					},
				},
			},
		}

		Expect(sutPropagationTestClient.Create(context.TODO(), noChannelSubscription)).NotTo(HaveOccurred())
		defer sutPropagationTestClient.Delete(context.TODO(), noChannelSubscription)

		_, reconcileErr := sutPropagationTestReconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: noChannelSubscriptionKey})
		Expect(reconcileErr).ToNot(HaveOccurred())

		time.Sleep(2 * time.Second)

		reconciledSubscription := &appsv1.Subscription{}
		Expect(sutPropagationTestClient.Get(context.TODO(), noChannelSubscriptionKey, reconciledSubscription)).NotTo(HaveOccurred())

		Expect(reconciledSubscription.Status.Phase).To(BeEmpty())
		Expect(reconciledSubscription.Status.Reason).To(BeEmpty())

		Expect(promTestUtils.CollectAndCount(metrics.PropagationFailedPullTime)).To(Equal(1))
		Expect(promTestUtils.CollectAndCount(metrics.PropagationSuccessfulPullTime)).To(BeZero())
	})

	It("should successfully propagate for subscriptions with a remote channel and a placement", func() {
		mgr, mgrErr := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
		Expect(mgrErr).NotTo(HaveOccurred())

		sutPropagationTestClient := mgr.GetClient()
		sutPropagationTestReconciler := newReconciler(mgr)

		ctrlErr := add(mgr, sutPropagationTestReconciler)
		Expect(ctrlErr).NotTo(HaveOccurred())

		ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
		mgrStopped := StartTestManager(ctx, mgr, nil)

		defer func() {
			cancel()
			mgrStopped.Wait()
		}()

		metrics.PropagationFailedPullTime.Reset()
		metrics.PropagationSuccessfulPullTime.Reset()

		successfulChannelKey := types.NamespacedName{
			Name:      "test-propagation-successful-channel",
			Namespace: "propagation-test-cases",
		}
		successfulChannel := &channelV1.Channel{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Channel",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      successfulChannelKey.Name,
				Namespace: successfulChannelKey.Namespace,
			},
			Spec: channelV1.ChannelSpec{
				Type:     channelV1.ChannelTypeGit,
				Pathname: "https://github.com/open-cluster-management-io/multicloud-operators-subscription",
			},
		}

		successfulSubscriptionKey := types.NamespacedName{
			Name:      "test-propagation-successful-sub",
			Namespace: "propagation-test-cases",
		}
		successfulSubscription := &appsv1.Subscription{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Subscription",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      successfulSubscriptionKey.Name,
				Namespace: successfulSubscriptionKey.Namespace,
				// the labels are mandatory for utils.IsSubscriptionBasicChanged to yield false (finalCommit defer)
				Labels: map[string]string{
					"app":                       successfulSubscriptionKey.Name,
					"app.kubernetes.io/part-of": successfulSubscriptionKey.Name,
				},
				Annotations: map[string]string{
					"apps.open-cluster-management.io/github-branch": "main",
					"apps.open-cluster-management.io/github-path":   "examples/git-simple-sub",
				},
			},
			Spec: appsv1.SubscriptionSpec{
				Channel: successfulChannelKey.String(),
				Placement: &placementv1.Placement{
					PlacementRef: &corev1.ObjectReference{
						Name: successfulPlacementRuleKey.Name,
						Kind: "PlacementRule",
					},
				},
			},
			// the status object is mandatory for utils.IsHubRelatedStatusChanged to yield false (finalCommit defer)
			Status: appsv1.SubscriptionStatus{
				Phase:    appsv1.SubscriptionPropagated,
				Message:  "",
				Statuses: appsv1.SubscriptionClusterStatusMap{},
			},
		}

		Expect(sutPropagationTestClient.Create(context.TODO(), successfulChannel)).NotTo(HaveOccurred())
		defer sutPropagationTestClient.Delete(context.TODO(), successfulChannel)

		Expect(sutPropagationTestClient.Create(context.TODO(), successfulSubscription)).NotTo(HaveOccurred())
		defer sutPropagationTestClient.Delete(context.TODO(), successfulSubscription)

		_, reconcileErr := sutPropagationTestReconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: successfulSubscriptionKey})
		Expect(reconcileErr).ToNot(HaveOccurred())

		time.Sleep(2 * time.Second)

		reconciledSubscription := &appsv1.Subscription{}
		Expect(sutPropagationTestClient.Get(context.TODO(), successfulSubscriptionKey, reconciledSubscription)).NotTo(HaveOccurred())

		Expect(reconciledSubscription.Status.Phase).To(Equal(appsv1.SubscriptionPropagated))

		Expect(promTestUtils.CollectAndCount(metrics.PropagationSuccessfulPullTime)).To(Equal(1))
	})
})
