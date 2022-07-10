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
	"crypto/tls"
	"encoding/pem"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
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

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
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
			Pathname: "https://" + GetTestGitRepoURLFromEnvVar() + ".git",
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

func TestGetCertChain(t *testing.T) {
	validCert := `
-----BEGIN CERTIFICATE-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAlRuRnThUjU8/prwYxbty
WPT9pURI3lbsKMiB6Fn/VHOKE13p4D8xgOCADpdRagdT6n4etr9atzDKUSvpMtR3
CP5noNc97WiNCggBjVWhs7szEe8ugyqF23XwpHQ6uV1LKH50m92MbOWfCtjU9p/x
qhNpQQ1AZhqNy5Gevap5k8XzRmjSldNAFZMY7Yv3Gi+nyCwGwpVtBUwhuLzgNFK/
yDtw2WcWmUU7NuC8Q6MWvPebxVtCfVp/iQU6q60yyt6aGOBkhAX0LpKAEhKidixY
nP9PNVBvxgu3XZ4P36gZV6+ummKdBVnc3NqwBLu5+CcdRdusmHPHd5pHf4/38Z3/
6qU2a/fPvWzceVTEgZ47QjFMTCTmCwNt29cvi7zZeQzjtwQgn4ipN9NibRH/Ax/q
TbIzHfrJ1xa2RteWSdFjwtxi9C20HUkjXSeI4YlzQMH0fPX6KCE7aVePTOnB69I/
a9/q96DiXZajwlpq3wFctrs1oXqBp5DVrCIj8hU2wNgB7LtQ1mCtsYz//heai0K9
PhE4X6hiE0YmeAZjR0uHl8M/5aW9xCoJ72+12kKpWAa0SFRWLy6FejNYCYpkupVJ
yecLk/4L1W0l6jQQZnWErXZYe0PNFcmwGXy1Rep83kfBRNKRy5tvocalLlwXLdUk
AIU+2GKjyT3iMuzZxxFxPFMCAwEAAQ==
-----END CERTIFICATE-----
and some more`

	byteArr, _ := pem.Decode([]byte(validCert))

	testCases := []struct {
		desc   string
		certs  string
		wanted tls.Certificate
	}{
		{
			desc:   "invalid cert",
			certs:  "",
			wanted: tls.Certificate{},
		},
		{
			desc: "empty cert",
			certs: `
-----BEGIN CERTIFICATE-----
-----END CERTIFICATE-----
			`,
			wanted: tls.Certificate{Certificate: [][]byte{{}}},
		},
		{
			desc:   "valid cert",
			certs:  validCert,
			wanted: tls.Certificate{Certificate: [][]byte{byteArr.Bytes}},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := getCertChain(tC.certs)
			if !reflect.DeepEqual(got, tC.wanted) {
				t.Errorf("wanted %v, got %v", tC.wanted, got)
			}
		})
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
	g.Expect(branch).To(gomega.Equal(plumbing.ReferenceName("")))
	g.Expect(branch.Short()).To(gomega.Equal(""))

	subanno := make(map[string]string)
	subanno[appv1.AnnotationGitBranch] = "notmaster"
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

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	g.Expect(mgr.GetCache().WaitForCacheSync(ctx)).Should(gomega.BeTrue())

	defer func() {
		cancel()
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

	user, pwd, sshKey, passphrase, clientkey, clientcert, err := GetChannelSecret(c, githubchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(user).To(gomega.Equal("admin"))
	g.Expect(pwd).To(gomega.Equal("1f2d1e2e67df"))
	g.Expect(string(sshKey)).To(gomega.Equal(""))
	g.Expect(string(passphrase)).To(gomega.Equal(""))
	g.Expect(string(clientkey)).To(gomega.Equal(""))
	g.Expect(string(clientcert)).To(gomega.Equal(""))

	// Test when secret ref is wrong
	secretRef.Name = "correct-secret_nogood"
	githubchn.Spec.SecretRef = secretRef

	user, pwd, sshKey, passphrase, clientkey, clientcert, err = GetChannelSecret(c, githubchn)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(user).To(gomega.Equal(""))
	g.Expect(pwd).To(gomega.Equal(""))
	g.Expect(string(sshKey)).To(gomega.Equal(""))
	g.Expect(string(passphrase)).To(gomega.Equal(""))
	g.Expect(string(clientkey)).To(gomega.Equal(""))
	g.Expect(string(clientcert)).To(gomega.Equal(""))

	// Test when secret has incorrect data
	chnSecret2 := &corev1.Secret{}
	err = yaml.Unmarshal([]byte(incorrectSecret), &chnSecret2)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), chnSecret2)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	secretRef.Name = "incorrect-secret"
	githubchn.Spec.SecretRef = secretRef

	user, pwd, sshKey, passphrase, clientkey, clientcert, err = GetChannelSecret(c, githubchn)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(user).To(gomega.Or(gomega.Equal(""), gomega.Equal("admin")))
	g.Expect(pwd).To(gomega.Equal(""))
	g.Expect(string(sshKey)).To(gomega.Equal(""))
	g.Expect(string(passphrase)).To(gomega.Equal(""))
	g.Expect(string(clientkey)).To(gomega.Equal(""))
	g.Expect(string(clientcert)).To(gomega.Equal(""))
}

