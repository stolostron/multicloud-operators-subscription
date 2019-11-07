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
	assert.NoError(t, err)

	err = c.Create(context.TODO(), githubsub)
	assert.NoError(t, err)

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
	assert.NoError(t, err)

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
	assert.Error(t, err)
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
	assert.NoError(t, err)

	// Test with a fake authentication secret but correct data keys in the secret
	chnSecret := &corev1.Secret{}
	err = yaml.Unmarshal([]byte(correctSecret), &chnSecret)
	assert.NoError(t, err)

	err = c.Create(context.TODO(), chnSecret)
	assert.NoError(t, err)

	secretRef := &corev1.ObjectReference{}
	secretRef.Name = "correct-secret"

	githubchn.Spec.SecretRef = secretRef
	_, err = subitem.cloneGitRepo()
	g.Expect(err.Error()).To(gomega.Equal("authentication required"))

	err = c.Delete(context.TODO(), chnSecret)
	assert.NoError(t, err)

	// Test with a fake authentication secret but correct data keys in the secret
	chnSecret = &corev1.Secret{}
	err = yaml.Unmarshal([]byte(incorrectSecret), &chnSecret)
	assert.NoError(t, err)

	err = c.Create(context.TODO(), chnSecret)
	assert.NoError(t, err)

	secretRef.Name = "incorrect-secret"
	_, err = subitem.cloneGitRepo()
	g.Expect(err.Error()).To(gomega.Equal("failed to get accressToken from the secret"))
}
