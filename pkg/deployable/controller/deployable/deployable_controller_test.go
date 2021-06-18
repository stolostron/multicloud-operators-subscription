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

package deployable

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
	placementrulev1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/deployable/utils"
)

var c client.Client

const timeout = time.Second * 5

var (
	dplname = "example-configmap"
	dplns   = "default"
	dplkey  = types.NamespacedName{
		Name:      dplname,
		Namespace: dplns,
	}
)

var (
	endpoint1 = spokeClusterV1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"name":          "endpoint1-ns",
				"local-cluster": "true",
			},
			Name: "endpoint1-ns",
		},
	}
	endpoint1ns = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "endpoint1-ns",
			Namespace: "endpoint1-ns",
		},
	}

	endpoint2 = spokeClusterV1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"name": "endpoint2-ns",
			},
			Name: "endpoint2-ns",
		},
	}
	endpoint2ns = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "endpoint2-ns",
			Namespace: "endpoint2-ns",
		},
	}

	endpointnss = []corev1.Namespace{endpoint1ns, endpoint2ns}
	endpoints   = []spokeClusterV1.ManagedCluster{endpoint1, endpoint2}
)

var (
	payload = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "payload",
		},
	}
)

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	instance := &appv1alpha1.Deployable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dplname,
			Namespace: dplns,
		},
		Spec: appv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: payload,
			},
		},
	}
	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	var expectedRequest = reconcile.Request{NamespacedName: dplkey}

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(2 * time.Second)

	dpllist := &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	for _, dpl := range dpllist.Items {
		err = c.Delete(context.TODO(), &dpl)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}
}

func TestPropagate(t *testing.T) {
	var err error

	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	for _, ns := range endpointnss {
		err = c.Create(context.TODO(), &ns)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	for _, ep := range endpoints {
		err = c.Create(context.TODO(), &ep)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	g.Expect(err).NotTo(gomega.HaveOccurred())

	placecluster := placementrulev1alpha1.GenericClusterReference{
		Name: endpoint1.GetName(),
	}

	placecluster2 := placementrulev1alpha1.GenericClusterReference{
		Name: endpoint2.GetName(),
	}

	instance := &appv1alpha1.Deployable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dplname,
			Namespace: dplns,
		},
		Spec: appv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: payload,
			},
			Placement: &placementrulev1alpha1.Placement{
				GenericPlacementFields: placementrulev1alpha1.GenericPlacementFields{
					Clusters: []placementrulev1alpha1.GenericClusterReference{placecluster, placecluster2},
				},
			},
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	var expectedRequest = reconcile.Request{NamespacedName: dplkey}

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(2 * time.Second)

	dpllist := &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{Namespace: endpoint1.GetName()})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(dpllist.Items) != 1 {
		t.Errorf("Failed to propagate to cluster endpoint1. items: %v", dpllist)
	}

	if len(dpllist.Items) == 1 {
		dpl := dpllist.Items[0]
		expgenname := instance.GetName() + "-"

		if dpl.GetGenerateName() != expgenname {
			t.Errorf("Incorrect generate name of generated deployable. \n\texpect:\t%s\n\tgot:\t%s", expgenname, dpl.GetGenerateName())
		}

		// If the target cluster has local-cluster:true label, check if the payload name is appended by -local
		sub := &unstructured.Unstructured{}
		err = json.Unmarshal(dpllist.Items[0].Spec.Template.Raw, sub)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(sub.GetName()).To(gomega.Equal("payload-local"))
	}

	dpllist2 := &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist2, &client.ListOptions{Namespace: endpoint2.GetName()})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(dpllist2.Items) != 1 {
		t.Errorf("Failed to propagate to cluster endpoint2. items: %v", dpllist)
	}

	if len(dpllist2.Items) == 1 {
		dpl := dpllist2.Items[0]
		expgenname := instance.GetName() + "-"

		if dpl.GetGenerateName() != expgenname {
			t.Errorf("Incorrect generate name of generated deployable. \n\texpect:\t%s\n\tgot:\t%s", expgenname, dpl.GetGenerateName())
		}

		// If the target cluster does not have local-cluster:true label, check if the payload name is NOT appended by -local
		sub := &unstructured.Unstructured{}
		err = json.Unmarshal(dpllist2.Items[0].Spec.Template.Raw, sub)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(sub.GetName()).To(gomega.Equal("payload"))
	}

	//delete the instance, verify the propagated dpls in the endpoint1 cluster should be removed
	instance1 := &appv1alpha1.Deployable{}
	err = c.Get(context.TODO(), dplkey, instance1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if err == nil {
		err = c.Delete(context.TODO(), instance1)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

		time.Sleep(2 * time.Second)

		dpllist = &appv1alpha1.DeployableList{}
		err = c.List(context.TODO(), dpllist, &client.ListOptions{Namespace: endpoint1.GetName()})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		if len(dpllist.Items) != 0 {
			t.Errorf("Failed to delete propagated deployable in cluster endpoint1. items: %v", dpllist)
		}
	}
}

