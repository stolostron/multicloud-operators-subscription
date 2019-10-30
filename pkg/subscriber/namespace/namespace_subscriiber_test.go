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
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
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
			APIVersion: "app.ibm.com/v1alpha1",
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
	chns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      id.Namespace,
			Namespace: id.Namespace,
		},
	}
)

func TestDefaultSubscriber(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()
	// prepare default channel
	ns := chns.DeepCopy()
	dpl := chdpl.DeepCopy()

	g.Expect(c.Create(context.TODO(), ns)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Create(context.TODO(), dpl)).NotTo(gomega.HaveOccurred())

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())
	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	cfgmap := &corev1.ConfigMap{}

	time.Sleep(10 * time.Second)

	g.Expect(c.Get(context.TODO(), workloadkey, cfgmap)).NotTo(gomega.HaveOccurred())

	// clean up
	c.Delete(context.TODO(), dpl)

	time.Sleep(10 * time.Second)
	g.Expect(c.Get(context.TODO(), workloadkey, cfgmap)).To(gomega.HaveOccurred())

	c.Delete(context.TODO(), ns)
}
