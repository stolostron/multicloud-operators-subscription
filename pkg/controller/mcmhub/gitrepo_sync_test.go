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

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	testutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
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
			Pathname: "https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git",
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

func TestGetGitResources(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec := newReconciler(mgr).(*ReconcileSubscription)

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	g.Expect(mgr.GetCache().WaitForCacheSync(ctx)).Should(gomega.BeTrue())

	githubsub.UID = "dummyid"

	annotations := make(map[string]string)
	annotations[appv1.AnnotationGitPath] = "test/github"
	githubsub.SetAnnotations(annotations)

	// No channel yet. It will fail and return false.
	_, err = rec.GetGitResources(githubsub, false)
	g.Expect(err).To(gomega.HaveOccurred())

	err = c.Create(context.TODO(), githubchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(2 * time.Second)

	_, err = rec.GetGitResources(githubsub, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(2 * time.Second)

	err = c.Delete(context.TODO(), githubchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}
