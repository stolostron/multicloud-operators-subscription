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
	"os"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

var (
	channelStr = `apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: test-channel
  namespace: default
spec:
  type: GitHub
  pathname: https://github.com/open-cluster-management/multicloud-operators-subscription.git`

	subscriptionStr = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    apps.open-cluster-management.io/github-path: test/github
  uid: dummyid
spec:
  channel: default/test-channel`
)

func TestUpdateGitDeployablesAnnotation(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	rec := newReconciler(mgr).(*ReconcileSubscription)

	sub := &appv1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionStr), &sub)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Should fail because no channel
	updated := rec.UpdateGitDeployablesAnnotation(sub)
	g.Expect(updated).To(gomega.BeFalse())

	channel := &chnv1.Channel{}
	err = yaml.Unmarshal([]byte(channelStr), &channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(2 * time.Second)

	updated = rec.UpdateGitDeployablesAnnotation(sub)
	g.Expect(updated).To(gomega.BeTrue())

	annotations := sub.GetAnnotations()
	g.Expect(annotations[appv1.AnnotationDeployables]).NotTo(gomega.BeEmpty())

	err = os.RemoveAll(utils.GetLocalGitFolder(channel, sub))
	g.Expect(err).NotTo(gomega.HaveOccurred())
}
