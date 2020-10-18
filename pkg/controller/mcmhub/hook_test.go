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
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	plrv1alpha1 "github.com/open-cluster-management/multicloud-operators-placementrule/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ansibleGitURL = "https://github.com/open-cluster-management/multicloud-operators-subscription"
	pullInterval  = time.Second * 3
)

//Prehook should:
// 1. download from git, if the subscription asks for the prehook
// 2. create the prehook resource on the cluster, while keep the subscription
// wait for the prehook resource status
// 3. subscription can resume properly once the prehook resource is deployed
type hookTest struct {
	interval            time.Duration
	hookRequeueInterval Option
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
	testNs := "ansible"
	dSubKey := types.NamespacedName{Name: "t-sub", Namespace: testNs}
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
			Name:      dSubKey.Name,
			Namespace: dSubKey.Namespace,
			Annotations: map[string]string{
				subv1.AnnotationGitBranch: "master",
				subv1.AnnotationGitPath:   "test/hooks/ansible/pre-and-post",
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
		chnIns:              chn.DeepCopy(),
		subIns:              subIns.DeepCopy(),

		testNs: testNs,

		chnKey:         chnKey,
		subKey:         dSubKey,
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
var _ = Describe("multiple reconcile signal of the same subscription instance spec", func() {
	var (
		testPath = newHookTest()
		ctx      = context.TODO()
	)

	It("should not create new ansiblejob instance", func() {
		subIns := testPath.subIns.DeepCopy()
		chnIns := testPath.chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-reconcile-1", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-reconcile-1", subIns.GetNamespace()))
		subIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())

		Expect(k8sClt.Create(ctx, subIns)).Should(Succeed())

		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns)).Should(Succeed())
		}()

		waitForAnsibleJobs := func() error {
			aList := &ansiblejob.AnsibleJobList{}
			if err := k8sClt.List(context.TODO(), aList, &client.ListOptions{Namespace: subIns.GetNamespace()}); err != nil {
				return err
			}

			if len(aList.Items) > 1 {
				for _, i := range aList.Items {
					fmt.Printf("ansiblejob =  %s/%s\n", i.GetNamespace(), i.GetName())
				}

				u := &subv1.Subscription{}

				k := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}
				_ = k8sClt.Get(ctx, k, u)

				fmt.Printf("sub %s = u %+v\n", k, u)

				return fmt.Errorf("extra ansiblejob is created")
			}

			return nil
		}

		Consistently(waitForAnsibleJobs, specTimeOut, pullInterval).Should(Succeed())
	})
})

func UpdateHostDeployableStatus(clt client.Client, sKey types.NamespacedName, tPhase dplv1.DeployablePhase) error {
	hubdpl := &dplv1.Deployable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sKey.Name + "-deployable",
			Namespace: sKey.Namespace,
		},
		Spec: dplv1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: &corev1.ConfigMap{},
			},
		},
	}

	t := &dplv1.Deployable{}

	hubdplKey := types.NamespacedName{Name: hubdpl.GetName(), Namespace: hubdpl.GetNamespace()}

	if err := clt.Get(context.TODO(), hubdplKey, t); err != nil {
		return nil
	}

	t.Status.Phase = tPhase

	return clt.Status().Update(context.TODO(), t.DeepCopy())
}

func ManagedClusterUpdateHubStatus(clt client.Client, subKey types.NamespacedName, tPhase subv1.SubscriptionPhase) error {
	a := &subv1.Subscription{}
	ctx := context.TODO()

	if err := clt.Get(ctx, subKey, a); err != nil {
		return err
	}

	statusTS := metav1.Now()
	a.Status.LastUpdateTime = statusTS
	a.Status.Phase = subv1.SubscriptionPropagated
	a.Status.Statuses = subv1.SubscriptionClusterStatusMap{
		"spoke": &subv1.SubscriptionPerClusterStatus{
			SubscriptionPackageStatus: map[string]*subv1.SubscriptionUnitStatus{
				"pkg1": {
					Phase:          tPhase,
					LastUpdateTime: statusTS,
				},
			},
		},
	}

	return clt.Status().Update(ctx, a)
}

func waitForHostDeployable(clt client.Client, subKey types.NamespacedName) error {
	t := &dplv1.Deployable{}
	hostDplKey := types.NamespacedName{Name: fmt.Sprintf("%s-deployable", subKey.Name),
		Namespace: subKey.Namespace}

	return clt.Get(context.TODO(), hostDplKey, t)
}

