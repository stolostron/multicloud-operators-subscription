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
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	crdv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

var (
	host = types.NamespacedName{
		Name:      "cluster",
		Namespace: "namspace",
	}
	source = "synctest"
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

func TestSyncStart(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(mgr.Add(sync)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()
}

func TestHouseKeeping(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sync.houseKeeping()
}

func TestRegisterDeRegister(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	dpl := dplinstance.DeepCopy()
	hostnn := sharedkey
	dplnn := types.NamespacedName{
		Name:      dpl.Name,
		Namespace: dpl.Namespace,
	}

	g.Expect(sync.RegisterTemplate(hostnn, dpl, source)).NotTo(gomega.HaveOccurred())

	resmap, ok := sync.KubeResources[configmapgvk]
	g.Expect(ok).Should(gomega.BeTrue())

	reskey := sync.generateResourceMapKey(hostnn, dplnn)
	tplunit, ok := resmap.TemplateMap[reskey]
	g.Expect(ok).Should(gomega.BeTrue())

	target := workloadconfigmap.DeepCopy()

	converted := unstructured.Unstructured{}
	converted.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(target)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	anno := map[string]string{
		dplv1alpha1.AnnotationHosting:    sharedkey.Namespace + "/" + sharedkey.Name,
		appv1alpha1.AnnotationHosting:    sharedkey.Namespace + "/" + sharedkey.Name,
		appv1alpha1.AnnotationSyncSource: source,
	}
	lbls := make(map[string]string)

	converted.SetAnnotations(anno)
	converted.SetLabels(lbls)

	g.Expect(tplunit.Unstructured.Object).Should(gomega.BeEquivalentTo(converted.Object))

	g.Expect(sync.DeRegisterTemplate(hostnn, dplnn, source)).NotTo(gomega.HaveOccurred())

	_, ok = resmap.TemplateMap[reskey]
	g.Expect(ok).Should(gomega.BeFalse())
}

var (
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

func TestApply(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	dpl := dplinstance.DeepCopy()
	hostnn := sharedkey
	dplnn := sharedkey

	g.Expect(sync.RegisterTemplate(hostnn, dpl, source)).NotTo(gomega.HaveOccurred())

	resmap, ok := sync.KubeResources[configmapgvk]
	g.Expect(ok).Should(gomega.BeTrue())

	reskey := sync.generateResourceMapKey(hostnn, dplnn)
	tplunit, ok := resmap.TemplateMap[reskey]
	g.Expect(ok).Should(gomega.BeTrue())

	sub := subinstance.DeepCopy()
	g.Expect(c.Create(context.TODO(), sub)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), sub)

	nri := sync.DynamicClient.Resource(resmap.GroupVersionResource)
	g.Expect(sync.applyTemplate(nri, resmap.Namespaced, reskey, tplunit, false)).NotTo(gomega.HaveOccurred())

	cfgmap := &corev1.ConfigMap{}
	g.Expect(c.Get(context.TODO(), sharedkey, cfgmap)).NotTo(gomega.HaveOccurred())

	g.Expect(sync.DeRegisterTemplate(hostnn, dplnn, source)).NotTo(gomega.HaveOccurred())
	time.Sleep(1 * time.Second)

	err = c.Get(context.TODO(), sharedkey, cfgmap)

	g.Expect(errors.IsNotFound(err)).Should(gomega.BeTrue())
}

var (
	crdgvk = schema.GroupVersionKind{
		Group:   "samplecontroller.k8s.io",
		Version: "v1alpha1",
		Kind:    "Foo",
	}

	crdkey = types.NamespacedName{
		Name: "foos.samplecontroller.k8s.io",
	}

	crd = crdv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: crdkey.Name,
		},
		Spec: crdv1beta1.CustomResourceDefinitionSpec{
			Group:   "samplecontroller.k8s.io",
			Version: "v1alpha1",
			Names: crdv1beta1.CustomResourceDefinitionNames{
				Plural: "foos",
				Kind:   "Foo",
			},
			Scope: crdv1beta1.NamespaceScoped,
		},
	}
)

func TestClusterScopedApply(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	stop := make(chan struct{})
	sync.dynamicFactory.Start(stop)

	defer close(stop)

	sub := subinstance.DeepCopy()

	g.Expect(c.Create(context.TODO(), sub)).NotTo((gomega.HaveOccurred()))
	defer c.Delete(context.TODO(), sub)

	hostnn := sharedkey
	dplnn := sharedkey
	dpl := dplinstance.DeepCopy()
	dpl.Spec.Template = &runtime.RawExtension{
		Object: crd.DeepCopy(),
	}
	g.Expect(sync.RegisterTemplate(hostnn, dpl, source)).NotTo(gomega.HaveOccurred())

	_, ok := sync.KubeResources[crdgvk]
	g.Expect(ok).Should(gomega.BeFalse())

	time.Sleep(1 * time.Second)

	sync.houseKeeping()

	result := &crdv1beta1.CustomResourceDefinition{}
	g.Expect(c.Get(context.TODO(), crdkey, result)).NotTo(gomega.HaveOccurred())

	_, ok = sync.KubeResources[crdgvk]
	g.Expect(ok).Should(gomega.BeTrue())

	g.Expect(sync.DeRegisterTemplate(hostnn, dplnn, source)).NotTo(gomega.HaveOccurred())

	time.Sleep(1 * time.Second)

	err = c.Get(context.TODO(), crdkey, result)
	g.Expect(errors.IsNotFound(err)).Should(gomega.BeTrue())
}

