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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

func TestTopoAnnotationUpdateHelm(t *testing.T) {
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

	g.Expect(subAnno[subv1.AnnotationTopo]).ShouldNot(gomega.HaveLen(0))
	fmt.Println(subAnno[subv1.AnnotationTopo])
}

func TestTopoAnnotationUpdateNsOrObjChannel(t *testing.T) {
	var (
		tpChnKey = types.NamespacedName{
			Name:      "test-chn",
			Namespace: "test-chn-namespace",
		}

		tpChn = &chnv1.Channel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tpChnKey.Name,
				Namespace: tpChnKey.Namespace,
			},
			Spec: chnv1.ChannelSpec{
				Type: chnv1.ChannelTypeNamespace,
			},
		}

		cfgMapKey = types.NamespacedName{
			Name:      "skip-cert-verify",
			Namespace: tpChnKey.Namespace,
		}

		cfgMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfgMapKey.Name,
				Namespace: cfgMapKey.Namespace,
			},
			Data: map[string]string{
				"insecureSkipVerify": "true",
			},
		}
	)

	var (
		tpSubKey = types.NamespacedName{
			Name:      "topo-anno-sub",
			Namespace: "topo-anno-sub-namespace",
		}

		tpSub = &subv1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subkey.Name,
				Namespace: subkey.Namespace,
			},
			Spec: subv1.SubscriptionSpec{
				Channel: chnkey.String(),
			},
		}
	)

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

	g.Expect(c.Create(cfgMap)).Should(gomega.Succeed())

	defer func() {
		g.Expect(c.Delete(cfgMap)).Should(gomega.Succeed())
	}()

	rec := newReconciler(mgr).(*ReconcileSubscription)

	g.Expect(c.Create(tpChn)).NotTo(gomega.HaveOccurred())

	defer func() {
		g.Expect(c.Delete(tpChn)).Should(gomega.Succeed())
	}()

	g.Expect(c.Create(tpSub)).NotTo(gomega.HaveOccurred())

	defer func() {
		g.Expect(c.Delete(tpSub)).Should(gomega.Succeed())
	}()

	g.Expect(rec.doMCMHubReconcile(tpSub)).NotTo(gomega.HaveOccurred())

	subAnno := tpSub.GetAnnotations()
	g.Expect(subAnno).ShouldNot(gomega.HaveLen(0))

	g.Expect(subAnno[subv1.AnnotationTopo]).ShouldNot(gomega.HaveLen(0))
	fmt.Println(subAnno[subv1.AnnotationTopo])
}