func forceUpdatePrehook(clt client.Client, preKey types.NamespacedName) func() error {
	return func() error {
		pre := &ansiblejob.AnsibleJob{}

		if err := clt.Get(context.TODO(), preKey, pre); err != nil {
			return err
		}

		newPre := pre.DeepCopy()

		newPre.Status.AnsibleJobResult.Status = "successful"

		return clt.Status().Update(context.TODO(), newPre)
	}
}

var _ = Describe("given a subscription pointing to a git path without hook folders", func() {
	var (
		ctx    = context.TODO()
		testNs = "normal-sub"
		subKey = types.NamespacedName{Name: "t-sub", Namespace: testNs}
		chnKey = types.NamespacedName{Name: "t-chn", Namespace: testNs}

		chnIns = &chnv1.Channel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      chnKey.Name,
				Namespace: chnKey.Namespace,
			},
			Spec: chnv1.ChannelSpec{
				Pathname: ansibleGitURL,
				Type:     chnv1.ChannelTypeGit,
			},
		}

		subIns = &subv1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subKey.Name,
				Namespace: subKey.Namespace,
				Annotations: map[string]string{
					subv1.AnnotationGitBranch: "release-2.0",
					subv1.AnnotationGitPath:   "test/github/resources",
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
	)

	It("should download the git to local and add deployables annotations to subscription", func() {
		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())
		Expect(k8sClt.Create(ctx, subIns)).Should(Succeed())

		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns)).Should(Succeed())
		}()

		waitForSubscription := func() error {
			u := &subv1.Subscription{}

			if err := k8sClt.Get(ctx, subKey, u); err != nil {
				return fmt.Errorf("failed to get subscription %s, err: %s", subKey, err.Error())
			}
			fmt.Printf("izhang ======  u = %+v\n", u)

			an := u.GetAnnotations()

			if getCommitID(u) == "" || an[subv1.AnnotationDeployables] == "" || an[subv1.AnnotationTopo] == "" {
				return fmt.Errorf("failed to get the commitID, deployables or topo annotation")
			}

			return nil
		}

		Eventually(waitForSubscription, specTimeOut, pullInterval).Should(Succeed())
	})
})

