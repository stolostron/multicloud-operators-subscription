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

package utils

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	"gopkg.in/src-d/go-git.v4/plumbing"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

var (
	sharedkey = types.NamespacedName{
		Name:      "testkey",
		Namespace: "default",
	}

	githubchn = &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: chnv1.ChannelSpec{
			Type:     "Git",
			Pathname: "https://github.com/open-cluster-management/multicloud-operators-subscription.git",
		},
	}

	githubsub = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: appv1.SubscriptionSpec{
			Channel: sharedkey.String(),
		},
	}
)

const correctSecret = `apiVersion: v1
kind: Secret
metadata:
  name: correct-secret
  namespace: default
type: Opaque
data:
  user: YWRtaW4=
  accessToken: MWYyZDFlMmU2N2Rm`

const incorrectSecret = `apiVersion: v1
kind: Secret
metadata:
  name: incorrect-secret
  namespace: default
type: Opaque
data:
  user: YWRtaW4=
  password: MWYyZDFlMmU2N2Rm`

func Test_ParseKubeResoures(t *testing.T) {
	testYaml1 := `---
---
---
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap-1
  namespace: default
data:
  path: resource
---
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap-2
  namespace: default
data:
  path: resource
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap-3
  namespace: default
data:
  path: resource
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap-4
  namespace: default
data:
  path: resource
---`
	ret := ParseKubeResoures([]byte(testYaml1))

	if len(ret) != 4 {
		t.Errorf("faild to parse yaml objects, wanted %v, got %v", 4, len(ret))
	}

	testYaml2 := `---
---
---
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap-1
  namespace: default
data:
  path: resource
---
---`
	ret = ParseKubeResoures([]byte(testYaml2))

	if len(ret) != 1 {
		t.Errorf("faild to parse yaml objects, wanted %v, got %v", 1, len(ret))
	}

	testYaml3 := `---
---
---
---
apiVersiondfdfd: v1
kinddfdfdfdf: ConfigMap
metadata:
  name: test-configmap-1
  namespace: default
data:
  path: resource
---
---`
	ret = ParseKubeResoures([]byte(testYaml3))

	if len(ret) != 0 {
		t.Errorf("faild to parse yaml objects, wanted %v, got %v", 0, len(ret))
	}

	testYaml4 := `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap-1
  namespace: default
data:
  path: resource`
	ret = ParseKubeResoures([]byte(testYaml4))

	if len(ret) != 1 {
		t.Errorf("faild to parse yaml objects, wanted %v, got %v", 1, len(ret))
	}

	testYaml5 := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap-1
  namespace: default
data:
  path: resource`
	ret = ParseKubeResoures([]byte(testYaml5))

	if len(ret) != 1 {
		t.Errorf("faild to parse yaml objects, wanted %v, got %v", 1, len(ret))
	}
}

func TestParseMultiDocYAML(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// This tests that a multi document YAML can be parsed properly
	// and handle the --- delimiter correctly
	// The test file contains --- characters in a resource and delimeters --- with trailing spaces
	content, err := ioutil.ReadFile("../../test/github/multiresource/multiresource.yaml")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	items := ParseYAML(content)
	// There are 3 config maps
	g.Expect(len(items)).To(gomega.Equal(3))

	configMapWithCert := &corev1.ConfigMap{}
	err = yaml.Unmarshal([]byte(items[0]), &configMapWithCert)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	// The first config map contains a certificate
	g.Expect(configMapWithCert.Data["ca.crt"]).To(gomega.HavePrefix("-----BEGIN"))
	g.Expect(configMapWithCert.Data["ca.crt"]).To(gomega.HaveSuffix("CERTIFICATE-----"))
}

func TestGetSubscriptionBranch(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	branch := GetSubscriptionBranch(githubsub)
	g.Expect(branch).To(gomega.Equal(plumbing.Master))
	g.Expect(branch.Short()).To(gomega.Equal("master"))

	subanno := make(map[string]string)
	subanno[appv1alpha1.AnnotationGitBranch] = "notmaster"
	githubsub.SetAnnotations(subanno)

	branchRef := plumbing.NewBranchReferenceName("notmaster")

	branch = GetSubscriptionBranch(githubsub)
	g.Expect(branch).To(gomega.Equal(branchRef))
	g.Expect(branch.Short()).To(gomega.Equal("notmaster"))
}

func TestGetChannelSecret(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	stop := make(chan struct{})
	defer close(stop)

	g.Expect(mgr.GetCache().WaitForCacheSync(stop)).Should(gomega.BeTrue())

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Test with a fake authentication secret but correct data keys in the secret
	chnSecret := &corev1.Secret{}
	err = yaml.Unmarshal([]byte(correctSecret), &chnSecret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), chnSecret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	secretRef := &corev1.ObjectReference{}
	secretRef.Name = "correct-secret"

	githubchn.Spec.SecretRef = secretRef

	user, pwd, err := GetChannelSecret(c, githubchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(user).To(gomega.Equal("admin"))
	g.Expect(pwd).To(gomega.Equal("1f2d1e2e67df"))

	// Test when secret ref is wrong
	secretRef.Name = "correct-secret_nogood"
	githubchn.Spec.SecretRef = secretRef

	_, _, err = GetChannelSecret(c, githubchn)
	g.Expect(err).To(gomega.HaveOccurred())

	// Test when secret has incorrect data
	chnSecret2 := &corev1.Secret{}
	err = yaml.Unmarshal([]byte(incorrectSecret), &chnSecret2)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), chnSecret2)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	secretRef.Name = "incorrect-secret"
	githubchn.Spec.SecretRef = secretRef

	_, _, err = GetChannelSecret(c, githubchn)
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestKustomizeOverrideString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	stop := make(chan struct{})
	defer close(stop)

	g.Expect(mgr.GetCache().WaitForCacheSync(stop)).Should(gomega.BeTrue())

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	subscriptionYAML := `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: github-resource-subscription
  namespace: default
spec:
  channel: github-ns/github-ch
  placement:
  local: true
  packageOverrides:
    - packageName: kustomize/overlays/production/kustomization.yaml
      packageOverrides:
      - value: |
          namePrefix: production-testtest-
          commonLabels:
            org: acmeCorporation-test
          patchesStrategicMerge:
          - deployment.yaml
          - configMap.yaml`

	subscription := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ov := subscription.Spec.PackageOverrides[0]

	// Set the cloned Git repo root directory to this Git repository root.
	repoRoot := "../.."
	kustomizeDir := filepath.Join(repoRoot, "test/github/kustomize/overlays/production")

	kustomizeDirs := make(map[string]string)
	kustomizeDirs[kustomizeDir+"/"] = kustomizeDir + "/"

	// backup the original kustomization.yaml
	orig := kustomizeDir + "/kustomization.yml"
	backup := kustomizeDir + "/kustomization.yml.BAK"
	err = copy(orig, backup)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	pov := ov.PackageOverrides[0]
	err = OverrideKustomize(pov, kustomizeDir)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = copy(backup, orig)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = os.Remove(backup)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestKustomizeOverrideYAML(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	stop := make(chan struct{})
	defer close(stop)

	g.Expect(mgr.GetCache().WaitForCacheSync(stop)).Should(gomega.BeTrue())

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	subscriptionYAML := `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: github-resource-subscription
  namespace: default
spec:
  channel: github-ns/github-ch
  placement:
  local: true
  packageOverrides:
    - packageName: kustomize/overlays/production/kustomization.yaml
      packageOverrides:
      - value:
          namePrefix: production-testtest-
          commonLabels:
            org: acmeCorporation-test
          patchesStrategicMerge:
          - deployment.yaml
          - configMap.yaml`

	subscription := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ov := subscription.Spec.PackageOverrides[0]

	// Set the cloned Git repo root directory to this Git repository root.
	repoRoot := "../.."
	kustomizeDir := filepath.Join(repoRoot, "test/github/kustomize/overlays/production")

	kustomizeDirs := make(map[string]string)
	kustomizeDirs[kustomizeDir+"/"] = kustomizeDir + "/"

	// backup the original kustomization.yaml
	orig := kustomizeDir + "/kustomization.yml"
	backup := kustomizeDir + "/kustomization.yml.BAK"
	err = copy(orig, backup)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	pov := ov.PackageOverrides[0]
	err = OverrideKustomize(pov, kustomizeDir)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = copy(backup, orig)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = os.Remove(backup)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func copy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}

	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return out.Close()
}

func TestSortResources(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	chartDirs, kustomizeDirs, crdsAndNamespaceFiles, rbacFiles, otherFiles, err := SortResources("../..", "../../test/github")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(chartDirs)).To(gomega.Equal(4))
	g.Expect(len(kustomizeDirs)).To(gomega.Equal(7))
	g.Expect(len(crdsAndNamespaceFiles)).To(gomega.Equal(2))
	g.Expect(len(rbacFiles)).To(gomega.Equal(3))
	g.Expect(len(otherFiles)).To(gomega.Equal(5))
}

func TestNestedKustomize(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// If there are nested kustomizations, process only the parent kustomization.
	chartDirs, kustomizeDirs, crdsAndNamespaceFiles, rbacFiles, otherFiles, err := SortResources("../..", "../../test/github/nestedKustomize")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(chartDirs)).To(gomega.Equal(0))
	g.Expect(len(crdsAndNamespaceFiles)).To(gomega.Equal(0))
	g.Expect(len(rbacFiles)).To(gomega.Equal(0))
	g.Expect(len(otherFiles)).To(gomega.Equal(0))
	g.Expect(len(kustomizeDirs)).To(gomega.Equal(2))

	g.Expect(kustomizeDirs["../../test/github/nestedKustomize/wordpress/"]).To(gomega.Equal("../../test/github/nestedKustomize/wordpress/"))
	g.Expect(kustomizeDirs["../../test/github/nestedKustomize/wordpress2/"]).To(gomega.Equal("../../test/github/nestedKustomize/wordpress2/"))
}

func TestSimple(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	g.Expect("hello").To(gomega.Equal("hello"))
}

func TestIsClusterAdminLocal(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	stop := make(chan struct{})
	defer close(stop)

	g.Expect(mgr.GetCache().WaitForCacheSync(stop)).Should(gomega.BeTrue())

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// The mutation webhook does not exist.

	subscriptionYAML := `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
spec:
  channel: github-ns/github-ch
  placement:
    local: true`

	subscription := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeFalse())

	subscriptionYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    apps.open-cluster-management.io/hosting-subscription: demo-ns/demo-subscription
spec:
  channel: github-ns/github-ch
  placement:
    local: true`

	subscription = &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeFalse())

	subscriptionYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    apps.open-cluster-management.io/hosting-subscription: demo-ns/demo-subscription
    apps.open-cluster-management.io/cluster-admin: "false"
