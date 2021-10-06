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
	"sigs.k8s.io/controller-runtime/pkg/manager"
	policyReportV1alpha2 "sigs.k8s.io/wg-policy-prototypes/policy-report/pkg/api/wgpolicyk8s.io/v1alpha2"
)

var (
	cluster    = "local-cluster"
	appSubNs   = "appsub-ns"
	appSubName = "appsub-test"
	message    = "Failed to deploy to cluster"
)

func TestAppSubPropagationFailedPolicyReport(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	policyReportV1alpha2.AddToScheme(mgr.GetScheme())

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

	g.Expect(CreateFailedPolicyReportResult(c, cluster, appSubNs, appSubName, message)).NotTo(gomega.HaveOccurred())

	time.Sleep(1 * time.Second)

	policyReport, err := getClusterPolicyReport(c, appSubNs, appSubName, cluster, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	prResultFoundIndex := -1
	prResultSource := appSubNs + "/" + appSubName
	for i, result := range policyReport.Results {
		if result.Source == prResultSource && result.Policy == "APPSUB_FAILURE" {
			prResultFoundIndex = i
			break
		}
	}
	g.Expect(prResultFoundIndex).Should(gomega.Equal(0))
}
