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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	gerr "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	crdv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

var (
	host = types.NamespacedName{
		Name:      "cluster",
		Namespace: "namspace",
	}

	sourceprefix = "synctest-"
)

var (
	configmapgvk = schema.GroupVersionKind{
		Version: "v1",
		Kind:    "ConfigMap",
	}

	sharedkey = types.NamespacedName{
		Name:      "workload",
		Namespace: "default",
	}
	workloadconfigmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
	}
	dplinstance = dplv1alpha1.Deployable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
			Annotations: map[string]string{
				dplv1alpha1.AnnotationLocal: "true",
			},
		},
		Spec: dplv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: &workloadconfigmap,
			},
		},
	}
)

var _ = Describe("test GVK validation", func() {
	It("should return the correct GVK", func() {
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		gvk := schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1beta1",
			Kind:    "StatefulSet",
		}
		validgvk := schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1beta1",
			Kind:    "StatefulSet",
		}
		Expect(sync.GetValidatedGVK(gvk)).To(Equal(&validgvk))

		gvk = schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1beta1",
			Kind:    "StatefulSet",
		}
		Expect(sync.GetValidatedGVK(gvk)).To(Equal(&validgvk))

		gvk = schema.GroupVersionKind{
			Group:   "extensions",
			Version: "v1beta1",
			Kind:    "Deployment",
		}
		validgvk = schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		}
		Expect(sync.GetValidatedGVK(gvk)).To(Equal(&validgvk))

		gvk = schema.GroupVersionKind{
			Group:   "apps.open-cluster-management.io",
			Kind:    "Deployable",
			Version: "v1",
		}
		Expect(sync.GetValidatedGVK(gvk)).To(BeNil())
	})
})

var _ = Describe("test register and deregister", func() {
	It("should register and deregister resource to kubeResource map", func() {
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		dpl := dplinstance.DeepCopy()
		hostnn := sharedkey
		dplnn := types.NamespacedName{
			Name:      dpl.Name,
			Namespace: dpl.Namespace,
		}
		source := sourceprefix + hostnn.String()

		Expect(sync.RegisterTemplate(hostnn, dpl, source)).NotTo(HaveOccurred())

		resmap, ok := sync.KubeResources[configmapgvk]
		Expect(ok).Should(BeTrue())

		reskey := sync.generateResourceMapKey(hostnn, dplnn)
		tplunit, ok := resmap.TemplateMap[reskey]
		Expect(ok).Should(BeTrue())

		target := workloadconfigmap.DeepCopy()

		converted := unstructured.Unstructured{}
		converted.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(target)
		Expect(err).NotTo(HaveOccurred())

		anno := map[string]string{
			appv1alpha1.AnnotationHosting:    sharedkey.Namespace + "/" + sharedkey.Name,
			dplv1alpha1.AnnotationHosting:    sharedkey.Namespace + "/" + sharedkey.Name,
			appv1alpha1.AnnotationSyncSource: source,
		}
		lbls := make(map[string]string)

		converted.SetAnnotations(anno)
		converted.SetLabels(lbls)

		Expect(tplunit.Unstructured.Object).Should(BeEquivalentTo(converted.Object))

		Expect(sync.DeRegisterTemplate(hostnn, dplnn, source)).NotTo(HaveOccurred())

		_, ok = resmap.TemplateMap[reskey]
		Expect(ok).Should(BeFalse())
	})
})