func TestOverride(t *testing.T) {
	var err error

	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	configData1 := make(map[string]string)
	configData1["purpose"] = "for test"

	configMapTpl := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config1",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		Data: configData1,
	}

	clusteroveride := appv1alpha1.ClusterOverride{RawExtension: runtime.RawExtension{Raw: []byte("{\"path\": \"data\", \"value\": {\"foo\": \"bar\"}}")}}
	clusteroverideArray := []appv1alpha1.ClusterOverride{clusteroveride}

	override := appv1alpha1.Overrides{
		ClusterName:      "endpoint2-ns",
		ClusterOverrides: clusteroverideArray,
	}
	overrideArray := []appv1alpha1.Overrides{override}

	dplobj := &appv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Deployable",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      dplname,
			Namespace: dplns,
		},
		Spec: appv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: configMapTpl,
			},
			Placement: &placementrulev1alpha1.Placement{
				GenericPlacementFields: placementrulev1alpha1.GenericPlacementFields{
					ClusterSelector: &metav1.LabelSelector{},
				},
			},
			Overrides: overrideArray,
		},
	}

	err = c.Create(context.TODO(), dplobj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	var expectedRequest = reconcile.Request{NamespacedName: dplkey}

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(2 * time.Second)

	dpllist := &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(dpllist.Items) != 3 {
		t.Errorf("Failed to propagate to cluster endpoints. dpl items should be 3")
	}

	for _, dpl := range dpllist.Items {
		if dpl.GetGenerateName() == "" {
			continue
		}

		expgenname := dplobj.GetName() + "-"

		if dpl.GetGenerateName() != expgenname {
			t.Errorf("Incorrect generate name of generated deployable. \n\texpect:\t%s\n\tgot:\t%s", expgenname, dpl.GetGenerateName())
		}

		// verify override
		if dpl.Namespace == "endpoint1-ns" {
			template := &unstructured.Unstructured{}

			json.Unmarshal(dpl.Spec.Template.Raw, template)

			var expectecdData = make(map[string]interface{})
			expectecdData["purpose"] = "for test"

			if !reflect.DeepEqual(expectecdData, template.Object["data"]) {
				t.Errorf("Incorrect deployable data override. expected data: %#v, actual data: %#v", expectecdData, template.Object["data"])
			}
		}

		if dpl.Namespace == "endpoint2-ns" {
			template := &unstructured.Unstructured{}
			json.Unmarshal(dpl.Spec.Template.Raw, template)

			var expectecdData = make(map[string]interface{})
			expectecdData["foo"] = "bar"

			if !reflect.DeepEqual(expectecdData, template.Object["data"]) {
				t.Errorf("Incorrect deployable data override. expected data: %#v, actual data: %#v", expectecdData, template.Object["data"])
			}
		}
	}

	//clean up root deploayble and
	instance1 := &appv1alpha1.Deployable{}
	err = c.Get(context.TODO(), dplkey, instance1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Delete(context.TODO(), instance1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
}

func TestRollingUpdateStatus(t *testing.T) {
	var err error

	//create 1 managed clusters for the rolling update test
	endpointns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "postrollingendpoint-ns",
			Namespace: "postrollingendpoint-ns",
			Labels: map[string]string{
				"name": "postrollingendpoint",
			},
		},
	}
	endpoint := spokeClusterV1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"name": "postrollingendpoint",
			},
			Name: "postrollingendpoint-ns",
		},
	}

	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	err = c.Create(context.TODO(), &endpointns)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), &endpoint)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	//Make sure the namepsaces are created correctly.
	namereq := metav1.LabelSelectorRequirement{}
	namereq.Key = "name"
	namereq.Operator = metav1.LabelSelectorOpIn

	namereq.Values = []string{"postrollingendpoint"}
	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{namereq},
	}

	clSelector, err := utils.ConvertLabels(labelSelector)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	nslist := &corev1.NamespaceList{}
	err = c.List(context.TODO(), nslist, &client.ListOptions{LabelSelector: clSelector})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(nslist.Items) != 1 {
		t.Errorf("Failed to create namespaces. items: %v", nslist.Items)
	}

	//create target dpl - versionConfigMapTpl
	configData1 := make(map[string]string)
	configData1["purpose"] = "rolling update"

	anno1 := make(map[string]string)
	anno1["apps.open-cluster-management.io/is-local-deployable"] = "false"

	versionConfigMapTpl := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config1",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		Data: configData1,
	}

	rollingVersionConfigmapDpl := &appv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Deployable",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "postrollingupdate-version-configmap",
			Namespace:   "default",
			Annotations: anno1,
		},
		Spec: appv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: versionConfigMapTpl,
			},
		},
	}

	err = c.Create(context.TODO(), rollingVersionConfigmapDpl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	//create configMapTpl dpl. Now it is no target deploayble annotation
	configData2 := make(map[string]string)
	configData2["purpose"] = "test"

	anno2 := make(map[string]string)
	anno2["apps.open-cluster-management.io/is-local-deployable"] = "false"

	configMapTpl := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config2",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		Data: configData2,
	}

	rollingConfigmapDpl := &appv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Deployable",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "postrollingupdate-configmap",
			Namespace:   "default",
			Annotations: anno2,
			Labels: map[string]string{
				"name": "postrollingendpoint",
			},
		},
		Spec: appv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: configMapTpl,
			},
			Placement: &placementrulev1alpha1.Placement{
				GenericPlacementFields: placementrulev1alpha1.GenericPlacementFields{
					ClusterSelector: labelSelector,
				},
			},
		},
	}

	err = c.Create(context.TODO(), rollingConfigmapDpl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	rollingDplKey := types.NamespacedName{
		Name:      "postrollingupdate-configmap",
		Namespace: "default",
	}

	var expectedRequest = reconcile.Request{NamespacedName: rollingDplKey}

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(2 * time.Second)

	dpllist := &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{LabelSelector: clSelector})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(dpllist.Items) != 2 {
		t.Errorf("Failed to propagate to cluster endpoints. items: %v", dpllist)
	}

	//set up those propagated deployables to deployed status
	for _, dpl := range dpllist.Items {
		expgenname := rollingConfigmapDpl.GetName() + "-"

		if dpl.Namespace != "default" && dpl.GetGenerateName() != expgenname {
			t.Errorf("Incorrect generate name of generated deployable. \n\texpect:\t%s\n\tgot:\t%s", expgenname, dpl.GetGenerateName())
		}

		dpl.Status.Phase = "Deployed"
		now := metav1.Now()
		dpl.Status.LastUpdateTime = &now
		err = c.Status().Update(context.TODO(), &dpl)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	time.Sleep(2 * time.Second)

	dpllist = &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{LabelSelector: clSelector})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	rootDpl := &appv1alpha1.Deployable{}

	for _, dpl := range dpllist.Items {
		if dpl.Status.Phase != "Deployed" {
			t.Errorf("The dpl is not deployed yet. Dpl namespace: %#v, status: %#v", dpl.GetNamespace(), dpl.Status)
		}

		if dpl.Namespace == "default" {
			rootDpl = dpl.DeepCopy()
		}
	}

	//annotate rolling update target to the root deployable and trigger reconcile
	rootDpl.Annotations["apps.open-cluster-management.io/rollingupdate-target"] = "postrollingupdate-version-configmap"
	err = c.Update(context.TODO(), rootDpl)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(2 * time.Second)

	dpllist = &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{LabelSelector: clSelector})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	for _, dpl := range dpllist.Items {
		expgenname := rollingConfigmapDpl.GetName() + "-"

		if dpl.Namespace != "default" && dpl.GetGenerateName() != expgenname {
			t.Errorf("Incorrect generate name of generated deployable. \n\texpect:\t%s\n\tgot:\t%s", expgenname, dpl.GetGenerateName())
		}

		//verify template override on managed clusters by the given rollingupdate-target in the parent deployable.
		if dpl.Namespace == "default" {
			template := &unstructured.Unstructured{}
			json.Unmarshal(dpl.Spec.Template.Raw, template)

			var expectecdData = make(map[string]interface{})
			expectecdData["purpose"] = "rolling update"

			if !reflect.DeepEqual(expectecdData, template.Object["data"]) {
				t.Errorf("Incorrect deployable rolling update. expected data: %#v, actual data: %#v", expectecdData, template.Object["data"])
			}
		} else {
			template := &unstructured.Unstructured{}
			json.Unmarshal(dpl.Spec.Template.Raw, template)

			var expectecdData = make(map[string]interface{})
			expectecdData["purpose"] = "rolling update"

			if !reflect.DeepEqual(expectecdData, template.Object["data"]) {
				t.Errorf("Incorrect deployable rolling update. expected data: %#v, actual data: %#v", expectecdData, template.Object["data"])
			}

			if dpl.Status.Phase != "" {
				t.Errorf("After rolling update, the previously deployed deployable status should be reset. dpl.Status: %#v", dpl.Status)
			}
		}
	}
}

