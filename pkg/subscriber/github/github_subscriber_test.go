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

package github

import (
	"context"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"gopkg.in/src-d/go-git.v4/plumbing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1alpha1 "github.com/IBM/multicloud-operators-channel/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
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

var c client.Client

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
			Type:     "GitHub",
			PathName: "https://github.com/IBM/multicloud-operators-subscription.git",
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

func TestGitHubSubscriber(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	err = c.Create(context.TODO(), githubchn)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())
	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	g.Expect(defaultSubscriber.SubscribeItem(subitem)).NotTo(gomega.HaveOccurred())

	time.Sleep(3 * time.Second)

	g.Expect(defaultSubscriber.UnsubscribeItem(sharedkey)).NotTo(gomega.HaveOccurred())
}

func TestResourceLableSelector(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	subitem := &SubscriberItem{}
	subitem.Subscription = githubsub
	subitem.Channel = githubchn

	rsc := &unstructured.Unstructured{}
	err := yaml.Unmarshal([]byte(rsc1), &rsc)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test kube resource with no package filter
	errMsg := subitem.checkFilters(rsc)
	g.Expect(errMsg).To(gomega.Equal(""))

	matchLabels := make(map[string]string)
	matchLabels["environment"] = "dev"
	lblSelector := &metav1.LabelSelector{}
	lblSelector.MatchLabels = matchLabels
	pkgFilter := &appv1alpha1.PackageFilter{}
	pkgFilter.LabelSelector = lblSelector
	githubsub.Spec.PackageFilter = pkgFilter

	// Test kube resource with package filter having a matching label
	errMsg = subitem.checkFilters(rsc)
	g.Expect(errMsg).To(gomega.Equal(""))

	matchLabels = make(map[string]string)
	matchLabels["city"] = "Toronto"
	matchLabels["environment"] = "dev"
	lblSelector.MatchLabels = matchLabels

	// Test kube resource with package filter having multiple matching labels
	errMsg = subitem.checkFilters(rsc)
	g.Expect(errMsg).To(gomega.Equal(""))

	matchLabels = make(map[string]string)
	matchLabels["city"] = "paris"
	matchLabels["environment"] = "dev"
	lblSelector.MatchLabels = matchLabels

	// Test kube resource with package filter having some matching labels
	errMsg = subitem.checkFilters(rsc)
	g.Expect(errMsg).To(gomega.Equal("Failed to pass label check on resource " + rsc.GetName()))

	err = yaml.Unmarshal([]byte(rsc2), &rsc)
	assert.NoError(t, err)

	matchLabels = make(map[string]string)
	matchLabels["environment"] = "dev"
	lblSelector.MatchLabels = matchLabels

	// Test kube resource with package filter having no annotation
	errMsg = subitem.checkFilters(rsc)
	g.Expect(errMsg).To(gomega.Equal(""))

	annotations := make(map[string]string)
	annotations["need"] = "this"
	githubsub.Spec.PackageFilter.Annotations = annotations

	// Test kube resource with package filter having some matching labels
	errMsg = subitem.checkFilters(rsc)
	g.Expect(errMsg).To(gomega.Equal(""))

	annotations["need"] = "not"

	// Test kube resource with package filter having some matching labels
	errMsg = subitem.checkFilters(rsc)
	g.Expect(errMsg).To(gomega.Equal("Failed to pass annotation check to deployable " + rsc.GetName()))
}

