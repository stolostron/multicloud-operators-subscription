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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	spokeClusterV1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis"
	mgr "sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
)

const (
	StartTimeout        = 30 // seconds
	hookRequeueInterval = time.Second * 1
)

var k8sManager mgr.Manager
var k8sClt client.Client
var setRequeueInterval = func(r *ReconcileSubscription) {
	r.hookRequeueInterval = hookRequeueInterval
}

func TestHookReconcile(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Hook Test Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	By("bootstrapping test environment")

	//	t := true
	//	if os.Getenv("TEST_USE_EXISTING_CLUSTER") == "true" {
	//		testEnv = &envtest.Environment{
	//			UseExistingCluster: &t,
	//		}
	//	} else {
	//		customAPIServerFlags := []string{"--disable-admission-plugins=NamespaceLifecycle,LimitRanger,ServiceAccount," +
	//			"TaintNodesByCondition,Priority,DefaultTolerationSeconds,DefaultStorageClass,StorageObjectInUseProtection," +
	//			"PersistentVolumeClaimResize,ResourceQuota",
	//		}
	//
	//		apiServerFlags := append([]string(nil), envtest.DefaultKubeAPIServerFlags...)
	//		apiServerFlags = append(apiServerFlags, customAPIServerFlags...)
	//
	//		testEnv = &envtest.Environment{
	//			CRDDirectoryPaths: []string{
	//				filepath.Join("..", "..", "..", "deploy", "crds"),
	//				filepath.Join("..", "..", "..", "hack", "test"),
	//			},
	//			KubeAPIServerFlags: apiServerFlags,
	//		}
	//	}

	//	cfg, err := testEnv.Start()
	//	Expect(err).ToNot(HaveOccurred())
	//	Expect(cfg).ToNot(BeNil())

	err := apis.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = ansiblejob.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = spokeClusterV1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	close(done)
}, StartTimeout)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
})