func prepareRollingUpdateDployables(labelSelector *metav1.LabelSelector) (*appv1alpha1.Deployable, *appv1alpha1.Deployable) {
	//create target dpl - versionConfigMapTpl
	configData1 := make(map[string]string)
	configData1["purpose"] = "rolling update"

	anno1 := make(map[string]string)
	anno1["apps.open-cluster-management.io/is-local-deployable"] = "false"

	versionConfigMapTpl := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config1",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		Data: configData1,
	}

	rollingVersionConfigmapDpl := &appv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Deployable",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "version-configmap",
			Namespace:   "default",
			Annotations: anno1,
		},
		Spec: appv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: versionConfigMapTpl,
			},
		},
	}

	//create configMapTpl dpl. Now it is no target deploayble annotation
	configData2 := make(map[string]string)
	configData2["purpose"] = "test"

	anno2 := make(map[string]string)
	anno2["apps.open-cluster-management.io/is-local-deployable"] = "false"

	configMapTpl := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config2",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		Data: configData2,
	}

	rollingConfigmapDpl := &appv1alpha1.Deployable{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.open-cluster-management.io/v1",
			Kind:       "Deployable",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "rollingupdate-configmap",
			Namespace:   "default",
			Annotations: anno2,
			Labels: map[string]string{
				"name": "rollingendpoint",
			},
		},
		Spec: appv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: configMapTpl,
			},
			Placement: &placementrulev1alpha1.Placement{
				GenericPlacementFields: placementrulev1alpha1.GenericPlacementFields{
					ClusterSelector: labelSelector,
				},
			},
		},
	}

	return rollingVersionConfigmapDpl, rollingConfigmapDpl
}

