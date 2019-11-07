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

	chnv1alpha1 "github.com/IBM/multicloud-operators-channel/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
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

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec := newReconciler(mgr, mgr.GetClient(), nil)
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

func TestDoReconcileIncludingErrorPaths(t *testing.T) {
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

	rec := newReconciler(mgr, mgr.GetClient(), nil).(*ReconcileSubscription)

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
