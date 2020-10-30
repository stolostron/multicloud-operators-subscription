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
	"testing"
	"time"

	tlog "github.com/go-logr/logr/testing"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	spokeClusterV1 "github.com/open-cluster-management/api/cluster/v1"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	mgr "sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
)

const (
	StartTimeout        = 30 // seconds
	hookRequeueInterval = time.Second * 1
	testGitPath         = "../../.."
)

var k8sManager mgr.Manager
var k8sClt client.Client
var specTimeOut = pullInterval * 10
var setRequeueInterval = func(r *ReconcileSubscription) {
	r.hookRequeueInterval = hookRequeueInterval
}

var gitOps GitOps
var defaultCommit = "test-00000001"

func TestHookReconcile(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Hook Test Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	By("bootstrapping test environment")

	err := apis.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = ansiblejob.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = spokeClusterV1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sManager, err = mgr.New(cfg, mgr.Options{MetricsBindAddress: "0"})
	Expect(err).ToNot(HaveOccurred())

	cFunc := func(repo, branch, user, pwd string) (string, error) {
		return defaultCommit, nil
	}

	cloneFunc := func(string, string, string, string, string, bool) (string, error) {
		return defaultCommit, nil
	}

	localRepoDidr := func(*chnv1.Channel, *subv1.Subscription) string {
		return testGitPath
	}

	defaulRequeueInterval = time.Second * 1

	gitOps = NewHookGit(k8sManager.GetClient(), setHubGitOpsLogger(tlog.NullLogger{}),
		setHubGitOpsInterval(hookRequeueInterval*1),
		setGetCommitFunc(cFunc),
		setGetCloneFunc(cloneFunc),
		setLocalDirResovler(localRepoDidr),
	)

	rec := newReconciler(k8sManager, setRequeueInterval, resetHubGitOps(gitOps))
	// adding the reconcile to manager
	Expect(add(k8sManager, rec)).Should(Succeed())
	go func() {
		Expect(k8sManager.Start(ctrl.SetupSignalHandler())).ToNot(HaveOccurred())
	}()

	k8sClt = k8sManager.GetClient()
	Expect(k8sClt).ToNot(BeNil())

	close(done)
}, StartTimeout)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
})
