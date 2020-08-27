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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestPreHookLogic(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	k8sClt := mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	g.Expect(mgr.GetCache().WaitForCacheSync(stopMgr)).Should(gomega.BeTrue())

	hookRequeueInterval := time.Second * 1
	rec := newReconciler(mgr, hookRequeueInterval).(*ReconcileSubscription)

	testNs := "ansible"
	subKey := types.NamespacedName{Name: "t-sub", Namespace: testNs}
	chnKey := types.NamespacedName{Name: "t-chn", Namespace: testNs}
	preAnsibleKey := types.NamespacedName{Name: "prehook-test", Namespace: testNs}
	ansibleGitURl := "https://github.com/ianzhang366/acm-applifecycle-samples"

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

	subInsWithoutPrehook := &subv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subKey.Name,
			Namespace: subKey.Namespace,
			Annotations: map[string]string{
				subv1.AnnotationGitBranch: "master",
				subv1.AnnotationGitPath:   "git-ops/ansible/resources",
			},
		},
		Spec: subv1.SubscriptionSpec{
			Channel: chnKey.String(),
			Prehook: preAnsibleKey.String(),
		},
	}

	ctx := context.TODO()
	g.Expect(k8sClt.Create(ctx, chn.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, chn.DeepCopy())).Should(gomega.Succeed())
	}()

	//asumming the github have the ansible YAML
	//subscription with prehook, then after reconcile, we should be able to
	//detect the ansibleJob instance from cluster and the subscription status
	//shouldn't be propagated
	g.Expect(k8sClt.Create(ctx, subInsWithoutPrehook.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, subInsWithoutPrehook.DeepCopy())).Should(gomega.Succeed())
	}()

	r, err := rec.Reconcile(reconcile.Request{NamespacedName: subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(r.RequeueAfter).Should(gomega.Equal(hookRequeueInterval))

	ansibleIns := &ansiblejob.AnsibleJob{}

	g.Eventually(k8sClt.Get(ctx, preAnsibleKey, ansibleIns), time.Second*5).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, ansibleIns)).Should(gomega.Succeed())
	}()
}
