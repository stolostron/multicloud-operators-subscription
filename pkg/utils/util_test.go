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
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

// https://github.com/kubernetes/kubernetes/issues/82130
// before we update to latest k8s, let's skip this test case, especially, the
// CheckAndInstallCRD() is not used by other code
//func TestKubernetes(t *testing.T) {
//	g := gomega.NewGomegaWithT(t)
//
//	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
//	// channel when it is finished.
//	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
//	g.Expect(err).NotTo(gomega.HaveOccurred())
//
//	c = mgr.GetClient()
//
//	//start manager mgr
//	stopMgr, mgrStopped := StartTestManager(mgr, g)
//
//	defer func() {
//		close(stopMgr)
//		mgrStopped.Wait()
//	}()
//
//	//Test:  create testfoo crd
//	err = CheckAndInstallCRD(cfg, "../../deploy/crds/apps.open-cluster-management.io_subscriptions_crd.yaml")
//	g.Expect(err).NotTo(gomega.HaveOccurred())
//}

func TestNamespacedNameFormat(t *testing.T) {
	n := "tname"
	ns := "tnamespace"
	nsn := types.NamespacedName{
		Name:      n,
		Namespace: ns,
	}

	fnsn := NamespacedNameFormat(nsn.String())
	if !reflect.DeepEqual(nsn, fnsn) {
		t.Errorf("Format NamespacedName string failed.\n\tExpect:%v\n\tResult:%v", nsn, fnsn)
	}

	fnsn = NamespacedNameFormat("incorrect format")
	if !reflect.DeepEqual(types.NamespacedName{}, fnsn) {
		t.Errorf("Format NamespacedName string failed.\n\tExpect:%v\n\tResult:%v", types.NamespacedName{}, fnsn)
	}
}

func TestEventlog(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	rec, err := NewEventRecorder(cfg, mgr.GetScheme())
	g.Expect(err).NotTo(gomega.HaveOccurred())

	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}
	g.Expect(c.Create(context.TODO(), obj)).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), obj)

	rec.RecordEvent(obj, "testreason", "testmsg", nil)
	rec.RecordEvent(obj, "testreason", "testmsg", errors.New("testeventerr"))

	time.Sleep(1 * time.Second)
}

var (
	dpln       = "test-dpl"
	dplns      = "test-dpl-ns"
	hostdplkey = types.NamespacedName{
		Name:      dpln,
		Namespace: dplns,
	}

	subn       = "test-sub"
	subns      = "test-sub-ns"
	hostsubkey = types.NamespacedName{
		Name:      subn,
		Namespace: subns,
	}

	cln       = "test-cluster"
	clns      = "test-cluster-ns"
	hostclkey = types.NamespacedName{
		Name:      cln,
		Namespace: clns,
	}

	cmn    = "test-configmap"
	cmns   = "test-configmap-ns"
	cfgmap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmn,
			Namespace: cmns,
			Annotations: map[string]string{
				dplv1alpha1.AnnotationHosting:        hostdplkey.String(),
				dplv1alpha1.AnnotationSubscription:   hostsubkey.String(),
				dplv1alpha1.AnnotationManagedCluster: hostclkey.String(),
				appv1alpha1.AnnotationSyncSource:     "testsource-" + hostsubkey.String(),
			},
		},
	}
)

func TestAnnotations(t *testing.T) {
	cl := GetClusterFromResourceObject(cfgmap)

	if !reflect.DeepEqual(*cl, hostclkey) {
		t.Errorf("Failed to get cluster from object .\n\tExpect:%v\n\tResult:%v", hostclkey, cl)
	}

	dplkey := GetHostDeployableFromObject(cfgmap)

	if !reflect.DeepEqual(*dplkey, hostdplkey) {
		t.Errorf("Failed to get cluster from object .\n\tExpect:%v\n\tResult:%v", hostdplkey, *dplkey)
	}

	subkey := GetHostSubscriptionFromObject(cfgmap)

	if !reflect.DeepEqual(*subkey, hostsubkey) {
		t.Errorf("Failed to get cluster from object .\n\tExpect:%v\n\tResult:%v", hostsubkey, *subkey)
	}
}
