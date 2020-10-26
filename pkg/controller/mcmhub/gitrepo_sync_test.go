// Copyright 2020 The Kubernetes Authors.
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

var (
	sharedkey = types.NamespacedName{
		Name:      "gittest",
		Namespace: "default",
	}
	githubchn = &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: chnv1.ChannelSpec{
			Type:     "Git",
			Pathname: "https://github.com/open-cluster-management/multicloud-operators-subscription.git",
		},
	}

	githubsub = &appv1.Subscription{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Subscription",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: appv1.SubscriptionSpec{
			Channel: sharedkey.String(),
		},
	}
)

func TestUpdateGitDeployablesAnnotation(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec := newReconciler(mgr).(*ReconcileSubscription)

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	githubsub.UID = "dummyid"

	annotations := make(map[string]string)
	annotations[appv1.AnnotationGitPath] = "test/github"
	githubsub.SetAnnotations(annotations)

	// No channel yet. It will fail and return false.
	ret, err := rec.UpdateGitDeployablesAnnotation(githubsub)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(ret).To(gomega.BeFalse())

	err = c.Create(context.TODO(), githubchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(2 * time.Second)

	ret, err = rec.UpdateGitDeployablesAnnotation(githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ret).To(gomega.BeTrue())

	time.Sleep(2 * time.Second)

	subDeployables := rec.getSubscriptionDeployables(githubsub)
	// To align with 3 new test yaml resources being added to test/github repo in #331 :-)
	g.Expect(len(subDeployables)).To(gomega.Equal(42))

	rec.deleteSubscriptionDeployables(githubsub)

	time.Sleep(2 * time.Second)

	subDeployables = rec.getSubscriptionDeployables(githubsub)
	g.Expect(len(subDeployables)).To(gomega.Equal(0))

	err = c.Delete(context.TODO(), githubchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}