spec:
  channel: github-ns/github-ch
  placement:
    local: true`

	subscription = &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeFalse())

	subscriptionYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    apps.open-cluster-management.io/hosting-subscription: demo-ns/demo-subscription
    apps.open-cluster-management.io/cluster-admin: "true"
spec:
  channel: github-ns/github-ch
  placement:
    local: true`

	subscription = &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeTrue())

	subscriptionYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    apps.open-cluster-management.io/cluster-admin: "true"
spec:
  channel: github-ns/github-ch
  placement:
    local: true`

	subscription = &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeFalse())

	webhookYAML := `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: ocm-mutating-webhook
webhooks:
- admissionReviewVersions:
  - v1beta1
  name: webhook.admission.cloud.com
  clientConfig:
    caBundle: ZHVtbXkK
    service:
      name: ocm-webhook
      namespace: default
      port: 443
  sideEffects: None`

	theWebhook := &admissionv1.MutatingWebhookConfiguration{}
	err = yaml.Unmarshal([]byte(webhookYAML), &theWebhook)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), theWebhook)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), theWebhook)

	time.Sleep(1 * time.Second)

	subscriptionYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    apps.open-cluster-management.io/hosting-subscription: demo-ns/demo-subscription
    apps.open-cluster-management.io/cluster-admin: "true"
  spec:
    channel: github-ns/github-ch
    placement:
      local: true`

	subscription = &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeFalse())
}

