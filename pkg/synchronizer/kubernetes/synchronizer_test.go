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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	promTestUtils "github.com/prometheus/client_golang/prometheus/testutil"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/metrics"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

var (
	clusterNamespace = &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster2",
		},
		Spec: corev1.NamespaceSpec{},
	}

	host = types.NamespacedName{
		Name:      "cluster2",
		Namespace: "cluster2",
	}
	hostworkload4 = types.NamespacedName{
		Name:      "subscription-4",
		Namespace: "appsub-ns-1",
	}
	hostworkload5 = types.NamespacedName{
		Name:      "github-resource-subscription-local",
		Namespace: "appsub-ns-1",
	}
	hostSub = types.NamespacedName{
		Name:      "appsub-1",
		Namespace: "appsub-ns-1",
	}

	selector = map[string]string{"a": "b"}

	workload1Configmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configmap1",
			Namespace: "appsub-ns-1",
			Annotations: map[string]string{
				appv1alpha1.AnnotationHosting: hostSub.Namespace + "/" + hostSub.Name,
			},
		},
	}
	workload2Deployment = apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment1",
			Namespace: "appsub-ns-1",
			Annotations: map[string]string{
				appv1alpha1.AnnotationHosting: hostSub.Namespace + "/" + hostSub.Name,
			},
		},
		Spec: apps.DeploymentSpec{
			Replicas: func() *int32 { i := int32(1); return &i }(),
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selector,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "foobar",
							Image: "foo/bar",
						},
					},
				},
			},
		},
	}
	workload3Configmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configmap2",
			Namespace: "appsub-ns-1",
			Annotations: map[string]string{
				appv1alpha1.AnnotationHosting: "not owned",
			},
		},
	}
	workload4Configmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configmap2",
			Namespace: "appsub-ns-1",
			Annotations: map[string]string{
				appv1alpha1.AnnotationResourceDoNotDeleteOption: "true",
			},
		},
	}
	workload4Subscription = appv1alpha1.Subscription{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Subscription",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostworkload4.Name,
			Namespace: hostworkload4.Namespace,
			Annotations: map[string]string{
				appv1alpha1.AnnotationHosting: hostworkload4.Namespace + "/" + hostworkload4.Name,
			},
		},
	}
	workload5Subscription = appv1alpha1.Subscription{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Subscription",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostworkload5.Name,
			Namespace: hostworkload5.Namespace,
			Annotations: map[string]string{
				appv1alpha1.AnnotationResourceReconcileOption: "merge",
				appv1alpha1.AnnotationHosting:                 hostworkload5.Namespace + "/" + hostworkload5.Name,
			},
			Labels: map[string]string{
				"label1": "label",
			},
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: "appsub-ns-1/acm-hive-openshift-releases-chn-1",
		},
	}
	workload6Subscription = appv1alpha1.Subscription{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Subscription",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostworkload5.Name,
			Namespace: hostworkload5.Namespace,
			Annotations: map[string]string{
				appv1alpha1.AnnotationResourceReconcileOption: "merge",
				appv1alpha1.AnnotationHosting:                 hostworkload5.Namespace + "/" + hostworkload5.Name,
			},
			Labels: map[string]string{
				"label1": "label",
			},
		},
	}

	legacyAppSubStatus = &appv1alpha1.Subscription{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Subscription",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hive-clusterimagesets-subscription-fast-2",
			Namespace: "appsub-ns-1",
			Annotations: map[string]string{
				"apps.open-cluster-management.io/git-branch": "release-2.6",
				"apps.open-cluster-management.io/git-path":   "clusterImageSets/fast",
				"meta.helm.sh/release-namespace":             "appsub-ns-1",
			},
			Generation: 1,
			Labels: map[string]string{
				"app":                          "hive-clusterimagesets",
				"app.kubernetes.io/managed-by": "Helm",
				"subscription-pause":           "false",
			},
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: "appsub-ns-1/acm-hive-openshift-releases-chn-2",
		},
	}
)