var rollingEndpointnss = []corev1.Namespace{}

var rollingEndpoints = []spokeClusterV1.ManagedCluster{}

var g *gomega.GomegaWithT
var requests chan reconcile.Request
var recFn reconcile.Reconciler
var expectedRequest reconcile.Request
var clSelector labels.Selector

func TestRollingUpdate(t *testing.T) {
	var err error

	//create 10 managed clusters for the rolling update test
	for i := 1; i <= 10; i++ {
		endpointns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rollingendpoint" + strconv.Itoa(i) + "-ns",
				Namespace: "rollingendpoint" + strconv.Itoa(i) + "-ns",
				Labels: map[string]string{
					"name": "rollingendpoint",
				},
			},
		}
		endpoint := spokeClusterV1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"name": "rollingendpoint",
				},
				Name: "rollingendpoint" + strconv.Itoa(i) + "-ns",
			},
		}

		rollingEndpointnss = append(rollingEndpointnss, endpointns)
		rollingEndpoints = append(rollingEndpoints, endpoint)
	}

	g = gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	recFn, requests = SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	for _, ns := range rollingEndpointnss {
		err = c.Create(context.TODO(), &ns)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	for _, ep := range rollingEndpoints {
		err = c.Create(context.TODO(), &ep)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	//Make sure the 10 namepsaces are created correctly.
	namereq := metav1.LabelSelectorRequirement{}
	namereq.Key = "name"
	namereq.Operator = metav1.LabelSelectorOpIn

	namereq.Values = []string{"rollingendpoint"}
	labelSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{namereq},
	}

	clSelector, err = utils.ConvertLabels(labelSelector)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	nslist := &corev1.NamespaceList{}
	err = c.List(context.TODO(), nslist, &client.ListOptions{LabelSelector: clSelector})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(nslist.Items) != 10 {
		t.Errorf("Failed to create namespaces. items: %v", nslist.Items)
	}

	rollingVersionConfigmapDpl, rollingConfigmapDpl := prepareRollingUpdateDployables(labelSelector)

	err = c.Create(context.TODO(), rollingVersionConfigmapDpl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), rollingConfigmapDpl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	rollingDplKey := types.NamespacedName{
		Name:      "rollingupdate-configmap",
		Namespace: "default",
	}

	expectedRequest = reconcile.Request{NamespacedName: rollingDplKey}

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(2 * time.Second)

	dpllist := &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{LabelSelector: clSelector})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	if len(dpllist.Items) != 11 {
		t.Errorf("Failed to propagate to cluster endpoints. items: %v", dpllist)
	}

	//set up those propagated deployables to deployed status
	for _, dpl := range dpllist.Items {
		expgenname := rollingConfigmapDpl.GetName() + "-"

		if dpl.Namespace != "default" && dpl.GetGenerateName() != expgenname {
			t.Errorf("Incorrect generate name of generated deployable. \n\texpect:\t%s\n\tgot:\t%s", expgenname, dpl.GetGenerateName())
		}

		if dpl.Namespace != "default" {
			dpl.Status.Phase = "Deployed"
			now := metav1.Now()
			dpl.Status.LastUpdateTime = &now
			err = c.Status().Update(context.TODO(), &dpl)
			g.Expect(err).NotTo(gomega.HaveOccurred())
		}
	}

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(2 * time.Second)

	dpllist = &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{LabelSelector: clSelector})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	rootDpl := &appv1alpha1.Deployable{}

	for _, dpl := range dpllist.Items {
		if dpl.Namespace != "default" {
			if dpl.Status.Phase != "Deployed" {
				t.Errorf("The dpl is not deployed yet. Dpl namespace: %#v, status: %#v", dpl.GetNamespace(), dpl.Status)
			}
		} else {
			rootDpl = dpl.DeepCopy()

			if len(rootDpl.Status.PropagatedStatus) != 10 {
				t.Errorf("Incorrect propagated status array. rootDpl.Status.PropagatedStatus: %#v", rootDpl.Status.PropagatedStatus)
			}
		}
	}

	annotateRollingUpdate(t, rootDpl, rollingConfigmapDpl)
}