func TestKustomizeOverrideString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	g.Expect(mgr.GetCache().WaitForCacheSync(ctx)).Should(gomega.BeTrue())

	defer func() {
		cancel()
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

	subscription := &appv1.Subscription{}
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

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	g.Expect(mgr.GetCache().WaitForCacheSync(ctx)).Should(gomega.BeTrue())

	defer func() {
		cancel()
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

	subscription := &appv1.Subscription{}
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

func TestIncorrectKustomizeOverride(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

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
    - packageName: kustomize/overlays/production/kustomization.yaml`

	subscription := &appv1.Subscription{}
	err := yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ov := subscription.Spec.PackageOverrides[0]

	err = CheckPackageOverride(ov)
	g.Expect(err).To(gomega.HaveOccurred())
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

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	c = mgr.GetClient()
	g.Expect(c).ToNot(gomega.BeNil())

	g.Expect(mgr.GetCache().WaitForCacheSync(ctx)).Should(gomega.BeTrue())

	defer func() {
		cancel()
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

	subscription := &appv1.Subscription{}
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

	subscription = &appv1.Subscription{}
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

	subscription = &appv1.Subscription{}
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

	subscription = &appv1.Subscription{}
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

	subscription = &appv1.Subscription{}
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

	subscription = &appv1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeFalse())
}

func TestIsClusterAdminRemote(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
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

	subscription := &appv1.Subscription{}
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

	subscription = &appv1.Subscription{}
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

	subscription = &appv1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeFalse())

	// user group: system:serviceaccounts:default
	// user identity: system:serviceaccounts:default:adminsa
	subscriptionYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: default
  annotations:
    open-cluster-management.io/user-group: c3lzdGVtOnNlcnZpY2VhY2NvdW50cyxzeXN0ZW06c2VydmljZWFjY291bnRzOmRlZmF1bHQsc3lzdGVtOmF1dGhlbnRpY2F0ZWQ=
    open-cluster-management.io/user-identity: c3lzdGVtOnNlcnZpY2VhY2NvdW50OmRlZmF1bHQ6YWRtaW5zYQ==
    apps.open-cluster-management.io/cluster-admin: "true"
  spec:
    channel: github-ns/github-ch
    placement:
      placementRef:
        name: dev-clusters`

	subscription = &appv1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(IsClusterAdmin(c, subscription, nil)).To(gomega.BeTrue())
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
			Name: "some-other-binding-name",
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
			{
				Kind:      "ServiceAccount",
				Name:      "adminsa",
				Namespace: "default",
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind: "ClusterRole",
			Name: appv1.SubscriptionAdmin,
		},
	}
}

func TestGetOwnerAndRepo(t *testing.T) {
	testCases := []struct {
		desc   string
		url    string
		wanted []string
	}{
		{
			desc:   "invalid url",
			url:    "",
			wanted: []string{},
		},
		{
			desc:   "invalid git url length 1",
			url:    "https:",
			wanted: []string{},
		},
		{
			desc:   "invalid git url length 2",
			url:    "https://google.com",
			wanted: []string{},
		},
		{
			desc:   "valid owner",
			url:    "https://github.com/open-cluster-management-io",
			wanted: []string{"github.com", "open-cluster-management-io"},
		},
		{
			desc:   "valid owner and repo",
			url:    "https://github.com/open-cluster-management-io/multicloud-operators-subscription",
			wanted: []string{"open-cluster-management-io", "multicloud-operators-subscription"},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got, err := getOwnerAndRepo(tC.url)
			if !reflect.DeepEqual(got, tC.wanted) {
				t.Errorf("wanted %v, got %v, err %v", tC.wanted, got, err)
			}
		})
	}
}

