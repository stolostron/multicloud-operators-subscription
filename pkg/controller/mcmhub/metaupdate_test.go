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
	"context"
	"fmt"
	"testing"

	"github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

func TestTopoAnnotationUpdate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	g.Expect(mgr.GetCache().WaitForCacheSync(stopMgr)).Should(gomega.BeTrue())

	g.Expect(c.Create(context.TODO(), cfgMap)).Should(gomega.Succeed())

	defer func() {
		g.Expect(c.Delete(context.TODO(), cfgMap)).Should(gomega.Succeed())
	}()

	rec := newReconciler(mgr).(*ReconcileSubscription)

	chn := chHelm.DeepCopy()
	g.Expect(c.Create(context.TODO(), chn)).NotTo(gomega.HaveOccurred())

	defer func() {
		g.Expect(c.Delete(context.TODO(), chn)).Should(gomega.Succeed())
	}()

	subIns := subscription.DeepCopy()
	subIns.SetName("helm-sub")
	subIns.Spec.Channel = helmKey.String()
	subIns.Spec.PackageFilter = nil
	g.Expect(c.Create(context.TODO(), subIns)).NotTo(gomega.HaveOccurred())

	defer func() {
		g.Expect(c.Delete(context.TODO(), subIns)).Should(gomega.Succeed())
	}()

	g.Expect(rec.doMCMHubReconcile(subIns)).NotTo(gomega.HaveOccurred())

	subAnno := subIns.GetAnnotations()
	g.Expect(subAnno).ShouldNot(gomega.HaveLen(0))

	g.Expect(subAnno[appv1alpha1.AnnotationTopo]).ShouldNot(gomega.HaveLen(0))
	fmt.Println(subAnno[appv1alpha1.AnnotationTopo])
}
