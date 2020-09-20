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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	plrv1alpha1 "github.com/open-cluster-management/multicloud-operators-placementrule/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ansibleGitURL = "https://github.com/ianzhang366/acm-applifecycle-samples"
)

type TSetUp struct {
	g    *gomega.GomegaWithT
	mgr  manager.Manager
	stop chan struct{}
	wg   *sync.WaitGroup
}

func NewTSetUp(t *testing.T) *TSetUp {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	g.Expect(mgr.GetCache().WaitForCacheSync(stopMgr)).Should(gomega.BeTrue())

	return &TSetUp{
		g:    g,
		mgr:  mgr,
		stop: stopMgr,
		wg:   mgrStopped,
	}
}

//Prehook should:
// 1. download from git, if the subscription asks for the prehook
// 2. create the prehook resource on the cluster, while keep the subscription
// wait for the prehook resource status
// 3. subscription can resume properly once the prehook resource is deployed
type hookTest struct {
	interval            time.Duration
	hookRequeueInterval Option
	suffixFunc          Option
	chnIns              *chnv1.Channel
	subIns              *subv1.Subscription

	testNs string

	chnKey         types.NamespacedName
	subKey         types.NamespacedName
	hookSecretRef  corev1.ObjectReference
	preAnsibleKey  types.NamespacedName
	postAnsibleKey types.NamespacedName
}

func newHookTest() *hookTest {
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
	hookSecretRef := corev1.ObjectReference{Name: "hook-secret", Namespace: "test"}

	preAnsibleKey := types.NamespacedName{Name: "prehook-test", Namespace: testNs}
	postAnsibleKey := types.NamespacedName{Name: "posthook-test", Namespace: testNs}

	chn := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      chnKey.Name,
			Namespace: chnKey.Namespace,
		},
		Spec: chnv1.ChannelSpec{
			Pathname: ansibleGitURL,
			Type:     chnv1.ChannelTypeGit,
		},
	}

	subIns := &subv1.Subscription{
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
			Placement: &plrv1alpha1.Placement{
				GenericPlacementFields: plrv1alpha1.GenericPlacementFields{
					Clusters: []plrv1alpha1.GenericClusterReference{
						{Name: "test-cluster"},
					},
				},
			},
		},
	}

	return &hookTest{
		interval:            hookRequeueInterval,
		hookRequeueInterval: setRequeueInterval,
		suffixFunc:          setSufficFunc,
		chnIns:              chn.DeepCopy(),
		subIns:              subIns.DeepCopy(),

		testNs: testNs,

		chnKey:         chnKey,
		subKey:         subKey,
		hookSecretRef:  hookSecretRef,
		preAnsibleKey:  preAnsibleKey,
		postAnsibleKey: postAnsibleKey,
	}
}

