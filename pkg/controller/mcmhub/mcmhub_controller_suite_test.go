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

package mcmhub

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	addonV1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	clusterapi "open-cluster-management.io/api/cluster/v1beta1"
	workV1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/apis"
	ansiblejob "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/ansible/v1alpha1"
	placementv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var cfg *rest.Config
var c client.Client

var (
	successfulPlacementRuleKey     types.NamespacedName
	successfulPlacementDecisionKey types.NamespacedName
	successfulPlacementRule        *placementv1.PlacementRule
	successfulPlacementDecision    *clusterapi.PlacementDecision
)

var (
	mcmmgr                       manager.Manager
	errMgr                       error
	sutPropagationTestClient     client.Client
	sutPropagationTestReconciler reconcile.Reconciler
)

func TestMain(m *testing.M) {
	t := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "deploy", "crds"),
			filepath.Join("..", "..", "..", "hack", "test"),
		},
	}

	ansiblejob.AddToScheme(scheme.Scheme)
	apis.AddToScheme(scheme.Scheme)
	spokeClusterV1.AddToScheme(scheme.Scheme)
	appSubStatusV1alpha1.AddToScheme(scheme.Scheme)
	workV1.AddToScheme(scheme.Scheme)
	addonV1alpha1.AddToScheme(scheme.Scheme)

	var err error
	if cfg, err = t.Start(); err != nil {
		log.Fatal(err)
	}

	mcmmgr, errMgr = manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	if errMgr != nil {
		log.Fatal(errMgr)
	}

	sutPropagationTestClient = mcmmgr.GetClient()
	sutPropagationTestReconciler = newReconciler(mcmmgr)

	ctrlErr := add(mcmmgr, sutPropagationTestReconciler)

	if ctrlErr != nil {
		log.Fatal(ctrlErr)
	}

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mcmmgr, nil)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	var c client.Client

	if c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme}); err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-chn-namespace"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "ch-helm-ns"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tp-chn-namespace"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tp-chn-helm-namespace"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "ansible-pre-1"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sub-namespace"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "topo-anno-sub-namespace"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "topo-helm-sub-ns"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "propagation-test-cases"},
	})
	if err != nil {
		log.Fatal(err)
	}

	successfulPlacementRuleKey = types.NamespacedName{
		Name:      "test-propagation-successful-placement",
		Namespace: "propagation-test-cases",
	}
	successfulPlacementRule = &placementv1.PlacementRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      successfulPlacementRuleKey.Name,
			Namespace: successfulPlacementRuleKey.Namespace,
		},
		Spec: placementv1.PlacementRuleSpec{
			GenericPlacementFields: placementv1.GenericPlacementFields{
				ClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"name": "cluster-1"},
				},
			},
		},
	}

	successfulPlacementDecisionKey = types.NamespacedName{
		Name:      "test-propagation-successful-placement",
		Namespace: "propagation-test-cases",
	}
	successfulPlacementDecision = &clusterapi.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"cluster.open-cluster-management.io/placementrule": successfulPlacementRuleKey.Name,
			},
			Name:      successfulPlacementDecisionKey.Name,
			Namespace: successfulPlacementDecisionKey.Namespace,
		},
	}

	// create placementRule, update placementRule cluster decision
	err = c.Create(context.TODO(), successfulPlacementRule)
	if err != nil {
		log.Fatal(err)
	}

	placementRule := &placementv1.PlacementRule{}
	err = c.Get(context.TODO(), successfulPlacementRuleKey, placementRule)

	if err != nil {
		log.Fatal(err)
	}

	placementRule.Status = placementv1.PlacementRuleStatus{
		Decisions: []placementv1.PlacementDecision{
			{
				ClusterName:      "cluster-1",
				ClusterNamespace: "cluster-1",
			},
		},
	}

	err = c.Status().Update(context.TODO(), placementRule)
	if err != nil {
		log.Fatal(err)
	}

	// create placementrule's placementDecision, update placementDecision cluster decision
	err = c.Create(context.TODO(), successfulPlacementDecision)
	if err != nil {
		log.Fatal(err)
	}

	placementDecision := &clusterapi.PlacementDecision{}
	err = c.Get(context.TODO(), successfulPlacementDecisionKey, placementDecision)

	if err != nil {
		log.Fatal(err)
	}

	placementDecision.Status = clusterapi.PlacementDecisionStatus{
		Decisions: []clusterapi.ClusterDecision{
			{
				ClusterName: "cluster-1",
				Reason:      "",
			},
		},
	}

	err = c.Status().Update(context.TODO(), placementDecision)

	if err != nil {
		log.Fatal(err)
	}

	code := m.Run()

	t.Stop()
	os.Exit(code)
}

// SetupTestReconcile returns a reconcile.Reconcile implementation that delegates to inner and
// writes the request to requests after Reconcile is finished.
func SetupTestReconcile(inner reconcile.Reconciler) (reconcile.Reconciler, chan reconcile.Request) {
	requests := make(chan reconcile.Request)
	fn := reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(ctx, req)
		requests <- req

		return result, err
	})

	return fn, requests
}

// StartTestManager adds recFn
func StartTestManager(ctx context.Context, mgr manager.Manager, g *GomegaWithT) *sync.WaitGroup {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		wg.Done()
		mgr.Start(ctx)
	}()

	return wg
}