var _ = Describe("given a subscription pointing to a git path,where pre hook folder present", func() {
	var (
		testPath = newHookTest()
		ctx      = context.TODO()
	)

	It("should create a prehook ansiblejob instance and wait till the prehook is subscription", func() {
		subIns := testPath.subIns.DeepCopy()
		chnIns := testPath.chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-pre-0", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-pre-0", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		subIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())
		Expect(k8sClt.Create(ctx, subIns)).Should(Succeed())

		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns)).Should(Succeed())
		}()
		ansibleIns := &ansiblejob.AnsibleJob{}

		waitForPreHookCR := func() error {
			aList := &ansiblejob.AnsibleJobList{}

			if err := k8sClt.List(context.TODO(), aList, &client.ListOptions{Namespace: subIns.GetNamespace()}); err != nil {
				return err
			}

			if len(aList.Items) == 0 {
				return errors.New("post hook is not created")
			}

			for _, h := range aList.Items {
				fmt.Printf("hoook ->>>>>>> %v/%v\n", h.GetNamespace(), h.GetName())
			}

			ansibleIns = aList.Items[0].DeepCopy()

			return nil
		}

		Eventually(waitForPreHookCR, specTimeOut, pullInterval).Should(Succeed())
		//test if the ansiblejob have a owner set
		Expect(ansibleIns.GetOwnerReferences()).ShouldNot(HaveLen(0))

		Expect(ansibleIns.Spec.TowerAuthSecretName).Should(Equal(GetReferenceString(&testPath.hookSecretRef)))
		Expect(ansibleIns.Spec.JobTemplateName).ShouldNot(HaveLen(0))

		an := ansibleIns.GetAnnotations()
		Expect(an).ShouldNot(HaveLen(0))
		Expect(an[subv1.AnnotationHosting]).Should(Equal(subKey.String()))

		foundKey := types.NamespacedName{Name: ansibleIns.GetName(), Namespace: ansibleIns.GetNamespace()}

		//after prehook is ready
		checkPrehookAnnotationAndStatus := func() error {
			updateSub := &subv1.Subscription{}

			if err := k8sClt.Get(context.TODO(), subKey, updateSub); err != nil {
				return err
			}

			fmt.Printf("checkPrehookAnnotationAndStatus ----> updateSub \n%#v\n", updateSub)

			// when the prehook is not ready
			if updateSub.Status.Phase != subv1.SubscriptionPropagationFailed {
				return fmt.Errorf("subscription status is incorrect while waiting for prehook")
			}

			a := updateSub.GetAnnotations()[subv1.AnnotationTopo]

			if !strings.Contains(a, foundKey.String()) {
				return fmt.Errorf("topo annotation is not update while waiting for prehook")
			}

			return nil
		}

		Eventually(checkPrehookAnnotationAndStatus, specTimeOut, pullInterval).Should(Succeed())
	})

	It("should create a prehook ansiblejob instance", func() {
		subIns := testPath.subIns.DeepCopy()
		chnIns := testPath.chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-pre-1", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-pre-1", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		subIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())
		Expect(k8sClt.Create(ctx, subIns)).Should(Succeed())

		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns)).Should(Succeed())
		}()
		ansibleIns := &ansiblejob.AnsibleJob{}

		waitForPreHookCR := func() error {
			aList := &ansiblejob.AnsibleJobList{}

			if err := k8sClt.List(context.TODO(), aList, &client.ListOptions{Namespace: subIns.GetNamespace()}); err != nil {
				return err
			}

			if len(aList.Items) == 0 {
				return errors.New("post hook is not created")
			}

			for _, h := range aList.Items {
				fmt.Printf("hoook ->>>>>>> %v/%v\n", h.GetNamespace(), h.GetName())
			}

			ansibleIns = aList.Items[0].DeepCopy()

			return nil
		}

		Eventually(waitForPreHookCR, specTimeOut, pullInterval).Should(Succeed())
		//test if the ansiblejob have a owner set
		Expect(ansibleIns.GetOwnerReferences()).ShouldNot(HaveLen(0))

		Expect(ansibleIns.Spec.TowerAuthSecretName).Should(Equal(GetReferenceString(&testPath.hookSecretRef)))
		Expect(ansibleIns.Spec.JobTemplateName).ShouldNot(HaveLen(0))

		an := ansibleIns.GetAnnotations()
		Expect(an).ShouldNot(HaveLen(0))
		Expect(an[subv1.AnnotationHosting]).Should(Equal(subKey.String()))

		foundKey := types.NamespacedName{Name: ansibleIns.GetName(), Namespace: ansibleIns.GetNamespace()}
		// there's an update request triggered, so we might want to wait for a bit
		time.Sleep(3 * time.Second)

		updateSub := &subv1.Subscription{}

		Expect(k8sClt.Get(context.TODO(), subKey, updateSub)).Should(Succeed())

		// when the prehook is not ready
		Expect(updateSub.Status.Phase).Should(Equal(subv1.SubscriptionPropagationFailed))

		//after prehook is ready
		Eventually(forceUpdatePrehook(k8sClt, foundKey), specTimeOut, pullInterval).Should(Succeed())

		// there's an update request triggered, so we might want to wait for a bit
		statucCheck := func() error {
			updateSub := &subv1.Subscription{}

			if err := k8sClt.Get(context.TODO(), subKey, updateSub); err != nil {
				return err
			}

			if updateSub.Status.AnsibleJobsStatus.LastPrehookJob != foundKey.String() ||
				len(updateSub.Status.AnsibleJobsStatus.PrehookJobsHistory) == 0 {

				u := &ansiblejob.AnsibleJob{}
				_ = k8sClt.Get(context.TODO(), foundKey, u)

				return fmt.Errorf("failed to find the prehook %s in status", foundKey)
			}

			return nil
		}

		Eventually(statucCheck, specTimeOut, pullInterval).Should(Succeed())
	})

	It("should reconcile with no propagated status, when git path is invalid", func() {
		subIns := testPath.subIns.DeepCopy()
		chnIns := testPath.chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-pre-2", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-pre-2", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())

		a := subIns.GetAnnotations()
		a[subv1.AnnotationGitPath] = "git-ops/ansible/resources-nonexit"
		subIns.SetAnnotations(a)

		// tells the subscription operator to process the hooks
		subIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

		Expect(k8sClt.Create(ctx, subIns)).Should(Succeed())

		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns.DeepCopy())).Should(Succeed())
		}()

		nSub := &subv1.Subscription{}

		waitForFileNoneFoundInStatus := func() error {
			if err := k8sClt.Get(ctx, subKey, nSub); err != nil {
				if k8serrors.IsNotFound(err) {
					return nil
				}
				return err
			}

			st := nSub.Status

			if st.Phase == subv1.SubscriptionPropagated {
				return errors.New(fmt.Sprintf("waiting for phase %s, got %s",
					subv1.SubscriptionPropagationFailed, st.Phase))
			}

			return nil
		}

		Consistently(waitForFileNoneFoundInStatus, specTimeOut, pullInterval).Should(Succeed())
	})
})

