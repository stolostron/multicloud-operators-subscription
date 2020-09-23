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
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
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

	nsChannelYaml = `apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: ns
  namespace: ch-ns
spec:
  type: Namespace`

	nsSubYaml = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: ns-sub
  namespace: ns-sub-ns
spec:
  channel: ch-ns/ns`

	nsSubDplYaml = `apiVersion: apps.open-cluster-management.io/v1
kind: Deployable
metadata:
  name: ns-sub-deployable
  namespace: ns-sub-ns
spec:
  placement:
  template:
status:
  lastUpdateTime: "2020-05-12T00:26:48Z"
  phase: Propagated
  targetClusters:
    cluster1:
      lastUpdateTime: "2020-05-12T00:26:48Z"
      phase: Deployed
      resourceStatus:
        lastUpdateTime: "2020-05-12T00:25:47Z"
        message: Active
        phase: Subscribed
        statuses:
          /:
            packages:
              dev-test:
                lastUpdateTime: "2020-05-12T00:25:47Z"
                phase: Subscribed
              payload-cfg-namespace-channel-5m9rm:
                lastUpdateTime: "2020-05-09T21:34:14Z"
                phase: Subscribed
              payload-cfg-namespace-channel-m9j9d:
                lastUpdateTime: "2020-05-09T02:22:28Z"
                phase: Subscribed`

	helmChannelYaml = `apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: helm
  namespace: ch-helm
spec:
  type: HelmRepo`

	helmSubYaml = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: helm-sub
  namespace: helm-sub-ns
  spec:
    channel: ch-helm/helm
    name: gbapp`

	helmSubDplYaml = `apiVersion: apps.open-cluster-management.io/v1
kind: Deployable
metadata:
  name: nginx-deployable
  namespace: ns-sub-1
spec:
  placement:
  template:
status:
  lastUpdateTime: "2020-05-12T03:15:41Z"
  phase: Propagated
  targetClusters:
    cluster1:
      lastUpdateTime: "2020-05-12T03:15:41Z"
      phase: Deployed
      resourceStatus:
        lastUpdateTime: "2020-05-12T03:07:21Z"
        message: Active
        phase: Subscribed
        statuses:
          /:
            packages:
              predev-ch-nginx-ingress-1.36.3:
                lastUpdateTime: "2020-05-12T03:07:21Z"
                phase: Subscribed
                resourceStatus:
                  conditions:
                  - lastTransitionTime: "2020-04-29T04:33:44Z"
                    status: "True"
                    type: Initialized
                  - lastTransitionTime: "2020-04-29T04:33:47Z"
                    message: "failed to install release, no matches for kind Deployment in version extensions/v1beta1"
                    reason: InstallError
                    status: "True"
                    type: ReleaseFailed`
)

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
	annotations[appv1.AnnotationGitBranch] = "branch1"
	annotations[appv1.AnnotationGitPath] = "test/github"
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

func TestUpdateSubscriptionStatus(t *testing.T) {
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

	// verify namespace channel subscription status as Subscribed
	nssub := &appv1.Subscription{}
	err = yaml.Unmarshal([]byte(nsSubYaml), &nssub)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	nschn := &chnv1.Channel{}
	err = yaml.Unmarshal([]byte(nsChannelYaml), &nschn)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	nsdpl := &dplv1.Deployable{}
	err = yaml.Unmarshal([]byte(nsSubDplYaml), &nsdpl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = rec.updateSubscriptionStatus(nssub, nsdpl, nschn)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	for _, cstatus := range nssub.Status.Statuses {
		for _, pkgStatus := range cstatus.SubscriptionPackageStatus {
			g.Expect(string(pkgStatus.Phase)).To(gomega.Equal("Subscribed"))
		}
	}

	time.Sleep(2 * time.Second)

	// verify helmRepo channel subscription status as Failed
	helmsub := &appv1.Subscription{}
	err = yaml.Unmarshal([]byte(helmSubYaml), &helmsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	helmchn := &chnv1.Channel{}
	err = yaml.Unmarshal([]byte(helmChannelYaml), &helmchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	helmdpl := &dplv1.Deployable{}
	err = yaml.Unmarshal([]byte(helmSubDplYaml), &helmdpl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = rec.updateSubscriptionStatus(helmsub, helmdpl, helmchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	for _, cstatus := range helmsub.Status.Statuses {
		for _, pkgStatus := range cstatus.SubscriptionPackageStatus {
			g.Expect(string(pkgStatus.Phase)).To(gomega.Equal("Failed"))
		}
	}
}
