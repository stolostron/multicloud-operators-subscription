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

package subscription

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

var (
	srtNotFoundName   = "nothere"
	testNamespaceName = "testns"
	testKeys          = types.NamespacedName{
		Name:      "test-chn",
		Namespace: testNamespaceName,
	}

	srt = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testKeys.Name,
			Namespace: testKeys.Namespace,
		},
	}

	skey = types.NamespacedName{
		Name:      "test-sub",
		Namespace: testNamespaceName,
	}

	sub = &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      skey.Name,
			Namespace: skey.Namespace,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel:       testKeys.String(),
			PackageFilter: nil,
		},
	}
)

//label related test variables
var (
	testLbKeys = types.NamespacedName{
		Name:      "test-chn-lb",
		Namespace: testNamespaceName,
	}

	srtLb = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testKeys.Name,
			Namespace: testKeys.Namespace,
			Labels:    map[string]string{utils.SercertReferredMarker: "true", sub.GetName(): "true"},
		},
	}

	cfgLb = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testKeys.Name,
			Namespace: testKeys.Namespace,
			Labels:    map[string]string{utils.SercertReferredMarker: "true", sub.GetName(): "true"},
		},
	}

	srtWithoutLb = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-chn-aa",
			Namespace: testKeys.Namespace,
		},
	}
)

// func TestListAndDeployReferredObject(t *testing.T) {

// }

// func TestListReferredObjectByName(t *testing.T) {
// 	g := gomega.NewGomegaWithT(t)
// 	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
// 	// channel when it is finid'Cu
// 	mgr, err := manager.New(cfg, manager.Options{})
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// 	c = mgr.GetClient()

// 	stopMgr, mgrStopped := StartTestManager(mgr, g)

// 	defer func() {
// 		close(stopMgr)
// 		mgrStopped.Wait()
// 	}()

// 	rec := newReconciler(mgr, mgr.GetClient(), nil).(*ReconcileSubscription)

// 	// get the default reonciler then list the secert by name
// 	srtD := srt.DeepCopy()
// 	g.Expect(c.Create(context.TODO(), srtD)).NotTo(gomega.HaveOccurred())

// 	defer c.Delete(context.TODO(), srtD)

// 	//testing if the new referred object is labeled correctly
// 	obj, _ := rec.ListReferredObjectByName(sub, srtD, testKeys.Name)
// 	g.Expect(obj).ShouldNot(gomega.BeNil())

// 	obj, _ = rec.ListReferredObjectByName(sub, srtD, srtNotFoundName)
// 	g.Expect(obj).Should(gomega.BeNil())
// }

// func TestListReferredObjectByLabel(t *testing.T) {
// 	g := gomega.NewGomegaWithT(t)
// 	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
// 	// channel when it is finid'Cu
// 	mgr, err := manager.New(cfg, manager.Options{})
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// 	c = mgr.GetClient()

// 	stopMgr, mgrStopped := StartTestManager(mgr, g)

// 	defer func() {
// 		close(stopMgr)
// 		mgrStopped.Wait()
// 	}()

// 	rec := newReconciler(mgr, mgr.GetClient(), nil).(*ReconcileSubscription)

// 	// get the default reonciler then list the secert by name
// 	srtDLb := srtLb.DeepCopy()
// 	g.Expect(c.Create(context.TODO(), srtDLb)).NotTo(gomega.HaveOccurred())

// 	defer c.Delete(context.TODO(), srtDLb)

// 	srtDWoLb := srtWithoutLb.DeepCopy()
// 	g.Expect(c.Create(context.TODO(), srtDWoLb)).NotTo(gomega.HaveOccurred())

// 	defer c.Delete(context.TODO(), srtDWoLb)

// 	rq := types.NamespacedName{Namespace: testNamespaceName, Name: sub.GetName()}
// 	//testing if the new referred object is labeled correctly
// 	gvk := srtDLb.GetObjectKind().GroupVersionKind()
// 	gvk.Kind = "Secret"
// 	gvk.Version = "v1"
// 	obj, _ := rec.ListReferredObjectByLabel(rq, gvk)
// 	g.Expect(obj).ShouldNot(gomega.BeNil())
// }

// func TestListSubscriptionOwnedObjectsAndDelete(t *testing.T) {
// 	g := gomega.NewGomegaWithT(t)
// 	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
// 	// channel when it is finid'Cu
// 	mgr, err := manager.New(cfg, manager.Options{})
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// 	c = mgr.GetClient()