func TestHarvestExisting(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	cfgmap := workloadconfigmap.DeepCopy()

	var anno = map[string]string{
		dplv1alpha1.AnnotationHosting:    sharedkey.Namespace + "/" + sharedkey.Name,
		appv1alpha1.AnnotationHosting:    sharedkey.Namespace + "/" + sharedkey.Name,
		appv1alpha1.AnnotationSyncSource: source,
	}

	cfgmap.SetAnnotations(anno)

	g.Expect(c.Create(context.TODO(), cfgmap)).NotTo(gomega.HaveOccurred())

	time.Sleep(1 * time.Second)

	g.Expect(c.Get(context.TODO(), sharedkey, cfgmap)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), cfgmap)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	resgvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "ConfigMap",
	}

	hostnn := sync.Extension.GetHostFromObject(cfgmap)
	dplnn := utils.GetHostDeployableFromObject(cfgmap)
	reskey := sync.generateResourceMapKey(*hostnn, *dplnn)

	// object should be havested back before source is found
	resmap := sync.KubeResources[resgvk]
	g.Expect(sync.checkServerObjects(resmap)).NotTo(gomega.HaveOccurred())

	tplunit, ok := resmap.TemplateMap[reskey]
	g.Expect(ok).Should(gomega.BeTrue())
	g.Expect(tplunit.Source).Should(gomega.Equal(source))

	time.Sleep(1 * time.Second)
	g.Expect(c.Get(context.TODO(), sharedkey, cfgmap)).NotTo(gomega.HaveOccurred())
}

var (
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
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"tkey": "tval",
			},
			Ports: []corev1.ServicePort{serviceport1},
		},
	}
)

func TestServiceResource(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	svc := service.DeepCopy()

	var anno = map[string]string{
		"app.ibm.com/hosting-deployable":   sharedkey.Namespace + "/" + sharedkey.Name,
		"app.ibm.com/hosting-subscription": sharedkey.Namespace + "/" + sharedkey.Name,
		appv1alpha1.AnnotationSyncSource:   source,
	}

	svc.SetAnnotations(anno)

	g.Expect(c.Create(context.TODO(), svc)).NotTo(gomega.HaveOccurred())

	sub := subinstance.DeepCopy()
	g.Expect(c.Create(context.TODO(), sub)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), sub)

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(c.Get(context.TODO(), sharedkey, svc)).NotTo(gomega.HaveOccurred())

	resgvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Service",
	}

	// object should be havested back before source is found
	resmap := sync.KubeResources[resgvk]
	hostnn := sharedkey
	dplnn := sharedkey

	reskey := sync.generateResourceMapKey(hostnn, dplnn)

	// havest existing from cluster
	g.Expect(sync.checkServerObjects(resmap)).NotTo(gomega.HaveOccurred())

	tplunit, ok := resmap.TemplateMap[reskey]
	g.Expect(ok).Should(gomega.BeTrue())
	g.Expect(tplunit.Source).Should(gomega.Equal(source))

	// load template before start, pretend to be added by subscribers
	dpl := dplinstance.DeepCopy()
	svc = service.DeepCopy()
	svc.SetAnnotations(anno)
	svc.Spec.Ports = []corev1.ServicePort{serviceport2}
	dpl.Spec.Template = &runtime.RawExtension{
		Object: svc,
	}

	g.Expect(sync.RegisterTemplate(sharedkey, dpl, source)).NotTo(gomega.HaveOccurred())

	tplunit, ok = resmap.TemplateMap[reskey]
	g.Expect(ok).Should(gomega.BeTrue())
	g.Expect(tplunit.Source).Should(gomega.Equal(source))

	converted := unstructured.Unstructured{}
	converted.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(svc.DeepCopy())
	g.Expect(err).NotTo(gomega.HaveOccurred())

	lbls := make(map[string]string)

	converted.SetAnnotations(anno)
	converted.SetLabels(lbls)

	g.Expect(tplunit.Unstructured.Object).Should(gomega.BeEquivalentTo(converted.Object))

	nri := sync.DynamicClient.Resource(resmap.GroupVersionResource)
	g.Expect(sync.applyTemplate(nri, resmap.Namespaced, reskey, tplunit, true)).NotTo(gomega.HaveOccurred())

	g.Expect(c.Get(context.TODO(), sharedkey, svc)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), svc)

	g.Expect(svc.Spec.Ports[0]).Should(gomega.Equal(serviceport2))
}