func TestSubscribeInvalidResource(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	subitem := &SubscriberItem{}
	subitem.Subscription = githubsub
	subitem.Channel = githubchn
	subitem.synchronizer = defaultSubscriber.synchronizer

	pkgMap := make(map[string]bool)

	// Test subscribing an invalid kubernetes resource
	_, _, err = subitem.subscribeResource([]byte(invalidRsc), pkgMap)
	g.Expect(err).To(gomega.HaveOccurred())
}
func TestCloneGitRepo(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	subitem := &SubscriberItem{}
	subitem.Subscription = githubsub
	subitem.Channel = githubchn
	subitem.synchronizer = defaultSubscriber.synchronizer
	commitid, err := subitem.cloneGitRepo()
	g.Expect(commitid).ToNot(gomega.Equal(""))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test with a fake authentication secret but correct data keys in the secret
	chnSecret := &corev1.Secret{}
	err = yaml.Unmarshal([]byte(correctSecret), &chnSecret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), chnSecret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	secretRef := &corev1.ObjectReference{}
	secretRef.Name = "correct-secret"

	githubchn.Spec.SecretRef = secretRef
	_, err = subitem.cloneGitRepo()
	g.Expect(err.Error()).To(gomega.Equal("authentication required"))

	noUserKey := make(map[string][]byte)
	chnSecret.Data = noUserKey
	err = c.Update(context.TODO(), chnSecret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = subitem.cloneGitRepo()
	g.Expect(err.Error()).To(gomega.Equal("failed to get user from the secret"))

	err = c.Delete(context.TODO(), chnSecret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test with a fake authentication secret but correct data keys in the secret
	chnSecret = &corev1.Secret{}
	err = yaml.Unmarshal([]byte(incorrectSecret), &chnSecret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), chnSecret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	secretRef.Name = "incorrect-secret"
	_, err = subitem.cloneGitRepo()
	g.Expect(err.Error()).To(gomega.Equal("failed to get accressToken from the secret"))

	githubchn.Spec.SecretRef = nil
}

func TestSubscriptionWithRepoPath(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	pathConfigMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: path-config-map
  namespace: default
data:
  path: test/github/resources/deploy/crds`

	pathConfigMap := &corev1.ConfigMap{}
	err = yaml.Unmarshal([]byte(pathConfigMapYAML), &pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

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
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(subitem.indexFile.Entries)).To(gomega.BeZero())

	g.Expect(len(subitem.crdsAndNamespaceFiles)).To(gomega.Equal(1))
	g.Expect(subitem.crdsAndNamespaceFiles[0]).To(gomega.ContainSubstring("test/github/resources/deploy/crds/crontab.yaml"))

	g.Expect(len(subitem.otherFiles)).To(gomega.BeZero())

	githubsub.Spec.PackageFilter = nil

	err = c.Delete(context.TODO(), pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestRemoveNoMatchingName(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	pathConfigMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: path-config-map
  namespace: default
data:
  path: test/github/helmcharts`

	pathConfigMap := &corev1.ConfigMap{}
	err = yaml.Unmarshal([]byte(pathConfigMapYAML), &pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	filterRef := &corev1.LocalObjectReference{}
	filterRef.Name = "path-config-map"

	packageFilter := &appv1alpha1.PackageFilter{}
	packageFilter.FilterRef = filterRef

	githubsub.Spec.PackageFilter = packageFilter

	githubsub.Spec.Package = "chart1"

	subitem := &SubscriberItem{}
	subitem.Subscription = githubsub
	subitem.Channel = githubchn
	subitem.synchronizer = defaultSubscriber.synchronizer

	// Set the cloned Git repo root directory to this Git repository root.
	subitem.repoRoot = "../../.."

	err = subitem.sortClonedGitRepo()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	// 3 helm charts: test/github/helmcharts/chart1, test/github/helmcharts/chart2, test/github/helmcharts/otherCharts/chart1
	g.Expect(len(subitem.chartDirs)).To(gomega.Equal(3))

	// Filter out chart2
	g.Expect(len(subitem.indexFile.Entries)).To(gomega.Equal(1))

	// chart1 has two versions
	chartVersion, err := subitem.indexFile.Get("chart1", "1.1.1")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(chartVersion.GetName()).To(gomega.Equal("chart1"))
	g.Expect(chartVersion.GetVersion()).To(gomega.Equal("1.1.1"))

	chartVersion, err = subitem.indexFile.Get("chart1", "1.1.2")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(chartVersion.GetName()).To(gomega.Equal("chart1"))
	g.Expect(chartVersion.GetVersion()).To(gomega.Equal("1.1.2"))

	packageFilter.Version = "1.1.2"

	err = subitem.sortClonedGitRepo()
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(subitem.indexFile.Entries)).To(gomega.Equal(1))

	_, err = subitem.indexFile.Get("chart1", "1.1.1")
	g.Expect(err).To(gomega.HaveOccurred())

	chartVersion, err = subitem.indexFile.Get("chart1", "1.1.2")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(chartVersion.GetName()).To(gomega.Equal("chart1"))
	g.Expect(chartVersion.GetVersion()).To(gomega.Equal("1.1.2"))

	err = c.Delete(context.TODO(), pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	githubsub.Spec.Package = ""
	githubsub.Spec.PackageFilter = nil
}

func TestCheckTillerVersion(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	pathConfigMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: path-config-map
  namespace: default
data:
  path: test/github/helmcharts`

	pathConfigMap := &corev1.ConfigMap{}
	err = yaml.Unmarshal([]byte(pathConfigMapYAML), &pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

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
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(subitem.indexFile.Entries)).To(gomega.Equal(0))

	annotations["tillerVersion"] = "2.9.2"

	// In test/github/helmcharts directory, filter out all helm charts except charts with name "chart2"
	// and with tillerVersion annotation that satisfies subscription's tillerVersion annotation
	err = subitem.sortClonedGitRepo()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(subitem.indexFile.Entries)).To(gomega.Equal(1))

	err = c.Delete(context.TODO(), pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	githubsub.Spec.Package = ""
	githubsub.Spec.PackageFilter = nil
}

func TestGetBranch(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	g.Expect(Add(mgr, cfg, &id, 2)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	pathConfigMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: path-config-map
  namespace: default
data:
  path: test/github/helmcharts`

	pathConfigMap := &corev1.ConfigMap{}
	err = yaml.Unmarshal([]byte(pathConfigMapYAML), &pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer c.Delete(context.TODO(), pathConfigMap)

	filterRef := &corev1.LocalObjectReference{}
	filterRef.Name = "path-config-map"

	packageFilter := &appv1alpha1.PackageFilter{}
	packageFilter.FilterRef = filterRef
	githubsub.Spec.PackageFilter = packageFilter

	subitem := &SubscriberItem{}
	subitem.Subscription = githubsub
	subitem.Channel = githubchn
	subitem.SubscriberItem.SubscriptionConfigMap = pathConfigMap

	branch := subitem.getGitBranch()
	g.Expect(branch).To(gomega.Equal(plumbing.Master))
	g.Expect(branch.Short()).To(gomega.Equal("master"))

	pathConfigMap.Data["branch"] = "notmaster"
	err = c.Update(context.TODO(), pathConfigMap)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	branch = subitem.getGitBranch()
	g.Expect(branch).To(gomega.Equal(plumbing.ReferenceName("refs/heads/notmaster")))
	g.Expect(branch.Short()).To(gomega.Equal("notmaster"))

	githubsub.Spec.Package = ""
	githubsub.Spec.PackageFilter = nil
}
