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
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestPreHookLogic(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	g.Expect(mgr.GetCache().WaitForCacheSync(stopMgr)).Should(gomega.BeTrue())

	g.Expect(c.Create(context.TODO(), cfgMap)).Should(gomega.Succeed())

	defer func() {
		g.Expect(c.Delete(context.TODO(), cfgMap)).Should(gomega.Succeed())
	}()

	rec := newReconciler(mgr, time.Second*1).(*ReconcileSubscription)

	subKey := types.NamespacedName{Name: "t-sub", Namespace: "t-ns"}
	chnKey := types.NamespacedName{Name: "t-chn", Namespace: "t-ns"}
	ansibleGitURl := "https://"

	chn := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      chnKey.Name,
			Namespace: chnKey.Namespace,
		},
		Spec: chnv1.ChannelSpec{
			Pathname: ansibleGitURl,
			Type:     chnv1.ChannelTypeGit,
		},
	}

	//subscription doen't have prehook, then reconcile should process
	subInsWithoutPrehook := &subv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subKey.Name,
			Namespace: subKey.Namespace,
		},
		Spec: subv1.SubscriptionSpec{
			Channel: chnKey.String(),
		},
	}

	//subscription with prehook, then after reconcile, we should be able to
	//detect the ansibleJob instance from cluster and the subscription status
	//shouldn't be propagated
}