func annotateRollingUpdate(t *testing.T, rootDpl, rollingConfigmapDpl *appv1alpha1.Deployable) {
	var err error

	//annotate rolling update target to the root deployable and trigger reconcile, 3 deployables are expected to rolling update.
	rootDpl.Annotations["apps.open-cluster-management.io/rollingupdate-target"] = "version-configmap"
	err = c.Update(context.TODO(), rootDpl)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(2 * time.Second)

	dpllist := &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{LabelSelector: clSelector})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	updatedDpls := make(map[string]appv1alpha1.Deployable)
	noUpdatedDpls := make(map[string]appv1alpha1.Deployable)

	var expectecdDataRoot = make(map[string]interface{})
	expectecdDataRoot["purpose"] = "rolling update"

	var expectecdDataManaged = make(map[string]interface{})
	expectecdDataManaged["purpose"] = "rolling update"

	for _, dpl := range dpllist.Items {
		expgenname := rollingConfigmapDpl.GetName() + "-"

		if dpl.Namespace != "default" && dpl.GetGenerateName() != expgenname {
			t.Errorf("Incorrect generate name of generated deployable. \n\texpect:\t%s\n\tgot:\t%s", expgenname, dpl.GetGenerateName())
		}

		//verify template override on managed clusters by the given rollingupdate-target in the parent deployable.
		if dpl.Namespace == "default" {
			template := &unstructured.Unstructured{}
			json.Unmarshal(dpl.Spec.Template.Raw, template)

			if !reflect.DeepEqual(expectecdDataRoot, template.Object["data"]) {
				t.Errorf("Incorrect deployable rolling update. expected data: %#v, actual data: %#v", expectecdDataRoot, template.Object["data"])
			}
		} else {
			template := &unstructured.Unstructured{}
			json.Unmarshal(dpl.Spec.Template.Raw, template)

			if dpl.Status.Phase == "" {
				if !reflect.DeepEqual(expectecdDataManaged, template.Object["data"]) {
					t.Errorf("Incorrect deployable rolling update. expected data: %#v, actual data: %#v", expectecdDataManaged, template.Object["data"])
				}
				updatedDpls[dpl.Namespace] = dpl
			} else {
				noUpdatedDpls[dpl.Namespace] = dpl
			}
		}
	}

	if len(updatedDpls) != 3 {
		t.Errorf("3 deployables should be rolling updated this time. Actual updated dpl number: %v", len(updatedDpls))
	}

	anotherRollingUpdate(t, updatedDpls, noUpdatedDpls, rollingConfigmapDpl)
}

