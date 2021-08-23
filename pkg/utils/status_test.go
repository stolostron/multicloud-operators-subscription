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
	v1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	cluster    = "local-cluster"
	appSubNs   = "appsub-ns"
	appSubName = "appsub-test"
	message    = "Failed to deploy to cluster"
)

func TestAppSubPropagationFailedPackageStatus(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
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

	g.Expect(CreatePropagatioFailedAppSubPackageStatus(c, cluster, true, appSubNs, appSubName, message)).NotTo(gomega.HaveOccurred())

	time.Sleep(1 * time.Second)

	pkgKey := types.NamespacedName{
		Name:      appSubName,
		Namespace: cluster,
	}
	pkgstatus := &v1alpha1.SubscriptionPackageStatus{}
	g.Expect(c.Get(context.TODO(), pkgKey, pkgstatus)).NotTo(gomega.HaveOccurred())
	g.Expect(pkgstatus.Namespace).To(gomega.Equal(cluster))
	g.Expect(len(pkgstatus.Statuses.SubscriptionPackageStatus)).To(gomega.Equal(1))

	pkgFailStatus := pkgstatus.Statuses.SubscriptionPackageStatus[0]
	g.Expect(pkgFailStatus.Phase).To(gomega.Equal(v1alpha1.PackagePropagationFailed))
	g.Expect(pkgFailStatus.Message).To(gomega.Equal(message))

	g.Expect(c.Delete(context.TODO(), pkgstatus)).NotTo(gomega.HaveOccurred())
}