func TestIsClusterAdminRemote(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	clusterRole := subAdminClusterRole()
	clusterRoleBinding := subAdminClusterRoleBinding()

	err = c.Create(context.TODO(), clusterRole)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), clusterRole)

	err = c.Create(context.TODO(), clusterRoleBinding)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), clusterRoleBinding)

	time.Sleep(1 * time.Second)

	webhookYAML := `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: ocm-mutating-webhook
webhooks:
- admissionReviewVersions:
  - v1beta1
  name: webhook.admission.cloud.com
  clientConfig:
    caBundle: ZHVtbXkK
    service:
      name: ocm-webhook
      namespace: default
      port: 443
  sideEffects: None`

	theWebhook := &admissionv1.MutatingWebhookConfiguration{}
	err = yaml.Unmarshal([]byte(webhookYAML), &theWebhook)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), theWebhook)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), theWebhook)

	// user group: subscription-admin,test-group
	// user identity: bob
	subscriptionYAML := `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    open-cluster-management.io/user-group: c3Vic2NyaXB0aW9uLWFkbWluLHRlc3QtZ3JvdXA=
    open-cluster-management.io/user-identity: Ym9i
    apps.open-cluster-management.io/cluster-admin: "true"
  spec:
    channel: github-ns/github-ch
    placement:
      placementRef:
        name: dev-clusters`

	subscription := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeTrue())

	// user group: subscription-admin,test-group
	// user identity: joe
	subscriptionYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    open-cluster-management.io/user-group: c3Vic2NyaXB0aW9uLWFkbWluLHRlc3QtZ3JvdXA=
    open-cluster-management.io/user-identity: am9l
    apps.open-cluster-management.io/cluster-admin: "true"
  spec:
    channel: github-ns/github-ch
    placement:
      placementRef:
        name: dev-clusters`

	subscription = &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeTrue())

	// user group: test-group
	// user identity: jane
	subscriptionYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    open-cluster-management.io/user-group: dGVzdC1ncm91cAo=
    open-cluster-management.io/user-identity: amFuZQ==
    apps.open-cluster-management.io/cluster-admin: "true"
  spec:
    channel: github-ns/github-ch
    placement:
      placementRef:
        name: dev-clusters`

	subscription = &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeFalse())
}

func subAdminClusterRole() *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: appv1.SubscriptionAdmin,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"*"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
		},
	}
}

func subAdminClusterRoleBinding() *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: appv1.SubscriptionAdmin,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind: "User",
				Name: "bob",
			},
			{
				Kind: "Group",
				Name: "subscription-admin",
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind: "ClusterRole",
			Name: appv1.SubscriptionAdmin,
		},
	}
}
