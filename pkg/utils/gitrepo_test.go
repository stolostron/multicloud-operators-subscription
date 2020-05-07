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
	"os"
	"path/filepath"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	"gopkg.in/src-d/go-git.v4/plumbing"
	corev1 "k8s.io/api/core/v1"
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
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

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

func TestKustomize(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

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
            org: acmeCorporation-roke
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
	g.Expect(len(chartDirs)).To(gomega.Equal(3))
	g.Expect(len(kustomizeDirs)).To(gomega.Equal(4))
	g.Expect(len(crdsAndNamespaceFiles)).To(gomega.Equal(2))
	g.Expect(len(rbacFiles)).To(gomega.Equal(2))
	g.Expect(len(otherFiles)).To(gomega.Equal(5))
}