// happyPath is defined as the following:
//asumming the github have the ansible YAML
//subscription, with prehook, after reconcile, should be able to
//detect the ansibleJob instance from cluster and the subscription status
//shouldn't be propagated
func TestPrehookHappyPathMain(t *testing.T) {
	tSetup := NewTSetUp(t)

	g := tSetup.g
	mgr := tSetup.mgr
	k8sClt := mgr.GetClient()

	defer func() {
		close(tSetup.stop)
		tSetup.wg.Wait()
	}()

	testPath := newHookTest()

	rec := newReconciler(mgr, testPath.hookRequeueInterval, testPath.suffixFunc).(*ReconcileSubscription)

	ctx := context.TODO()
	g.Expect(k8sClt.Create(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())
	}()

	applyIns := testPath.subIns.DeepCopy()

	applyIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

	g.Expect(k8sClt.Create(ctx, applyIns)).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, applyIns)).Should(gomega.Succeed())
	}()

	r, err := rec.Reconcile(reconcile.Request{NamespacedName: testPath.subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(r.RequeueAfter).Should(gomega.Equal(testPath.interval))

	ansibleIns := &ansiblejob.AnsibleJob{}

	g.Expect(k8sClt.Get(ctx, testPath.preAnsibleKey, ansibleIns)).Should(gomega.Succeed())

	//test if the ansiblejob have a owner set
	g.Expect(ansibleIns.GetOwnerReferences()).ShouldNot(gomega.HaveLen(0))

	g.Expect(ansibleIns.Spec.TowerAuthSecretName).Should(gomega.Equal(GetReferenceString(&testPath.hookSecretRef)))
	g.Expect(ansibleIns.Spec.JobTemplateName).Should(gomega.Equal("Demo Job Template"))

	defer func() {
		g.Expect(k8sClt.Delete(ctx, ansibleIns)).Should(gomega.Succeed())
	}()

	// there's an update request triggered, so we might want to wait for a bit
	time.Sleep(3 * time.Second)

	updateSub := &subv1.Subscription{}

	g.Expect(k8sClt.Get(context.TODO(), testPath.subKey, updateSub)).Should(gomega.Succeed())

	// when the prehook is not ready
	g.Expect(updateSub.Status.Phase).Should(gomega.Equal(subv1.SubscriptionPropagationFailed))

	//after prehook is ready
	forceUpdatePrehook(k8sClt, testPath.preAnsibleKey)

	r, err = rec.Reconcile(reconcile.Request{NamespacedName: testPath.subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(r.RequeueAfter).Should(gomega.Equal(testPath.interval))

	// there's an update request triggered, so we might want to wait for a bit

	statucCheck := func() error {
		updateSub := &subv1.Subscription{}

		r, err = rec.Reconcile(reconcile.Request{NamespacedName: testPath.subKey})

		if err := k8sClt.Get(context.TODO(), testPath.subKey, updateSub); err != nil {
			return err
		}

		if updateSub.Status.AnsibleJobsStatus.LastPrehookJob != testPath.preAnsibleKey.String() ||
			len(updateSub.Status.AnsibleJobsStatus.PrehookJobsHistory) == 0 {
			return fmt.Errorf("failed to find the prehook %s in status", testPath.preAnsibleKey)
		}

		return nil
	}

	g.Eventually(statucCheck, time.Second*30, time.Second*3).Should(gomega.Succeed())
}

func TestPrehookHappyPathNoDuplicateInstanceOnReconciles(t *testing.T) {
	tSetup := NewTSetUp(t)

	g := tSetup.g
	mgr := tSetup.mgr
	k8sClt := mgr.GetClient()

	defer func() {
		close(tSetup.stop)
		tSetup.wg.Wait()
	}()

	testPath := newHookTest()

	//use the default instance name generator
	rec := newReconciler(mgr, testPath.hookRequeueInterval).(*ReconcileSubscription)

	ctx := context.TODO()
	g.Expect(k8sClt.Create(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())
	}()

	applyIns := testPath.subIns.DeepCopy()

	applyIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

	g.Expect(k8sClt.Create(ctx, applyIns)).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, applyIns)).Should(gomega.Succeed())
	}()

	defer func() {
		ansibleList := &ansiblejob.AnsibleJobList{}
		g.Expect(k8sClt.List(ctx, ansibleList)).Should(gomega.Succeed())

		for _, ansibleIns := range ansibleList.Items {
			g.Expect(k8sClt.Delete(ctx, &ansibleIns)).Should(gomega.Succeed())
		}
	}()

	r, err := rec.Reconcile(reconcile.Request{NamespacedName: testPath.subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(r.RequeueAfter).Should(gomega.Equal(testPath.interval))

	ansibleList := &ansiblejob.AnsibleJobList{}

	g.Expect(k8sClt.List(ctx, ansibleList)).Should(gomega.Succeed())
	g.Expect(ansibleList.Items).Should(gomega.HaveLen(1))

	_, err = rec.Reconcile(reconcile.Request{NamespacedName: testPath.subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(k8sClt.List(ctx, ansibleList)).Should(gomega.Succeed())
	g.Expect(ansibleList.Items).Should(gomega.HaveLen(1))
}

// GitResourceNoneExistPath is defined as the following:
//asumming the github doesn't have the ansible YAML or channel is pointing to a wrong
//path or download from git failed
// upon the fail of the download, the reconcile of subscription will stop and
// error message should be print in the log or put the error to the status field
func TestPrehookGitResourceNoneExistPath(t *testing.T) {
	tSetup := NewTSetUp(t)

	g := tSetup.g
	mgr := tSetup.mgr
	k8sClt := mgr.GetClient()

	defer func() {
		close(tSetup.stop)
		tSetup.wg.Wait()
	}()

	testPath := newHookTest()

	rec := newReconciler(mgr, testPath.hookRequeueInterval, testPath.suffixFunc).(*ReconcileSubscription)

	ctx := context.TODO()
	g.Expect(k8sClt.Create(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())
	}()

	applyIns := testPath.subIns.DeepCopy()

	a := testPath.subIns.GetAnnotations()
	a[subv1.AnnotationGitPath] = "git-ops/ansible/resources-nonexit"
	applyIns.SetAnnotations(a)

	// tells the subscription operator to process the hooks
	applyIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

	g.Expect(k8sClt.Create(ctx, applyIns)).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.subIns.DeepCopy())).Should(gomega.Succeed())
	}()

	r, err := rec.Reconcile(reconcile.Request{NamespacedName: testPath.subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(r.RequeueAfter).Should(gomega.Equal(testPath.interval))

	ansibleIns := &ansiblejob.AnsibleJob{}

	g.Expect(k8sClt.Get(ctx, testPath.preAnsibleKey, ansibleIns)).ShouldNot(gomega.Succeed())
}

func forceUpdatePrehook(clt client.Client, preKey types.NamespacedName) error {
	pre := &ansiblejob.AnsibleJob{}

	if err := clt.Get(context.TODO(), preKey, pre); err != nil {
		return err
	}

	newPre := pre.DeepCopy()

	newPre.Status.AnsibleJobResult.Status = "successful"

	return clt.Status().Update(context.TODO(), newPre)
}

func forceUpdateSubDpl(clt client.Client, subIns *subv1.Subscription) error {
	hubdpl := &dplv1.Deployable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subIns.Name + "-deployable",
			Namespace: subIns.Namespace,
		},
		Spec: dplv1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: &corev1.ConfigMap{},
			},
		},
	}

	_ = clt.Create(context.TODO(), hubdpl)

	t := &dplv1.Deployable{}

	hubdplKey := types.NamespacedName{Name: hubdpl.GetName(), Namespace: hubdpl.GetNamespace()}

	_ = clt.Get(context.TODO(), hubdplKey, t)

	t.Status.Phase = dplv1.DeployablePropagated

	return clt.Status().Update(context.TODO(), t.DeepCopy())
}

