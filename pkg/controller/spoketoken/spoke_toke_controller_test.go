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
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	clusterName = "cluster1"
)

var (
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
)

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	host := ""

	clusterID := types.NamespacedName{Name: clusterName, Namespace: clusterName}

	// Temporarily only wait 6 seconds. real controller will be every minute.
	Add(mgr, 6, cfg, &clusterID, false)

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

	// the spoken token update loop is triggerred every 6 sec in the test.
	time.Sleep(time.Second * 10)

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

	time.Sleep(time.Second * 9) // would need to change this if we change controller loop interval.

	theDeletedSecret := &corev1.Secret{}
	// Check that cluster1/cluster1-cluster-secret is deleted upon the deletion of the service account.
	g.Expect(kerrors.IsNotFound(c.Get(context.TODO(), secretkey, theDeletedSecret))).To(gomega.BeTrue())

	Add(mgr, 6, cfg, &clusterID, false)
}
