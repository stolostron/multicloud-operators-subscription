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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

var (
	sharedkey = types.NamespacedName{
		Name:      "githubtest",
		Namespace: "default",
	}
	githubsub2 = &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
			Annotations: map[string]string{
				appv1alpha1.AnnotationGitBranch: "main",
			},
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: sharedkey.String(),
		},
	}
	hostworkload4 = types.NamespacedName{
		Name:      "configmap4",
		Namespace: "default",
	}
	host = types.NamespacedName{
		Name:      "cluster1",
		Namespace: "cluster1",
	}

	hostSub = types.NamespacedName{
		Name:      "appsub-1",
		Namespace: "appsub-ns-1",
	}

	workload1Configmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configmap1",
			Namespace: "default",
			Annotations: map[string]string{
				appv1alpha1.AnnotationHosting: hostSub.Namespace + "/" + hostSub.Name,
			},
		},
	}

	selector = map[string]string{"a": "b"}

	workload2Deployment = apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment1",
			Namespace: "default",
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
			Namespace: "default",
			Annotations: map[string]string{
				appv1alpha1.AnnotationHosting: "not owned",
			},
		},
	}

	workload4Subscription = appv1alpha1.Subscription{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Subscription",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configmap4",
			Namespace: "default",
			Annotations: map[string]string{
				appv1alpha1.AnnotationHosting: hostworkload4.Namespace + "/" + hostworkload4.Name,
			},
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
			Namespace:  "default",
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
			Namespace:  "default",
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
			Namespace:  "default",
			APIVersion: "",
			Kind:       "ConfigMap",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).To(HaveOccurred())
	})

	It("should not be owned by the subscription", func() {
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil, false, false)
		Expect(err).NotTo(HaveOccurred())

		workload1 := workload3Configmap.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		pkgStatus := appSubStatusV1alpha1.SubscriptionUnitStatus{
			Name:       "configmap2",
			Namespace:  "default",
			APIVersion: "v1",
			Kind:       "ConfigMap",
		}

		err = sync.DeleteSingleSubscribedResource(hostSub, pkgStatus)
		Expect(err).NotTo(HaveOccurred())
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
	It("should purge", func() {
		workload1 := workload1Configmap.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		subscriptionYAML := `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: github-resource-subscription-local
  namespace: default
spec:
  channel: github-ns/github-ch
  placement:
  local: true
  packageOverrides:
    - packageName: kustomize/overlays/production/kustomization.yaml`
		subscription := &appv1alpha1.Subscription{}
		err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
		Expect(err).NotTo(HaveOccurred())

		err = sync.PurgeAllSubscribedResources(subscription)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should purge everything", func() {
		workload1 := workload1Configmap.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		subAnnotations := make(map[string]string)
		subAnnotations[appv1alpha1.AnnotationClusterAdmin] = "true"
		subAnnotations[appv1alpha1.AnnotationResourceReconcileOption] = "merge"
		subAnnotations[appv1alpha1.AnnotationGitBranch] = "main"
		subscription := &appv1alpha1.Subscription{}
		githubsub2.SetAnnotations(subAnnotations)

		err = sync.PurgeAllSubscribedResources(subscription)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("test ProcessSubResources", func() {
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

	It("should create new resource", func() {
		subscriptionYAML := `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: github-resource-subscription-local
  namespace: default
spec:
  channel: github-ns/github-ch
  placement:
  local: true
  packageOverrides:
    - packageName: kustomize/overlays/production/kustomization.yaml`
		subscription := &appv1alpha1.Subscription{}
		err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
		Expect(err).NotTo(HaveOccurred())

		resource := &unstructured.Unstructured{}
		resource.SetNamespace("default")
		resource.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "apps.open-cluster-management.io/v1",
			Kind:    "Subscription",
		})
		resource.SetAnnotations(make(map[string]string))
		resource.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Subscription",
			Name:       subscription.Name,
			UID:        subscription.UID,
		}})

		resourceList := []ResourceUnit{{Resource: resource, Gvk: resource.GetObjectKind().GroupVersionKind()}}
		allowedGroupResources, deniedGroupResources := utils.GetAllowDenyLists(*subscription)

		err = sync.ProcessSubResources(subscription, resourceList, allowedGroupResources, deniedGroupResources, false)
		Expect(err).NotTo(HaveOccurred())
	})
	It("should have 0 resourcews", func() {
		subscriptionYAML := `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: github-resource-subscription-local
  namespace: default
spec:
  channel: github-ns/github-ch
  placement:
  local: true
  packageOverrides:
    - packageName: kustomize/overlays/production/kustomization.yaml`
		subscription := &appv1alpha1.Subscription{}
		err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
		Expect(err).NotTo(HaveOccurred())

		resourceList := []ResourceUnit{}
		allowedGroupResources, deniedGroupResources := utils.GetAllowDenyLists(*subscription)

		err = sync.ProcessSubResources(subscription, resourceList, allowedGroupResources, deniedGroupResources, false)
		Expect(err).NotTo(HaveOccurred())
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

	It("should fail finding hosting appsub", func() {

		subscription, err := sync.getHostingAppSub(hostSub)
		Expect(err).To(HaveOccurred())
		Expect(subscription).To(BeNil())
	})

	It("should pass finding hosting appsub", func() {

		workload1 := workload4Subscription.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), workload1)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), workload1)

		_, err := sync.getHostingAppSub(hostworkload4)
		Expect(err).NotTo(HaveOccurred())
	})
})