var _ = Describe("test apply", func() {
	var (
		sharedkey = types.NamespacedName{
			Name:      "workload",
			Namespace: "default",
		}
		subinstance = appv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sharedkey.Name,
				Namespace: sharedkey.Namespace,
			},
			Spec: appv1alpha1.SubscriptionSpec{
				Channel: sharedkey.String(),
			},
		}
	)

	It("should apply the resource from kubeResource map to cluster", func() {
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		dpl := dplinstance.DeepCopy()
		hostnn := sharedkey
		dplnn := sharedkey
		source := sourceprefix + hostnn.String()

		Expect(sync.RegisterTemplate(hostnn, dpl, source)).NotTo(HaveOccurred())

		resmap, ok := sync.KubeResources[configmapgvk]
		Expect(ok).Should(BeTrue())

		reskey := sync.generateResourceMapKey(hostnn, dplnn)
		tplunit, ok := resmap.TemplateMap[reskey]
		Expect(ok).Should(BeTrue())

		sub := subinstance.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), sub)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), sub)

		nri := sync.DynamicClient.Resource(resmap.GroupVersionResource)
		Expect(sync.applyTemplate(nri, resmap.Namespaced, reskey, tplunit, false)).NotTo(HaveOccurred())

		cfgmap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(context.TODO(), sharedkey, cfgmap)).NotTo(HaveOccurred())
		newtplobj := cfgmap.DeepCopy()

		Expect(sync.DeRegisterTemplate(hostnn, dplnn, source)).NotTo(HaveOccurred())
		time.Sleep(1 * time.Second)

		err = k8sClient.Get(context.TODO(), sharedkey, cfgmap)

		Expect(errors.IsNotFound(err)).Should(BeTrue())

		// test create new with disallowed information
		nu := &unstructured.Unstructured{}
		nu.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(newtplobj)
		nu.DeepCopyInto(tplunit.Unstructured)

		Expect(err).NotTo(HaveOccurred())
		Expect(sync.applyTemplate(nri, resmap.Namespaced, reskey, tplunit, false)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), newtplobj)

		Expect(k8sClient.Get(context.TODO(), sharedkey, cfgmap)).ShouldNot(HaveOccurred())
	})
})

