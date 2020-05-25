// Copyright 2019 The Kubernetes Authors.
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

package git

import (
	"context"
	"time"

	"github.com/ghodss/yaml"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

const rsc1 = `apiVersion: v1
data:
  keyOne: "true"
kind: ConfigMap
metadata:
  name: TestConfigMap1
  namespace: default
  labels:
    environment: dev
    city: Toronto`

const rsc2 = `apiVersion: v1
data:
  keyOne: "true"
kind: ConfigMap
metadata:
  name: TestConfigMap2
  namespace: default
  annotations:
    need: this
  labels:
    environment: dev
    city: Toronto`

const invalidRsc = `apiVersion: v1
data:
  keyOne: "true"
kind: IsThisConfigMap
metadata:
  name: TestConfigMap2
  namespace: default
  annotations:
    need: this
  labels:
    environment: dev
    city: Toronto`

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

var id = types.NamespacedName{
	Name:      "endpoint",
	Namespace: "default",
}

var (
	sharedkey = types.NamespacedName{
		Name:      "githubtest",
		Namespace: "default",
	}
	githubchn = &chnv1alpha1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type:     "Git",
			Pathname: "https://github.com/open-cluster-management/multicloud-operators-subscription.git",
		},
	}
	githubsub = &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: sharedkey.String(),
		},
	}
	subitem = &appv1alpha1.SubscriberItem{
		Subscription: githubsub,
		Channel:      githubchn,
	}
)

var _ = Describe("github subscriber reconcile logic", func() {
	It("should reconcile on github subscription creation", func() {
		// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
		// channel when it is finished.
		err := k8sClient.Create(context.TODO(), githubchn)
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Create(context.TODO(), githubsub)
		Expect(err).NotTo(HaveOccurred())

		Expect(defaultSubscriber.SubscribeItem(subitem)).NotTo(HaveOccurred())

		time.Sleep(k8swait)

		Expect(defaultSubscriber.UnsubscribeItem(sharedkey)).NotTo(HaveOccurred())

	})

	It("should pass resource label selector", func() {
		subitem := &SubscriberItem{}
		subitem.Subscription = githubsub
		subitem.Channel = githubchn

		rsc := &unstructured.Unstructured{}
		err := yaml.Unmarshal([]byte(rsc1), &rsc)
		Expect(err).Should(Succeed())

		// Test kube resource with no package filter
		errMsg := subitem.checkFilters(rsc)
		Expect(errMsg).To(gomega.Equal(""))

		matchLabels := make(map[string]string)
		matchLabels["environment"] = "dev"
		lblSelector := &metav1.LabelSelector{}
		lblSelector.MatchLabels = matchLabels
		pkgFilter := &appv1alpha1.PackageFilter{}
		pkgFilter.LabelSelector = lblSelector
		githubsub.Spec.PackageFilter = pkgFilter

		// Test kube resource with package filter having a matching label
		errMsg = subitem.checkFilters(rsc)
		Expect(errMsg).To(Equal(""))

		matchLabels = make(map[string]string)
		matchLabels["city"] = "Toronto"
		matchLabels["environment"] = "dev"
		lblSelector.MatchLabels = matchLabels

		// Test kube resource with package filter having multiple matching labels
		errMsg = subitem.checkFilters(rsc)
		Expect(errMsg).To(Equal(""))

		matchLabels = make(map[string]string)
		matchLabels["city"] = "paris"
		matchLabels["environment"] = "dev"
		lblSelector.MatchLabels = matchLabels

		// Test kube resource with package filter having some matching labels
		errMsg = subitem.checkFilters(rsc)
		Expect(errMsg).To(Equal("Failed to pass label check on resource " + rsc.GetName()))

		err = yaml.Unmarshal([]byte(rsc2), &rsc)
		//assert.NoError(t, err)
		Expect(err).Should(Succeed())

		matchLabels = make(map[string]string)
		matchLabels["environment"] = "dev"
		lblSelector.MatchLabels = matchLabels

		// Test kube resource with package filter having no annotation
		errMsg = subitem.checkFilters(rsc)
		Expect(errMsg).To(Equal(""))

		annotations := make(map[string]string)
		annotations["need"] = "this"
		githubsub.Spec.PackageFilter.Annotations = annotations

		// Test kube resource with package filter having some matching labels
		errMsg = subitem.checkFilters(rsc)
		Expect(errMsg).To(Equal(""))

		annotations["need"] = "not"

		// Test kube resource with package filter having some matching labels
		errMsg = subitem.checkFilters(rsc)
		Expect(errMsg).To(Equal("Failed to pass annotation check to deployable " + rsc.GetName()))

	})
})

