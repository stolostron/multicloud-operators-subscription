// Copyright 2026 The Kubernetes Authors.
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

package helmrepo

// This is a Ginkgo spec (registered into the suite run by
// TestSubscriptionNamespaceReconcile in helmrepo_subscriber_suite_test.go)
// rather than a standalone `func TestXxx`, because it depends on the
// package-level k8sManager/testEnv that BeforeSuite populates.

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	releasev1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

// This covers the fix in manageHelmCR (helm_subscriber_item.go) that passes
// the SubscriberItem's live clusterAdmin field into both
// utils.CreateHelmCRManifest (which stamps the cluster-admin annotation onto
// the generated HelmRelease CR) and ProcessSubResources, instead of the
// previous hardcoded false. The annotation on the HelmRelease CR is what
// pkg/helmrelease/release later reads to decide whether the chart may deploy
// cluster-scoped resources.
var _ = Describe("manageHelmCR cluster-admin annotation propagation", func() {
	It("stamps the cluster-admin annotation on the generated HelmRelease CR only when clusterAdmin is true", func() {
		mgr, err := manager.New(k8sManager.GetConfig(), manager.Options{
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		testClient := mgr.GetClient()

		ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)

		go func() {
			_ = mgr.Start(ctx)
		}()

		defer cancel()

		clusterNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cluster-scope-helmcr"}}
		Expect(testClient.Create(context.TODO(), clusterNS)).NotTo(HaveOccurred())

		defer func() { _ = testClient.Delete(context.TODO(), clusterNS) }()

		testSub := &appv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "helmcr-clusteradmin-test",
				Namespace: "default",
			},
		}
		Expect(testClient.Create(context.TODO(), testSub)).NotTo(HaveOccurred())

		defer func() { _ = testClient.Delete(context.TODO(), testSub) }()

		testChn := &chnv1.Channel{
			ObjectMeta: metav1.ObjectMeta{Name: "helmcr-clusteradmin-chn", Namespace: "default"},
			Spec: chnv1.ChannelSpec{
				Type:     chnv1.ChannelTypeHelmRepo,
				Pathname: "https://charts.example.com/",
			},
		}

		chartDirs := map[string]string{
			"../../../test/github/helmcharts/chart1/": "../../../test/github/helmcharts/chart1/",
		}

		indexFile, err := utils.GenerateHelmIndexFile(testSub, "../../..", chartDirs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(indexFile.Entries)).To(Equal(1))

		defaultExtension := &kubesynchronizer.SubscriptionExtension{}
		syncid := &types.NamespacedName{Namespace: "cluster-scope-helmcr", Name: "cluster-scope-helmcr"}
		sync, err := kubesynchronizer.CreateSynchronizer(
			mgr.GetConfig(), k8sManager.GetConfig(), mgr.GetScheme(), syncid, 60, defaultExtension, true, false)
		Expect(err).NotTo(HaveOccurred())

		hrsi := &SubscriberItem{}
		hrsi.Channel = testChn
		hrsi.Subscription = testSub
		hrsi.synchronizer = sync

		releaseCRName, err := utils.PkgToReleaseCRName(testSub, "chart1")
		Expect(err).NotTo(HaveOccurred())

		releaseKey := types.NamespacedName{Name: releaseCRName, Namespace: testSub.Namespace}
		deployedHR := &releasev1.HelmRelease{}

		defer func() { _ = testClient.Delete(context.TODO(), deployedHR) }()

		// Non-admin: the generated HelmRelease CR must not carry the
		// cluster-admin annotation.
		hrsi.clusterAdmin = false
		Expect(hrsi.manageHelmCR(indexFile)).NotTo(HaveOccurred())

		Expect(testClient.Get(context.TODO(), releaseKey, deployedHR)).NotTo(HaveOccurred())
		Expect(deployedHR.GetAnnotations()[appv1alpha1.AnnotationClusterAdmin]).NotTo(Equal("true"))

		// Admin: re-subscribing with clusterAdmin=true must stamp the
		// cluster-admin annotation onto the (now-existing) HelmRelease CR.
		hrsi.clusterAdmin = true
		Expect(hrsi.manageHelmCR(indexFile)).NotTo(HaveOccurred())

		Expect(testClient.Get(context.TODO(), releaseKey, deployedHR)).NotTo(HaveOccurred())
		Expect(deployedHR.GetAnnotations()[appv1alpha1.AnnotationClusterAdmin]).To(Equal("true"))
	})
})
