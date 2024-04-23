/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	appsubReportV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	cluster    = "local-cluster"
	appSubNs   = "appsub-ns"
	appSubName = "appsub-test"
	message    = "Failed to deploy to cluster"
)

func TestAppSubPropagationFailedAppsubReport(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	appsubReportV1alpha1.AddToScheme(mgr.GetScheme())

	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	stop := make(chan struct{})
	defer close(stop)

	g.Expect(mgr.GetCache().WaitForCacheSync(ctx)).Should(gomega.BeTrue())

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	g.Expect(CreateFailedAppsubReportResult(c, cluster, appSubNs, appSubName, message)).NotTo(gomega.HaveOccurred())

	time.Sleep(1 * time.Second)

	appsubReport, err := getClusterAppsubReport(c, cluster, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	prResultFoundIndex := -1
	prResultSource := appSubNs + "/" + appSubName

	for i, result := range appsubReport.Results {
		if result.Source == prResultSource {
			prResultFoundIndex = i
			break
		}
	}

	g.Expect(prResultFoundIndex).Should(gomega.Equal(0))
}
