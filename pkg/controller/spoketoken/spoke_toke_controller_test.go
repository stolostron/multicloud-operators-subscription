// Copyright 2020 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package spoketoken

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	clusterName = "cluster1"
)

var (
	sakey = types.NamespacedName{
		Name:      "application-manager",
		Namespace: "open-cluster-management-agent-addon",
	}

	secretkey = types.NamespacedName{
		Name:      clusterName + secretSuffix,
		Namespace: clusterName,
	}

	clusterNamespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}

	agentNamespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-cluster-management-agent-addon",
		},
	}

	sa1 = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "application-manager",
			Namespace: "open-cluster-management-agent-addon",
		},
		Secrets: []corev1.ObjectReference{
			{
				Name: "application-manager-dockercfg-1",
			},
		},
	}

	saToBeIgnored = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sa-to-be-ignored",
			Namespace: "open-cluster-management-agent-addon",
		},
		Secrets: []corev1.ObjectReference{
			{
				Name: "application-manager-dockercfg-1",
			},
		},
	}

	dockerSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "application-manager-dockercfg-1",
			Namespace: "open-cluster-management-agent-addon",
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": "application-manager",
				"openshift.io/token-secret.name":     "application-manager-token-1",
				"openshift.io/token-secret.value":    "dummy-1",
			},
		},
	}

	secret1 = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "application-manager-token-1",
			Namespace:   "open-cluster-management-agent-addon",
			Annotations: map[string]string{"kubernetes.io/service-account.name": "application-manager"},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}

	sa2 = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "application-manager",
			Namespace: "open-cluster-management-agent-addon",
		},
	}
)

var expectedRequest = reconcile.Request{NamespacedName: sakey}

const timeout = time.Second * 5

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	host := ""

	clusterID := types.NamespacedName{Name: clusterName, Namespace: clusterName}

	rec := newReconciler(mgr, c, &clusterID, host).(*ReconcileAgentToken)

	recFn, requests := SetupTestReconcile(rec)

	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Create the cluster namespace. This is the namespace where the secret gets created.
	g.Expect(c.Create(context.TODO(), clusterNamespace)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), clusterNamespace)

	// Create the addon agent namespace. This is where the source service account and its secrets are located.
	g.Expect(c.Create(context.TODO(), agentNamespace)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), agentNamespace)

	// Create the addon agent service account.
	g.Expect(c.Create(context.TODO(), sa1)).NotTo(gomega.HaveOccurred())

	g.Expect(c.Create(context.TODO(), dockerSecret)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), dockerSecret)

	// Create the addon agent service account token secret and reconcile.
	g.Expect(c.Create(context.TODO(), secret1)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), secret1)

	time.Sleep(time.Second * 2)

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	theSecret := &corev1.Secret{}

	// Check that cluster1/cluster1-cluster-secret is created.
	g.Expect(c.Get(context.TODO(), secretkey, theSecret)).NotTo(gomega.HaveOccurred())

	// Verify the secret data
	g.Expect(string(theSecret.Data["name"])).To(gomega.Equal(clusterName))
	g.Expect(string(theSecret.Data["server"])).To(gomega.Equal(host))
	g.Expect(string(theSecret.Data["config"])).NotTo(gomega.BeEmpty())

	configData := &Config{}
	g.Expect(json.Unmarshal(theSecret.Data["config"], configData)).NotTo(gomega.HaveOccurred())
	g.Expect(configData.BearerToken).To(gomega.Equal(dockerSecret.Annotations["openshift.io/token-secret.value"]))
	g.Expect(configData.TLSClientConfig["insecure"]).To(gomega.BeTrue())

	// Verify the labels
	secretLabels := theSecret.GetLabels()
	g.Expect(secretLabels["argocd.argoproj.io/secret-type"]).To(gomega.Equal("cluster"))
	g.Expect(secretLabels["apps.open-cluster-management.io/secret-type"]).To(gomega.Equal("acm-cluster"))
	g.Expect(secretLabels["apps.open-cluster-management.io/cluster-name"]).To(gomega.Equal("cluster1"))

	// Delete the addon agent service account and reconcile
	g.Expect(c.Delete(context.TODO(), sa1)).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	// Check that cluster1/cluster1-cluster-secret is deleted upon the deletion of the service account.
	time.Sleep(time.Second * 2)

	theDeletedSecret := &corev1.Secret{}
	g.Expect(kerrors.IsNotFound(c.Get(context.TODO(), secretkey, theDeletedSecret))).To(gomega.BeTrue())

	// Verify that service accounts other than open-cluster-management-agent-addon/klusterlet-addon-appmgr-
	// trigger a reconcile.
	unexpectedSakey := types.NamespacedName{
		Name:      saToBeIgnored.Name,
		Namespace: saToBeIgnored.Namespace,
	}

	unexpectedRequest := reconcile.Request{NamespacedName: unexpectedSakey}

	// Create the service account to be ignored.
	g.Expect(c.Create(context.TODO(), saToBeIgnored)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), saToBeIgnored)

	g.Eventually(requests, timeout).ShouldNot(gomega.Receive(gomega.Equal(unexpectedRequest)))

	// Test case 3: create a SA without secret list, expect the spoke token controller to create the SA secret successfully
	c.Delete(context.TODO(), sa1)
	c.Delete(context.TODO(), secret1)

	g.Expect(c.Create(context.TODO(), sa2)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), sa2)

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	// Check the secret open-cluster-management-agent-addon/application-manager is created.
	time.Sleep(time.Second * 2)

	saSecret := &corev1.Secret{}
	g.Expect(c.Get(context.TODO(), sakey, saSecret)).NotTo(gomega.HaveOccurred())

	klog.Infof("new SA secet: %#v", saSecret)

	g.Expect(saSecret.Annotations["kubernetes.io/service-account.name"]).To(gomega.Equal(sakey.Name))
	g.Expect(string(saSecret.Type)).To(gomega.Equal("kubernetes.io/service-account-token"))

	// append the token to the SA secret, expect the cluster secret will be generated using the SA secret token
	saSecret.Data = map[string][]byte{}
	saSecret.Data["token"] = []byte("new-token-2")
	g.Expect(c.Update(context.TODO(), saSecret)).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	// Check that cluster secret cluster1/cluster1-cluster-secret is created.
	time.Sleep(time.Second * 2)

	clusterSecret := &corev1.Secret{}
	g.Expect(c.Get(context.TODO(), secretkey, clusterSecret)).NotTo(gomega.HaveOccurred())

	// Verify the secret data
	g.Expect(string(clusterSecret.Data["name"])).To(gomega.Equal(clusterName))
	g.Expect(string(clusterSecret.Data["server"])).To(gomega.Equal(host))
	g.Expect(string(clusterSecret.Data["config"])).NotTo(gomega.BeEmpty())

	configData = &Config{}
	g.Expect(json.Unmarshal(clusterSecret.Data["config"], configData)).NotTo(gomega.HaveOccurred())
	g.Expect(configData.BearerToken).To(gomega.Equal(string(saSecret.Data["token"])))
	g.Expect(configData.TLSClientConfig["insecure"]).To(gomega.BeTrue())

	// delete the SA secret, expect the SA secret is regenerated
	g.Expect(c.Delete(context.TODO(), saSecret)).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	time.Sleep(time.Second * 10)

	newSaSecret := &corev1.Secret{}
	g.Expect(c.Get(context.TODO(), sakey, newSaSecret)).NotTo(gomega.HaveOccurred())

	klog.Infof("new SA secet: %#v", newSaSecret)
}