//Happy path should be, the subscription status is set, then the postHook should
//be deployed
func TestPosthookHappyPathWithPreHooks(t *testing.T) {
	tSetup := NewTSetUp(t)

	g := tSetup.g
	mgr := tSetup.mgr
	k8sClt := mgr.GetClient()

	defer func() {
		close(tSetup.stop)
		tSetup.wg.Wait()
	}()

	testPath := newHookTest()

	rec := newReconciler(mgr, testPath.hookRequeueInterval, testPath.suffixFunc).(*ReconcileSubscription)

	ctx := context.TODO()
	g.Expect(k8sClt.Create(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())
	}()

	subIns := testPath.subIns.DeepCopy()
	postSubName := "test-posthook"
	subIns.SetName(postSubName)

	// tells the subscription operator to process the hooks
	subIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

	g.Expect(k8sClt.Create(ctx, subIns)).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, subIns)).Should(gomega.Succeed())
	}()

	subKey := testPath.subKey
	subKey.Name = postSubName

	// mock the subscription deployable status,which is copied over to the
	// subsritption status

	//reconcile tries to create posthook and failed on the not-ready status
	r, err := rec.Reconcile(reconcile.Request{NamespacedName: subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(r.RequeueAfter).Should(gomega.Equal(testPath.interval))

	//mock the status of managed cluster
	g.Expect(forceUpdateSubDpl(k8sClt, subIns)).Should(gomega.Succeed())

	//make sure the prehook is created
	g.Expect(forceUpdatePrehook(k8sClt, testPath.preAnsibleKey)).Should(gomega.Succeed())

	time.Sleep(5 * time.Second)

	ansibleIns := &ansiblejob.AnsibleJob{}
	waitForPostHookCR := func() error {
		//reconcile update the status of the subscription itself
		r, err = rec.Reconcile(reconcile.Request{NamespacedName: subKey})
		if err != nil {
			return err
		}

		return k8sClt.Get(ctx, testPath.postAnsibleKey, ansibleIns)
	}

	g.Eventually(waitForPostHookCR, time.Second*30, time.Second*3).Should(gomega.Succeed())
	//test if the ansiblejob have a owner set
	g.Expect(ansibleIns.GetOwnerReferences()).ShouldNot(gomega.HaveLen(0))

	g.Expect(ansibleIns.Spec.TowerAuthSecretName).Should(gomega.Equal(GetReferenceString(&testPath.hookSecretRef)))

	defer func() {
		g.Expect(k8sClt.Delete(ctx, ansibleIns)).Should(gomega.Succeed())
	}()

	// there's an update request triggered, so we might want to wait for a bit

	waitFroPosthookStatus := func() error {
		r, err = rec.Reconcile(reconcile.Request{NamespacedName: subKey})

		updateSub := &subv1.Subscription{}

		if err := k8sClt.Get(context.TODO(), subKey, updateSub); err != nil {
			return err
		}

		updateStatus := updateSub.Status.AnsibleJobsStatus

		dErr := fmt.Errorf("failed to get status %s", subKey)

		if updateStatus.LastPrehookJob != testPath.preAnsibleKey.String() ||
			len(updateStatus.PrehookJobsHistory) == 0 {
			return dErr
		}

		if updateStatus.LastPosthookJob != testPath.postAnsibleKey.String() ||
			len(updateStatus.PosthookJobsHistory) == 0 {
			return dErr
		}

		return nil
	}

	g.Eventually(waitFroPosthookStatus, time.Second*30, time.Second*3).Should(gomega.Succeed())
}

//Happy path should be, the subscription status is set, then the postHook should
//be deployed
func TestInstaceGenerateUponChangesOfSubscription(t *testing.T) {
	tSetup := NewTSetUp(t)

	g := tSetup.g
	mgr := tSetup.mgr
	k8sClt := mgr.GetClient()

	defer func() {
		close(tSetup.stop)
		tSetup.wg.Wait()
	}()

	testPath := newHookTest()

	rec := newReconciler(mgr, testPath.hookRequeueInterval, testPath.suffixFunc).(*ReconcileSubscription)

	ctx := context.TODO()
	g.Expect(k8sClt.Create(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())
	}()

	subIns := testPath.subIns.DeepCopy()
	postSubName := "test-posthook"
	subIns.SetName(postSubName)

	// tells the subscription operator to process the hooks
	subIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

	g.Expect(k8sClt.Create(ctx, subIns)).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, subIns)).Should(gomega.Succeed())
	}()

	subKey := testPath.subKey
	subKey.Name = postSubName

	// mock the subscription deployable status,which is copied over to the
	// subsritption status

	//reconcile tries to create posthook and failed on the not-ready status
	r, err := rec.Reconcile(reconcile.Request{NamespacedName: subKey})

	g.Expect(err).Should(gomega.Succeed())
	g.Expect(r.RequeueAfter).Should(gomega.Equal(testPath.interval))

	//mock the status of managed cluster
	g.Expect(forceUpdateSubDpl(k8sClt, subIns)).Should(gomega.Succeed())

	//make sure the prehook is created
	g.Expect(forceUpdatePrehook(k8sClt, testPath.preAnsibleKey)).Should(gomega.Succeed())

	time.Sleep(5 * time.Second)

	ansibleIns := &ansiblejob.AnsibleJob{}
	waitForPostHookCR := func() error {
		//reconcile update the status of the subscription itself
		r, err = rec.Reconcile(reconcile.Request{NamespacedName: subKey})
		if err != nil {
			return err
		}

		return k8sClt.Get(ctx, testPath.postAnsibleKey, ansibleIns)
	}

	g.Eventually(waitForPostHookCR, time.Second*30, time.Second*3).Should(gomega.Succeed())
	//test if the ansiblejob have a owner set
	g.Expect(ansibleIns.GetOwnerReferences()).ShouldNot(gomega.HaveLen(0))

	g.Expect(ansibleIns.Spec.TowerAuthSecretName).Should(gomega.Equal(GetReferenceString(&testPath.hookSecretRef)))

	defer func() {
		g.Expect(k8sClt.Delete(ctx, ansibleIns)).Should(gomega.Succeed())
	}()

	sAnno := subIns.GetAnnotations()
	sAnno["test"] = "ian"
	subIns.SetAnnotations(sAnno)

	updateSub := subIns.DeepCopy()

	g.Expect(k8sClt.Get(context.TODO(), subKey, updateSub)).Should(gomega.Succeed())

	g.Expect(k8sClt.Update(context.TODO(), updateSub)).Should(gomega.Succeed())

	waitFor2ndGenerateInstance := func() error {
		r, err = rec.Reconcile(reconcile.Request{NamespacedName: subKey})
		aList := &ansiblejob.AnsibleJobList{}

		if err := k8sClt.List(context.TODO(), aList, &client.ListOptions{Namespace: subKey.Namespace}); err != nil {
			return err
		}

		fmt.Printf("izhang -----> list all the ansiblejob %v\n", aList.Items)
		if len(aList.Items) < 3 {
			return errors.New("failed to regenerate ansiblejob upon the subscription changes")
		}

		return nil
	}

	g.Eventually(waitFor2ndGenerateInstance, time.Second*30, time.Second*3).Should(gomega.Succeed())

	// there's an update request triggered, so we might want to wait for a bit

	//	waitFroPosthookStatus := func() error {
	//		r, err = rec.Reconcile(reconcile.Request{NamespacedName: subKey})
	//
	//		updateSub := &subv1.Subscription{}
	//
	//		if err := k8sClt.Get(context.TODO(), subKey, updateSub); err != nil {
	//			return err
	//		}
	//
	//		updateStatus := updateSub.Status.AnsibleJobsStatus
	//
	//		dErr := fmt.Errorf("failed to get status %s", subKey)
	//
	//		if updateStatus.LastPrehookJob != testPath.preAnsibleKey.String() ||
	//			len(updateStatus.PrehookJobsHistory) == 0 {
	//			return dErr
	//		}
	//
	//		if updateStatus.LastPosthookJob != testPath.postAnsibleKey.String() ||
	//			len(updateStatus.PosthookJobsHistory) == 0 {
	//			return dErr
	//		}
	//
	//		return nil
	//	}
	//
	//	g.Eventually(waitFroPosthookStatus, time.Second*30, time.Second*3).Should(gomega.Succeed())
}

