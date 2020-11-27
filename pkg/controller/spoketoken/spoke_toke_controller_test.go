// Copyright 2020 The Kubernetes Authors.
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
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	clusterName = "cluster1"
)

var (
	sakey = types.NamespacedName{
		Name:      "klusterlet-addon-appmgr",
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
			Name:      "klusterlet-addon-appmgr",
			Namespace: "open-cluster-management-agent-addon",
		},
		Secrets: []corev1.ObjectReference{
			{
				Name: "klusterlet-addon-appmgr-token-1",
			},
			{
				Name: "klusterlet-addon-appmgr-dockercfg-tlxd5",
			},
		},
	}

	sa2 = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "klusterlet-addon-appmgr",
			Namespace: "open-cluster-management-agent-addon",
		},
		Secrets: []corev1.ObjectReference{
			{
				Name: "klusterlet-addon-appmgr-token-2",
			},
			{
				Name: "klusterlet-addon-appmgr-dockercfg-tlxd5",
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
				Name: "klusterlet-addon-appmgr-token-2",
			},
			{
				Name: "klusterlet-addon-appmgr-dockercfg-tlxd5",
			},
		},
	}

	secret1 = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "klusterlet-addon-appmgr-token-1",
			Namespace: "open-cluster-management-agent-addon",
		},
		Data: map[string][]byte{
			"token": []byte("ZHVtbXkxCg=="),
		},
	}

	secret2 = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "klusterlet-addon-appmgr-token-2",
			Namespace: "open-cluster-management-agent-addon",
		},
		Data: map[string][]byte{
			"token": []byte("dGVzdDIK"),
		},
	}
)

var expectedRequest = reconcile.Request{NamespacedName: sakey}

const timeout = time.Second * 5

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	host := "https://localhost:6443"

	clusterID := types.NamespacedName{Name: clusterName, Namespace: clusterName}

	rec := newReconciler(mgr, c, &clusterID, host).(*ReconcileAgentToken)

	recFn, requests := SetupTestReconcile(rec)

	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the cluster namespace. This is the namespace where the secret gets created.
	g.Expect(c.Create(context.TODO(), clusterNamespace)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), clusterNamespace)

	// Create the cluster namespace. This is where the source service account and its secrets are located.
	g.Expect(c.Create(context.TODO(), agentNamespace)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), agentNamespace)

	// Create the source service account.
	g.Expect(c.Create(context.TODO(), sa1)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), sa1)

	// Create the source service account token secret and reconcile.
	g.Expect(c.Create(context.TODO(), secret1)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), secret1)

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
	g.Expect(configData.BearerToken).To(gomega.Equal(string(secret1.Data["token"])))
	g.Expect(configData.TLSClientConfig["insecure"]).To(gomega.BeTrue())

	// Verify the labels
	secretLabels := theSecret.GetLabels()
	g.Expect(secretLabels["argocd.argoproj.io/secret-type"]).To(gomega.Equal("cluster"))
	g.Expect(secretLabels["apps.open-cluster-management.io/secret-type"]).To(gomega.Equal("acm-cluster"))

	// Update the source service account token secret and reconcile.
	g.Expect(c.Create(context.TODO(), secret2)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), secret2)
	g.Expect(c.Update(context.TODO(), sa2)).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	theUpdatedSecret := &corev1.Secret{}

	// Check that cluster1/cluster1-cluster-secret is created.
	g.Expect(c.Get(context.TODO(), secretkey, theUpdatedSecret)).NotTo(gomega.HaveOccurred())

	// Verify the updated secret data
	configData2 := &Config{}

	g.Expect(string(theUpdatedSecret.Data["config"])).NotTo(gomega.BeEmpty())
	g.Expect(json.Unmarshal(theUpdatedSecret.Data["config"], configData2)).NotTo(gomega.HaveOccurred())
	g.Expect(configData2.BearerToken).To(gomega.Equal(string(secret2.Data["token"])))

	// Delete the source service account and reconcile
	g.Expect(c.Delete(context.TODO(), sa2)).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	theDeletedSecret := &corev1.Secret{}

	// Check that cluster1/cluster1-cluster-secret is deleted upon the deletion of the service account.
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
}
