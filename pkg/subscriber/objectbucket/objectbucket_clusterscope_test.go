// Copyright 2026 The Kubernetes Authors.
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

package objectbucket

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	awsutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/aws"
)

// TestObjectBucketRespectsClusterAdminAnnotation verifies that an object
// store channel subscription honors the cluster-admin annotation: a
// non-admin subscription must not be able to deploy a cluster-scoped
// resource (e.g. ClusterRoleBinding), while an admin subscription may.
func TestObjectBucketRespectsClusterAdminAnnotation(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	testClient := mgr.GetClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	clusterNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cluster-scope-objstore"}}
	g.Expect(testClient.Create(context.TODO(), clusterNS)).NotTo(gomega.HaveOccurred())

	defer func() { _ = testClient.Delete(context.TODO(), clusterNS) }()

	testSub := &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "objstore-clusterscope-test",
			Namespace: "default",
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: sharedkey.String(),
		},
	}
	g.Expect(testClient.Create(context.TODO(), testSub)).NotTo(gomega.HaveOccurred())

	defer func() { _ = testClient.Delete(context.TODO(), testSub) }()

	defaultExtension := &kubesynchronizer.SubscriptionExtension{}
	syncid := &types.NamespacedName{
		Namespace: "cluster-scope-objstore",
		Name:      "cluster-scope-objstore",
	}
	sync, err := kubesynchronizer.CreateSynchronizer(mgr.GetConfig(), cfg, mgr.GetScheme(), syncid, 60, defaultExtension, true, false)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	crb := &unstructured.Unstructured{}
	crb.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "ClusterRoleBinding",
	})
	crb.SetName("attacker-crb-objstore")
	crb.SetAnnotations(map[string]string{
		appv1alpha1.AnnotationHosting: testSub.Namespace + "/" + testSub.Name,
	})
	g.Expect(unstructured.SetNestedMap(crb.Object, map[string]interface{}{
		"apiGroup": "rbac.authorization.k8s.io",
		"kind":     "ClusterRole",
		"name":     "cluster-admin",
	}, "roleRef")).NotTo(gomega.HaveOccurred())
	g.Expect(unstructured.SetNestedSlice(crb.Object, []interface{}{
		map[string]interface{}{
			"kind": "User",
			"name": "attacker",
		},
	}, "subjects")).NotTo(gomega.HaveOccurred())

	resourceList := []kubesynchronizer.ResourceUnit{{Resource: crb, Gvk: crb.GroupVersionKind()}}
	deployedCRBKey := types.NamespacedName{Name: "attacker-crb-objstore"}

	// Non-admin: the ClusterRoleBinding must be rejected and never created.
	err = sync.ProcessSubResources(testSub, resourceList, nil, nil, false, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	deployedCRB := &unstructured.Unstructured{}
	deployedCRB.SetGroupVersionKind(crb.GroupVersionKind())
	getErr := testClient.Get(context.TODO(), deployedCRBKey, deployedCRB)
	g.Expect(errors.IsNotFound(getErr)).To(gomega.BeTrue())

	// Admin: the same ClusterRoleBinding is now allowed to be deployed.
	err = sync.ProcessSubResources(testSub, resourceList, nil, nil, true, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	getErr = testClient.Get(context.TODO(), deployedCRBKey, deployedCRB)
	g.Expect(getErr).NotTo(gomega.HaveOccurred())

	g.Expect(testClient.Delete(context.TODO(), deployedCRB)).NotTo(gomega.HaveOccurred())
}

// TestObjectBucketDoSubscriptionRespectsClusterAdmin exercises the real
// doSubscription() code path in objectbucket_subscriber_item.go end to end
// (via a fake, fully reachable S3 server) to prove the fix that passes the
// SubscriberItem's live clusterAdmin field into ProcessSubResources instead
// of a hardcoded false.
func TestObjectBucketDoSubscriptionRespectsClusterAdmin(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	testClient := mgr.GetClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	clusterNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cluster-scope-dosub"}}
	g.Expect(testClient.Create(context.TODO(), clusterNS)).NotTo(gomega.HaveOccurred())

	defer func() { _ = testClient.Delete(context.TODO(), clusterNS) }()

	testSub := &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "objstore-dosub-test",
			Namespace: "default",
		},
	}
	g.Expect(testClient.Create(context.TODO(), testSub)).NotTo(gomega.HaveOccurred())

	defer func() { _ = testClient.Delete(context.TODO(), testSub) }()

	server, awsHandler, _ := utils.SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "clusterscope-dosub-bucket"
	g.Expect(awsHandler.Create(bucket)).NotTo(gomega.HaveOccurred())

	crbYAML := `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: attacker-crb-dosub
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: User
  name: attacker
`
	g.Expect(awsHandler.Put(bucket, awsutils.DeployableObject{
		Name:    "crb.yaml",
		Content: []byte(crbYAML),
	})).NotTo(gomega.HaveOccurred())

	channel := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{Name: "objstore-dosub-channel", Namespace: "default"},
		Spec: chnv1.ChannelSpec{
			Type:               chnv1.ChannelTypeObjectBucket,
			Pathname:           server.URL + "/" + bucket,
			InsecureSkipVerify: true,
		},
	}

	defaultExtension := &kubesynchronizer.SubscriptionExtension{}
	syncid := &types.NamespacedName{Namespace: "cluster-scope-dosub", Name: "cluster-scope-dosub"}
	sync, err := kubesynchronizer.CreateSynchronizer(mgr.GetConfig(), cfg, mgr.GetScheme(), syncid, 60, defaultExtension, true, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	obsi := &SubscriberItem{
		SubscriberItem: appv1alpha1.SubscriberItem{Subscription: testSub, Channel: channel},
		synchronizer:   sync,
		clusterAdmin:   false,
	}

	deployedCRBKey := types.NamespacedName{Name: "attacker-crb-dosub"}
	deployedCRB := &unstructured.Unstructured{}
	deployedCRB.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding",
	})

	// Non-admin: doSubscription must reject the cluster-scoped resource
	// bundled in the bucket (ProcessSubResources blocks it internally
	// without surfacing a top-level error, so doSubscription still reports
	// success/failure via obsi.successful, not via a panic/crash).
	obsi.doSubscription()

	getErr := testClient.Get(context.TODO(), deployedCRBKey, deployedCRB)
	g.Expect(errors.IsNotFound(getErr)).To(gomega.BeTrue())

	// Admin: doSubscription must now allow the same resource through.
	obsi.clusterAdmin = true
	obsi.doSubscription()

	g.Expect(testClient.Get(context.TODO(), deployedCRBKey, deployedCRB)).NotTo(gomega.HaveOccurred())

	g.Expect(testClient.Delete(context.TODO(), deployedCRB)).NotTo(gomega.HaveOccurred())
}