//Happy path should be, the subscription status is set, then the postHook should
//be deployed
func TestPosthookManagedClusterPackageFailedPath(t *testing.T) {
	tSetup := NewTSetUp(t)

	g := tSetup.g
	mgr := tSetup.mgr
	k8sClt := mgr.GetClient()

	defer func() {
		close(tSetup.stop)
		tSetup.wg.Wait()
	}()

	testPath := newHookTest()

	rec := newReconciler(mgr, testPath.hookRequeueInterval, testPath.suffixFunc).(*ReconcileSubscription)

	ctx := context.TODO()
	g.Expect(k8sClt.Create(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, testPath.chnIns.DeepCopy())).Should(gomega.Succeed())
	}()

	subIns := testPath.subIns.DeepCopy()

	g.Expect(k8sClt.Create(ctx, subIns)).Should(gomega.Succeed())

	defer func() {
		g.Expect(k8sClt.Delete(ctx, subIns)).Should(gomega.Succeed())
	}()

	statusTS := metav1.Now()
	subIns.Status.LastUpdateTime = statusTS
	subIns.Status.Statuses = subv1.SubscriptionClusterStatusMap{
		"spoke": &subv1.SubscriptionPerClusterStatus{
			SubscriptionPackageStatus: map[string]*subv1.SubscriptionUnitStatus{
				"pkg1": {
					Phase:          subv1.SubscriptionSubscribed,
					LastUpdateTime: statusTS,
				},
			},
		},
	}

	g.Expect(k8sClt.Status().Update(context.TODO(), subIns)).Should(gomega.Succeed())
	// mock the subscription deployable status,which is copied over to the
	// subsritption status

	g.Expect(forceUpdateSubDpl(k8sClt, subIns)).Should(gomega.Succeed())

	//reconcile should be able to find the subscription and it should able to
	//pares status.statues
	_, err := rec.Reconcile(reconcile.Request{NamespacedName: testPath.subKey})

	g.Expect(err).Should(gomega.BeNil())

	ansibleIns := &ansiblejob.AnsibleJob{}

	g.Expect(k8sClt.Get(ctx, testPath.postAnsibleKey, ansibleIns)).ShouldNot(gomega.Succeed())
}
