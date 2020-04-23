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

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	subYAMLStr = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: githubtest
  namespace: default
spec:
  channel: default/github-ch`

	targetsubYAMLStr = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: githubtest-target
  namespace: default
  labels:
    key1: val1
    key2: val2
spec:
  channel: default/github-ch-2`
)

func TestSetNewSubscription(t *testing.T) {
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

	err = rec.setNewSubscription(githubsub, true, false)
	g.Expect(err).To(gomega.HaveOccurred())

	err = c.Create(context.TODO(), githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = rec.setNewSubscription(githubsub, true, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Delete(context.TODO(), githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestPrepareDeployableForSubscription(t *testing.T) {
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

	annotations := make(map[string]string)
	annotations[appv1.AnnotationWebhookEventCount] = "1"
	annotations[appv1.AnnotationGithubBranch] = "branch1"
	annotations[appv1.AnnotationGithubPath] = "test/github"
	githubsub.SetAnnotations(annotations)

	subDpl, err := rec.prepareDeployableForSubscription(githubsub, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(subDpl).NotTo(gomega.BeNil())
}

func TestUpdateSubscriptionToTarget(t *testing.T) {
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

	targetsub := &appv1.Subscription{}
	err = yaml.Unmarshal([]byte(targetsubYAMLStr), &targetsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), targetsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(2 * time.Second)

	sub := &appv1.Subscription{}
	err = yaml.Unmarshal([]byte(subYAMLStr), &sub)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	annotations := make(map[string]string)
	annotations[appv1.AnnotationRollingUpdateTarget] = targetsub.Name
	sub.SetAnnotations(annotations)

	time.Sleep(2 * time.Second)

	newSub, updated, err := rec.updateSubscriptionToTarget(sub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(updated).To(gomega.BeTrue())
	g.Expect(newSub).NotTo(gomega.BeNil())

	labels := newSub.GetLabels()
	g.Expect(labels["key1"]).To(gomega.Equal("val1"))
	g.Expect(labels["key2"]).To(gomega.Equal("val2"))

	g.Expect(newSub.Spec.Channel).To(gomega.Equal(targetsub.Spec.Channel))

	err = c.Delete(context.TODO(), targetsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}
