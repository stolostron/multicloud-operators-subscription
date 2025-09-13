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

package objectbucket

import (
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	awsutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/aws"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestProcessKustomizationWithAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

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

	// Create test channel
	channel := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-channel",
			Namespace: "test-namespace",
		},
		Spec: chnv1.ChannelSpec{
			Type:     chnv1.ChannelTypeObjectBucket,
			Pathname: server.URL + "/test-bucket",
		},
	}

	// Create synchronizer
	defaultExtension := &kubesynchronizer.SubscriptionExtension{}
	syncid := &types.NamespacedName{
		Namespace: "cluster1",
		Name:      "cluster1",
	}
	synchronizer, err := kubesynchronizer.CreateSynchronizer(mgr.GetConfig(), cfg, mgr.GetScheme(), syncid, 60, defaultExtension, true, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create SubscriberItem
	subscriberItem := &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: sub,
			Channel:      channel,
		},
		bucket:       bucket,
		objectStore:  awsHandler,
		synchronizer: synchronizer,
		clusterAdmin: false,
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
  namespace: original-namespace
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

	// Test processKustomization
	allKeys := []string{"kustomization.yaml", "configmap.yaml"}
	kustomizeDir := ""

	resources, err := subscriberItem.processKustomization(kustomizeDir, allKeys)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(1)) // Should have 1 ConfigMap resource

	// Verify the resource
	resource := resources[0]
	g.Expect(resource.Resource.GetKind()).To(gomega.Equal("ConfigMap"))
	g.Expect(resource.Resource.GetName()).To(gomega.Equal("test-config"))
	// For non-admin, namespace should be set to subscription namespace
	g.Expect(resource.Resource.GetNamespace()).To(gomega.Equal("test-namespace"))
}

func TestProcessKustomizationWithAdminAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

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

	// Create test channel
	channel := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-channel",
			Namespace: "test-namespace",
		},
		Spec: chnv1.ChannelSpec{
			Type:     chnv1.ChannelTypeObjectBucket,
			Pathname: server.URL + "/test-bucket",
		},
	}

	// Create synchronizer
	defaultExtension := &kubesynchronizer.SubscriptionExtension{}
	syncid := &types.NamespacedName{
		Namespace: "cluster1",
		Name:      "cluster1",
	}
	synchronizer, err := kubesynchronizer.CreateSynchronizer(mgr.GetConfig(), cfg, mgr.GetScheme(), syncid, 60, defaultExtension, true, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create SubscriberItem with admin privileges
	subscriberItem := &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: sub,
			Channel:      channel,
		},
		bucket:       bucket,
		objectStore:  awsHandler,
		synchronizer: synchronizer,
		clusterAdmin: true, // Admin privileges
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
	resources, err := subscriberItem.processKustomization(kustomizeDir, allKeys)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(1))

	// Verify the resource keeps original namespace for admin
	resource := resources[0]
	g.Expect(resource.Resource.GetKind()).To(gomega.Equal("ConfigMap"))
	g.Expect(resource.Resource.GetName()).To(gomega.Equal("test-config"))
	g.Expect(resource.Resource.GetNamespace()).To(gomega.Equal("original-namespace"))
}

func TestProcessKustomizationWithSubdirectoryAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

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

	// Create test channel
	channel := &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-channel",
			Namespace: "test-namespace",
		},
		Spec: chnv1.ChannelSpec{
			Type:     chnv1.ChannelTypeObjectBucket,
			Pathname: server.URL + "/test-bucket",
		},
	}

	// Create synchronizer
	defaultExtension := &kubesynchronizer.SubscriptionExtension{}
	syncid := &types.NamespacedName{
		Namespace: "cluster1",
		Name:      "cluster1",
	}
	synchronizer, err := kubesynchronizer.CreateSynchronizer(mgr.GetConfig(), cfg, mgr.GetScheme(), syncid, 60, defaultExtension, true, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create SubscriberItem
	subscriberItem := &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: sub,
			Channel:      channel,
		},
		bucket:       bucket,
		objectStore:  awsHandler,
		synchronizer: synchronizer,
		clusterAdmin: false,
	}

	// Test data in subdirectory
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
  namespace: original-namespace
data:
  key: value
`

	// Upload test files to subdirectory
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

	// Test processKustomization with subdirectory
	allKeys := []string{"app/kustomization.yaml", "app/configmap.yaml"}
	kustomizeDir := "app"

	resources, err := subscriberItem.processKustomization(kustomizeDir, allKeys)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(1))

	// Verify the resource
	resource := resources[0]
	g.Expect(resource.Resource.GetKind()).To(gomega.Equal("ConfigMap"))
	g.Expect(resource.Resource.GetName()).To(gomega.Equal("test-config"))
	g.Expect(resource.Resource.GetNamespace()).To(gomega.Equal("test-namespace"))
}

func TestProcessKustomizationWithPackageFilterAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Setup fake S3 server
	server, awsHandler, _ := utils.SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err = awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create test subscription with package filter
	sub := &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sub",
			Namespace: "test-namespace",
		},
		Spec: appv1.SubscriptionSpec{
			Package: "test-config", // Only allow this specific package
			PackageFilter: &appv1.PackageFilter{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"env": "test",
					},
				},
			},
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
		},
	}

	// Create synchronizer
	defaultExtension := &kubesynchronizer.SubscriptionExtension{}
	syncid := &types.NamespacedName{
		Namespace: "cluster1",
		Name:      "cluster1",
	}
	synchronizer, err := kubesynchronizer.CreateSynchronizer(mgr.GetConfig(), cfg, mgr.GetScheme(), syncid, 60, defaultExtension, true, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create SubscriberItem
	subscriberItem := &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: sub,
			Channel:      channel,
		},
		bucket:       bucket,
		objectStore:  awsHandler,
		synchronizer: synchronizer,
		clusterAdmin: false,
	}

	// Test data - configmap with matching labels
	configMapYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: original-namespace
  labels:
    env: test
data:
  key: value
`

	// Test data - secret that should be filtered out
	secretYaml := `
apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: original-namespace
  labels:
    env: prod
data:
  key: dmFsdWU=
`

	kustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
- secret.yaml
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

	secretObj := awsutils.DeployableObject{
		Name:    "secret.yaml",
		Content: []byte(secretYaml),
	}
	err = awsHandler.Put(bucket, secretObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	allKeys := []string{"kustomization.yaml", "configmap.yaml", "secret.yaml"}
	kustomizeDir := ""

	// Test processKustomization with package filter
	resources, err := subscriberItem.processKustomization(kustomizeDir, allKeys)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	// Should only have 1 resource (ConfigMap) because Secret doesn't match package name and labels
	g.Expect(len(resources)).To(gomega.Equal(1))

	// Verify the resource
	resource := resources[0]
	g.Expect(resource.Resource.GetKind()).To(gomega.Equal("ConfigMap"))
	g.Expect(resource.Resource.GetName()).To(gomega.Equal("test-config"))
}