// 	stopMgr, mgrStopped := StartTestManager(mgr, g)

// 	defer func() {
// 		close(stopMgr)
// 		mgrStopped.Wait()
// 	}()

// 	rec := newReconciler(mgr, mgr.GetClient(), nil).(*ReconcileSubscription)

// 	// get the default reonciler then list the secert by name
// 	srtDLb := srtLb.DeepCopy()
// 	g.Expect(c.Create(context.TODO(), srtDLb)).NotTo(gomega.HaveOccurred())

// 	defer c.Delete(context.TODO(), srtDLb)

// 	srtDWoLb := srtWithoutLb.DeepCopy()
// 	g.Expect(c.Create(context.TODO(), srtDWoLb)).NotTo(gomega.HaveOccurred())

// 	defer c.Delete(context.TODO(), srtDWoLb)

// 	rq := types.NamespacedName{Namespace: testNamespaceName, Name: sub.GetName()}
// 	//testing if the new referred object is labeled correctly
// 	gvk := srtDLb.GetObjectKind().GroupVersionKind()
// 	gvk.Kind = "Secret"
// 	gvk.Version = "v1"
// 	err = rec.ListSubscriptionOwnedObjectsAndDelete(rq, gvk)
// 	g.Expect(err).Should(gomega.BeNil())

// 	resSrt := &corev1.Secret{}
// 	g.Expect(c.Get(context.TODO(), testLbKeys, resSrt)).Should(gomega.HaveOccurred())
// }

// func TestDeployReferredObject(t *testing.T) {
// 	g := gomega.NewGomegaWithT(t)
// 	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
// 	// channel when it is finid'Cu
// 	mgr, err := manager.New(cfg, manager.Options{})
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// 	c = mgr.GetClient()

// 	stopMgr, mgrStopped := StartTestManager(mgr, g)

// 	defer func() {
// 		close(stopMgr)
// 		mgrStopped.Wait()
// 	}()

// 	rec := newReconciler(mgr, mgr.GetClient(), nil).(*ReconcileSubscription)

// 	// get the default reonciler then list the secert by name
// 	srtDLb := srtLb.DeepCopy()

// 	defer c.Delete(context.TODO(), srtDLb)

// 	//testing if the new referred object is labeled correctly
// 	gvk := srtDLb.GetObjectKind().GroupVersionKind()
// 	gvk.Kind = "Secret"
// 	gvk.Version = "v1"
// 	err = rec.DeployReferredObject(sub, gvk, srtDLb)
// 	g.Expect(err).Should(gomega.BeNil())

// 	// get the default reonciler then list the secert by name
// 	cfgDLb := cfgLb.DeepCopy()

// 	defer c.Delete(context.TODO(), cfgDLb)

// 	//testing if the new referred object is labeled correctly
// 	gvk = cfgDLb.GetObjectKind().GroupVersionKind()
// 	gvk.Kind = "ConfigMap"
// 	gvk.Version = "v1"
// 	err = rec.DeployReferredObject(sub, gvk, cfgDLb)
// 	g.Expect(err).Should(gomega.BeNil())

// 	resCfg := &corev1.ConfigMap{}
// 	c.Get(context.TODO(), testLbKeys, resCfg)
// 	g.Expect(resCfg.GetName()).ShouldNot(gomega.BeNil())
// }

// func TestCleanUpObject(t *testing.T) {
// 	testCases := []struct {
// 		desc     string
// 		obj      runtime.Object
// 		objLabel map[string]string
// 		ns       string
// 	}{
// 		{
// 			desc:     "",
// 			obj:      &srtLb,
// 			objLabel: srtLb.GetLabels(),
// 			ns:       testNamespaceName,
// 		},
// 	}
// 	for _, tC := range testCases {
// 		t.Run(tC.desc, func(t *testing.T) {
// 			gvk := tC.obj.GetObjectKind().GroupVersionKind()
// 			gvk.Group = " "
// 			gvk.Kind = "Secret"
// 			gvk.Version = "v1"
// 			_, err := CleanUpObjectAndSetLabelsWithNamespace(tC.ns, gvk, tC.obj, tC.objLabel)
// 			if err != nil {
// 				t.Errorf("Failed to clean up the object due to %v ", err)
// 			}
// 		})
// 	}
// }