func TestSkipHooksOnManaged(t *testing.T) {
	testCases := []struct {
		desc         string
		resourcePath string
		curPath      string
		wanted       bool
	}{
		{
			desc:         "empty string",
			resourcePath: "",
			curPath:      "",
			wanted:       false,
		},
		{
			desc:         "valid prehook",
			resourcePath: "myResource",
			curPath:      "myResource/prehook/my/current/path/",
			wanted:       true,
		},
		{
			desc:         "valid posthook",
			resourcePath: "myResource",
			curPath:      "myResource/posthook/my/current/path/",
			wanted:       true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := SkipHooksOnManaged(tC.resourcePath, tC.curPath)
			if got != tC.wanted {
				t.Errorf("wanted %v, got %v", tC.wanted, got)
			}
		})
	}
}

func TestGetKnownHostFromURL(t *testing.T) {
	tmpfile, err := ioutil.TempFile("", "temptest")
	if err != nil {
		t.Error("error creating temp file")
	}

	defer os.Remove(tmpfile.Name()) // clean up

	testCases := []struct {
		desc        string
		sshURL      string
		filepath    string
		expectError bool
	}{
		{
			desc:        "invalid ssh url",
			sshURL:      "ssh:\r\n",
			filepath:    "",
			expectError: true,
		},
		{
			desc:        "invalid filepath",
			sshURL:      "",
			filepath:    "",
			expectError: true,
		},
		{
			desc:        "valid ssh url with port",
			sshURL:      "ssh://git@github.com:22/open-cluster-management-io/multicloud-operators-subscription.git",
			filepath:    tmpfile.Name(),
			expectError: false,
		},
		{
			desc:        "valid git url",
			sshURL:      "git@github.com:open-cluster-management-io/multicloud-operators-subscription.git",
			filepath:    tmpfile.Name(),
			expectError: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := getKnownHostFromURL(tC.sshURL, tC.filepath)
			if got != nil && !tC.expectError { // If error and we don't expect an error
				t.Errorf("wanted error %v, got %v", tC.expectError, got)
			}
		})
	}
}

func TestGetLatestCommitID(t *testing.T) {
	testCases := []struct {
		desc   string
		url    string
		branch string
		wanted string
	}{
		{
			desc:   "get correct SHA",
			url:    "https://github.com/stolostron/application-lifecycle-samples",
			branch: "lennysgarage-helloworld",
			wanted: "156bf795dadb1e5eeb2a03e171ff4b317d403498",
		},
		{
			desc:   "invalid branch",
			url:    "https://github.com/stolostron/application-lifecycle-samples",
			branch: "mumbled-garbage-branch-amwdwk",
			wanted: "",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got, err := GetLatestCommitID(tC.url, tC.branch)
			if got != tC.wanted {
				t.Errorf("wanted %v, got %v, err %v", tC.wanted, got, err)
			}
		})
	}
}
