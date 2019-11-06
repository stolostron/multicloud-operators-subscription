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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/onsi/gomega"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

var (
	testNamespaceName = "testns"
	testKeys          = types.NamespacedName{
		Name:      "test-chn",
		Namespace: testNamespaceName,
	}

	srt = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testKeys.Name,
			Namespace: testKeys.Namespace,
		},
	}

	skey = types.NamespacedName{
		Name:      "test-sub",
		Namespace: testNamespaceName,
	}

	sub = &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      skey.Name,
			Namespace: skey.Namespace,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel:       testKeys.String(),
			PackageFilter: nil,
		},
	}
)

//label related test variables
var (
	srtLb = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "srtfordeletion",
			Namespace: sub.GetNamespace(),
			Labels:    map[string]string{SercertReferredMarker: "true", sub.GetName(): "true"},
		},
	}
)

func TestListAndDeployReferredObject(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	rec := newReconciler(mgr, mgr.GetClient(), nil).(*ReconcileSubscription)
	gvk := schema.GroupVersionKind{Group: "", Kind: SecretKindStr, Version: "v1"}
	g.Expect(rec.ListAndDeployReferredObject(sub, gvk, srt)).ShouldNot(gomega.HaveOccurred())

	resSrt := &corev1.Secret{}
	g.Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: sub.GetNamespace(), Name: srt.GetName()}, resSrt)).Should(gomega.BeNil())

	g.Expect(resSrt.GetName()).Should(gomega.Equal(srt.GetName()))
}

func TestDeleteReferredObjects(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finid'Cu
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	rec := newReconciler(mgr, mgr.GetClient(), nil).(*ReconcileSubscription)

	// get the default reonciler then list the secert by name
	srtDLb := srtLb.DeepCopy()
	g.Expect(c.Create(context.TODO(), srtDLb)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), srtDLb)

	rq := types.NamespacedName{Namespace: sub.GetNamespace(), Name: sub.GetName()}
	//testing if the new referred object is labeled correctly
	gvk := schema.GroupVersionKind{Group: "", Kind: SecretKindStr, Version: "v1"}

	err = rec.DeleteReferredObjects(rq, gvk)
	g.Expect(err).Should(gomega.BeNil())

	resSrt := &corev1.Secret{}
	g.Expect(c.Get(context.TODO(), types.NamespacedName{Name: srtLb.GetName(), Namespace: srtLb.GetNamespace()}, resSrt)).Should(gomega.HaveOccurred())
}
