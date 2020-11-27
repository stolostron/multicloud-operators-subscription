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

package e2e

import (
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	clt "github.com/open-cluster-management/applifecycle-backend-e2e/client"
	"github.com/open-cluster-management/applifecycle-backend-e2e/webapp/server"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
)

const (
	defaultAddr    = "localhost:8765"
	defaultCfgDir  = "../cluster_config"
	defaultDataDir = "../applifecycle-backend-e2e/default-e2e-test-data"
	logLvl         = 1
	testTimeout    = 30
	pullInterval   = 3 * time.Second

	runEndpoint     = "/run"
	clusterEndpoint = "/clusters"
	Success         = "succeed"

	JUnitResult = "results"
)

func TestAppLifecycleAPI_E2E(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Subscription-E2E-test",
		[]Reporter{printer.NewlineReporter{}, reporters.NewJUnitReporter(JUnitResult)})
}

var DefaultRunner = clt.NewRunner(defaultAddr, runEndpoint)

var _ = BeforeSuite(func(done Done) {
	By("bootstrapping test environment")

	srv := server.NewServer(defaultAddr, defaultCfgDir, defaultDataDir, logLvl, testTimeout)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}

		fmt.Println("test server started")
	}()

	Eventually(func() error {
		return clt.IsSeverUp(defaultAddr, clusterEndpoint)
	}, testTimeout, pullInterval)

	close(done)
}, testTimeout)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	gexec.KillAndWait(3 * time.Second)
})
