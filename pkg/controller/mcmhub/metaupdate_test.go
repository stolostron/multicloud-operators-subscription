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
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
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
			Namespace: "tp-chn-namespace",
		}

		cfgMapKey = types.NamespacedName{
			Name:      "skip-cert-verify",
			Namespace: tpChnKey.Namespace,
		}

		tpCfgMap = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfgMapKey.Name,
				Namespace: cfgMapKey.Namespace,
			},
			Data: map[string]string{
				"insecureSkipVerify": "true",
			},
		}

		tpChn = &chnv1.Channel{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Channel",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      tpChnKey.Name,
				Namespace: tpChnKey.Namespace,
			},
			Spec: chnv1.ChannelSpec{
				Type: chnv1.ChannelTypeNamespace,
				ConfigMapRef: &corev1.ObjectReference{
					Name:      cfgMapKey.Name,
					Namespace: cfgMapKey.Namespace,
				},
			},
		}

		selector = map[string]string{"a": "b"}

		tDeploy = &apps.Deployment{
			TypeMeta: metav1.TypeMeta{
				Kind: "Deployment",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpl-deployment",
				Namespace: tpChnKey.Namespace,
			},
			Spec: apps.DeploymentSpec{
				Replicas: func() *int32 { i := int32(1); return &i }(),
				Selector: &metav1.LabelSelector{MatchLabels: selector},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: selector,
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Image: "foo/bar",
							},
						},
					},
				},
			},
		}

		dplCm = &dplv1.Deployable{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Deployable",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpl-cm",
				Namespace: tpChnKey.Namespace,
				Labels: map[string]string{
					chnv1.KeyChannel:     tpChn.Name,
					chnv1.KeyChannelType: string(tpChn.Spec.Type),
				},
			},
			Spec: dplv1.DeployableSpec{
				Template: &runtime.RawExtension{
					Object: tpCfgMap,
				},
			},
		}

		dplDeploy = &dplv1.Deployable{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Deployable",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpl-deploy",
				Namespace: tpChnKey.Namespace,
				Labels: map[string]string{
					chnv1.KeyChannel:     tpChn.Name,
					chnv1.KeyChannelType: string(tpChn.Spec.Type),
				},
			},
			Spec: dplv1.DeployableSpec{
				Template: &runtime.RawExtension{
					Object: tDeploy,
				},
			},
		}
	)

	var (
		tpSubKey = types.NamespacedName{
			Name:      "topo-anno-sub",
			Namespace: "topo-anno-sub-namespace",
		}

		tpSub = &subv1.Subscription{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.open-cluster-management.io/v1",
				Kind:       "Subscription",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      tpSubKey.Name,
				Namespace: tpSubKey.Namespace,
			},
			Spec: subv1.SubscriptionSpec{
				Channel: tpChnKey.String(),
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

	ctx := context.TODO()

	g.Expect(mgr.GetCache().WaitForCacheSync(stopMgr)).Should(gomega.BeTrue())

	g.Expect(c.Create(ctx, tpCfgMap)).Should(gomega.Succeed())

	defer func() {
		g.Expect(c.Delete(ctx, tpCfgMap)).Should(gomega.Succeed())
	}()

	g.Expect(c.Create(ctx, dplDeploy)).Should(gomega.Succeed())

	defer func() {
		g.Expect(c.Delete(ctx, dplDeploy)).Should(gomega.Succeed())
	}()

	g.Expect(c.Create(ctx, dplCm)).Should(gomega.Succeed())

	defer func() {
		g.Expect(c.Delete(ctx, dplCm)).Should(gomega.Succeed())
	}()

	rec := newReconciler(mgr).(*ReconcileSubscription)

	g.Expect(c.Create(ctx, tpChn)).NotTo(gomega.HaveOccurred())

	defer func() {
		g.Expect(c.Delete(ctx, tpChn)).Should(gomega.Succeed())
	}()

	g.Expect(c.Create(ctx, tpSub)).NotTo(gomega.HaveOccurred())

	defer func() {
		g.Expect(c.Delete(ctx, tpSub)).Should(gomega.Succeed())
	}()

	g.Expect(rec.doMCMHubReconcile(tpSub)).NotTo(gomega.HaveOccurred())

	subAnno := tpSub.GetAnnotations()
	g.Expect(subAnno).ShouldNot(gomega.HaveLen(0))

	g.Expect(subAnno[subv1.AnnotationTopo]).ShouldNot(gomega.HaveLen(0))
}
