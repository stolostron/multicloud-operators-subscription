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
package listener

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	channelYAML3 = `apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: test-github-channel
  namespace: test
spec:
  type: GitHub
  pathname: https://gitlab.com/ekdjbdfh/testrepo.git"`

	subscriptionYAML3 = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: test
spec:
  channel: test/test-github-channel
  placement:
    local: false`

	channelWebhookSecret = `apiVersion: v1
kind: Secret
metadata:
  name: gitlab-secret
  namespace: test
data:
  secret: bXlzZWNyZXQK`
)

func TestGitlabWebhookHandler(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	listener, err := CreateWebhookListener(cfg, cfg, scheme.Scheme, "", "", false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test that non-github event is not handled.
	req, err := http.NewRequest("POST", "/webhook", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(listener.HandleWebhook)
	handler.ServeHTTP(rr, req)
	g.Expect(rr.Code).To(gomega.Equal(http.StatusBadRequest))

	// Test 0 byte request body
	req, err = http.NewRequest("POST", "/webhook", bytes.NewBuffer([]byte("")))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set(GitlabEventHeader, "ping")

	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(listener.HandleWebhook)
	handler.ServeHTTP(rr, req)
	g.Expect(rr.Code).To(gomega.Equal(http.StatusInternalServerError))
}

func TestGitlabWebhookHandler2(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	listener, err := CreateWebhookListener(cfg, cfg, scheme.Scheme, "", "", false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subscription := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML3), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	reqBody, err := json.Marshal(map[string]string{
		"name": "joe",
		"age":  "19",
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	req2, err := http.NewRequest("POST", "/webhook", bytes.NewBuffer(reqBody))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	req2.Header.Set(GitlabEventHeader, "ping")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(listener.HandleWebhook)
	handler.ServeHTTP(rr, req2)
	g.Expect(rr.Code).To(gomega.Equal(http.StatusOK))

	key := types.NamespacedName{
		Name:      "test-subscription",
		Namespace: "test",
	}
	subscription2 := &appv1alpha1.Subscription{}
	err = c.Get(context.TODO(), key, subscription)

	g.Expect(err).NotTo(gomega.HaveOccurred())

	subAnnotations := subscription2.GetAnnotations()
	g.Expect(subAnnotations[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.BeEmpty())

	err = c.Delete(context.TODO(), subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestGitlabWebhookHandler3(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	listener, err := CreateWebhookListener(cfg, cfg, scheme.Scheme, "", "", false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	channel := &chnv1alpha1.Channel{}
	err = yaml.Unmarshal([]byte(channelYAML3), &channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	newAnnotations := make(map[string]string)
	newAnnotations[appv1alpha1.AnnotationWebhookEnabled] = "true"
	channel.SetAnnotations(newAnnotations)

	err = c.Create(context.TODO(), channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subscription := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML3), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	bodyStr := `{
		"repository": {
		  "url": "ekdjbdfh/testrepo",
		  "description": "ekdjbdfh/testrepo",
		  "homepage": "https://gitlab.com/ekdjbdfh/testrepo",
		  "name": "testrepo"
		}
	  }`

	pl := &GitLabPayload{}
	err = json.Unmarshal([]byte(bodyStr), pl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	reqBody, err := json.Marshal(pl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	req2, err := http.NewRequest("POST", "/webhook", bytes.NewBuffer(reqBody))
	g.Expect(err).NotTo(gomega.HaveOccurred())
	req2.Header.Set(GitlabEventHeader, "ping")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(listener.HandleWebhook)
	handler.ServeHTTP(rr, req2)
	g.Expect(rr.Code).To(gomega.Equal(http.StatusOK))

	key := types.NamespacedName{
		Name:      "test-subscription",
		Namespace: "test",
	}
	subscription2 := &appv1alpha1.Subscription{}
	err = c.Get(context.TODO(), key, subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subAnnotations := subscription2.GetAnnotations()
	g.Expect(subAnnotations[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.BeEmpty()) // ping event gets ignored

	req3, err := http.NewRequest("POST", "/webhook", bytes.NewBuffer(reqBody))
	g.Expect(err).NotTo(gomega.HaveOccurred())
	req3.Header.Set(GitlabEventHeader, GitLabPushEvents)

	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(listener.HandleWebhook)
	handler.ServeHTTP(rr, req3)
	g.Expect(rr.Code).To(gomega.Equal(http.StatusOK))

	time.Sleep(2 * time.Second)

	subscription3 := &appv1alpha1.Subscription{}
	err = c.Get(context.TODO(), key, subscription3)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subAnnotations2 := subscription3.GetAnnotations()
	g.Expect(subAnnotations2[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.Equal("0")) // annotation added

	err = c.Delete(context.TODO(), subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Delete(context.TODO(), channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestWithWebhookSecret(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	listener, err := CreateWebhookListener(cfg, cfg, scheme.Scheme, "", "", false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	channel := &chnv1alpha1.Channel{}
	err = yaml.Unmarshal([]byte(channelYAML3), &channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	newAnnotations := make(map[string]string)
	newAnnotations[appv1alpha1.AnnotationWebhookEnabled] = "true"
	newAnnotations[appv1alpha1.AnnotationWebhookSecret] = "gitlab-secret"
	channel.SetAnnotations(newAnnotations)

	err = c.Create(context.TODO(), channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subscription := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML3), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	secret := &corev1.Secret{}
	err = yaml.Unmarshal([]byte(channelWebhookSecret), &secret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), secret)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	bodyStr := `{
		"repository": {
		  "url": "ekdjbdfh/testrepo",
		  "description": "ekdjbdfh/testrepo",
		  "homepage": "https://gitlab.com/ekdjbdfh/testrepo",
		  "name": "testrepo"
		}
	  }`

	pl := &GitLabPayload{}
	err = json.Unmarshal([]byte(bodyStr), pl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	reqBody, err := json.Marshal(pl)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	req, err := http.NewRequest("POST", "/webhook", bytes.NewBuffer(reqBody))
	g.Expect(err).NotTo(gomega.HaveOccurred())
	req.Header.Set(GitlabEventHeader, GitLabPushEvents)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(listener.HandleWebhook)
	handler.ServeHTTP(rr, req)
	g.Expect(rr.Code).To(gomega.Equal(http.StatusOK))

	time.Sleep(2 * time.Second)

	key := types.NamespacedName{
		Name:      "test-subscription",
		Namespace: "test",
	}

	subscription2 := &appv1alpha1.Subscription{}
	err = c.Get(context.TODO(), key, subscription2)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subAnnotations := subscription2.GetAnnotations()
	g.Expect(subAnnotations[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.BeEmpty()) // secret mismatch. webhook event not processed for the subscription.

	req, err = http.NewRequest("POST", "/webhook", bytes.NewBuffer(reqBody))
	g.Expect(err).NotTo(gomega.HaveOccurred())
	req.Header.Set(GitlabEventHeader, GitLabPushEvents)
	req.Header.Set(gitlabSignatureHeader, "mysecret")

	handler.ServeHTTP(rr, req)
	g.Expect(rr.Code).To(gomega.Equal(http.StatusOK))

	time.Sleep(2 * time.Second)

	subscription3 := &appv1alpha1.Subscription{}
	err = c.Get(context.TODO(), key, subscription3)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subAnnotations2 := subscription3.GetAnnotations()
	g.Expect(subAnnotations2[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.Equal("0")) // secret match. webhook event processed for the subscription.

	err = c.Delete(context.TODO(), subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Delete(context.TODO(), channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}