//Happy path should be, the subscription status is set, then the postHook should
//be deployed
var _ = Describe("given a subscription pointing to a git path,where post hook folder present", func() {
	var (
		testPath = newHookTest()
		ctx      = context.TODO()
	)

	It("upon the change of a subscription, it should create 2nd ansiblejob instance for hook(s)", func() {
		subIns := testPath.subIns.DeepCopy()
		chnIns := testPath.chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-post-1", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-post-1", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		a := subIns.GetAnnotations()
		a[subv1.AnnotationGitPath] = "test/hooks/ansible/post-only"
		subIns.SetAnnotations(a)

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())

		// tells the subscription operator to process the hooks
		subIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

		Expect(k8sClt.Create(ctx, subIns)).Should(Succeed())

		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns)).Should(Succeed())
		}()

		// mock the subscription deployable status to propagation,which is copied over to the
		// subsritption status
		mockManagedCluster := func() error {
			return ManagedClusterUpdateHubStatus(k8sClt, subKey, subv1.SubscriptionSubscribed)
		}

		Eventually(func() error {
			return waitForHostDeployable(k8sClt, subKey)
		}, specTimeOut, pullInterval).Should(Succeed())

		Eventually(mockManagedCluster, specTimeOut, pullInterval).Should(Succeed())

		mockHostDpl := func() error {
			return UpdateHostDeployableStatus(k8sClt, subKey, dplv1.DeployablePropagated)
		}
		// mock the subscription deployable status to propagation,which is copied over to the
		// subsritption status
		Eventually(mockHostDpl, specTimeOut, pullInterval).Should(Succeed())

		ansibleIns := &ansiblejob.AnsibleJob{}
		waitForPostHookCR := func() error {
			aList := &ansiblejob.AnsibleJobList{}

			if err := k8sClt.List(ctx, aList, &client.ListOptions{Namespace: subKey.Namespace}); err != nil {
				return err
			}

			if len(aList.Items) == 0 {
				return errors.New("post hook is not created")
			}

			ansibleIns = aList.Items[0].DeepCopy()

			return nil
		}

		// it seems the travis CI needs more time
		Eventually(waitForPostHookCR, specTimeOut, pullInterval).Should(Succeed())

		fmt.Println("\nfirst posthook applied when the managed cluster status updated")
		//test if the ansiblejob have a owner set
		Expect(ansibleIns.GetOwnerReferences()).ShouldNot(HaveLen(0))
		Expect(ansibleIns.Spec.TowerAuthSecretName).Should(Equal(GetReferenceString(&testPath.hookSecretRef)))

		modifySubSpec := func() error {
			u := &subv1.Subscription{}
			if err := k8sClt.Get(context.TODO(), subKey, u); err != nil {
				return nil
			}

			u.Spec.HookSecretRef.Namespace = "debug"
			return k8sClt.Update(context.TODO(), u.DeepCopy())
		}

		Eventually(modifySubSpec, specTimeOut, pullInterval).Should(Succeed())

		// since the modifySub will regenerate the deployable and managed
		// cluster status, we meed to mock the process again
		Eventually(mockManagedCluster, specTimeOut, pullInterval).Should(Succeed())
		Eventually(mockHostDpl, specTimeOut, pullInterval).Should(Succeed())

		waitForNthGenerateInstance := func(n int) func() error {
			fmt.Printf("enter ----> waitForNthGenerateInstance\n")
			defer fmt.Printf("exit <---- waitForNthGenerateInstance\n")
			return func() error {
				u := &subv1.Subscription{}
				if err := k8sClt.Get(context.TODO(), subKey, u); err != nil {
					return err
				}

				aList := &ansiblejob.AnsibleJobList{}

				if err := k8sClt.List(context.TODO(), aList, &client.ListOptions{Namespace: subKey.Namespace}); err != nil {
					return err
				}

				if len(aList.Items) < n {
					for _, i := range aList.Items {
						fmt.Printf("debug -----> list all the ansiblejob %v/%v\n", i.GetNamespace(), i.GetName())
					}

					return errors.New("failed to regenerate ansiblejob upon the subscription changes")
				}

				return nil
			}
		}

		Eventually(waitForNthGenerateInstance(2), specTimeOut, pullInterval).Should(Succeed())
		fmt.Println("\n2nd posthook applied when the spec of the subscription updated")

		// there's an update request triggered, so we might want to wait for a bit
		waitFroPosthookStatus := func() error {
			updateSub := &subv1.Subscription{}

			if err := k8sClt.Get(context.TODO(), subKey, updateSub); err != nil {
				return err
			}

			updateStatus := updateSub.Status.AnsibleJobsStatus

			an := updateSub.GetAnnotations()[subv1.AnnotationTopo]

			dErr := fmt.Errorf("failed to get ansiblejob status %s, %#v", subKey, updateStatus)

			if len(updateStatus.PosthookJobsHistory) < 2 {
				return dErr
			}

			if !strings.Contains(an, updateStatus.LastPosthookJob) {
				return fmt.Errorf("the last applied hook is not in topo annotation")
			}

			return nil
		}

		Eventually(waitFroPosthookStatus, specTimeOut, pullInterval).Should(Succeed())

		modifySubCommit := func() error {
			fmt.Printf("enter ----> modifySubCommit\n")
			defer fmt.Printf("exit <---- modifySubCommit\n")

			u := &subv1.Subscription{}
			if err := k8sClt.Get(context.TODO(), subKey, u); err != nil {
				return nil
			}

			a := u.GetAnnotations()
			// this update will be override by the actual git commit id, so it
			// wont create an extra ansiblejob instance
			a[subv1.AnnotationGitCommit] = "update-from-test"
			u.SetAnnotations(a)

			return k8sClt.Update(context.TODO(), u.DeepCopy())
		}

		// since the modifySub will regenerate the deployable and managed
		// cluster status, we meed to mock the process again

		Eventually(modifySubCommit, specTimeOut, pullInterval).Should(Succeed())

		Eventually(mockManagedCluster, specTimeOut, pullInterval).Should(Succeed())
		Eventually(mockHostDpl, specTimeOut, pullInterval).Should(Succeed())

		fmt.Println("\n3nd posthook should not apply when the commit id of the subscription annotation updated")
		//normally it took around 5 reconcile to get the desired state
		Eventually(waitForNthGenerateInstance(2), specTimeOut, pullInterval).Should(Succeed())

		checkTopo := func() error {
			u := &subv1.Subscription{}
			err := k8sClt.Get(context.TODO(), subKey, u)
			if err != nil {
				return err
			}

			tStr := u.GetAnnotations()[subv1.AnnotationTopo]

			aSt := u.Status.AnsibleJobsStatus.LastPosthookJob

			if !strings.Contains(tStr, aSt) {
				return fmt.Errorf("topo annotation is not updated")
			}

			dplAnn := u.GetAnnotations()[subv1.AnnotationDeployables]
			if len(dplAnn) == 0 {
				return fmt.Errorf("deployables annotation is missing")
			}

			return nil
		}

		Eventually(checkTopo, specTimeOut, pullInterval).Should(Succeed())
	})

	It("if package status of managed cluster is not updated, should not create posthook", func() {
		subIns := testPath.subIns.DeepCopy()
		chnIns := testPath.chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-post-3", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-post-3", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		a := subIns.GetAnnotations()
		//post only path
		a[subv1.AnnotationGitPath] = "test/hooks/ansible/post-only"
		subIns.SetAnnotations(a)

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())

		Expect(k8sClt.Create(ctx, subIns)).Should(Succeed())

		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns)).Should(Succeed())
		}()

		// mock the subscription deployable status to propagation,which is copied over to the
		// subsritption status
		Eventually(
			func() error {
				return UpdateHostDeployableStatus(k8sClt, subKey, dplv1.DeployablePropagated)
			}, specTimeOut, pullInterval).Should(Succeed())

		Eventually(
			func() error {
				return ManagedClusterUpdateHubStatus(k8sClt, subKey, subv1.SubscriptionPropagationFailed)
			}, specTimeOut, pullInterval).Should(Succeed())

		// failed to due the managed cluster isn't reporting back
		waitForAnsibleJobs := func() error {
			aList := &ansiblejob.AnsibleJobList{}
			if err := k8sClt.List(context.TODO(), aList, &client.ListOptions{Namespace: subIns.GetNamespace()}); err != nil {
				return err
			}

			if len(aList.Items) > 0 {
				return errors.New("ansiblejob is not coming up")
			}

			return nil
		}

		Consistently(waitForAnsibleJobs, specTimeOut, pullInterval).Should(Succeed())
	})
})

