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
	"testing"
	"time"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1alpha1 "github.com/IBM/multicloud-operators-channel/pkg/apis/app/v1alpha1"
	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

var c client.Client

var id = types.NamespacedName{
	Name:      "tend",
	Namespace: "tch",
}

var (
	workloadkey = types.NamespacedName{
		Name:      "testworkload",
		Namespace: "tch",
	}

	workloadconfigmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadkey.Name,
			Namespace: workloadkey.Namespace,
		},
	}

	chdpl = &dplv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployable",
			APIVersion: "multicloud-apps.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "chdpl",
			Namespace: id.Namespace,
		},
		Spec: dplv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: &workloadconfigmap,
			},
		},
	}
)

var (
	defaultworkloadkey = types.NamespacedName{
		Name:      "testworkload",
		Namespace: "default",
	}

	defaultworkloadconfigmap = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultworkloadkey.Name,
			Namespace: defaultworkloadkey.Namespace,
		},
	}

	defaultchdpl = &dplv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployable",
			APIVersion: "multicloud-apps.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dftdpl",
			Namespace: id.Namespace,
		},
		Spec: dplv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: &defaultworkloadconfigmap,
			},
		},
	}
)

var (
	chns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      id.Namespace,
			Namespace: id.Namespace,
		},
	}
)

// for secret reconciler tests
var (
	channel = &chnv1alpha1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      id.Name,
			Namespace: id.Namespace,
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type: chnv1alpha1.ChannelTypeNamespace,
		},
	}

	// a deployable annotated secert will sit at the channel namespace
	dplSrtName = "dpl-srt"
	dplSrt     = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dplSrtName,
			Namespace:   id.Namespace,
			Annotations: map[string]string{appv1alpha1.AnnotationDeployables: "true"},
		},
	}

	// a normal secert will sit at the channel namespace
	noneDplSrtName = "none-dplsrt"
	noneDplSrt     = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      noneDplSrtName,
			Namespace: id.Namespace,
		},
	}

	subkey = types.NamespacedName{
		Name:      "test-sub",
		Namespace: "test-sub-namespace",
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

func TestDefaultSubscriber(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()
	// prepare default channel
	ns := chns.DeepCopy()
	dpl := chdpl.DeepCopy()
	dpldft := defaultchdpl.DeepCopy()
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	g.Expect(Add(mgr, cfg, &types.NamespacedName{}, 2)).NotTo(gomega.HaveOccurred())
	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	g.Expect(c.Create(context.TODO(), ns)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Create(context.TODO(), dpl)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Create(context.TODO(), dpldft)).NotTo(gomega.HaveOccurred())

	cfgmap := &corev1.ConfigMap{}
	tdpl := &dplv1alpha1.Deployable{}

	time.Sleep(15 * time.Second)
	g.Expect(c.Get(context.TODO(), types.NamespacedName{Name: dpl.GetName(), Namespace: dpl.GetNamespace()}, tdpl)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), types.NamespacedName{Name: dpldft.GetName(), Namespace: dpldft.GetNamespace()}, tdpl)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), workloadkey, cfgmap)).To(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), defaultworkloadkey, cfgmap)).To(gomega.HaveOccurred())

	// clean up
	c.Delete(context.TODO(), dpl)
	c.Delete(context.TODO(), dpldft)

	time.Sleep(15 * time.Second)
	g.Expect(c.Get(context.TODO(), workloadkey, cfgmap)).To(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), defaultworkloadkey, cfgmap)).To(gomega.HaveOccurred())

	c.Delete(context.TODO(), ns)
}
