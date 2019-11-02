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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

var (
	host = types.NamespacedName{
		Name:      "cluster",
		Namespace: "namspace",
	}
	source = "synctest"
)

var (
	workloadconfigmapgvk = schema.GroupVersionKind{
		Version: "v1",
		Kind:    "ConfigMap",
	}

	sharedkey = types.NamespacedName{
		Name:      "configmap",
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

	subinstance = appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
	}
)

func TestSyncStart(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sync, err := CreateSynchronizer(cfg, cfg, &host, 10, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(mgr.Add(sync)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	time.Sleep(5 * time.Second)
}

func TestRegisterDeRegister(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(mgr.Add(sync)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	time.Sleep(1 * time.Second)

	sub := subinstance.DeepCopy()

	g.Expect(c.Create(context.TODO(), sub)).NotTo((gomega.HaveOccurred()))
	defer c.Delete(context.TODO(), sub)

	dpl := dplinstance.DeepCopy()
	g.Expect(sync.RegisterTemplate(sharedkey, dpl, source)).NotTo(gomega.HaveOccurred())

	tplmap := sync.KubeResources[workloadconfigmapgvk].TemplateMap
	if len(tplmap) != 1 {
		t.Error("Failed to register template to map:", tplmap)
	}

	time.Sleep(10 * time.Second)

	result := &corev1.ConfigMap{}

	g.Expect(c.Get(context.TODO(), sharedkey, result)).NotTo(gomega.HaveOccurred())

	if result.Name != workloadconfigmap.Name || result.Namespace != workloadconfigmap.Namespace {
		t.Error("Got wrong configmap workload: ", workloadconfigmap)
	}

	g.Expect(sync.DeRegisterTemplate(sharedkey, sharedkey, source)).NotTo(gomega.HaveOccurred())

	if len(tplmap) != 0 {
		t.Error("Failed to deregister template from map:", tplmap)
	}

	g.Expect(errors.IsNotFound(c.Get(context.TODO(), sharedkey, result))).To(gomega.BeTrue())
}

var (
	crdgvk = schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1beta1",
		Kind:    "CustomResourceDefinition",
	}

	crdkey = types.NamespacedName{
		Name: "foos.samplecontroller.k8s.io",
	}

	crd = crdv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       crdgvk.Kind,
			APIVersion: crdgvk.Group + "/" + crdgvk.Version,
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

	crddpl = dplv1alpha1.Deployable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
			Annotations: map[string]string{
				dplv1alpha1.AnnotationLocal: "true",
			},
		},
		Spec: dplv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: &crd,
			},
		},
	}

	crdsub = appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
	}
)

func TestCRDRegisterDeRegister(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(mgr.Add(sync)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	time.Sleep(1 * time.Second)

	sub := crdsub.DeepCopy()

	g.Expect(c.Create(context.TODO(), sub)).NotTo((gomega.HaveOccurred()))
	defer c.Delete(context.TODO(), sub)

	dpl := crddpl.DeepCopy()
	g.Expect(sync.RegisterTemplate(sharedkey, dpl, source)).NotTo(gomega.HaveOccurred())

	tplmap := sync.KubeResources[crdgvk].TemplateMap
	if len(tplmap) != 1 {
		t.Error("Failed to register template to map:", tplmap)
	}

	time.Sleep(10 * time.Second)

	result := &crdv1beta1.CustomResourceDefinition{}

	g.Expect(c.Get(context.TODO(), crdkey, result)).NotTo(gomega.HaveOccurred())

	if result.Name != crdkey.Name {
		t.Error("Got wrong crd: ", result)
	}

	g.Expect(sync.DeRegisterTemplate(sharedkey, sharedkey, source)).NotTo(gomega.HaveOccurred())

	if len(tplmap) != 0 {
		t.Error("Failed to deregister template from map:", tplmap)
	}

	g.Expect(errors.IsNotFound(c.Get(context.TODO(), sharedkey, result))).To(gomega.BeTrue())
}

func TestDeleteExpired(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	cfgmap := workloadconfigmap.DeepCopy()

	var anno = map[string]string{
		"app.ibm.com/hosting-deployable":   sharedkey.Namespace + "/" + sharedkey.Name,
		"app.ibm.com/hosting-subscription": sharedkey.Namespace + "/" + sharedkey.Name,
	}

	cfgmap.SetAnnotations(anno)

	g.Expect(c.Create(context.TODO(), cfgmap)).NotTo(gomega.HaveOccurred())

	time.Sleep(1 * time.Second)

	g.Expect(c.Get(context.TODO(), sharedkey, cfgmap)).NotTo(gomega.HaveOccurred())

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(mgr.Add(sync)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	time.Sleep(15 * time.Second)

	g.Expect(errors.IsNotFound(c.Get(context.TODO(), sharedkey, cfgmap))).To(gomega.BeTrue())
}

func TestRestart(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	cfgmap := workloadconfigmap.DeepCopy()

	var anno = map[string]string{
		"app.ibm.com/hosting-deployable":   sharedkey.Namespace + "/" + sharedkey.Name,
		"app.ibm.com/hosting-subscription": sharedkey.Namespace + "/" + sharedkey.Name,
	}

	cfgmap.SetAnnotations(anno)

	g.Expect(c.Create(context.TODO(), cfgmap)).NotTo(gomega.HaveOccurred())

	time.Sleep(1 * time.Second)

	g.Expect(c.Get(context.TODO(), sharedkey, cfgmap)).NotTo(gomega.HaveOccurred())

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sync, err := CreateSynchronizer(cfg, cfg, &host, 2, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(mgr.Add(sync)).NotTo(gomega.HaveOccurred())

	// load template before start, pretend to be added by subscribers
	dpl := dplinstance.DeepCopy()
	g.Expect(sync.RegisterTemplate(sharedkey, dpl, source)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	time.Sleep(10 * time.Second)

	g.Expect(c.Get(context.TODO(), sharedkey, cfgmap)).NotTo(gomega.HaveOccurred())
}
