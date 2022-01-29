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
	"testing"
	"time"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chnv1alpha1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"

	placement "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
)

var (
	chnkey = types.NamespacedName{
		Name:      "test-chn",
		Namespace: "test-chn-namespace",
	}

	channel = &chnv1alpha1.Channel{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Channel",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      chnkey.Name,
			Namespace: chnkey.Namespace,
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type: chnv1alpha1.ChannelTypeNamespace,
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

	subscription = &appv1alpha1.Subscription{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Subscription",
		},
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

var (
	labeltest1subkey = types.NamespacedName{
		Name:      "labeltest1sub",
		Namespace: "labeltest1namespace",
	}

	labeltest1Namespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "labeltest1namespace",
		},
	}

	labeltest1Channel = &chnv1alpha1.Channel{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Channel",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "labeltest1channel",
			Namespace: "labeltest1namespace",
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type: chnv1alpha1.ChannelTypeNamespace,
		},
	}

	labeltest1Subscription = &appv1alpha1.Subscription{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Subscription",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "labeltest1sub",
			Namespace: "labeltest1namespace",
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: "labeltest1namespace/labeltest1channel",
			Placement: &placement.Placement{
				PlacementRef: &corev1.ObjectReference{
					Name: "labeltest1Placement",
					Kind: "Placement",
				},
			},
		},
	}

	labeltest1ExpectedRequest = reconcile.Request{NamespacedName: labeltest1subkey}
)

var (
	labeltest2subkey = types.NamespacedName{
		Name:      "labeltest2sub",
		Namespace: "labeltest2namespace",
	}

	labeltest2Namespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "labeltest2namespace",
		},
	}

	labeltest2Channel = &chnv1alpha1.Channel{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Channel",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "labeltest2channel",
			Namespace: "labeltest2namespace",
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type: chnv1alpha1.ChannelTypeNamespace,
		},
	}

	labeltest2Subscription = &appv1alpha1.Subscription{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Subscription",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "labeltest2sub",
			Namespace: "labeltest2namespace",
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: "labeltest2namespace/labeltest2channel",
			Placement: &placement.Placement{
				PlacementRef: &corev1.ObjectReference{
					Name: "labeltest2Placement",
					Kind: "Placement",
				},
			},
		},
	}

	labeltest2ExpectedRequest = reconcile.Request{NamespacedName: labeltest2subkey}
)

var expectedRequest = reconcile.Request{NamespacedName: subkey}

const timeout = time.Second * 5

func TestMcMHubReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec := newReconciler(mgr)
	recFn, requests := SetupTestReconcile(rec)

	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

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

func TestDoMCMReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	instance := subscription.DeepCopy()

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	rec := newReconciler(mgr).(*ReconcileSubscription)

	chn := channel.DeepCopy()
	chn.Spec.SecretRef = nil
	chn.Spec.ConfigMapRef = nil
	g.Expect(c.Create(context.TODO(), chn)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), chn)

	g.Expect(c.Create(context.TODO(), instance)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), instance)

	g.Expect(rec.doMCMHubReconcile(instance)).NotTo(gomega.HaveOccurred())
}

func TestNewAppLabels(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec := newReconciler(mgr)

	recFn, requests := SetupTestReconcile(rec)

	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Create the app label test namespace.
	g.Expect(c.Create(context.TODO(), labeltest1Namespace)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), labeltest1Namespace)

	// Create a channel
	g.Expect(c.Create(context.TODO(), labeltest1Channel.DeepCopy())).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), labeltest1Channel)

	// Create a subscription
	g.Expect(c.Create(context.TODO(), labeltest1Subscription)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), labeltest1Subscription)

	time.Sleep(time.Second * 2)

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(labeltest1ExpectedRequest)))

	subscription := &appv1alpha1.Subscription{}

	g.Expect(c.Get(context.TODO(), labeltest1subkey, subscription)).NotTo(gomega.HaveOccurred())

	// Verify that the application labels are added and set with the subscription name
	g.Expect(subscription.Labels["app"]).To(gomega.Equal(labeltest1subkey.Name))
	g.Expect(subscription.Labels["app.kubernetes.io/part-of"]).To(gomega.Equal(labeltest1subkey.Name))
}

func TestSyncAppLabels(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec := newReconciler(mgr)

	recFn, requests := SetupTestReconcile(rec)

	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Create the app label test namespace.
	g.Expect(c.Create(context.TODO(), labeltest2Namespace.DeepCopy())).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), labeltest2Namespace)

	// Create a channel
	g.Expect(c.Create(context.TODO(), labeltest2Channel.DeepCopy())).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), labeltest2Channel)

	labels := make(map[string]string)

	labels["app"] = "existingAppLabel"

	labeltest2Subscription.SetLabels(labels)

	// Create a subscription
	g.Expect(c.Create(context.TODO(), labeltest2Subscription.DeepCopy())).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), labeltest2Subscription)

	time.Sleep(time.Second * 2)

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(labeltest2ExpectedRequest)))

	subscription := &appv1alpha1.Subscription{}

	g.Expect(c.Get(context.TODO(), labeltest2subkey, subscription)).NotTo(gomega.HaveOccurred())

	// Verify that the application labels are synch'd with the existing app label
	g.Expect(subscription.Labels["app"]).To(gomega.Equal("existingAppLabel"))
	g.Expect(subscription.Labels["app.kubernetes.io/part-of"]).To(gomega.Equal("existingAppLabel"))
}