var _ = Describe("test Delete Single Subscribed Resource", func() {
	var sync *KubeSynchronizer
	var err error

	BeforeEach(func() {
		sync, err = CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		err = sync.Start(context.TODO())
		if err != nil {
			klog.Error(err)
			return
		}
	})

	It("should delete the confimap and deployment without failure", func() {
		workload1 := workload1Configmap.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		pkgStatus := appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "configmap1",
			Namespace:  "appsub-ns-1",
			APIVersion: "v1",
			Kind:       "ConfigMap",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).NotTo(HaveOccurred())

		workload2 := workload2Deployment.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload2)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload2)

		pkgStatus = appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "deployment1",
			Namespace:  "appsub-ns-1",
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should have an invalid apiversion pkgStatus", func() {
		workload1 := workload1Configmap.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		pkgStatus := appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "configmap1",
			Namespace:  "appsub-ns-1",
			APIVersion: "",
			Kind:       "ConfigMap",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).To(HaveOccurred())

		// Failed resources with no apiversion are skipped
		pkgStatus.Phase = "Failed"

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should not be owned by the subscription", func() {
		workload1 := workload3Configmap.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		pkgStatus := appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "configmap2",
			Namespace:  "appsub-ns-1",
			APIVersion: "v1",
			Kind:       "ConfigMap",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should not delete if the resource has do-not-delete annotation", func() {
		workload1 := workload4Configmap.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		pkgStatus := appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "configmap2",
			Namespace:  "appsub-ns-1",
			APIVersion: "v1",
			Kind:       "ConfigMap",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).To(BeNil())
	})
})

var _ = Describe("test PurgeAllSubscribedResources", func() {
	var sync *KubeSynchronizer
	var err error

	BeforeEach(func() {
		sync, err = CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		err = sync.Start(context.TODO())
		if err != nil {
			klog.Error(err)
			return
		}
	})

	It("should not find appSubStatus", func() {
		appsub := workload5Subscription.DeepCopy()
		// Not actually creating subscription

		err = sync.PurgeAllSubscribedResources(appsub)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should purge everything", func() {
		appsub := workload5Subscription.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), appsub)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), appsub)

		subAnnotations := make(map[string]string)
		subAnnotations[appv1alpha1.AnnotationClusterAdmin] = "true"
		subAnnotations[appv1alpha1.AnnotationResourceReconcileOption] = "merge"
		subAnnotations[appv1alpha1.AnnotationGitBranch] = "main"
		appsub.SetAnnotations(subAnnotations)

		err = sync.PurgeAllSubscribedResources(appsub)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should purge legacy unit statuses", func() {
		appsub := workload5Subscription.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), appsub)).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)
		appsub.Status = appv1alpha1.SubscriptionStatus{
			LastUpdateTime: metav1.Now(),
			Statuses: appv1alpha1.SubscriptionClusterStatusMap{
				"/": &appv1alpha1.SubscriptionPerClusterStatus{
					SubscriptionPackageStatus: map[string]*appv1alpha1.SubscriptionUnitStatus{
						"acm-hive-openshift-releases-chn-1-ClusterImageSet-img4.6.4-x86-64-appsub": {
							Phase:          "Subscribed",
							LastUpdateTime: metav1.Now(),
						},
						"acm-hive-openshift-releases-chn-1-ClusterImageSet-img4.6.5-x86-64-appsub": {
							Phase:          "Subscribed",
							LastUpdateTime: metav1.Now(),
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(context.TODO(), appsub)).NotTo(HaveOccurred())
		time.Sleep(8 * time.Second)

		Expect(k8sClient.Get(context.TODO(),
			types.NamespacedName{Name: appsub.Name,
				Namespace: appsub.Namespace}, appsub)).NotTo(HaveOccurred())

		err = sync.PurgeAllSubscribedResources(appsub)
		Expect(err).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), appsub)
	})

	It("should fail to find legacy unit statuses", func() {
		appsub := legacyAppSubStatus.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), appsub)).NotTo(HaveOccurred())

		time.Sleep(4 * time.Second)
		appsub.Status = appv1alpha1.SubscriptionStatus{
			LastUpdateTime: metav1.Now(),
			Statuses: appv1alpha1.SubscriptionClusterStatusMap{
				"/": &appv1alpha1.SubscriptionPerClusterStatus{
					SubscriptionPackageStatus: map[string]*appv1alpha1.SubscriptionUnitStatus{
						"acm-hive-openshift-releases-chn-2-ClusterImageSet-img4.6.6-x86-64-appsub": {
							Phase:          "Subscribed",
							LastUpdateTime: metav1.Now(),
						},
						"acm-hive-openshift-releases-chn-2-ClusterImageSet-img4.6.7-x86-64-appsub": {
							Phase:          "Subscribed",
							LastUpdateTime: metav1.Now(),
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(context.TODO(), appsub)).NotTo(HaveOccurred())
		time.Sleep(8 * time.Second)

		Expect(k8sClient.Get(context.TODO(),
			types.NamespacedName{Name: "hive-clusterimagesets-subscription-fast-2",
				Namespace: "appsub-ns-1"}, appsub)).NotTo(HaveOccurred())

		err = sync.PurgeAllSubscribedResources(appsub)
		Expect(err).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), appsub)
	})
})

