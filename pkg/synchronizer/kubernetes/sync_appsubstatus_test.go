// Copyright 2021 The Kubernetes Authors.
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

package kubernetes

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	v1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	hostSub1 = types.NamespacedName{
		Name:      "appsub-1",
		Namespace: "default",
	}

	githubsub = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostSub1.Name,
			Namespace: hostSub1.Namespace,
		},
	}

	appSubUnitStatus1 = SubscriptionUnitStatus{
		Name:       "pkg1",
		Namespace:  hostSub1.Namespace,
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Phase:      string(appSubStatusV1alpha1.PackageDeployed),
		Message:    "Success",
	}

	appSubUnitStatus2 = SubscriptionUnitStatus{
		Name:       "pkg2",
		Namespace:  hostSub1.Namespace,
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Phase:      string(appSubStatusV1alpha1.PackageDeployFailed),
		Message:    "Success",
	}
)

var _ = Describe("test create/update/delete appsub status for standalone and managed cluster", func() {
	It("Test appsubReports and appsubstatus on standalone and managed cluster", func() {
		//Create apsub
		err := k8sClient.Create(context.TODO(), githubsub)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		// Standalone - No cluster appsubReport created, only appsubstatus created in appsub NS
		appSubUnitStatuses := []SubscriptionUnitStatus{}
		appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus1)
		appsubClusterStatus := SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "APPLY",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}

		s := &KubeSynchronizer{
			Interval:               0,
			localCachedClient:      &cachedClient{},
			remoteCachedClient:     &cachedClient{},
			LocalClient:            k8sClient,
			RemoteClient:           k8sClient,
			hub:                    true,
			standalone:             true,
			DynamicClient:          nil,
			RestMapper:             nil,
			kmtx:                   sync.Mutex{},
			SynchronizerID:         &types.NamespacedName{},
			Extension:              nil,
			dmtx:                   sync.Mutex{},
			SkipAppSubStatusResDel: false,
		}

		err = s.SyncAppsubClusterStatus(nil, appsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		// Check appsubstatus exiss and no appsub report in appsub NS
		appsubName := appsubClusterStatus.AppSub.Name
		pkgstatusNs := appsubClusterStatus.AppSub.Namespace
		pkgstatusName := appsubName

		pkgstatuses := &v1alpha1.SubscriptionStatusList{}
		listopts := &client.ListOptions{Namespace: pkgstatusNs}
		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(1))
		Expect(pkgstatuses.Items[0].Name).To(gomega.Equal(pkgstatusName))
		Expect(len(pkgstatuses.Items[0].Statuses.SubscriptionStatus)).To(gomega.Equal(1))

		// No cluster appsub report is created
		appsubReport := &appSubStatusV1alpha1.SubscriptionReport{}
		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, appsubReport)
		Expect(err).To(HaveOccurred())
		Expect(errors.IsNotFound(err)).To(gomega.BeTrue())

		// Update to add a subscription unit status
		appSubUnitStatus2.Phase = string(appSubStatusV1alpha1.PackageDeployed)
		appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus2)
		updateAppsubClusterStatus := SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "APPLY",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}
		err = s.SyncAppsubClusterStatus(nil, updateAppsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(1))

		Expect(pkgstatuses.Items[0].Name).To(gomega.Equal(pkgstatusName))
		Expect(len(pkgstatuses.Items[0].Statuses.SubscriptionStatus)).To(gomega.Equal(2))

		// Delete
		rmAppsubClusterStatus := SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "DELETE",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}

		err = s.SyncAppsubClusterStatus(nil, rmAppsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(0))

		// Wait for cleanup to complete
		time.Sleep(4 * time.Second)

		// Test appsubReports and appsubstatus on a managed cluster
		appSubUnitStatuses = []SubscriptionUnitStatus{}
		appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus1)

		appsubClusterStatus = SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "APPLY",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}

		s = &KubeSynchronizer{
			Interval:               0,
			localCachedClient:      &cachedClient{},
			remoteCachedClient:     &cachedClient{},
			LocalClient:            k8sClient,
			RemoteClient:           k8sClient,
			hub:                    false,
			standalone:             false,
			DynamicClient:          nil,
			RestMapper:             nil,
			kmtx:                   sync.Mutex{},
			SynchronizerID:         &types.NamespacedName{},
			Extension:              nil,
			dmtx:                   sync.Mutex{},
			SkipAppSubStatusResDel: false,
		}

		err = s.SyncAppsubClusterStatus(nil, appsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		// Check appsubstatus exiss and no appsub report in appsub NS
		appsubName = appsubClusterStatus.AppSub.Name
		pkgstatusNs = appsubClusterStatus.AppSub.Namespace
		pkgstatusName = appsubName

		pkgstatuses = &v1alpha1.SubscriptionStatusList{}
		listopts = &client.ListOptions{Namespace: pkgstatusNs}
		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(1))
		Expect(pkgstatuses.Items[0].Name).To(gomega.Equal(pkgstatusName))

		// Cluster appsub report is created with a deployed result
		cAppsubReport := &appSubStatusV1alpha1.SubscriptionReport{}
		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(cAppsubReport.Results)).To(gomega.Equal(1))
		Expect(cAppsubReport.Results[0].Result).To(gomega.Equal(appSubStatusV1alpha1.SubscriptionResult("deployed")))

		// Update to add a failed subscription unit status
		appSubUnitStatus2.Phase = string(appSubStatusV1alpha1.PackageDeployFailed)
		appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus2)
		updateAppsubClusterStatus = SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "APPLY",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}
		err = s.SyncAppsubClusterStatus(nil, updateAppsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(10 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(1))

		Expect(pkgstatuses.Items[0].Name).To(gomega.Equal(pkgstatusName))
		Expect(len(pkgstatuses.Items[0].Statuses.SubscriptionStatus)).To(gomega.Equal(2))

		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(cAppsubReport.Results)).To(gomega.Equal(1))
		Expect(cAppsubReport.Results[0].Result).To(gomega.Equal(appSubStatusV1alpha1.SubscriptionResult("failed")))

		// Update to change phase of failed subscription unit status to deployed
		appSubUnitStatus2.Phase = string(appSubStatusV1alpha1.PackageDeployed)
		appSubUnitStatuses = []SubscriptionUnitStatus{}
		appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus1)
		appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus2)
		updateAppsubClusterStatus = SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "APPLY",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}
		err = s.SyncAppsubClusterStatus(nil, updateAppsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(1))

		Expect(pkgstatuses.Items[0].Name).To(gomega.Equal(pkgstatusName))
		Expect(len(pkgstatuses.Items[0].Statuses.SubscriptionStatus)).To(gomega.Equal(2))

		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(cAppsubReport.Results)).To(gomega.Equal(1))
		Expect(cAppsubReport.Results[0].Result).To(gomega.Equal(appSubStatusV1alpha1.SubscriptionResult("deployed")))

		// Delete
		rmAppsubClusterStatus = SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "DELETE",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}

		err = s.SyncAppsubClusterStatus(nil, rmAppsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(0))

		// clean up cluster policy report after the test. Or it could fail the first standalone test if the test is done earlier
		err = k8sClient.Delete(context.TODO(), cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
	})
})
