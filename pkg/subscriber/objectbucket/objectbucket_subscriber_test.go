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
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1alpha1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
)

var c client.Client

var id = types.NamespacedName{
	Name:      "endpoint",
	Namespace: "default",
}

var (
	sharedkey = types.NamespacedName{
		Name:      "test",
		Namespace: "default",
	}

	channelSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "obj-secret",
			Namespace: sharedkey.Namespace,
		},
		Data: map[string][]byte{
			"AccessKeyID":     []byte("test-access-id"),
			"SecretAccessKey": []byte("test-secret-access-key"),
			"Region":          []byte("test-region"),
		},
	}

	channelConfigMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "obj-configmap",
			Namespace: sharedkey.Namespace,
		},
		Data: map[string]string{
			"name1": "value1",
			"name2": "value2",
		},
	}

	objConfigMap = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"anno1": "anno1",
			},
			Labels: map[string]string{
				"label1": "label",
			},
			Name:      "obj-configmap-2",
			Namespace: "obj-ns-2",
		},
		Data: map[string]string{
			"name3": "value3",
		},
	}

	objChannel = &chnv1alpha1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: chnv1alpha1.ChannelSpec{
			Pathname: "http://minio-minio.apps.hivemind-b.aws.red-chesterfield.com/fake-bucket",
			SecretRef: &corev1.ObjectReference{
				Name: "obj-secret",
			},
			ConfigMapRef: &corev1.ObjectReference{
				Name: "obj-configmap",
			},
		},
	}

	objSub = &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"apps.open-cluster-management.io/reconcile-option": "merge",
				"apps.open-cluster-management.io/":                 "cluster-admin",
			},
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "appsub-obj-1",
			},
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: sharedkey.String(),
			Package: objConfigMap.Name,
			PackageFilter: &appv1alpha1.PackageFilter{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"label1": "label",
					},
				},
				Annotations: map[string]string{
					"anno1": "anno1",
				},
			},
		},
	}
)

func TestObjectSubscriber(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	g.Expect(Add(mgr, cfg, &id, 2, false, false)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	defaultExtension := &kubesynchronizer.SubscriptionExtension{}
	syncid := &types.NamespacedName{
		Namespace: "cluster1",
		Name:      "cluster1",
	}
	defaultSynchronizer, err := kubesynchronizer.CreateSynchronizer(mgr.GetConfig(), cfg, mgr.GetScheme(), syncid, 60, defaultExtension, true, false)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	obssubitem := &SubscriberItem{}
	obssubitem.synchronizer = defaultSynchronizer
	obssubitem.Channel = objChannel
	obssubitem.SecondaryChannel = objChannel
	obssubitem.Subscription = objSub
	obssubitem.ChannelSecret = channelSecret
	obssubitem.ChannelConfigMap = channelConfigMap

	// Test 1: test get object Channel Config
	endpoint, accessKeyID, secretAccessKey, region, err := obssubitem.getChannelConfig(true)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(endpoint).To(gomega.Equal("http://minio-minio.apps.hivemind-b.aws.red-chesterfield.com"))
	g.Expect(accessKeyID).To(gomega.Equal("test-access-id"))
	g.Expect(secretAccessKey).To(gomega.Equal("test-secret-access-key"))
	g.Expect(region).To(gomega.Equal("test-region"))

	obssubitem.doSubscription()

	// Test 2: test doSubscribeManifest
	objByte, err := json.Marshal(objConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	objTemplate := &unstructured.Unstructured{}
	err = yaml.Unmarshal(objByte, objTemplate)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// If cluster admin, the original object namespace is respected
	obssubitem.clusterAdmin = true
	resource, err := obssubitem.doSubscribeManifest(objTemplate)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	objNamespace := objConfigMap.GetNamespace()

	g.Expect(resource.Resource.GetNamespace()).To(gomega.Equal(objNamespace))

	// If not cluster admin, the original object namespace is replaced into the appsub namespace
	obssubitem.clusterAdmin = false
	resource, err = obssubitem.doSubscribeManifest(objTemplate)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	appsubNamespace := objSub.GetNamespace()

	g.Expect(resource.Resource.GetNamespace()).To(gomega.Equal(appsubNamespace))

	// check if the app lable is appened to the resource template
	appLabel := resource.Resource.GetLabels()["app.kubernetes.io/part-of"]
	g.Expect(appLabel).To(gomega.Equal("appsub-obj-1"))
}
