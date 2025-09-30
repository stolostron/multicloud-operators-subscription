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
	"testing"

	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	awsutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/aws"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestProcessKustomizationForHubWithAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	rec := newReconciler(mgr).(*ReconcileSubscription)

	// Setup fake S3 server
	server, awsHandler, _ := utils.SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err = awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create test subscription
	sub := &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sub",
			Namespace: "test-namespace",
		},
		Spec: appv1.SubscriptionSpec{
			PackageOverrides: []*appv1.Overrides{
				{
					PackageName: "kustomization",
					PackageOverrides: []appv1.PackageOverride{
						{},
					},
				},
			},
		},
	}

	// Test data - kustomization.yaml
	kustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
`

	// Test data - configmap.yaml
	configMapYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: test-ns
data:
  key: value
`

	// Upload test files to fake S3
	kustomizeObj := awsutils.DeployableObject{
		Name:    "kustomization.yaml",
		Content: []byte(kustomizationYaml),
	}
	err = awsHandler.Put(bucket, kustomizeObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	configMapObj := awsutils.DeployableObject{
		Name:    "configmap.yaml",
		Content: []byte(configMapYaml),
	}
	err = awsHandler.Put(bucket, configMapObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test processKustomizationForHub
	allKeys := []string{"kustomization.yaml", "configmap.yaml"}
	kustomizeDir := ""

	resources, err := rec.processKustomizationForHub(kustomizeDir, allKeys, awsHandler, bucket, sub, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(1)) // Should have 1 ConfigMap resource

	// Verify the resource
	resource := resources[0]
	g.Expect(resource.Kind).To(gomega.Equal("ConfigMap"))
	g.Expect(resource.Name).To(gomega.Equal("test-config"))
	g.Expect(resource.Namespace).To(gomega.Equal("test-namespace")) // Should be set to subscription namespace for non-admin
}

func TestProcessKustomizationForHubWithAdminAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	rec := newReconciler(mgr).(*ReconcileSubscription)

	// Setup fake S3 server
	server, awsHandler, _ := utils.SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err = awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create test subscription
	sub := &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sub",
			Namespace: "test-namespace",
		},
	}

	// Test data - configmap with original namespace
	configMapYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: original-namespace
data:
  key: value
`

	kustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
`

	// Upload test files
	kustomizeObj := awsutils.DeployableObject{
		Name:    "kustomization.yaml",
		Content: []byte(kustomizationYaml),
	}
	err = awsHandler.Put(bucket, kustomizeObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	configMapObj := awsutils.DeployableObject{
		Name:    "configmap.yaml",
		Content: []byte(configMapYaml),
	}
	err = awsHandler.Put(bucket, configMapObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	allKeys := []string{"kustomization.yaml", "configmap.yaml"}
	kustomizeDir := ""

	// Test with admin privileges
	resources, err := rec.processKustomizationForHub(kustomizeDir, allKeys, awsHandler, bucket, sub, true)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(1))

	// Verify the resource keeps original namespace for admin
	resource := resources[0]
	g.Expect(resource.Kind).To(gomega.Equal("ConfigMap"))
	g.Expect(resource.Name).To(gomega.Equal("test-config"))
	g.Expect(resource.Namespace).To(gomega.Equal("original-namespace"))
}

func TestGetObjectBucketResourcesWithAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c := mgr.GetClient()
	rec := newReconciler(mgr).(*ReconcileSubscription)

	// Setup fake S3 server
	server, awsHandler, _ := utils.SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	// Create test secret
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			"AccessKeyID":     []byte("test-access-key"),
			"SecretAccessKey": []byte("test-secret-key"),
			"Region":          []byte("us-east-1"),
		},
	}

	// Create test channel
	channel := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-channel",
			Namespace: "test-namespace",
		},
		Spec: chnv1.ChannelSpec{
			Type:     chnv1.ChannelTypeObjectBucket,
			Pathname: server.URL + "/test-bucket",
			SecretRef: &v1.ObjectReference{
				Name: "test-secret",
			},
			InsecureSkipVerify: true,
		},
	}

	// Create test subscription
	sub := &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sub",
			Namespace: "test-namespace",
		},
		Spec: appv1.SubscriptionSpec{
			Channel: "test-namespace/test-channel",
		},
	}

	// Start manager context
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	go func() {
		err := mgr.Start(ctx)
		if err != nil {
			t.Logf("Manager start error: %v", err)
		}
	}()

	// Create test namespace first (ignore if already exists)
	testNamespace := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}
	err = c.Create(context.TODO(), testNamespace)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	// Create resources in cluster
	err = c.Create(context.TODO(), secret)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	bucket := "test-bucket"
	err = awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test data - regular YAML file
	configMapYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: test-ns
