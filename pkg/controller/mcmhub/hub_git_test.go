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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	plrv1alpha1 "github.com/open-cluster-management/multicloud-operators-placementrule/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func checkGitRegCommit(tbranch string) func() error {
	return func() error {
		rr := gitOps.GetRepoRecords()

		branchInfo := rr[ansibleGitURL].branchs[tbranch]
		if strings.EqualFold(branchInfo.lastCommitID, defaultCommit) {
			return nil
		}

		return fmt.Errorf("failed to get the update commit ID from the git registry")
	}
}

func registerSub(key types.NamespacedName) func() error {
	ctx := context.TODO()

	return func() error {
		u := &subv1.Subscription{}

		if err := k8sClt.Get(ctx, key, u); err != nil {
			return err
		}

		gitOps.RegisterBranch(u)

		return nil
	}
}

func deRegisterSub(key types.NamespacedName) func() error {
	return func() error {
		gitOps.DeregisterBranch(key)

		return nil
	}
}

var _ = PDescribe("hub git ops", func() {
	var (
		ctx    = context.TODO()
		testNs = "t-ns-gitops"

		dSubKey = types.NamespacedName{Name: "t-sub-git-ops", Namespace: testNs}
		chnKey  = types.NamespacedName{Name: "t-chn-git-ops", Namespace: testNs}

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
				Name:      dSubKey.Name,
				Namespace: dSubKey.Namespace,
				Annotations: map[string]string{
					subv1.AnnotationGitPath: "test/hooks/ansible/pre-and-post",
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

	It("register/de-register a subscription", func() {
		subIns := subIns.DeepCopy()
		chnIns := chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-hub-git-1", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-hub-git-1", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())
		Expect(k8sClt.Create(ctx, subIns.DeepCopy())).Should(Succeed())

		testBranch := "master"
		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns.DeepCopy())).Should(Succeed())
		}()

		Eventually(registerSub(subKey), pullInterval*3, pullInterval).Should(Succeed())

		sr := gitOps.GetSubRecords()

		Expect(sr[subKey]).Should(Equal(ansibleGitURL))

		rr := gitOps.GetRepoRecords()

		branchInfo := rr[ansibleGitURL].branchs[testBranch]
		Expect(branchInfo.registeredSub).Should(HaveKey(subKey))

		Eventually(checkGitRegCommit(testBranch), pullInterval*3, pullInterval).Should(Succeed())

		Eventually(deRegisterSub(subKey), pullInterval*3, pullInterval).Should(Succeed())
		sr = gitOps.GetSubRecords()

		Expect(sr).ShouldNot(HaveKey(subKey))

		rr = gitOps.GetRepoRecords()

		Expect(rr).ShouldNot(HaveKey(ansibleGitURL))
		Expect(rr).Should(HaveLen(0))
	})

	It("register/deRegisterSub the 2nd subscription", func() {
		subIns := subIns.DeepCopy()
		chnIns := chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-hub-git-2", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-hub-git-2", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())
		Expect(k8sClt.Create(ctx, subIns.DeepCopy())).Should(Succeed())

		sub2 := subIns.DeepCopy()
		sub2Key := types.NamespacedName{Namespace: subKey.Namespace, Name: "2ndsub"}

		sub2.SetName(sub2Key.Name)
		sub2.SetNamespace(sub2Key.Namespace)
		Expect(k8sClt.Create(ctx, sub2.DeepCopy())).Should(Succeed())

		testBranch := "master"
		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, sub2.DeepCopy())).Should(Succeed())
		}()

		Eventually(registerSub(subKey), pullInterval*3, pullInterval).Should(Succeed())
		Eventually(registerSub(sub2Key), pullInterval*3, pullInterval).Should(Succeed())

		sr := gitOps.GetSubRecords()

		Expect(sr[subKey]).Should(Equal(ansibleGitURL))
		Expect(sr[sub2Key]).Should(Equal(ansibleGitURL))

		rr := gitOps.GetRepoRecords()

		branchInfo := rr[ansibleGitURL].branchs[testBranch]

		Expect(branchInfo.registeredSub).Should(HaveKey(subKey))
		Expect(branchInfo.registeredSub).Should(HaveKey(sub2Key))

		Eventually(deRegisterSub(subKey), pullInterval*3, pullInterval).Should(Succeed())

		sr = gitOps.GetSubRecords()

		Expect(sr).ShouldNot(HaveKey(subKey))
		Expect(sr[sub2Key]).Should(Equal(ansibleGitURL))

		rr = gitOps.GetRepoRecords()

		branchInfo = rr[ansibleGitURL].branchs[testBranch]
		Expect(branchInfo.registeredSub).ShouldNot(HaveKey(subKey))
		Expect(branchInfo.registeredSub).Should(HaveKey(sub2Key))
		Expect(branchInfo.registeredSub).Should(HaveLen(1))

		Eventually(checkGitRegCommit(testBranch), pullInterval*3, pullInterval).Should(Succeed())
	})

	It("should update commitID, after prehook is applied", func() {
		subIns := subIns.DeepCopy()
		chnIns := chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-hub-git-3", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-hub-git-3", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())
		Expect(k8sClt.Create(ctx, subIns.DeepCopy())).Should(Succeed())

		testBranch := "master"
		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns.DeepCopy())).Should(Succeed())
		}()

		Eventually(registerSub(subKey), specTimeOut, pullInterval).Should(Succeed())

		sr := gitOps.GetSubRecords()

		Expect(sr[subKey]).Should(Equal(ansibleGitURL))

		rr := gitOps.GetRepoRecords()

		branchInfo := rr[ansibleGitURL].branchs[testBranch]
		//Expect(branchInfo.lastCommitID).Should(Equal(defaultCommit))
		Expect(branchInfo.registeredSub).Should(HaveKey(subKey))

		detectTargetCommit := func(key types.NamespacedName, t string) func() error {
			return func() error {
				u := &subv1.Subscription{}

				if err := k8sClt.Get(ctx, key, u); err != nil {
					return err
				}

				subCommit := getCommitID(u)

				rr := gitOps.GetRepoRecords()

				branchInfo := rr[ansibleGitURL].branchs[testBranch]

				if !strings.EqualFold(branchInfo.lastCommitID, defaultCommit) {
					return fmt.Errorf("subscription commit is not updated in git registry")
				}

				// here's trapped into prehook
				if strings.EqualFold(subCommit, t) {
					return nil
				}

				return fmt.Errorf("subscription commit is not updated")
			}
		}

		Eventually(detectTargetCommit(subKey, ""), specTimeOut, pullInterval).Should(Succeed())

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

		foundKey := types.NamespacedName{Name: ansibleIns.GetName(), Namespace: ansibleIns.GetNamespace()}

		Eventually(forceUpdatePrehook(k8sClt, foundKey), specTimeOut, pullInterval).Should(Succeed())

		Eventually(detectTargetCommit(subKey, defaultCommit), specTimeOut, pullInterval).Should(Succeed())
	})

	It("register the 2nd branch subscription", func() {
		subIns := subIns.DeepCopy()
		chnIns := chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-hub-git-4", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-hub-git-4", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())
		Expect(k8sClt.Create(ctx, subIns.DeepCopy())).Should(Succeed())

		sub2 := subIns.DeepCopy()
		sub2Key := types.NamespacedName{Namespace: subKey.Namespace, Name: "2ndsub"}

		testBranch2 := "release-2.1"
		setBranch(sub2, testBranch2)

		sub2.SetName(sub2Key.Name)
		sub2.SetNamespace(sub2Key.Namespace)
		Expect(k8sClt.Create(ctx, sub2.DeepCopy())).Should(Succeed())

		testBranch := "master"
		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, sub2.DeepCopy())).Should(Succeed())
		}()

		Eventually(registerSub(subKey), pullInterval*3, pullInterval).Should(Succeed())
		Eventually(registerSub(sub2Key), pullInterval*3, pullInterval).Should(Succeed())

		sr := gitOps.GetSubRecords()

		Expect(sr[subKey]).Should(Equal(ansibleGitURL))
		Expect(sr[sub2Key]).Should(Equal(ansibleGitURL))

		rr := gitOps.GetRepoRecords()

		branchInfo := rr[ansibleGitURL].branchs[testBranch]
		Expect(branchInfo.registeredSub).Should(HaveKey(subKey))

		branchInfo2 := rr[ansibleGitURL].branchs[testBranch2]
		Expect(branchInfo2.registeredSub).Should(HaveKey(sub2Key))

		Eventually(deRegisterSub(subKey), pullInterval*3, pullInterval).Should(Succeed())

		sr = gitOps.GetSubRecords()

		Expect(sr).ShouldNot(HaveKey(subKey))
		Expect(sr[sub2Key]).Should(Equal(ansibleGitURL))

		rr = gitOps.GetRepoRecords()

		branchInfo2 = rr[ansibleGitURL].branchs[testBranch2]
		Expect(branchInfo2.registeredSub).Should(HaveKey(sub2Key))

		Eventually(checkGitRegCommit(testBranch2), specTimeOut, pullInterval).Should(Succeed())
	})

	It("should update the deployables annotations right away when there's no prehook", func() {
		subIns := subIns.DeepCopy()
		chnIns := chnIns.DeepCopy()

		chnIns.SetNamespace(fmt.Sprintf("%s-git-post-1", chnIns.GetNamespace()))
		chnKey := types.NamespacedName{Name: chnIns.GetName(), Namespace: chnIns.GetNamespace()}
		subIns.Spec.Channel = chnKey.String()

		subIns.SetNamespace(fmt.Sprintf("%s-git-post-1", subIns.GetNamespace()))
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

		a := subIns.GetAnnotations()
		a[subv1.AnnotationGitPath] = "test/hooks/ansible/post-only"
		subIns.SetAnnotations(a)

		Expect(k8sClt.Create(ctx, chnIns.DeepCopy())).Should(Succeed())
		Expect(k8sClt.Create(ctx, subIns.DeepCopy())).Should(Succeed())

		defer func() {
			Expect(k8sClt.Delete(ctx, chnIns.DeepCopy())).Should(Succeed())
			Expect(k8sClt.Delete(ctx, subIns.DeepCopy())).Should(Succeed())
		}()

		detectTargetCommit := func(key types.NamespacedName, t string) func() error {
			return func() error {
				u := &subv1.Subscription{}

				if err := k8sClt.Get(ctx, key, u); err != nil {
					return err
				}

				subCommit := getCommitID(u)

				// here's trapped into prehook
				if !strings.EqualFold(subCommit, t) {
					return fmt.Errorf("subscription commit is not updated")
				}

				dplAnno := u.GetAnnotations()[subv1.AnnotationDeployables]
				if len(dplAnno) == 0 {
					return fmt.Errorf("deployables annotations is not updated")
				}

				return nil
			}
		}

		Eventually(detectTargetCommit(subKey, defaultCommit), specTimeOut, pullInterval).Should(Succeed())
	})
})
