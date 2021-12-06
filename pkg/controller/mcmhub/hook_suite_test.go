// Copyright 2021 The Kubernetes Authors.
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
	"log"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	workV1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/apis"
	ansiblejob "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/ansible/v1alpha1"
	subv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appsubReportV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	mgr "sigs.k8s.io/controller-runtime/pkg/manager"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
)

const (
	StartTimeout        = 30 // seconds
	hookRequeueInterval = time.Second * 1
	testGitPath         = "../../.."
)

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

	k8sManager, err := mgr.New(cfg, mgr.Options{MetricsBindAddress: "0"})
	Expect(err).ToNot(HaveOccurred())

	k8sClt = k8sManager.GetClient()
	Expect(k8sClt).ToNot(BeNil())

	err = apis.AddToScheme(k8sManager.GetScheme())
	Expect(err).NotTo(HaveOccurred())

	err = ansiblejob.AddToScheme(k8sManager.GetScheme())
	Expect(err).NotTo(HaveOccurred())

	err = spokeClusterV1.AddToScheme(k8sManager.GetScheme())
	Expect(err).NotTo(HaveOccurred())

	err = appsubReportV1alpha1.AddToScheme(k8sManager.GetScheme())
	Expect(err).NotTo(HaveOccurred())

	err = workV1.AddToScheme(k8sManager.GetScheme())
	Expect(err).NotTo(HaveOccurred())

	cloneFunc := func(*utils.GitCloneOption) (string, error) {
		return defaultCommit, nil
	}

	localRepoDidr := func(*subv1.Subscription) string {
		return testGitPath
	}

	defaulRequeueInterval = time.Second * 1

	gitOps = NewHookGit(k8sManager.GetClient(), setHubGitOpsLogger(logr.DiscardLogger{}),
		setHubGitOpsInterval(hookRequeueInterval*1),
		setGetCloneFunc(cloneFunc),
		setLocalDirResovler(localRepoDidr),
	)

	rec := newReconciler(k8sManager, setRequeueInterval, resetHubGitOps(gitOps))
	// adding the reconcile to manager
	Expect(add(k8sManager, rec)).Should(Succeed())
	go func() {
		Expect(k8sManager.Start(ctrl.SetupSignalHandler())).ToNot(HaveOccurred())
	}()

	err = k8sClt.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "normal-sub"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = k8sClt.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "ansible-reconcile-1"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = k8sClt.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "ansible-pre-0"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = k8sClt.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "ansible-pre-2"},
	})
	if err != nil {
		log.Fatal(err)
	}

	close(done)
}, StartTimeout)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
})
