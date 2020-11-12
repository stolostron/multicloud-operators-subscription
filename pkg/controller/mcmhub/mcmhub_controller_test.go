// Copyright 2019 The Kubernetes Authors.
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

var c client.Client

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

	helmKey = types.NamespacedName{
		Name:      "ch-helm",
		Namespace: "ch-helm-ns",
	}

	chHelm = &chnv1alpha1.Channel{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Channel",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmKey.Name,
			Namespace: helmKey.Namespace,
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type:               chnv1alpha1.ChannelTypeHelmRepo,
			Pathname:           "https://kubernetes-charts.storage.googleapis.com",
			InsecureSkipVerify: true,
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

var expectedRequest = reconcile.Request{NamespacedName: subkey}

const timeout = time.Second * 2

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

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
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

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
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
