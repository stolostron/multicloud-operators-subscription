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

package kubernetes

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	appsubReportV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	v1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	hostSub1 = types.NamespacedName{
		Name:      "appsub-1",
		Namespace: "default",
	}

	appSubUnitStatus1 = SubscriptionUnitStatus{
		Name:       "pkg1",
		Namespace:  hostSub1.Namespace,
		ApiVersion: "v1",
		Kind:       "ConfigMap",
		Phase:      string(appSubStatusV1alpha1.PackageDeployed),
		Message:    "Success",
	}

	appSubUnitStatus2 = SubscriptionUnitStatus{
		Name:       "pkg2",
		Namespace:  hostSub1.Namespace,
		ApiVersion: "v1",
		Kind:       "ConfigMap",
		Phase:      string(appSubStatusV1alpha1.PackageDeployFailed),
		Message:    "Success",
	}
)

var _ = Describe("test create/update/delete appsub status for standalone", func() {
	It("No cluster appsubReport created, only appsubstatus created in appsub NS", func() {
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

		err := s.SyncAppsubClusterStatus(nil, appsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		// Check appsubstatus exiss and no appsub report in appsub NS
		appsubName := appsubClusterStatus.AppSub.Name
		pkgstatusNs := appsubClusterStatus.AppSub.Namespace
		pkgstatusName := pkgstatusNs + "." + appsubName

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

		time.Sleep(4 * time.Second)

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

		time.Sleep(4 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(0))

		// Wait for cleanup to complete
		time.Sleep(4 * time.Second)
	})
})

var _ = Describe("test create/update/delete appsub status for managed cluster", func() {
	It("Test appsubReports and appsubstatus on a managed cluster", func() {
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

		err := s.SyncAppsubClusterStatus(nil, appsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		// Check appsubstatus exiss and no appsub report in appsub NS
		appsubName := appsubClusterStatus.AppSub.Name
		pkgstatusNs := appsubClusterStatus.AppSub.Namespace
		pkgstatusName := pkgstatusNs + "." + appsubName

		pkgstatuses := &v1alpha1.SubscriptionStatusList{}
		listopts := &client.ListOptions{Namespace: pkgstatusNs}
		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(1))
		Expect(pkgstatuses.Items[0].Name).To(gomega.Equal(pkgstatusName))

		// Cluster appsub report is created but no failure
		cAppsubReport := &appsubReportV1alpha1.SubscriptionReport{}
		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(cAppsubReport.Results)).To(gomega.Equal(0))

		// Update to add a failed subscription unit status
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

		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(cAppsubReport.Results)).To(gomega.Equal(1))

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

		time.Sleep(4 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(1))

		Expect(pkgstatuses.Items[0].Name).To(gomega.Equal(pkgstatusName))
		Expect(len(pkgstatuses.Items[0].Statuses.SubscriptionStatus)).To(gomega.Equal(2))

		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(cAppsubReport.Results)).To(gomega.Equal(0))

		// Delete
		rmAppsubClusterStatus := SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "DELETE",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}

		err = s.SyncAppsubClusterStatus(nil, rmAppsubClusterStatus, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(gomega.Equal(0))

		// clean up cluster policy report after the test. Or it could fail the first standalone test if the test is done ealier
		err = k8sClient.Delete(context.TODO(), cPolicyReport)
		Expect(err).NotTo(HaveOccurred())
	})
})