var _ = Describe("test subscribe invalid resource", func() {
	It("should not return error or panic", func() {
		subitem := &SubscriberItem{}
		subitem.Subscription = githubsub
		subitem.Channel = githubchn
		subitem.synchronizer = defaultSubscriber.synchronizer

		pkgMap := make(map[string]bool)

		// Test subscribing an invalid kubernetes resource
		_, _, err := subitem.subscribeResource([]byte(invalidRsc), pkgMap)
		Expect(err).To(HaveOccurred())

	})

	It("should clone the target repo", func() {
		subitem := &SubscriberItem{}
		subitem.Subscription = githubsub
		subitem.Channel = githubchn
		subitem.synchronizer = defaultSubscriber.synchronizer
		commitid, err := subitem.cloneGitRepo()
		Expect(commitid).ToNot(Equal(""))
		Expect(err).NotTo(HaveOccurred())

		// Test with a fake authentication secret but correct data keys in the secret
		chnSecret := &corev1.Secret{}
		err = yaml.Unmarshal([]byte(correctSecret), &chnSecret)
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Create(context.TODO(), chnSecret)
		Expect(err).NotTo(HaveOccurred())

		secretRef := &corev1.ObjectReference{}
		secretRef.Name = "correct-secret"

		githubchn.Spec.SecretRef = secretRef
		_, err = subitem.cloneGitRepo()
		Expect(err.Error()).To(Equal("authentication required"))

		noUserKey := make(map[string][]byte)
		chnSecret.Data = noUserKey
		err = k8sClient.Update(context.TODO(), chnSecret)
		Expect(err).NotTo(HaveOccurred())

		_, err = subitem.cloneGitRepo()
		Expect(err.Error()).To(Equal("failed to get user from the secret"))

		err = k8sClient.Delete(context.TODO(), chnSecret)
		Expect(err).NotTo(HaveOccurred())

		// Test with a fake authentication secret but correct data keys in the secret
		chnSecret = &corev1.Secret{}
		err = yaml.Unmarshal([]byte(incorrectSecret), &chnSecret)
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Create(context.TODO(), chnSecret)
		Expect(err).NotTo(HaveOccurred())

		secretRef.Name = "incorrect-secret"
		_, err = subitem.cloneGitRepo()
		Expect(err.Error()).To(Equal("failed to get accressToken from the secret"))

		githubchn.Spec.SecretRef = nil

	})

	It("should success on subscription with repo path", func() {
		pathConfigMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: path-config-map
  namespace: default
data:
  path: test/github/resources/deploy/crds`

		pathConfigMap := &corev1.ConfigMap{}
		err := yaml.Unmarshal([]byte(pathConfigMapYAML), &pathConfigMap)
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Create(context.TODO(), pathConfigMap)
		Expect(err).NotTo(HaveOccurred())

		filterRef := &corev1.LocalObjectReference{}
		filterRef.Name = "path-config-map"

		packageFilter := &appv1alpha1.PackageFilter{}
		packageFilter.FilterRef = filterRef

		githubsub.Spec.PackageFilter = packageFilter

		subitem := &SubscriberItem{}
		subitem.Subscription = githubsub
		subitem.Channel = githubchn
		subitem.synchronizer = defaultSubscriber.synchronizer

		// Set the cloned Git repo root directory to this Git repository root.
		subitem.repoRoot = "../../.."

		err = subitem.sortClonedGitRepo()
		Expect(err).NotTo(HaveOccurred())

		Expect(len(subitem.indexFile.Entries)).To(BeZero())

		Expect(len(subitem.crdsAndNamespaceFiles)).To(Equal(1))
		Expect(subitem.crdsAndNamespaceFiles[0]).To(ContainSubstring("test/github/resources/deploy/crds/crontab.yaml"))

		Expect(len(subitem.otherFiles)).To(BeZero())

		githubsub.Spec.PackageFilter = nil

		err = k8sClient.Delete(context.TODO(), pathConfigMap)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should remove no matching name", func() {
		pathConfigMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: path-config-map
  namespace: default
data:
  path: test/github/helmcharts`

		pathConfigMap := &corev1.ConfigMap{}
		err := yaml.Unmarshal([]byte(pathConfigMapYAML), &pathConfigMap)
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Create(context.TODO(), pathConfigMap)
		Expect(err).NotTo(HaveOccurred())

		filterRef := &corev1.LocalObjectReference{}
		filterRef.Name = "path-config-map"

		packageFilter := &appv1alpha1.PackageFilter{}
		packageFilter.FilterRef = filterRef

		githubsub.Spec.PackageFilter = packageFilter

		subanno := make(map[string]string)
		subanno[appv1alpha1.AnnotationGitPath] = "test/github/helmcharts"
		githubsub.SetAnnotations(subanno)

		githubsub.Spec.Package = "chart1"

		subitem := &SubscriberItem{}
		subitem.Subscription = githubsub
		subitem.Channel = githubchn
		subitem.synchronizer = defaultSubscriber.synchronizer

		// Set the cloned Git repo root directory to this Git repository root.
		subitem.repoRoot = "../../.."

		err = subitem.sortClonedGitRepo()
		Expect(err).NotTo(HaveOccurred())
		// 3 helm charts: test/github/helmcharts/chart1, test/github/helmcharts/chart2, test/github/helmcharts/otherCharts/chart1
		Expect(len(subitem.chartDirs)).To(Equal(3))

		// Filter out chart2
		Expect(len(subitem.indexFile.Entries)).To(Equal(1))

		// chart1 has two versions but it will only contain the latest version
		chartVersion, err := subitem.indexFile.Get("chart1", "1.1.2")
		Expect(err).NotTo(HaveOccurred())
		Expect(chartVersion.GetName()).To(Equal("chart1"))
		Expect(chartVersion.GetVersion()).To(Equal("1.1.2"))

		_, err = subitem.indexFile.Get("chart1", "1.1.1")
		Expect(err).To(HaveOccurred())

		packageFilter.Version = "1.1.1"

		err = subitem.sortClonedGitRepo()
		Expect(err).NotTo(HaveOccurred())

		Expect(len(subitem.indexFile.Entries)).To(Equal(1))

		_, err = subitem.indexFile.Get("chart1", "1.1.2")
		Expect(err).To(HaveOccurred())

		chartVersion, err = subitem.indexFile.Get("chart1", "1.1.1")
		Expect(err).NotTo(HaveOccurred())
		Expect(chartVersion.GetName()).To(Equal("chart1"))
		Expect(chartVersion.GetVersion()).To(Equal("1.1.1"))

		err = k8sClient.Delete(context.TODO(), pathConfigMap)
		Expect(err).NotTo(HaveOccurred())

		githubsub.Spec.Package = ""
		githubsub.Spec.PackageFilter = nil
	})

	It("should check Tiller version", func() {
		pathConfigMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: path-config-map
  namespace: default
data:
  path: test/github/helmcharts`

		pathConfigMap := &corev1.ConfigMap{}
		err := yaml.Unmarshal([]byte(pathConfigMapYAML), &pathConfigMap)

		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Create(context.TODO(), pathConfigMap)
		Expect(err).NotTo(HaveOccurred())

		filterRef := &corev1.LocalObjectReference{}
		filterRef.Name = "path-config-map"

		packageFilter := &appv1alpha1.PackageFilter{}
		packageFilter.FilterRef = filterRef

		annotations := make(map[string]string)
		annotations["tillerVersion"] = "2.8.0"

		packageFilter.Annotations = annotations

		githubsub.Spec.PackageFilter = packageFilter

		githubsub.Spec.Package = "chart2"

		subitem := &SubscriberItem{}
		subitem.Subscription = githubsub
		subitem.Channel = githubchn
		subitem.synchronizer = defaultSubscriber.synchronizer

		// Set the cloned Git repo root directory to this Git repository root.
		subitem.repoRoot = "../../.."

		err = subitem.sortClonedGitRepo()
		Expect(err).NotTo(HaveOccurred())
		Expect(len(subitem.indexFile.Entries)).To(Equal(0))

		annotations["tillerVersion"] = "2.9.2"

		// In test/github/helmcharts directory, filter out all helm charts except charts with name "chart2"
		// and with tillerVersion annotation that satisfies subscription's tillerVersion annotation
		err = subitem.sortClonedGitRepo()
		Expect(err).NotTo(HaveOccurred())
		Expect(len(subitem.indexFile.Entries)).To(Equal(1))

		err = k8sClient.Delete(context.TODO(), pathConfigMap)
		Expect(err).NotTo(HaveOccurred())

		githubsub.Spec.Package = ""
		githubsub.Spec.PackageFilter = nil
	})
})