var _ = Describe("test CRD discovery", func() {
	var (
		crdSharedkey = types.NamespacedName{
			Name:      "test-sub",
			Namespace: "default",
		}

		foocrdgvk = schema.GroupVersionKind{
			Group:   "samplecontroller.k8s.io",
			Version: "v1alpha1",
			Kind:    "Foo",
		}

		crdgvk = schema.GroupVersionKind{
			Group:   "apiextensions.k8s.io",
			Version: "v1beta1",
			Kind:    "CustomResourceDefinition",
		}

		tCrdkey = types.NamespacedName{
			Name: "foos.samplecontroller.k8s.io",
		}

		crd = crdv1beta1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       crdgvk.Kind,
				APIVersion: crdgvk.GroupVersion().String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: tCrdkey.Name,
			},
			Spec: crdv1beta1.CustomResourceDefinitionSpec{
				Group:   foocrdgvk.Group,
				Version: foocrdgvk.Version,
				Names: crdv1beta1.CustomResourceDefinitionNames{
					Plural: "foos",
					Kind:   foocrdgvk.Kind,
				},
				Scope: crdv1beta1.NamespaceScoped,
			},
		}

		dplinstance = dplv1alpha1.Deployable{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crdSharedkey.Name,
				Namespace: crdSharedkey.Namespace,
				Annotations: map[string]string{
					dplv1alpha1.AnnotationLocal: "true",
				},
			},
			Spec: dplv1alpha1.DeployableSpec{
				Template: &runtime.RawExtension{},
			},
		}

		subinstance = appv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      crdSharedkey.Name,
				Namespace: crdSharedkey.Namespace,
			},
			Spec: appv1alpha1.SubscriptionSpec{
				Channel: crdSharedkey.String(),
			},
		}

		interval     = 2
		waitInterval = interval * 3
	)

	It("should detect user applied CRD and rebuild the cache", func() {
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, interval, nil)
		Expect(err).NotTo(HaveOccurred())

		sch := make(chan struct{})
		defer close(sch)
		go sync.Start(sch)

		Expect(sync.KubeResources[foocrdgvk]).Should(BeNil())

		crdinstance := crd.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), crdinstance)).NotTo(HaveOccurred())
		Expect(k8sClient.Get(context.TODO(), tCrdkey, crdinstance)).NotTo(HaveOccurred())

		f := func(s *KubeSynchronizer) error {
			time.Sleep(time.Duration(waitInterval) * time.Second)

			s.kmtx.Lock()
			if s.KubeResources[foocrdgvk] == nil {
				return gerr.New("expecting a nil")
			}
			s.kmtx.Unlock()

			if err := k8sClient.Delete(context.TODO(), crdinstance); err != nil {
				return err
			}

			time.Sleep(time.Duration(waitInterval) * time.Second)

			if err := k8sClient.Get(context.TODO(), tCrdkey, crdinstance); err != nil {
				if !errors.IsNotFound(err) {
					return err
				}
			}

			time.Sleep(time.Duration(waitInterval) * time.Second)
			s.kmtx.Lock()
			if s.KubeResources[foocrdgvk] != nil {
				return gerr.New("expecting a nil")
			}
			s.kmtx.Unlock()

			return nil
		}

		Expect(f(sync)).Should(BeNil())
	})

	It("should be able to deploy CRD via subscription", func() {
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.

		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, interval, nil)
		Expect(err).NotTo(HaveOccurred())

		sch := make(chan struct{})
		defer close(sch)

		go sync.Start(sch)

		sub := subinstance.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), sub)).Should(Succeed())
		defer Expect(k8sClient.Delete(context.TODO(), sub)).Should(Succeed())

		time.Sleep(time.Duration(waitInterval) * time.Second)

		hostnn := crdSharedkey
		dpl := dplinstance.DeepCopy()
		dpl.Spec.Template = &runtime.RawExtension{
			Object: crd.DeepCopy(),
		}

		source := sourceprefix + hostnn.String()
		anno := map[string]string{
			dplv1alpha1.AnnotationHosting:    crdSharedkey.Namespace + "/" + crdSharedkey.Name,
			appv1alpha1.AnnotationHosting:    crdSharedkey.Namespace + "/" + crdSharedkey.Name,
			appv1alpha1.AnnotationSyncSource: source,
			dplv1alpha1.AnnotationLocal:      "true",
		}

		dpl.SetAnnotations(anno)

		dplU := DplUnit{
			Dpl: dpl,
			Gvk: crdgvk,
		}

		//just checking we don't have foo CRD on the test cluster
		sync.kmtx.Lock()
		_, ok := sync.KubeResources[foocrdgvk]
		sync.kmtx.Unlock()
		Expect(ok).Should(BeFalse())

		//apply CRD foo via subscription
		Expect(sync.AddTemplates(source, hostnn, []DplUnit{dplU})).Should(Succeed())

		time.Sleep(time.Duration(waitInterval) * time.Second)

		sync.kmtx.Lock()
		_, ok = sync.KubeResources[foocrdgvk]
		sync.kmtx.Unlock()
		Expect(ok).Should(BeTrue())

		crdgvk.Version = "v1"
		By("current foo CRD templates", func() {
			sync.kmtx.Lock()
			defer sync.kmtx.Unlock()
			printOut(sync.KubeResources, crdgvk, foocrdgvk)
		})

		result := &crdv1beta1.CustomResourceDefinition{}
		Expect(k8sClient.Get(context.TODO(), tCrdkey, result)).Should(Succeed())
		defer Expect(k8sClient.Delete(context.TODO(), result)).Should(Succeed())

		sync.kmtx.Lock()
		_, ok = sync.KubeResources[foocrdgvk]
		sync.kmtx.Unlock()
		Expect(ok).Should(BeTrue())

		By("current CRD templates", func() {
			sync.kmtx.Lock()
			defer sync.kmtx.Unlock()
			printOut(sync.KubeResources, crdgvk, foocrdgvk)
		})

		Expect(sync.CleanupByHost(hostnn, source)).Should(Succeed())

		time.Sleep(time.Duration(waitInterval) * time.Second)
		err = k8sClient.Get(context.TODO(), tCrdkey, result)
		Eventually(errors.IsNotFound(err), waitInterval).Should(BeTrue())
	})
})

