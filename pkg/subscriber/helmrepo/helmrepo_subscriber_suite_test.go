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

package helmrepo

import (
	"os"
	"testing"
	"time"

	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	mgr "sigs.k8s.io/controller-runtime/pkg/manager"

	"open-cluster-management.io/multicloud-operators-subscription/pkg/apis"
)

const (
	k8swait      = time.Second * 3
	StartTimeout = 30 // seconds
)

var testEnv *envtest.Environment
var k8sManager mgr.Manager
var k8sClient client.Client

func TestSubscriptionNamespaceReconcile(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t,
		"Helm Controller Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")

	t := true
	if os.Getenv("TEST_USE_EXISTING_CLUSTER") == "true" {
		testEnv = &envtest.Environment{
			UseExistingCluster: &t,
		}
	} else {
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "deploy", "crds"),
				filepath.Join("..", "..", "..", "hack", "test")},
		}
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = apis.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sManager, err = mgr.New(cfg, mgr.Options{MetricsBindAddress: "0"})
	Expect(err).ToNot(HaveOccurred())

	Expect(Add(k8sManager, k8sManager.GetConfig(), &types.NamespacedName{}, 2, false, false)).NotTo(HaveOccurred())
	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	gexec.KillAndWait(5 * time.Second)
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