var _ = Describe("given a subscription pointing to a git path,where both pre and post hook folder present", func() {
	var (
		testPath = newHookTest()
		ctx      = context.TODO()
	)

	It("should create 2 ansiblejob instance and they should be written into the subscription status", func() {
		subIns := testPath.subIns.DeepCopy()
		subIns.SetNamespace(fmt.Sprintf("%s-pre-post-1", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		// tells the subscription operator to process the hooks
		subIns.Spec.HookSecretRef = testPath.hookSecretRef.DeepCopy()

		Expect(k8sClt.Create(ctx, testPath.chnIns.DeepCopy())).Should(Succeed())
		Expect(k8sClt.Create(ctx, subIns)).Should(Succeed())

		defer func() {
			Expect(k8sClt.Delete(ctx, testPath.chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns)).Should(Succeed())
		}()

		mockManagedCluster := func() error {
			return ManagedClusterUpdateHubStatus(k8sClt, subKey, subv1.SubscriptionSubscribed)
		}

		mockHostDpl := func() error {
			return UpdateHostDeployableStatus(k8sClt, subKey, dplv1.DeployablePropagated)
		}

		preHookKey := types.NamespacedName{}

		waitForPreAnsibleJobs := func() error {
			aList := &ansiblejob.AnsibleJobList{}
			if err := k8sClt.List(context.TODO(), aList, &client.ListOptions{Namespace: subIns.GetNamespace()}); err != nil {
				return err
			}

			if len(aList.Items) != 1 {
				return errors.New("pre ansiblejob is not coming up")
			}

			u := &subv1.Subscription{}
			if err := k8sClt.Get(ctx, subKey, u); err != nil {
				return err
			}

			preHook := aList.Items[0].DeepCopy()

			preHookKey.Name = preHook.GetName()
			preHookKey.Namespace = preHook.GetNamespace()

			if u.Status.AnsibleJobsStatus.LastPrehookJob != preHookKey.String() {
				return fmt.Errorf("prehook is not wrote to the status while pending")
			}

			return nil
		}

		Eventually(waitForPreAnsibleJobs, specTimeOut, pullInterval).Should(Succeed())
		//make sure the prehook is created and update with target status
		Eventually(forceUpdatePrehook(k8sClt, preHookKey), specTimeOut, pullInterval).Should(Succeed())

		Eventually(func() error {
			return waitForHostDeployable(k8sClt, subKey)
		}, specTimeOut, pullInterval).Should(Succeed())
		// mock the subscription deployable status,which is copied over to the
		// subsritption status
		//mock the status of managed cluster
		_ = mockManagedCluster
		Eventually(mockHostDpl, specTimeOut, pullInterval).Should(Succeed())

		postHookKey := types.NamespacedName{}

		waitForPostAnsibleJobs := func() error {
			aList := &ansiblejob.AnsibleJobList{}
			if err := k8sClt.List(context.TODO(), aList, &client.ListOptions{Namespace: subIns.GetNamespace()}); err != nil {
				return err
			}

			for _, l := range aList.Items {
				fmt.Printf("got ansiblejob %v/%v\n", l.GetNamespace(), l.GetName())
			}

			if len(aList.Items) < 2 {
				return errors.New("post ansiblejob is not coming up")
			}

			for _, h := range aList.Items {
				if h.GetName() != preHookKey.Name {
					postHookKey.Name = h.GetName()
					postHookKey.Namespace = h.GetNamespace()
				}
			}

			return nil
		}

		Eventually(waitForPostAnsibleJobs, specTimeOut, pullInterval).Should(Succeed())
		// there's an update request triggered, so we might want to wait for a bit
		updateSub := &subv1.Subscription{}

		waitFroPosthookStatus := func() error {
			if err := k8sClt.Get(context.TODO(), subKey, updateSub); err != nil {
				return err
			}

			updateStatus := updateSub.Status.AnsibleJobsStatus

			if updateStatus.LastPrehookJob != preHookKey.String() ||
				len(updateStatus.PrehookJobsHistory) == 0 {
				return fmt.Errorf("failed to get prehook status %s", subKey)
			}

			if updateStatus.LastPosthookJob != postHookKey.String() ||
				len(updateStatus.PosthookJobsHistory) == 0 {
				return fmt.Errorf("failed to get posthook status %s", subKey)
			}

			return nil
		}

		Eventually(waitFroPosthookStatus, specTimeOut, pullInterval).Should(Succeed())
	})
})