var _ = Describe("harvest existing", func() {
	It("should add annotated resource to kubeResource map", func() {
		source := sourceprefix + sharedkey.String()
		cfgmap := workloadconfigmap.DeepCopy()

		var anno = map[string]string{
			dplv1alpha1.AnnotationHosting:    sharedkey.Namespace + "/" + sharedkey.Name,
			appv1alpha1.AnnotationHosting:    sharedkey.Namespace + "/" + sharedkey.Name,
			appv1alpha1.AnnotationSyncSource: source,
		}

		cfgmap.SetAnnotations(anno)

		Expect(k8sClient.Create(context.TODO(), cfgmap)).NotTo(HaveOccurred())

		time.Sleep(1 * time.Second)

		Expect(k8sClient.Get(context.TODO(), sharedkey, cfgmap)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), cfgmap)

		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.
		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		resgvk := schema.GroupVersionKind{
			Version: "v1",
			Kind:    "ConfigMap",
		}

		hostnn := sync.Extension.GetHostFromObject(cfgmap)
		dplnn := utils.GetHostDeployableFromObject(cfgmap)
		reskey := sync.generateResourceMapKey(*hostnn, *dplnn)

		// object should be havested back before source is found
		sync.kmtx.Lock()
		resmap := sync.KubeResources[resgvk]
		Expect(sync.checkServerObjects(resgvk, resmap)).NotTo(HaveOccurred())

		tplunit, ok := resmap.TemplateMap[reskey]
		sync.kmtx.Unlock()
		Expect(ok).Should(BeTrue())
		Expect(tplunit.Source).Should(Equal(source))

		time.Sleep(1 * time.Second)
		Expect(k8sClient.Get(context.TODO(), sharedkey, cfgmap)).NotTo(HaveOccurred())

	})
})

var _ = Describe("test service resource", func() {
	var (
		svcSharedkey = types.NamespacedName{
			Name:      "test-sub",
			Namespace: "default",
		}

		serviceport1 = corev1.ServicePort{
			Protocol: corev1.ProtocolTCP,
			Port:     8888,
			TargetPort: intstr.IntOrString{
				IntVal: 18888,
			},
		}

		serviceport2 = corev1.ServicePort{
			Protocol: corev1.ProtocolTCP,
			Port:     6666,
			TargetPort: intstr.IntOrString{
				IntVal: 16666,
			},
		}

		service = &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcSharedkey.Name,
				Namespace: svcSharedkey.Namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"tkey": "tval",
				},
				Ports: []corev1.ServicePort{serviceport1},
			},
		}

		subinstance = appv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcSharedkey.Name,
				Namespace: svcSharedkey.Namespace,
			},
			Spec: appv1alpha1.SubscriptionSpec{
				Channel: sharedkey.String(),
			},
		}

		dplinstance = dplv1alpha1.Deployable{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcSharedkey.Name,
				Namespace: svcSharedkey.Namespace,
				Annotations: map[string]string{
					dplv1alpha1.AnnotationLocal: "true",
				},
			},
			Spec: dplv1alpha1.DeployableSpec{
				Template: &runtime.RawExtension{
					Object: &workloadconfigmap,
				},
			},
		}
	)

	It("annotated service resource can be updated by subscription", func() {
		svc := service.DeepCopy()
		source := sourceprefix + svcSharedkey.String()

		var anno = map[string]string{
			"apps.open-cluster-management.io/hosting-deployable":   svcSharedkey.Namespace + "/" + svcSharedkey.Name,
			"apps.open-cluster-management.io/hosting-subscription": svcSharedkey.Namespace + "/" + svcSharedkey.Name,
			appv1alpha1.AnnotationSyncSource:                       source,
		}

		svc.SetAnnotations(anno)

		Expect(k8sClient.Create(context.TODO(), svc)).NotTo(HaveOccurred())

		sub := subinstance.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), sub)).NotTo(HaveOccurred())

		time.Sleep(k8swait)
		defer k8sClient.Delete(context.TODO(), sub)

		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.TODO(), svcSharedkey, svc)).NotTo(HaveOccurred())

		resgvk := schema.GroupVersionKind{
			Version: "v1",
			Kind:    "Service",
		}

		// object should be havested back before source is found
		resmap := sync.KubeResources[resgvk]
		hostnn := svcSharedkey
		dplnn := svcSharedkey

		reskey := sync.generateResourceMapKey(hostnn, dplnn)

		// havest existing from cluster
		Expect(sync.checkServerObjects(resgvk, resmap)).NotTo(HaveOccurred())

		tplunit, ok := resmap.TemplateMap[reskey]
		Expect(ok).Should(BeTrue())
		Expect(tplunit.Source).Should(Equal(source))

		// load template before start, pretend to be added by subscribers
		dpl := dplinstance.DeepCopy()
		svc = service.DeepCopy()
		svc.SetAnnotations(anno)
		svc.Spec.Ports = []corev1.ServicePort{serviceport2}
		dpl.Spec.Template = &runtime.RawExtension{
			Object: svc,
		}

		Expect(sync.RegisterTemplate(svcSharedkey, dpl, source)).NotTo(HaveOccurred())

		tplunit, ok = resmap.TemplateMap[reskey]
		Expect(ok).Should(BeTrue())
		Expect(tplunit.Source).Should(Equal(source))

		converted := unstructured.Unstructured{}
		converted.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(svc.DeepCopy())
		Expect(err).NotTo(HaveOccurred())

		lbls := make(map[string]string)

		converted.SetAnnotations(anno)
		converted.SetLabels(lbls)

		Expect(tplunit.Unstructured.Object).Should(BeEquivalentTo(converted.Object))

		nri := sync.DynamicClient.Resource(resmap.GroupVersionResource)
		Expect(sync.applyTemplate(nri, resmap.Namespaced, reskey, tplunit, true)).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.TODO(), svcSharedkey, svc)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), svc)

		Expect(svc.Spec.Ports[0]).Should(Equal(serviceport2))
	})
})