data:
  key: value
`

	// Upload regular YAML file
	configMapObj := awsutils.DeployableObject{
		Name:    "configmap.yaml",
		Content: []byte(configMapYaml),
	}
	err = awsHandler.Put(bucket, configMapObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test getObjectBucketResources with regular files
	resources, err := rec.getObjectBucketResources(sub, channel, nil, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(1))

	resource := resources[0]
	g.Expect(resource.Kind).To(gomega.Equal("ConfigMap"))
	g.Expect(resource.Name).To(gomega.Equal("test-config"))
	g.Expect(resource.Namespace).To(gomega.Equal("test-namespace")) // Should be subscription namespace for non-admin
}

func TestGetObjectBucketResourcesWithKustomizationAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c := mgr.GetClient()
	rec := newReconciler(mgr).(*ReconcileSubscription)

	// Setup fake S3 server
	server, awsHandler, _ := utils.SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	// Create test secret
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			"AccessKeyID":     []byte("test-access-key"),
			"SecretAccessKey": []byte("test-secret-key"),
			"Region":          []byte("us-east-1"),
		},
	}

	// Create test channel
	channel := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-channel",
			Namespace: "test-namespace",
		},
		Spec: chnv1.ChannelSpec{
			Type:     chnv1.ChannelTypeObjectBucket,
			Pathname: server.URL + "/test-bucket",
			SecretRef: &v1.ObjectReference{
				Name: "test-secret",
			},
			InsecureSkipVerify: true,
		},
	}

	// Create test subscription
	sub := &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sub",
			Namespace: "test-namespace",
		},
		Spec: appv1.SubscriptionSpec{
			Channel: "test-namespace/test-channel",
		},
	}

	// Start manager context
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	go func() {
		err := mgr.Start(ctx)
		if err != nil {
			t.Logf("Manager start error: %v", err)
		}
	}()

	// Create test namespace first (ignore if already exists)
	testNamespace := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}
	err = c.Create(context.TODO(), testNamespace)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	// Create resources in cluster
	err = c.Create(context.TODO(), secret)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	bucket := "test-bucket"
	err = awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test data - kustomization files
	kustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
`

	configMapYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: test-ns
data:
  key: value
`

	// Upload kustomization files
	kustomizeObj := awsutils.DeployableObject{
		Name:    "app/kustomization.yaml",
		Content: []byte(kustomizationYaml),
	}
	err = awsHandler.Put(bucket, kustomizeObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	configMapObj := awsutils.DeployableObject{
		Name:    "app/configmap.yaml",
		Content: []byte(configMapYaml),
	}
	err = awsHandler.Put(bucket, configMapObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Also add a regular file outside kustomization directory
	regularYaml := `
apiVersion: v1
kind: Secret
metadata:
  name: regular-secret
  namespace: test-ns
data:
  key: dmFsdWU=
`

	regularObj := awsutils.DeployableObject{
		Name:    "regular.yaml",
		Content: []byte(regularYaml),
	}
	err = awsHandler.Put(bucket, regularObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test getObjectBucketResources with kustomization and regular files
	resources, err := rec.getObjectBucketResources(sub, channel, nil, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(2)) // 1 from kustomization + 1 regular file

	// Verify we have both ConfigMap and Secret
	kinds := make(map[string]bool)
	for _, resource := range resources {
		kinds[resource.Kind] = true

		g.Expect(resource.Namespace).To(gomega.Equal("test-namespace")) // Should be subscription namespace for non-admin
	}

	g.Expect(kinds["ConfigMap"]).To(gomega.BeTrue())
	g.Expect(kinds["Secret"]).To(gomega.BeTrue())
}

func TestGetObjectBucketResourcesErrorAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	rec := newReconciler(mgr).(*ReconcileSubscription)

	// Create test channel with invalid pathname
	channel := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-channel",
			Namespace: "test-namespace",
		},
		Spec: chnv1.ChannelSpec{
			Type:     chnv1.ChannelTypeObjectBucket,
			Pathname: "invalid-url/bucket",
		},
	}

	// Create test subscription
	sub := &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sub",
			Namespace: "test-namespace",
		},
		Spec: appv1.SubscriptionSpec{
			Channel: "test-namespace/test-channel",
		},
	}

	// Test getObjectBucketResources with invalid channel - should return error
	_, err = rec.getObjectBucketResources(sub, channel, nil, false)
	g.Expect(err).To(gomega.HaveOccurred())
}
