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

package objectbucket

import (
	stdlog "log"
	"os"
	"sync"
	"testing"

	"path/filepath"

	"github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	customAPIServerFlags := []string{"--disable-admission-plugins=NamespaceLifecycle,LimitRanger,ServiceAccount," +
		"TaintNodesByCondition,Priority,DefaultTolerationSeconds,DefaultStorageClass,StorageObjectInUseProtection," +
		"PersistentVolumeClaimResize,ResourceQuota",
	}

	apiServerFlags := append([]string(nil), envtest.DefaultKubeAPIServerFlags...)
	apiServerFlags = append(apiServerFlags, customAPIServerFlags...)

	t := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "deploy", "crds"),
			filepath.Join("..", "..", "..", "hack", "test"),
		},
		KubeAPIServerFlags: apiServerFlags,
	}

	apis.AddToScheme(scheme.Scheme)

	var err error
	if cfg, err = t.Start(); err != nil {
		stdlog.Fatal(err)
	}

	code := m.Run()

	t.Stop()
	os.Exit(code)
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
