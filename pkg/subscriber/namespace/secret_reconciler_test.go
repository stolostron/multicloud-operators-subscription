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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	synckube "github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
)

var _ = Describe("base secret reconcile", func() {
	It("should reconcile with the default synchronizer", func() {
		// a deployable annotated secert will sit at the channel namespace
		var (
			chKey = types.NamespacedName{
				Name:      "tend",
				Namespace: "tch",
			}

			dplSrtName = "dpl-srt"
			dplSrt     = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        dplSrtName,
					Namespace:   chKey.Namespace,
					Annotations: map[string]string{appv1alpha1.AnnotationDeployables: "true"},
				},
			}

			// a normal secert will sit at the channel namespace
			noneDplSrtName = "none-dplsrt"
			noneDplSrt     = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      noneDplSrtName,
					Namespace: chKey.Namespace,
				},
			}

			channel = &chnv1alpha1.Channel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      chKey.Name,
					Namespace: chKey.Namespace,
				},
				Spec: chnv1alpha1.ChannelSpec{
					Type: chnv1alpha1.ChannelTypeNamespace,
				},
			}

			subkey = types.NamespacedName{
				Name:      "srt-test-sub",
				Namespace: "srt-test-sub-namespace",
			}

			subRef = &corev1.LocalObjectReference{
				Name: subkey.Name,
			}

			subscription = &appv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subkey.Name,
					Namespace: subkey.Namespace,
				},
				Spec: appv1alpha1.SubscriptionSpec{
					Channel: id.String(),
					PackageFilter: &appv1alpha1.PackageFilter{
						FilterRef: subRef,
					},
				},
			}
		)
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is find id
		defaultNsSubscriber.itemmap[subkey] = &NsSubscriberItem{
			SubscriberItem: appv1alpha1.SubscriberItem{
				Subscription: subscription,
				Channel:      channel,
			},
		}

		// Getting a secret reconciler which belongs to the subscription defined in the var and the subscription is
		// pointing to a namespace type of channel
		srtRec := newSecretReconciler(defaultNsSubscriber, k8sManager, subkey)

		// Create secrets at the channel namespace

		Expect(k8sClient.Create(context.TODO(), subscription)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), subscription)

		Expect(k8sClient.Create(context.TODO(), dplSrt)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), dplSrt)

		Expect(k8sClient.Create(context.TODO(), noneDplSrt)).NotTo(HaveOccurred())
		defer k8sClient.Delete(context.TODO(), noneDplSrt)

		time.Sleep(k8swait)
		dplSrtKey := types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}
		Expect(srtRec.getSecretsBySubLabel(dplSrtKey.Namespace)).ShouldNot(BeNil())
		//check up if the target secert is deployed at the subscriptions namespace
		dplSrtRq := reconcile.Request{NamespacedName: types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}}
		// Do secret reconcile which should pick up the dplSrt and deploy it to the subscription namespace (check point)
		//checking if the reconcile has any error
		Expect(srtRec.Reconcile(dplSrtRq)).ShouldNot(BeNil())

		expectSrt := &corev1.Secret{}
		Expect(k8sClient.Get(context.TODO(),
			types.NamespacedName{Name: dplSrtKey.Name, Namespace: subscription.Namespace},
			expectSrt)).Should(Succeed())

		defer func() {
			Expect(k8sClient.Delete(context.TODO(), expectSrt)).Should(Succeed())
		}()
	})
})

// this is a used to mock the synchronizer due to the houseKeeping cycle
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

func (f *fakeSynchronizer) RegisterTemplate(nKey fmt.Stringer, pDpl *dplv1alpha1.Deployable, s string) error {
	template := &unstructured.Unstructured{}
	err := json.Unmarshal(pDpl.Spec.Template.Raw, template)

	tplgvk := template.GetObjectKind().GroupVersionKind()
	f.Store[tplgvk] = map[string]bool{nKey.String(): true}

	return errors.Wrap(err, "failed to register template to fake synchronizer")
}

func (f *fakeSynchronizer) IsResourceNamespaced(gvk schema.GroupVersionKind) bool {
	return true
}

func (f *fakeSynchronizer) GetLocalClient() client.Client {
	return nil
}

func (f *fakeSynchronizer) CleanupByHost(key types.NamespacedName, s string) error { return nil }

func (f *fakeSynchronizer) GetInterval() int {
	return f.interval
}

func (f *fakeSynchronizer) AddTemplates(subType string, hostSub types.NamespacedName, dpls []kubernetes.DplUnit) error {
	return nil
}

func (f *fakeSynchronizer) assertTemplateRegistry(nKey types.NamespacedName, gvk schema.GroupVersionKind) bool {
	if _, ok := f.Store[gvk][nKey.String()]; !ok {
		return false
	}

	return true
}

var _ = Describe("fakeSynchronizer reconcile test", func() {
	It("should reconcile without error", func() {
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is find id
		var (
			subkey = types.NamespacedName{Name: "fake-secret-sub", Namespace: "test-sub-namespace"}
			chkey  = types.NamespacedName{Name: "fake-secret-ch", Namespace: "default"}

			subRef = &corev1.LocalObjectReference{
				Name: subkey.Name,
			}

			sub = &appv1alpha1.Subscription{
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

			dplSrtName = "fake-dpl-srt"
			dplSrt     = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        dplSrtName,
					Namespace:   chkey.Namespace,
					Annotations: map[string]string{appv1alpha1.AnnotationDeployables: "true"},
				},
			}

			noneDplSrtName = "fake-none-dplsrt"
			noneDplSrt     = &corev1.Secret{
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
		)

		spySync := &fakeSynchronizer{}
		tSubscriber, _ := CreateNsSubscriber(k8sManager.GetConfig(), k8sManager.GetScheme(), k8sManager, spySync)

		tSubscriber.itemmap[subkey] = &NsSubscriberItem{
			SubscriberItem: appv1alpha1.SubscriberItem{
				Subscription: sub,
				Channel:      channel,
			},
		}
		// Getting a secret reconciler which belongs to the subscription defined in the var and the subscription is
		// pointing to a namespace type of channel
		srtRec := newSecretReconciler(tSubscriber, k8sManager, subkey)

		Expect(k8sClient.Create(context.TODO(), dplSrt)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), dplSrt)

		Expect(k8sClient.Create(context.TODO(), noneDplSrt)).NotTo(HaveOccurred())

		defer k8sClient.Delete(context.TODO(), noneDplSrt)

		time.Sleep(k8swait)
		dplSrtKey := types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}
		Expect(srtRec.getSecretsBySubLabel(dplSrtKey.Namespace)).ShouldNot(BeNil())
		//check up if the target secert is deployed at the subscriptions namespace
		dplSrtRq := reconcile.Request{NamespacedName: types.NamespacedName{Name: dplSrt.GetName(), Namespace: dplSrt.GetNamespace()}}

		// Do secret reconcile which should pick up the dplSrt and deploy it to the subscription namespace (check point)
		//checking if the reconcile has any error
		Expect(srtRec.Reconcile(dplSrtRq)).ShouldNot(BeNil())

		spySync.assertTemplateRegistry(dplSrtKey, dplSrt.GroupVersionKind())

	})
})