var _ = Describe("test ProcessSubResources", Ordered, func() {
	var sync *KubeSynchronizer
	var err error

	BeforeEach(func() {
		sync, err = CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		err = sync.Start(context.TODO())
		if err != nil {
			klog.Error(err)
			return
		}

		metrics.LocalDeploymentFailedPullTime.Reset()
		metrics.LocalDeploymentSuccessfulPullTime.Reset()
	})

	BeforeAll(func() {
		Expect(k8sClient.Create(context.TODO(), clusterNamespace)).NotTo(HaveOccurred())
	})

	It("should err after overriding", func() {
		appsub := workload5Subscription.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), appsub)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), appsub)

		// Create a single resource
		resource := &unstructured.Unstructured{}
		resource.SetNamespace("appsub-ns-1")
		resource.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "apps.open-cluster-management.io/v1",
			Kind:    "Subscription",
		})
		resource.SetAnnotations(make(map[string]string))
		resource.SetLabels(map[string]string{"app.kubernetes.io/part-of": "appsub-obj-1"})
		resource.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Subscription",
			Name:       appsub.Name,
			UID:        appsub.UID,
		}})
		resource.SetKind("") // should error with empty kind

		resourceList := []ResourceUnit{{Resource: resource, Gvk: resource.GetObjectKind().GroupVersionKind()}}
		allowedGroupResources, deniedGroupResources := utils.GetAllowDenyLists(*appsub)

		err = sync.ProcessSubResources(appsub, resourceList, allowedGroupResources, deniedGroupResources, false)
		Expect(err).NotTo(HaveOccurred())

		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentFailedPullTime)).To(Equal(1))
		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentSuccessfulPullTime)).To(BeZero())
	})

	It("should create a single new resource", func() {
		appsub := workload5Subscription.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), appsub)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), appsub)

		// Create a single resource
		resource := &unstructured.Unstructured{}
		resource.SetNamespace("appsub-ns-1")
		resource.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "apps.open-cluster-management.io/v1",
			Kind:    "Subscription",
		})
		resource.SetAnnotations(make(map[string]string))
		resource.SetLabels(map[string]string{"app.kubernetes.io/part-of": "appsub-obj-1"})
		resource.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Subscription",
			Name:       appsub.Name,
			UID:        appsub.UID,
		}})

		resourceList := []ResourceUnit{{Resource: resource, Gvk: resource.GetObjectKind().GroupVersionKind()}}
		allowedGroupResources, deniedGroupResources := utils.GetAllowDenyLists(*appsub)

		err = sync.ProcessSubResources(appsub, resourceList, allowedGroupResources, deniedGroupResources, false)
		Expect(err).NotTo(HaveOccurred())

		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentFailedPullTime)).To(BeZero())
		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentSuccessfulPullTime)).To(Equal(1))
	})

	It("should create a single new resource v1 apiversion", func() {
		appsub := workload5Subscription.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), appsub)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), appsub)

		// Create a single resource
		resource := &unstructured.Unstructured{}
		resource.SetNamespace("appsub-ns-1")
		resource.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Subscription",
		})
		resource.SetAnnotations(make(map[string]string))
		resource.SetLabels(map[string]string{"app.kubernetes.io/part-of": "appsub-obj-1"})
		resource.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "v1",
			Kind:       "Subscription",
			Name:       appsub.Name,
			UID:        appsub.UID,
		}})

		resourceList := []ResourceUnit{{Resource: resource, Gvk: resource.GetObjectKind().GroupVersionKind()}}
		allowedGroupResources, deniedGroupResources := utils.GetAllowDenyLists(*appsub)

		err = sync.ProcessSubResources(appsub, resourceList, allowedGroupResources, deniedGroupResources, false)
		Expect(err).NotTo(HaveOccurred())

		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentFailedPullTime)).To(Equal(1))
		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentSuccessfulPullTime)).To(BeZero())
	})

	It("Configmap with missing namespace", func() {
		appsub := workload6Subscription.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), appsub)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), appsub)

		// Create a single resource configmap
		resource := &unstructured.Unstructured{}
		resource.SetNamespace("appsub-ns-1-2")
		resource.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		})
		resource.SetAnnotations(map[string]string{appv1alpha1.AnnotationClusterAdmin: "true"})
		resource.SetLabels(make(map[string]string))
		resource.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "Configmap3",
		}})

		resourceList := []ResourceUnit{{Resource: resource, Gvk: resource.GetObjectKind().GroupVersionKind()}}
		allowedGroupResources, deniedGroupResources := utils.GetAllowDenyLists(*appsub)

		err = sync.ProcessSubResources(appsub, resourceList, allowedGroupResources, deniedGroupResources, false)
		Expect(err).NotTo(HaveOccurred())

		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentFailedPullTime)).To(Equal(1))
		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentSuccessfulPullTime)).To(BeZero())
	})

	It("Configmap with missing namespace and different subscription", func() {
		appsub := workload5Subscription.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), appsub)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), appsub)

		// Create a single resource configmap
		resource := &unstructured.Unstructured{}
		resource.SetNamespace("appsub-ns-1-2")
		resource.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		})
		resource.SetAnnotations(map[string]string{appv1alpha1.AnnotationClusterAdmin: "true"})
		resource.SetLabels(make(map[string]string))
		resource.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       "Configmap3",
		}})

		resourceList := []ResourceUnit{{Resource: resource, Gvk: resource.GetObjectKind().GroupVersionKind()}}
		allowedGroupResources, deniedGroupResources := utils.GetAllowDenyLists(*appsub)

		err = sync.ProcessSubResources(appsub, resourceList, allowedGroupResources, deniedGroupResources, false)
		Expect(err).NotTo(HaveOccurred())

		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentFailedPullTime)).To(Equal(1))
		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentSuccessfulPullTime)).To(BeZero())
	})

	It("should have 0 resources", func() {
		appsub := workload5Subscription.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), appsub)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), appsub)

		resourceList := []ResourceUnit{}
		allowedGroupResources, deniedGroupResources := utils.GetAllowDenyLists(*appsub)

		err = sync.ProcessSubResources(appsub, resourceList, allowedGroupResources, deniedGroupResources, false)
		Expect(err).NotTo(HaveOccurred())

		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentFailedPullTime)).To(BeZero())
		Expect(promTestUtils.CollectAndCount(metrics.LocalDeploymentSuccessfulPullTime)).To(BeZero())
	})

	AfterAll(func() {
		k8sClient.Delete(context.TODO(), clusterNamespace)
	})
})

