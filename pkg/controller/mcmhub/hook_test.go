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

const (
	ansibleGitURl = "https://github.com/ianzhang366/acm-applifecycle-samples"
)

//Prehook should:
// 1. download from git, if the subscription asks for the prehook
// 2. create the prehook resource on the cluster, while keep the subscription
// wait for the prehook resource status
// 3. subscription can resume properly once the prehook resource is deployed
type preHookTest struct {
	interval            time.Duration
	hookRequeueInterval Option
	suffixFunc          Option
	chnIns              *chnv1.Channel
	subIns              *subv1.Subscription

	testNs string

	chnKey        types.NamespacedName
	subKey        types.NamespacedName
	preAnsibleKey types.NamespacedName
}

func newPreHookTest() *preHookTest {
	hookRequeueInterval := time.Second * 1
	setRequeueInterval := func(r *ReconcileSubscription) {
		r.hookRequeueInterval = hookRequeueInterval
	}

	setSufficFunc := func(r *ReconcileSubscription) {
		sf := func(s *subv1.Subscription) string {
			return ""
		}

		r.hooks.SetSuffixFunc(sf)
	}

	testNs := "ansible"
	subKey := types.NamespacedName{Name: "t-sub", Namespace: testNs}
	chnKey := types.NamespacedName{Name: "t-chn", Namespace: testNs}
	preAnsibleKey := types.NamespacedName{Name: "prehook-test", Namespace: testNs}

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

	subInsPrehook := &subv1.Subscription{
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

	return &preHookTest{
		interval:            hookRequeueInterval,
		hookRequeueInterval: setRequeueInterval,
		suffixFunc:          setSufficFunc,
		chnIns:              chn,
		subIns:              subInsPrehook,

		testNs: testNs,

		chnKey:        chnKey,
		subKey:        subKey,
		preAnsibleKey: preAnsibleKey,
	}

}

// happyPath is defined as the following:
//asumming the github have the ansible YAML
//subscription, with prehook, after reconcile, should be able to
//detect the ansibleJob instance from cluster and the subscription status
//shouldn't be propagated
func TestPrehookHappyPath(t *testing.T) {
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

	testPath := newPreHookTest()

	rec := newReconciler(mgr, testPath.hookRequeueInterval, testPath.suffixFunc).(*ReconcileSubscription)

	ctx := context.TODO()
	g.Expect(k8sClt.Create(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())
	}()

	g.Expect(k8sClt.Create(ctx, testPath.subIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.subIns.DeepCopy())).Should(gomega.Succeed())
	}()

	r, err := rec.Reconcile(reconcile.Request{NamespacedName: testPath.subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(r.RequeueAfter).Should(gomega.Equal(testPath.interval))

	ansibleIns := &ansiblejob.AnsibleJob{}

	g.Expect(k8sClt.Get(ctx, testPath.preAnsibleKey, ansibleIns)).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, ansibleIns)).Should(gomega.Succeed())
	}()
}

// GitResourceNoneExistPath is defined as the following:
//asumming the github doesn't the ansible YAML or channel is pointing to a wrong
//path or download from git failed
//A subscription with prehook, after reconcile, we should be able to
//1. print error to trace
//2. stop requeue the give subscritpion

func TestPrehookGitResourceNoneExistPath(t *testing.T) {
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

	testPath := newPreHookTest()

	rec := newReconciler(mgr, testPath.hookRequeueInterval, testPath.suffixFunc).(*ReconcileSubscription)

	ctx := context.TODO()
	g.Expect(k8sClt.Create(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())
	}()

	a := testPath.subIns.GetAnnotations()
	a[subv1.AnnotationGitPath] = "git-ops/ansible/resources-nonexit"
	testPath.subIns.SetAnnotations(a)

	g.Expect(k8sClt.Create(ctx, testPath.subIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.subIns.DeepCopy())).Should(gomega.Succeed())
	}()

	r, err := rec.Reconcile(reconcile.Request{NamespacedName: testPath.subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(r.RequeueAfter).Should(gomega.Equal(time.Duration(0)))

	ansibleIns := &ansiblejob.AnsibleJob{}

	g.Expect(k8sClt.Get(ctx, testPath.preAnsibleKey, ansibleIns)).ShouldNot(gomega.Succeed())
}