// TestObjectBucketSubscribeItemClusterAdminDeEscalation covers the privilege
// de-escalation fix in SubscribeItem (objectbucket_subscriber.go):
// clusterAdmin must track the live cluster-admin annotation on every call,
// including being reset to false when the annotation is later removed.
func TestObjectBucketSubscribeItemClusterAdminDeEscalation(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	defaultExtension := &kubesynchronizer.SubscriptionExtension{}
	syncid := &types.NamespacedName{Namespace: "objstore-deescalation", Name: "objstore-deescalation"}
	sync, err := kubesynchronizer.CreateSynchronizer(mgr.GetConfig(), cfg, mgr.GetScheme(), syncid, 60, defaultExtension, true, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subscriber := CreateObjectBucketSubsriber(cfg, mgr.GetScheme(), mgr, sync, 60)
	g.Expect(subscriber).NotTo(gomega.BeNil())

	channel := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{Name: "objstore-deescalation-chn", Namespace: "default"},
		Spec: chnv1.ChannelSpec{
			Type:     chnv1.ChannelTypeObjectBucket,
			Pathname: "http://127.0.0.1:1/nonexistent-bucket",
		},
	}

	itemKey := types.NamespacedName{Name: "objstore-deescalation-test", Namespace: "default"}

	adminSub := &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      itemKey.Name,
			Namespace: itemKey.Namespace,
			Annotations: map[string]string{
				appv1alpha1.AnnotationClusterAdmin: "true",
			},
		},
	}

	adminSubitem := &appv1alpha1.SubscriberItem{
		Subscription: adminSub,
		Channel:      channel,
	}

	g.Expect(subscriber.SubscribeItem(adminSubitem)).NotTo(gomega.HaveOccurred())
	g.Expect(subscriber.itemmap[itemKey].clusterAdmin).To(gomega.BeTrue())

	nonAdminSub := adminSub.DeepCopy()
	nonAdminSub.Annotations = map[string]string{}

	nonAdminSubitem := &appv1alpha1.SubscriberItem{
		Subscription: nonAdminSub,
		Channel:      channel,
	}

	g.Expect(subscriber.SubscribeItem(nonAdminSubitem)).NotTo(gomega.HaveOccurred())
	g.Expect(subscriber.itemmap[itemKey].clusterAdmin).To(gomega.BeFalse())

	// Avoid UnsubscribeItem here: Stop() blocks on a WaitGroup until the
	// background goroutine's in-flight doSubscription attempt against this
	// (deliberately unreachable) test channel finishes, which can take up
	// to the subscriber's retry interval (~90s). This test only cares about
	// the clusterAdmin bookkeeping done synchronously inside SubscribeItem,
	// so just drop the cached item and let the goroutine exit on its own.
	item := subscriber.itemmap[itemKey]
	delete(subscriber.itemmap, itemKey)

	if item != nil && item.stopch != nil {
		close(item.stopch)
	}
}
