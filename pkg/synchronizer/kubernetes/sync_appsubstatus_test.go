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
	"reflect"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
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

	legacyAppSub = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hive-clusterimagesets-subscription-fast-0",
			Namespace: "default",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/git-branch": "release-2.6",
				"apps.open-cluster-management.io/git-path":   "clusterImageSets/fast",
				"meta.helm.sh/release-namespace":             "default",
			},
			Generation: 1,
			Labels: map[string]string{
				"app":                          "hive-clusterimagesets",
				"app.kubernetes.io/managed-by": "Helm",
				"subscription-pause":           "false",
			},
		},
		Spec: appv1.SubscriptionSpec{
			Channel: "default/acm-hive-openshift-releases-chn-0",
		},
	}

	toBeDeletedType = types.NamespacedName{
		Name:      "hive-clusterimagesets-subscription-to-be-deleted",
		Namespace: "default",
	}

	appToBeDeleted = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hive-clusterimagesets-subscription-to-be-deleted",
			Namespace: "default",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/git-branch": "release-2.6",
				"apps.open-cluster-management.io/git-path":   "clusterImageSets/fast",
				"meta.helm.sh/release-namespace":             "default",
			},
			Generation: 1,
			Labels: map[string]string{
				"app":                          "hive-clusterimagesets",
				"app.kubernetes.io/managed-by": "Helm",
				"subscription-pause":           "false",
			},
		},
		Spec: appv1.SubscriptionSpec{},
	}

	legacyAppSubNoChannel = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hive-clusterimagesets-subscription-fast-10",
			Namespace: "default",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/git-branch": "release-2.6",
				"apps.open-cluster-management.io/git-path":   "clusterImageSets/fast",
				"meta.helm.sh/release-namespace":             "default",
			},
			Generation: 1,
			Labels: map[string]string{
				"app":                          "hive-clusterimagesets",
				"app.kubernetes.io/managed-by": "Helm",
				"subscription-pause":           "false",
			},
		},
		Spec: appv1.SubscriptionSpec{},
	}

	legacyAppSubInvalidResource = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hive-clusterimagesets-subscription-fast-11",
			Namespace: "default",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/git-branch": "release-2.6",
				"apps.open-cluster-management.io/git-path":   "clusterImageSets/fast",
				"meta.helm.sh/release-namespace":             "default",
			},
			Generation: 1,
			Labels: map[string]string{
				"app":                          "hive-clusterimagesets",
				"app.kubernetes.io/managed-by": "Helm",
				"subscription-pause":           "false",
			},
		},
		Spec: appv1.SubscriptionSpec{
			Channel: "default/acm-hive-openshift-releases-chn-11",
		},
	}

	legacyAppSubBadChannel = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hive-clusterimagesets-subscription-fast-12",
			Namespace: "default",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/git-branch": "release-2.6",
				"apps.open-cluster-management.io/git-path":   "clusterImageSets/fast",
				"meta.helm.sh/release-namespace":             "default",
			},
			Generation: 1,
			Labels: map[string]string{
				"app":                          "hive-clusterimagesets",
				"app.kubernetes.io/managed-by": "Helm",
				"subscription-pause":           "false",
			},
		},
		Spec: appv1.SubscriptionSpec{
			Channel: "default",
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

	rmAppSubUnitStatus = SubscriptionUnitStatus{
		Name:       "pkg2",
		Namespace:  appToBeDeleted.Namespace,
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

		err = s.SyncAppsubClusterStatus(nil, appsubClusterStatus, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		// Check appsubstatus exiss and no appsub report in appsub NS
		appsubName := appsubClusterStatus.AppSub.Name
		pkgstatusNs := appsubClusterStatus.AppSub.Namespace
		pkgstatusName := appsubName

		pkgstatuses := &appSubStatusV1alpha1.SubscriptionStatusList{}
		listopts := &client.ListOptions{Namespace: pkgstatusNs}
		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(Equal(1))
		Expect(pkgstatuses.Items[0].Name).To(Equal(pkgstatusName))
		Expect(len(pkgstatuses.Items[0].Statuses.SubscriptionPackageStatus)).To(Equal(1))

		// No cluster appsub report is created
		appsubReport := &appSubStatusV1alpha1.SubscriptionReport{}
		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, appsubReport)
		Expect(err).To(HaveOccurred())
		Expect(errors.IsNotFound(err)).To(BeTrue())

		// Update to add a subscription unit status
		appSubUnitStatus2.Phase = string(appSubStatusV1alpha1.PackageDeployed)
		appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus2)
		updateAppsubClusterStatus := SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "APPLY",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}
		err = s.SyncAppsubClusterStatus(nil, updateAppsubClusterStatus, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(Equal(1))

		Expect(pkgstatuses.Items[0].Name).To(Equal(pkgstatusName))
		Expect(len(pkgstatuses.Items[0].Statuses.SubscriptionPackageStatus)).To(Equal(2))

		// Delete
		rmAppsubClusterStatus := SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "DELETE",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}

		err = s.SyncAppsubClusterStatus(nil, rmAppsubClusterStatus, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(Equal(0))

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

		err = s.SyncAppsubClusterStatus(nil, appsubClusterStatus, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		// Check appsubstatus exiss and no appsub report in appsub NS
		appsubName = appsubClusterStatus.AppSub.Name
		pkgstatusNs = appsubClusterStatus.AppSub.Namespace
		pkgstatusName = appsubName

		pkgstatuses = &appSubStatusV1alpha1.SubscriptionStatusList{}
		listopts = &client.ListOptions{Namespace: pkgstatusNs}
		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(Equal(1))
		Expect(pkgstatuses.Items[0].Name).To(Equal(pkgstatusName))

		// Cluster appsub report is created with a deployed result
		cAppsubReport := &appSubStatusV1alpha1.SubscriptionReport{}
		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(cAppsubReport.Results)).To(Equal(1))
		Expect(cAppsubReport.Results[0].Result).To(Equal(appSubStatusV1alpha1.SubscriptionResult("deployed")))

		// Update to add a failed subscription unit status
		appSubUnitStatus2.Phase = string(appSubStatusV1alpha1.PackageDeployFailed)
		appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus2)
		updateAppsubClusterStatus = SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "APPLY",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}
		err = s.SyncAppsubClusterStatus(nil, updateAppsubClusterStatus, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(10 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(Equal(1))

		Expect(pkgstatuses.Items[0].Name).To(Equal(pkgstatusName))
		Expect(len(pkgstatuses.Items[0].Statuses.SubscriptionPackageStatus)).To(Equal(2))

		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(cAppsubReport.Results)).To(Equal(1))
		Expect(cAppsubReport.Results[0].Result).To(Equal(appSubStatusV1alpha1.SubscriptionResult("failed")))

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
		err = s.SyncAppsubClusterStatus(nil, updateAppsubClusterStatus, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(Equal(1))

		Expect(pkgstatuses.Items[0].Name).To(Equal(pkgstatusName))
		Expect(len(pkgstatuses.Items[0].Statuses.SubscriptionPackageStatus)).To(Equal(2))

		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: appsubClusterStatus.Cluster, Namespace: appsubClusterStatus.Cluster}, cAppsubReport)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(cAppsubReport.Results)).To(Equal(1))
		Expect(cAppsubReport.Results[0].Result).To(Equal(appSubStatusV1alpha1.SubscriptionResult("deployed")))

		// Delete
		rmAppsubClusterStatus = SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    hostSub1,
			Action:                    "DELETE",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}

		err = s.SyncAppsubClusterStatus(nil, rmAppsubClusterStatus, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(8 * time.Second)

		err = k8sClient.List(context.TODO(), pkgstatuses, listopts)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pkgstatuses.Items)).To(Equal(0))

		// clean up cluster policy report after the test. Or it could fail the first standalone test if the test is done earlier
		err = k8sClient.Delete(context.TODO(), cAppsubReport)
		Expect(err).NotTo(HaveOccurred())

		s = &KubeSynchronizer{
			Interval:               0,
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

		// Create appsub for legacy subscription statuses.
		appsubStatus := legacyAppSub.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), appsubStatus)).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		// Expect empty appsubstatuses
		expectedAppSubStatuses := []appSubStatusV1alpha1.SubscriptionUnitStatus{}
		appsubStatuses := s.getResourcesByLegacySubStatus(appsubStatus)
		Expect(appsubStatuses).To(Equal(expectedAppSubStatuses))

		// Update appsub with legacy statuses
		appsubStatus.Status = appv1.SubscriptionStatus{
			LastUpdateTime: metav1.Now(),
			Statuses: appv1.SubscriptionClusterStatusMap{
				"/": &appv1.SubscriptionPerClusterStatus{
					SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
						"acm-hive-openshift-releases-chn-0-ClusterImageSet-img4.6.1-x86-64-appsub": {
							Phase:          "Subscribed",
							LastUpdateTime: metav1.Now(),
						},
						"acm-hive-openshift-releases-chn-0-ClusterImageSet-img4.6.3-x86-64-appsub": {
							Phase:          "Subscribed",
							LastUpdateTime: metav1.Now(),
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(context.TODO(), appsubStatus)).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		// Expect appsubstatuses to contain two legacy statuses.
		expectedAppSubStatuses = []appSubStatusV1alpha1.SubscriptionUnitStatus{}
		expectedAppSubStatuses = append(expectedAppSubStatuses, appSubStatusV1alpha1.SubscriptionUnitStatus{Name: "img4.6.1-x86-64-appsub", Kind: "ClusterImageSet"})
		expectedAppSubStatuses = append(expectedAppSubStatuses, appSubStatusV1alpha1.SubscriptionUnitStatus{Name: "img4.6.3-x86-64-appsub", Kind: "ClusterImageSet"})
		appsubStatuses = s.getResourcesByLegacySubStatus(appsubStatus)
		Expect(reflect.DeepEqual(appsubStatuses, expectedAppSubStatuses)).To(BeTrue())

		// Empty Spec.Channel
		appsubNoChannel := legacyAppSubNoChannel.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), appsubNoChannel)).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		expectedAppSubStatuses = []appSubStatusV1alpha1.SubscriptionUnitStatus{}
		appsubStatuses = s.getResourcesByLegacySubStatus(appsubNoChannel)
		Expect(appsubStatuses).To(Equal(expectedAppSubStatuses))

		// Invalid Spec.Channel
		appsubBadChannel := legacyAppSubBadChannel.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), appsubBadChannel)).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		expectedAppSubStatuses = []appSubStatusV1alpha1.SubscriptionUnitStatus{}
		appsubStatuses = s.getResourcesByLegacySubStatus(appsubBadChannel)
		Expect(appsubStatuses).To(Equal(expectedAppSubStatuses))

		// Invalid resource name
		appsubInvalidresource := legacyAppSubInvalidResource.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), appsubInvalidresource)).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		appsubInvalidresource.Status = appv1.SubscriptionStatus{
			LastUpdateTime: metav1.Now(),
			Statuses: appv1.SubscriptionClusterStatusMap{
				"/": &appv1.SubscriptionPerClusterStatus{
					SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
						"img4.6.1_x86_64_appsub": {
							Phase:          "Subscribed",
							LastUpdateTime: metav1.Now(),
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(context.TODO(), appsubInvalidresource)).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		expectedAppSubStatuses = []appSubStatusV1alpha1.SubscriptionUnitStatus{}
		appsubStatuses = s.getResourcesByLegacySubStatus(appsubInvalidresource)
		Expect(appsubStatuses).To(Equal(expectedAppSubStatuses))

		// Empty resource name
		appsubInvalidresource.Status = appv1.SubscriptionStatus{
			LastUpdateTime: metav1.Now(),
			Statuses: appv1.SubscriptionClusterStatusMap{
				"/": &appv1.SubscriptionPerClusterStatus{
					SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
						"": {
							Phase:          "Subscribed",
							LastUpdateTime: metav1.Now(),
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(context.TODO(), appsubInvalidresource)).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)

		expectedAppSubStatuses = []appSubStatusV1alpha1.SubscriptionUnitStatus{}
		appsubStatuses = s.getResourcesByLegacySubStatus(appsubInvalidresource)
		Expect(appsubStatuses).To(Equal(expectedAppSubStatuses))

		// create appsub for a to be deleted appsub
		appsubToBeDeleted := appToBeDeleted.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), appsubToBeDeleted)).NotTo(HaveOccurred())
		time.Sleep(4 * time.Second)

		appSubUnitStatuses = []SubscriptionUnitStatus{}
		appSubUnitStatuses = append(appSubUnitStatuses, rmAppSubUnitStatus)

		// Delete
		rmClusterStatus := SubscriptionClusterStatus{
			Cluster:                   "cluster1",
			AppSub:                    toBeDeletedType,
			Action:                    "DELETE",
			SubscriptionPackageStatus: appSubUnitStatuses,
		}

		err = s.SyncAppsubClusterStatus(appsubToBeDeleted, rmClusterStatus, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		// shouldSkip should return false when there is appsubstatus with empty status
		emptyAppsubStatus := appSubStatusV1alpha1.SubscriptionStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "appsubstatus-1",
				Namespace: "default",
			},
			Statuses: appSubStatusV1alpha1.SubscriptionClusterStatusMap{},
		}

		Expect(shouldSkip(appsubClusterStatus, true, emptyAppsubStatus)).To(BeFalse())
	})

	Context("Update overall subscription status in appsubstatus", Ordered, func() {
		var s *KubeSynchronizer

		statussub := &appv1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "status-sub",
				Namespace: "default",
			},
		}

		BeforeAll(func() {
			//Create apsub
			err := k8sClient.Create(context.TODO(), statussub)
			Expect(err).NotTo(HaveOccurred())

			s = &KubeSynchronizer{
				Interval:               0,
				LocalClient:            k8sClient,
				RemoteClient:           k8sClient,
				hub:                    false,
				standalone:             false,
				DynamicClient:          nil,
				RestMapper:             nil,
				kmtx:                   sync.Mutex{},
				SynchronizerID:         &types.NamespacedName{Namespace: "cluster1", Name: "cluster1"},
				Extension:              nil,
				dmtx:                   sync.Mutex{},
				SkipAppSubStatusResDel: false,
			}
		})

		AfterAll(func() {
			err := k8sClient.Delete(context.TODO(), statussub)
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("check overall subscription status in appsubstatus", func(deployFailed bool, reason string) {
			err := s.UpdateAppsubOverallStatus(statussub, deployFailed, reason)
			Expect(err).NotTo(HaveOccurred())

			pkgstatus, err := GetAppsubReportStatus(k8sClient, false, false, statussub.Namespace, statussub.Name)
			Expect(err).ToNot(HaveOccurred())

			phase := appSubStatusV1alpha1.SubscriptionDeployed
			if deployFailed {
				phase = appSubStatusV1alpha1.SubscriptionDeployFailed
			}
			Expect(pkgstatus.Statuses.SubscriptionStatus.Phase).To(Equal(phase))

			if deployFailed {
				Expect(pkgstatus.Statuses.SubscriptionStatus.Message).To(Equal(reason))
			} else {
				Expect(pkgstatus.Statuses.SubscriptionStatus.Message).To(Equal(""))
			}
		},
			Entry(nil, true, "something happened"),
			Entry(nil, false, "something happened"),
		)
	})
})