var _ = Describe("test resource overwrite", func() {
	var (
		configMapSharedkey = types.NamespacedName{
			Name:      "test-config-map",
			Namespace: "default",
		}

		configMap = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapSharedkey.Name,
				Namespace: configMapSharedkey.Namespace,
			},
			Data: map[string]string{
				"name": "bob",
				"age":  "19",
			},
		}

		templateConfigMap = corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapSharedkey.Name,
				Namespace: configMapSharedkey.Namespace,
			},
			Data: map[string]string{
				"name": "joe",
			},
		}

		subinstance = appv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapSharedkey.Name,
				Namespace: configMapSharedkey.Namespace,
			},
			Spec: appv1alpha1.SubscriptionSpec{
				Channel: sharedkey.String(),
			},
		}

		dplinstance = dplv1alpha1.Deployable{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapSharedkey.Name,
				Namespace: configMapSharedkey.Namespace,
				Annotations: map[string]string{
					dplv1alpha1.AnnotationLocal: "true",
				},
			},
			Spec: dplv1alpha1.DeployableSpec{
				Template: &runtime.RawExtension{
					Object: &templateConfigMap,
				},
			},
		}
	)

	It("resource owned by others can be replaced by subscription", func() {
		// Create a config map that is not owned by any subscription
		cm := configMap.DeepCopy()
		source := sourceprefix + configMapSharedkey.String()

		Expect(k8sClient.Create(context.TODO(), cm)).NotTo(HaveOccurred())

		// Create a subscription with overwrite annotations
		sub := subinstance.DeepCopy()
		subAnnotations := make(map[string]string)
		subAnnotations[appv1alpha1.AnnotationClusterAdmin] = "true"
		subAnnotations[appv1alpha1.AnnotationResourceReconcileOption] = "replace"
		sub.SetAnnotations(subAnnotations)
		Expect(k8sClient.Create(context.TODO(), sub)).NotTo(HaveOccurred())

		time.Sleep(k8swait)
		defer k8sClient.Delete(context.TODO(), sub)

		Expect(k8sClient.Get(context.TODO(), configMapSharedkey, cm)).NotTo(HaveOccurred())
		Expect(cm.Data["name"]).To(Equal("bob"))

		cmAnnotations := cm.GetAnnotations()
		Expect(cmAnnotations["apps.open-cluster-management.io/hosting-deployable"]).To(Equal(""))
		Expect(cmAnnotations["apps.open-cluster-management.io/hosting-subscription"]).To(Equal(""))

		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		resgvk := schema.GroupVersionKind{
			Version: "v1",
			Kind:    "ConfigMap",
		}

		// object should be havested back before source is found
		resmap := sync.KubeResources[resgvk]
		hostnn := configMapSharedkey
		dplnn := configMapSharedkey

		reskey := sync.generateResourceMapKey(hostnn, dplnn)

		// havest existing from cluster
		Expect(sync.checkServerObjects(resgvk, resmap)).NotTo(HaveOccurred())

		// load template before start, pretend to be added by subscribers
		dpl := dplinstance.DeepCopy()
		tplcm := templateConfigMap.DeepCopy()
		var anno = map[string]string{
			"apps.open-cluster-management.io/hosting-deployable":   configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			"apps.open-cluster-management.io/hosting-subscription": configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			"apps.open-cluster-management.io/cluster-admin":        "true",
			"apps.open-cluster-management.io/reconcile-option":     "replace",
			appv1alpha1.AnnotationSyncSource:                       source,
		}
		tplcm.SetAnnotations(anno)
		dpl.Spec.Template = &runtime.RawExtension{
			Object: tplcm,
		}

		Expect(sync.RegisterTemplate(configMapSharedkey, dpl, source)).NotTo(HaveOccurred())

		tplunit, ok := resmap.TemplateMap[reskey]
		Expect(ok).Should(BeTrue())
		Expect(tplunit.Source).Should(Equal(source))

		nri := sync.DynamicClient.Resource(resmap.GroupVersionResource)
		Expect(sync.applyTemplate(nri, resmap.Namespaced, reskey, tplunit, false)).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.TODO(), configMapSharedkey, cm)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), cm)

		Expect(cm.Data["name"]).Should(Equal("joe"))
		// age field should be deleted because the reconcile option was replace
		Expect(cm.Data["age"]).Should(Equal(""))

		// If reconcile option is replace, the hosting annotations should added to existing resource, that is not owned by the subscription.
		cmAnnotations = cm.GetAnnotations()
		Expect(cmAnnotations["apps.open-cluster-management.io/hosting-deployable"]).To(Equal(configMapSharedkey.Namespace + "/" + configMapSharedkey.Name))
		Expect(cmAnnotations["apps.open-cluster-management.io/hosting-subscription"]).To(Equal(configMapSharedkey.Namespace + "/" + configMapSharedkey.Name))
	})

	It("resource owned by others can be merged by subscription", func() {
		// Create a config map that is not owned by any subscription
		cm := configMap.DeepCopy()
		source := sourceprefix + configMapSharedkey.String()

		Expect(k8sClient.Create(context.TODO(), cm)).NotTo(HaveOccurred())

		// Create a subscription with overwrite annotations
		sub := subinstance.DeepCopy()
		subAnnotations := make(map[string]string)
		subAnnotations[appv1alpha1.AnnotationClusterAdmin] = "true"
		subAnnotations[appv1alpha1.AnnotationResourceReconcileOption] = "merge"
		sub.SetAnnotations(subAnnotations)
		Expect(k8sClient.Create(context.TODO(), sub)).NotTo(HaveOccurred())

		time.Sleep(k8swait)
		defer k8sClient.Delete(context.TODO(), sub)

		Expect(k8sClient.Get(context.TODO(), configMapSharedkey, cm)).NotTo(HaveOccurred())
		Expect(cm.Data["name"]).To(Equal("bob"))

		cmAnnotations := cm.GetAnnotations()
		Expect(cmAnnotations["apps.open-cluster-management.io/hosting-deployable"]).To(Equal(""))
		Expect(cmAnnotations["apps.open-cluster-management.io/hosting-subscription"]).To(Equal(""))

		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		resgvk := schema.GroupVersionKind{
			Version: "v1",
			Kind:    "ConfigMap",
		}

		// object should be havested back before source is found
		resmap := sync.KubeResources[resgvk]
		hostnn := configMapSharedkey
		dplnn := configMapSharedkey

		reskey := sync.generateResourceMapKey(hostnn, dplnn)

		// havest existing from cluster
		Expect(sync.checkServerObjects(resgvk, resmap)).NotTo(HaveOccurred())

		// load template before start, pretend to be added by subscribers
		dpl := dplinstance.DeepCopy()
		tplcm := templateConfigMap.DeepCopy()
		var anno = map[string]string{
			"apps.open-cluster-management.io/hosting-deployable":   configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			"apps.open-cluster-management.io/hosting-subscription": configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			"apps.open-cluster-management.io/cluster-admin":        "true",
			"apps.open-cluster-management.io/reconcile-option":     "merge",
			appv1alpha1.AnnotationSyncSource:                       source,
		}
		tplcm.SetAnnotations(anno)
		dpl.Spec.Template = &runtime.RawExtension{
			Object: tplcm,
		}

		Expect(sync.RegisterTemplate(configMapSharedkey, dpl, source)).NotTo(HaveOccurred())

		tplunit, ok := resmap.TemplateMap[reskey]
		Expect(ok).Should(BeTrue())
		Expect(tplunit.Source).Should(Equal(source))

		nri := sync.DynamicClient.Resource(resmap.GroupVersionResource)
		Expect(sync.applyTemplate(nri, resmap.Namespaced, reskey, tplunit, false)).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.TODO(), configMapSharedkey, cm)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), cm)

		Expect(cm.Data["name"]).Should(Equal("joe"))
		// age field should be kept because the reconcile option was merge
		Expect(cm.Data["age"]).Should(Equal("19"))

		// With merge option, the hosting annotations should not be added when existing resource, that is not owned by the subscription,
		// gets overwritten
		cmAnnotations = cm.GetAnnotations()
		Expect(cmAnnotations["apps.open-cluster-management.io/hosting-deployable"]).To(Equal(""))
		Expect(cmAnnotations["apps.open-cluster-management.io/hosting-subscription"]).To(Equal(""))
	})

	It("resource owned by others can not be updated by subscription without the annotations", func() {
		// Create a config map that is not owned by any subscription
		cm := configMap.DeepCopy()
		source := sourceprefix + configMapSharedkey.String()

		Expect(k8sClient.Create(context.TODO(), cm)).NotTo(HaveOccurred())

		// Create a subscription with overwrite annotations
		sub := subinstance.DeepCopy()
		Expect(k8sClient.Create(context.TODO(), sub)).NotTo(HaveOccurred())

		time.Sleep(k8swait)
		defer k8sClient.Delete(context.TODO(), sub)

		Expect(k8sClient.Get(context.TODO(), configMapSharedkey, cm)).NotTo(HaveOccurred())
		Expect(cm.Data["name"]).To(Equal("bob"))

		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		resgvk := schema.GroupVersionKind{
			Version: "v1",
			Kind:    "ConfigMap",
		}

		// object should be havested back before source is found
		resmap := sync.KubeResources[resgvk]
		hostnn := configMapSharedkey
		dplnn := configMapSharedkey

		reskey := sync.generateResourceMapKey(hostnn, dplnn)

		// havest existing from cluster
		Expect(sync.checkServerObjects(resgvk, resmap)).NotTo(HaveOccurred())

		// load template before start, pretend to be added by subscribers
		dpl := dplinstance.DeepCopy()
		tplcm := templateConfigMap.DeepCopy()
		var anno = map[string]string{
			"apps.open-cluster-management.io/hosting-deployable":   configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			"apps.open-cluster-management.io/hosting-subscription": configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			appv1alpha1.AnnotationSyncSource:                       source,
		}
		tplcm.SetAnnotations(anno)
		dpl.Spec.Template = &runtime.RawExtension{
			Object: tplcm,
		}

		Expect(sync.RegisterTemplate(configMapSharedkey, dpl, source)).NotTo(HaveOccurred())

		tplunit, ok := resmap.TemplateMap[reskey]
		Expect(ok).Should(BeTrue())
		Expect(tplunit.Source).Should(Equal(source))

		nri := sync.DynamicClient.Resource(resmap.GroupVersionResource)
		Expect(sync.applyTemplate(nri, resmap.Namespaced, reskey, tplunit, false)).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.TODO(), configMapSharedkey, cm)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), cm)

		// The name in config map should not have been updated. If it did, the value should be joe
		Expect(cm.Data["name"]).Should(Equal("bob"))
	})

	It("resource owned by subscription can be merged", func() {
		// Create a config map that is not owned by any subscription
		cm := configMap.DeepCopy()
		source := sourceprefix + configMapSharedkey.String()

		var anno = map[string]string{
			"apps.open-cluster-management.io/hosting-deployable":   configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			"apps.open-cluster-management.io/hosting-subscription": configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			appv1alpha1.AnnotationSyncSource:                       source,
		}

		cm.SetAnnotations(anno)

		Expect(k8sClient.Create(context.TODO(), cm)).NotTo(HaveOccurred())

		// Create a subscription with overwrite annotations
		sub := subinstance.DeepCopy()
		subAnnotations := make(map[string]string)
		subAnnotations[appv1alpha1.AnnotationClusterAdmin] = "true"
		subAnnotations[appv1alpha1.AnnotationResourceReconcileOption] = "merge"
		sub.SetAnnotations(subAnnotations)
		Expect(k8sClient.Create(context.TODO(), sub)).NotTo(HaveOccurred())

		time.Sleep(k8swait)
		defer k8sClient.Delete(context.TODO(), sub)

		Expect(k8sClient.Get(context.TODO(), configMapSharedkey, cm)).NotTo(HaveOccurred())
		Expect(cm.Data["name"]).To(Equal("bob"))

		sync, err := CreateSynchronizer(k8sManager.GetConfig(), k8sManager.GetConfig(), k8sManager.GetScheme(), &host, 2, nil)
		Expect(err).NotTo(HaveOccurred())

		resgvk := schema.GroupVersionKind{
			Version: "v1",
			Kind:    "ConfigMap",
		}

		// object should be havested back before source is found
		resmap := sync.KubeResources[resgvk]
		hostnn := configMapSharedkey
		dplnn := configMapSharedkey

		reskey := sync.generateResourceMapKey(hostnn, dplnn)

		// havest existing from cluster
		Expect(sync.checkServerObjects(resgvk, resmap)).NotTo(HaveOccurred())

		// load template before start, pretend to be added by subscribers
		dpl := dplinstance.DeepCopy()
		tplcm := templateConfigMap.DeepCopy()
		anno = map[string]string{
			"apps.open-cluster-management.io/hosting-deployable":   configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			"apps.open-cluster-management.io/hosting-subscription": configMapSharedkey.Namespace + "/" + configMapSharedkey.Name,
			"apps.open-cluster-management.io/reconcile-option":     "merge",
			appv1alpha1.AnnotationSyncSource:                       source,
		}
		tplcm.SetAnnotations(anno)
		dpl.Spec.Template = &runtime.RawExtension{
			Object: tplcm,
		}

		Expect(sync.RegisterTemplate(configMapSharedkey, dpl, source)).NotTo(HaveOccurred())

		tplunit, ok := resmap.TemplateMap[reskey]
		Expect(ok).Should(BeTrue())
		Expect(tplunit.Source).Should(Equal(source))

		nri := sync.DynamicClient.Resource(resmap.GroupVersionResource)
		Expect(sync.applyTemplate(nri, resmap.Namespaced, reskey, tplunit, false)).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.TODO(), configMapSharedkey, cm)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), cm)

		Expect(cm.Data["name"]).Should(Equal("joe"))
		// age field should be kept because the reconcile option was merge
		Expect(cm.Data["age"]).Should(Equal("19"))
	})
})

func printOut(kubeResources map[schema.GroupVersionKind]*ResourceMap, filters ...schema.GroupVersionKind) {
	set := map[schema.GroupVersionKind]bool{}

	for _, f := range filters {
		if _, ok := set[f]; !ok {
			set[f] = true
		}
	}

	for gvk, mp := range kubeResources {
		if set[gvk] {
			fmt.Printf("gvk %v, with map %#v\n", gvk, mp)
		}
	}
}