var _ = Describe("test IsResourceNamespaced", func() {
	var sync *KubeSynchronizer
	var err error

	BeforeEach(func() {
		sync, err = CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		err = sync.Start(context.TODO())
		if err != nil {
			klog.Error(err)
			return
		}
	})

	It("should pass finding GVR", func() {
		resource := unstructured.Unstructured{}
		resource.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "apps/v1",
			Kind:    "Deployment",
		})

		isNamespaced := sync.IsResourceNamespaced(&resource)
		Expect(isNamespaced).To(BeTrue())
	})

	It("should fail finding GVR", func() {
		resource := unstructured.Unstructured{}
		resource.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "",
			Kind:    "",
		})

		isNamespaced := sync.IsResourceNamespaced(&resource)
		Expect(isNamespaced).To(BeFalse())
	})
})

var _ = Describe("test getHostingAppSub", func() {
	var sync *KubeSynchronizer
	var err error

	BeforeEach(func() {
		sync, err = CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		err = sync.Start(context.TODO())
		if err != nil {
			klog.Error(err)
			return
		}
	})

	It("should not find hosting appsub", func() {
		// No actual subscription should exist
		subscription, err := sync.getHostingAppSub(hostSub)
		Expect(err).To(HaveOccurred())
		Expect(subscription).To(BeNil())
	})

	It("should find hosting appsub", func() {
		workload1 := workload4Subscription.DeepCopy()
		// Actually creating the subscription
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		_, err := sync.getHostingAppSub(hostworkload4)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("test cleanup of resources", func() {
	var sync *KubeSynchronizer
	var err error

	BeforeEach(func() {
		sync, err = CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		err = sync.Start(context.TODO())
		if err != nil {
			klog.Error(err)
			return
		}
	})
	It("should cleanup the appsubstatus, the confimap and deployment without failure", func() {
		workload1 := workload1Configmap.DeepCopy()
		workload1.Annotations = map[string]string{appv1alpha1.AnnotationHosting: "appsub-ns-1/appsubstatus-1"}
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		workload2 := workload2Deployment.DeepCopy()
		workload2.Annotations = map[string]string{appv1alpha1.AnnotationHosting: "appsub-ns-1/appsubstatus-1"}
		Expect(k8sClient.Create(context.TODO(), workload2)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload2)

		appSubStatus := &appSubStatusV1alpha1.SubscriptionStatus{
			TypeMeta: metav1.TypeMeta{
				Kind:       "SubscriptionStatus",
				APIVersion: "apps.open-cluster-management.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "appsubstatus-1",
				Namespace: "appsub-ns-1",
			},
		}

		Expect(k8sClient.Create(context.TODO(), appSubStatus)).NotTo(HaveOccurred())
		time.Sleep(8 * time.Second)
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: "appsub-ns-1", Name: "appsubstatus-1"}, appSubStatus)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), appSubStatus)

		appSubStatus.Statuses = appSubStatusV1alpha1.SubscriptionClusterStatusMap{
			SubscriptionStatus: []appSubStatusV1alpha1.SubscriptionUnitStatus{
				{
					Name:           "configmap1",
					Namespace:      "appsub-ns-1",
					APIVersion:     "v1",
					Kind:           "ConfigMap",
					LastUpdateTime: metav1.Now(),
				},
				{
					Name:           "deployment1",
					Namespace:      "appsub-ns-1",
					APIVersion:     "apps/v1",
					Kind:           "Deployment",
					LastUpdateTime: metav1.Now(),
				},
			},
		}

		Expect(k8sClient.Update(context.TODO(), appSubStatus)).NotTo(HaveOccurred())

		startCleanup(sync)

		Eventually(func() bool {
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: "appsub-ns-1", Name: "configmap1"}, workload1); err != nil {
				return true
			}
			return false
		}).Should(BeTrue())
		Eventually(func() bool {
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: "appsub-ns-1", Name: "deployment1"}, workload2); err != nil {
				return true
			}
			return false
		}).Should(BeTrue())
		Eventually(func() bool {
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: "appsub-ns-1", Name: "appsubstatus-1"}, appSubStatus); err != nil {
				return true
			}
			return false
		}).Should(BeTrue())
	})
})