func anotherRollingUpdate(t *testing.T, updatedDpls, noUpdatedDpls map[string]appv1alpha1.Deployable, rollingConfigmapDpl *appv1alpha1.Deployable) {
	var err error

	// continue to set up 1 out of the 3 updted dpls to "Deploy" status.
	// 3 deployables are still expected to rolling update, but there is a new deploayble coming

	newUpdatedDpl := appv1alpha1.Deployable{}
	for _, dpl := range updatedDpls {
		newUpdatedDpl = dpl
		break
	}

	newUpdatedDpl.Status.Phase = "Deployed"
	now := metav1.Now()
	newUpdatedDpl.Status.LastUpdateTime = &now
	err = c.Status().Update(context.TODO(), &newUpdatedDpl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(2 * time.Second)

	dpllist := &appv1alpha1.DeployableList{}
	err = c.List(context.TODO(), dpllist, &client.ListOptions{LabelSelector: clSelector})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	newUpdatedDpls := make(map[string]appv1alpha1.Deployable)
	newNoUpdatedDpls := make(map[string]appv1alpha1.Deployable)

	for _, dpl := range dpllist.Items {
		expgenname := rollingConfigmapDpl.GetName() + "-"

		if dpl.Namespace != "default" && dpl.GetGenerateName() != expgenname {
			t.Errorf("Incorrect generate name of generated deployable. \n\texpect:\t%s\n\tgot:\t%s", expgenname, dpl.GetGenerateName())
		}

		//verify template override on managed clusters by the given rollingupdate-target in the parent deployable.
		if dpl.Namespace != "default" {
			template := &unstructured.Unstructured{}
			json.Unmarshal(dpl.Spec.Template.Raw, template)

			var expectecdData = make(map[string]interface{})
			expectecdData["purpose"] = "rolling update"

			if dpl.Status.Phase == "" {
				newUpdatedDpls[dpl.Namespace] = dpl
			} else {
				newNoUpdatedDpls[dpl.Namespace] = dpl
			}
		}
	}

	newNum := 0
	existingNum := 0

	for dplkey := range newUpdatedDpls {
		if _, ok := updatedDpls[dplkey]; ok {
			existingNum++
		} else {
			newNum++
		}
	}

	newNoNum := 0
	existingNoNum := 0

	for dplkey := range newNoUpdatedDpls {
		if _, ok := noUpdatedDpls[dplkey]; ok {
			existingNoNum++
		} else {
			newNoNum++
		}
	}

	if newNum != 1 || newNoNum != 1 {
		t.Errorf("Incorrect rolling update. old dpls: %v, new dpls: %v", getMapkey(updatedDpls), getMapkey(newUpdatedDpls))
	}
}

func getMapkey(dplMap map[string]appv1alpha1.Deployable) string {
	keys := []string{}

	for k := range dplMap {
		keys = append(keys, k)
	}

	return strings.Join(keys, ",")
}
