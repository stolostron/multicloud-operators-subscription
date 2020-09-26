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
	stdlog "log"
	"os"
	"path/filepath"
	"sync"
	"testing"

	tlog "github.com/go-logr/logr/testing"
	"github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	spokeClusterV1 "github.com/open-cluster-management/api/cluster/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

const (
	skipCRDupdate = true
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	customAPIServerFlags := []string{"--disable-admission-plugins=NamespaceLifecycle,LimitRanger,ServiceAccount," +
		"TaintNodesByCondition,Priority,DefaultTolerationSeconds,DefaultStorageClass,StorageObjectInUseProtection," +
		"PersistentVolumeClaimResize,ResourceQuota",
	}

	apiServerFlags := append([]string(nil), envtest.DefaultKubeAPIServerFlags...)
	apiServerFlags = append(apiServerFlags, customAPIServerFlags...)

	updateCRDs := func() error {
		if skipCRDupdate {
			return nil
		}

		storepath := "../../../hack/test"

		tests := []string{"deployable", "channel", "ansiblejob", "managedcluster", "helmrelease", "placementrule"}

		return utils.UpdateCRDs(storepath, tests, tlog.NullLogger{})
	}

	if err := updateCRDs(); err != nil {
		stdlog.Fatal(err)
	}

	t := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "deploy", "crds"),
			filepath.Join("..", "..", "..", "hack", "test"),
		},
		KubeAPIServerFlags: apiServerFlags,
	}

	ansiblejob.AddToScheme(scheme.Scheme)
	apis.AddToScheme(scheme.Scheme)
	spokeClusterV1.AddToScheme(scheme.Scheme)

	var err error
	if cfg, err = t.Start(); err != nil {
		stdlog.Fatal(err)
	}

	code := m.Run()

	t.Stop()
	os.Exit(code)
}

// SetupTestReconcile returns a reconcile.Reconcile implementation that delegates to inner and
// writes the request to requests after Reconcile is finished.
func SetupTestReconcile(inner reconcile.Reconciler) (reconcile.Reconciler, chan reconcile.Request) {
	requests := make(chan reconcile.Request)
	fn := reconcile.Func(func(req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(req)
		requests <- req

		return result, err
	})

	return fn, requests
}

// StartTestManager adds recFn
func StartTestManager(mgr manager.Manager, g *gomega.GomegaWithT) (chan struct{}, *sync.WaitGroup) {
	stop := make(chan struct{})
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()
		g.Expect(mgr.Start(stop)).NotTo(gomega.HaveOccurred())
	}()

	return stop, wg
}
