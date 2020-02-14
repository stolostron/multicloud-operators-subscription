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

package namespace

import (
	"context"
	"testing"
	"time"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestSecretReconcile(t *testing.T) {
	// set up the reconcile
	g := gomega.NewGomegaWithT(t)
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is find id

	defaultitem = &appv1alpha1.SubscriberItem{
		Subscription: subscription,
		Channel:      channel,
	}

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Getting a secret reconciler which belongs to the subscription defined in the var and the subscription is
	// pointing to a namespace type of channel
	srtRec := newSecretReconciler(defaultNsSubscriber, mgr, subkey, nil)

	// Create secrets at the channel namespace

	g.Expect(c.Create(context.TODO(), subscription)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), subscription)

	g.Expect(c.Create(context.TODO(), dplSrt)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), dplSrt)

	g.Expect(c.Create(context.TODO(), noneDplSrt)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), noneDplSrt)

	dplSrtKey := types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}
	g.Expect(srtRec.getSecretsBySubLabel(dplSrtKey.Namespace)).ShouldNot(gomega.BeNil())
	//check up if the target secert is deployed at the subscriptions namespace
	dplSrtRq := reconcile.Request{NamespacedName: types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}}

	time.Sleep(4 * time.Second)

	//checked up if the secret is deployed at the subscription namespace

	// Do secret reconcile which should pick up the dplSrt and deploy it to the subscription namespace (check point)
	//checking if the reconcile has any error
	g.Expect(srtRec.Reconcile(dplSrtRq)).ShouldNot(gomega.BeNil())
}
