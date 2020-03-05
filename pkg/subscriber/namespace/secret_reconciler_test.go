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

package namespace

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	synckube "github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
)

func TestSecretReconcile(t *testing.T) {
	// set up the reconcile
	g := gomega.NewGomegaWithT(t)
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is find id

	defaultitem = &appv1alpha1.SubscriberItem{
		Subscription: subscription,
		Channel:      channel,
	}

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Getting a secret reconciler which belongs to the subscription defined in the var and the subscription is
	// pointing to a namespace type of channel
	srtRec := newSecretReconciler(defaultNsSubscriber, mgr, subkey)

	// Create secrets at the channel namespace

	g.Expect(c.Create(context.TODO(), subscription)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), subscription)

	g.Expect(c.Create(context.TODO(), dplSrt)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), dplSrt)

	g.Expect(c.Create(context.TODO(), noneDplSrt)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), noneDplSrt)

	dplSrtKey := types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}
	g.Expect(srtRec.getSecretsBySubLabel(dplSrtKey.Namespace)).ShouldNot(gomega.BeNil())
	//check up if the target secert is deployed at the subscriptions namespace
	dplSrtRq := reconcile.Request{NamespacedName: types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}}

	time.Sleep(4 * time.Second)

	//checked up if the secret is deployed at the subscription namespace

	// Do secret reconcile which should pick up the dplSrt and deploy it to the subscription namespace (check point)
	//checking if the reconcile has any error
	g.Expect(srtRec.Reconcile(dplSrtRq)).ShouldNot(gomega.BeNil())
}

type fakeSynchronizer struct {
	Store    map[schema.GroupVersionKind]map[string]bool
	name     string
	interval int
}

func (f *fakeSynchronizer) CreateValiadtor(s string) *synckube.Validator {
	f.name = s
	f.Store = map[schema.GroupVersionKind]map[string]bool{}

	return &synckube.Validator{
		KubeSynchronizer: &synckube.KubeSynchronizer{},
		Store:            map[schema.GroupVersionKind]map[string]bool{},
	}
}

func (f *fakeSynchronizer) ApplyValiadtor(v *synckube.Validator) {
}

func (f *fakeSynchronizer) GetValidatedGVK(gvk schema.GroupVersionKind) *schema.GroupVersionKind {
	return &gvk
}

func (f *fakeSynchronizer) RegisterTemplate(nKey types.NamespacedName, pDpl *dplv1alpha1.Deployable, s string) error {
	template := &unstructured.Unstructured{}
	err := json.Unmarshal(pDpl.Spec.Template.Raw, template)

	tplgvk := template.GetObjectKind().GroupVersionKind()
	f.Store[tplgvk] = map[string]bool{nKey.String(): true}

	return errors.Wrap(err, "failed to register template to fake synchronizer")
}

func (f *fakeSynchronizer) IsResourceNamespaced(gvk schema.GroupVersionKind) bool {
	return true
}

func (f *fakeSynchronizer) CleanupByHost(key types.NamespacedName, s string) {}

func (f *fakeSynchronizer) GetInterval() int {
	return f.interval
}

func (f *fakeSynchronizer) assertTemplateRegistry(nKey types.NamespacedName, gvk schema.GroupVersionKind) bool {
	if _, ok := f.Store[gvk][nKey.String()]; !ok {
		return false
	}

	return true
}

func TestSecretReconcileSpySync(t *testing.T) {
	// set up the reconcile
	g := gomega.NewGomegaWithT(t)
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is find id
	subkey := types.NamespacedName{Name: "secret-sub", Namespace: "test-sub-namespace"}
	chkey := types.NamespacedName{Name: "secret-ch", Namespace: "default"}

	sub := &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subkey.Name,
			Namespace: subkey.Namespace,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: chkey.String(),
			PackageFilter: &appv1alpha1.PackageFilter{
				FilterRef: subRef,
			},
		},
	}

	dplSrtName = "dpl-srt"
	dplSrt = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dplSrtName,
			Namespace:   chkey.Namespace,
			Annotations: map[string]string{appv1alpha1.AnnotationDeployables: "true"},
		},
	}

	noneDplSrtName = "none-dplsrt"
	noneDplSrt = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      noneDplSrtName,
			Namespace: chkey.Namespace,
		},
	}

	channel = &chnv1alpha1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      chkey.Name,
			Namespace: chkey.Namespace,
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type: chnv1alpha1.ChannelTypeNamespace,
		},
	}

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	spySync := &fakeSynchronizer{}
	tSubscriber, _ := CreateNsSubscriber(cfg, mgr.GetScheme(), mgr, spySync)

	tSubscriber.itemmap[subkey] = &NsSubscriberItem{
		SubscriberItem: appv1alpha1.SubscriberItem{
			Subscription: sub,
			Channel:      channel,
		},
	}
	// Getting a secret reconciler which belongs to the subscription defined in the var and the subscription is
	// pointing to a namespace type of channel
	srtRec := newSecretReconciler(tSubscriber, mgr, subkey)

	g.Expect(c.Create(context.TODO(), dplSrt)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), dplSrt)

	g.Expect(c.Create(context.TODO(), noneDplSrt)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), noneDplSrt)

	dplSrtKey := types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}
	g.Expect(srtRec.getSecretsBySubLabel(dplSrtKey.Namespace)).ShouldNot(gomega.BeNil())
	//check up if the target secert is deployed at the subscriptions namespace
	dplSrtRq := reconcile.Request{NamespacedName: types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}}

	// Do secret reconcile which should pick up the dplSrt and deploy it to the subscription namespace (check point)
	//checking if the reconcile has any error
	g.Expect(srtRec.Reconcile(dplSrtRq)).ShouldNot(gomega.BeNil())

	spySync.assertTemplateRegistry(dplSrtKey, dplSrt.GroupVersionKind())
}
