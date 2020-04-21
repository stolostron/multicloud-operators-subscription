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
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	sharedkey = types.NamespacedName{
		Name:      "githubtest",
		Namespace: "default",
	}
	githubchn = &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: chnv1.ChannelSpec{
			Type:     "GitHub",
			Pathname: "https://github.com/open-cluster-management/multicloud-operators-subscription.git",
		},
	}
	githubsub = &appv1.Subscription{
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
	annotations[appv1.AnnotationGithubPath] = "test/github"
	githubsub.SetAnnotations(annotations)

	// No channel yet. It will fail and return false.
	ret := rec.UpdateGitDeployablesAnnotation(githubsub)
	g.Expect(ret).To(gomega.BeFalse())

	err = c.Create(context.TODO(), githubchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(3 * time.Second)

	ret = rec.UpdateGitDeployablesAnnotation(githubsub)
	g.Expect(ret).To(gomega.BeTrue())
}
