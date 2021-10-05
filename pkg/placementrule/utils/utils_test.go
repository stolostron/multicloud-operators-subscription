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

package utils

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
)

var (
	cluster1 = &spokeClusterV1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"name": "cluster1",
				"key1": "c1v1",
				"key2": "c1v2",
			},
		},
	}
	cluster2 = &spokeClusterV1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster2",
			Labels: map[string]string{
				"name": "cluster2",
				"key1": "c2v1",
				"key2": "c2v2",
			},
		},
	}

	clusters = []*spokeClusterV1.ManagedCluster{cluster1, cluster2}

	cl1 = appv1alpha1.GenericClusterReference{Name: "cluster1"}

	placementrule1 = &appv1alpha1.PlacementRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "placmentrule-1",
			Namespace: "default",
			Annotations: map[string]string{
				"open-cluster-management.io/user-identity": "a3ViZTphZG1pbg==",
				"open-cluster-management.io/user-group":    "c3lzdGVtOmNsdXN0ZXItYWRtaW5zLHN5c3RlbTphdXRoZW50aWNhdGVk",
			},
		},
		Spec: appv1alpha1.PlacementRuleSpec{
			GenericPlacementFields: appv1alpha1.GenericPlacementFields{
				Clusters: []appv1alpha1.GenericClusterReference{cl1},
			},
		},
	}
)

func TestLocal(t *testing.T) {
	if ToPlaceLocal(nil) {
		t.Error("Failed to check local placement for nil placement")
	}

	pl := &appv1alpha1.Placement{}
	if ToPlaceLocal(pl) {
		t.Error("Failed to check local placement for nil local")
	}

	l := false
	pl.Local = &l

	if ToPlaceLocal(pl) {
		t.Error("Failed to check local placement for false local")
	}

	l = true

	if !ToPlaceLocal(pl) {
		t.Error("Failed to check local placement for true local")
	}
}

func TestLoadCRD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	g.Expect(CheckAndInstallCRD(cfg, "../../../deploy/crds/apps.open-cluster-management.io_subscriptions_crd_v1.yaml")).NotTo(gomega.HaveOccurred())
}

func TestEventRecorder(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	rec, err := NewEventRecorder(cfg, scheme.Scheme)
	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}
	g.Expect(c.Create(context.TODO(), cfgmap)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), cfgmap)

	rec.RecordEvent(cfgmap, "no reason", "no message", err)
}

func TestPlacementRule(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	kubeClient := kubernetes.NewForConfigOrDie(cfg)

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	for _, cl := range clusters {
		clinstance := cl.DeepCopy()
		err = c.Create(context.TODO(), clinstance)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		defer c.Delete(context.TODO(), clinstance)
	}

	err = c.Create(context.TODO(), placementrule1)
	defer c.Delete(context.TODO(), placementrule1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// test PlaceByGenericPlacmentFields
	clmap, err := PlaceByGenericPlacmentFields(c, placementrule1.Spec.GenericPlacementFields, kubeClient, placementrule1)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(clmap)).To(gomega.Equal(1))

	// test IsReadyACMClusterRegistry
	ret := IsReadyACMClusterRegistry(mgr.GetAPIReader())
	g.Expect(ret).To(gomega.Equal(true))

	// test FilteClustersByIdentity
	err = FilteClustersByIdentity(kubeClient, placementrule1, clmap)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(clmap)).To(gomega.Equal(0))
}
