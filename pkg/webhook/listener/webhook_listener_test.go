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
	"os"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

const (
	channelYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: test-github-channel
  namespace: test
spec:
  type: GitHub
  pathname: https://github.com/IBM/charts.git`

	subscriptionYAML = `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: test-subscription
  namespace: test
spec:
  channel: test/test-github-channel
  placement:
    local: false`
)

func TestWebhookHandler(t *testing.T) {
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

	// Test that github event is handled.
	req, err = http.NewRequest("POST", "/webhook", nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("X-Github-Event", "ping")

	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(listener.HandleWebhook)
	handler.ServeHTTP(rr, req)
	g.Expect(rr.Code).To(gomega.Equal(http.StatusInternalServerError))

	reqBody, err := json.Marshal(map[string]string{
		"name": "joe",
		"age":  "19",
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	req2, err := http.NewRequest("POST", "/webhook", bytes.NewBuffer(reqBody))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Github-Event", "ping")

	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(listener.HandleWebhook)
	handler.ServeHTTP(rr, req2)
	g.Expect(rr.Code).To(gomega.Equal(http.StatusOK))
}

func TestWebhookHandler2(t *testing.T) {
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
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
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

	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Github-Event", "ping")

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

func TestWebhookHandler3(t *testing.T) {
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
	err = yaml.Unmarshal([]byte(channelYAML), &channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subscription := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
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
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Github-Event", "ping")

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

	err = c.Delete(context.TODO(), channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestUpdateSubscription(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	key := types.NamespacedName{
		Name:      "test-subscription",
		Namespace: "test",
	}
	subscription := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(subscriptionYAML), &subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Create(context.TODO(), subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subscription = &appv1alpha1.Subscription{}
	err = c.Get(context.TODO(), key, subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	subAnnotations := subscription.GetAnnotations()
	g.Expect(subAnnotations[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.BeEmpty())

	listener, err := CreateWebhookListener(cfg, cfg, scheme.Scheme, "", "", false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test that webhook-event=0 annotation gets added when there is no annotation.
	updatedSubscription := listener.updateSubscription(*subscription)
	subAnnotations2 := updatedSubscription.GetAnnotations()
	g.Expect(subAnnotations2[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.Equal("0"))

	// Test that webhook-event annotation gets incremented on subsequent updates.
	updatedSubscription2 := listener.updateSubscription(*updatedSubscription)
	subAnnotations3 := updatedSubscription2.GetAnnotations()
	g.Expect(subAnnotations3[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.Equal("1"))

	// Test that webhook-event=0 annotation gets added when there are other annotations except webhook-event.
	newAnnotations := make(map[string]string)
	newAnnotations["test"] = "123"
	updatedSubscription2.SetAnnotations(newAnnotations)
	updatedSubscription3 := listener.updateSubscription(*updatedSubscription2)
	subAnnotations4 := updatedSubscription3.GetAnnotations()
	g.Expect(subAnnotations4[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.Equal("0"))

	// Test that webhook-event=0 annotation gets added when it fails to parse and increment existing webhook-event annotation.
	newAnnotations = make(map[string]string)
	newAnnotations[appv1alpha1.AnnotationWebhookEventCount] = "abc"
	updatedSubscription3.SetAnnotations(newAnnotations)
	updatedSubscription4 := listener.updateSubscription(*updatedSubscription3)
	subAnnotations5 := updatedSubscription4.GetAnnotations()
	g.Expect(subAnnotations5[appv1alpha1.AnnotationWebhookEventCount]).To(gomega.Equal("0"))

	err = c.Delete(context.TODO(), subscription)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestParseRequest(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	listener, err := CreateWebhookListener(cfg, cfg, scheme.Scheme, "", "", false)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	reqBody, err := json.Marshal(map[string]string{
		"name": "joe",
		"age":  "19",
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	req, err := http.NewRequest("POST", "/webhook", bytes.NewBuffer(reqBody))
	g.Expect(err).NotTo(gomega.HaveOccurred())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Github-Event", "ping")
	body, signature, event, err := listener.ParseRequest(req)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(body).NotTo(gomega.BeEmpty())
	g.Expect(signature).To(gomega.BeEmpty())
	g.Expect(event).NotTo(gomega.BeNil())

	// Test when the header does not contain X-Github-Event
	req2, err := http.NewRequest("POST", "/webhook", bytes.NewBuffer(reqBody))
	if err != nil {
		t.Fatal(err)
	}

	req2.Header.Set("Content-Type", "application/json")

	body, signature, event, err = listener.ParseRequest(req)

	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(body).To(gomega.BeNil())
	g.Expect(signature).To(gomega.BeEmpty())
	g.Expect(event).To(gomega.BeNil())
}

func TestValidateSecret(t *testing.T) {
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

	annotations := make(map[string]string)
	ret := listener.validateSecret("", annotations, "default", []byte("test"))

	g.Expect(ret).To(gomega.BeFalse())

	annotations[appv1alpha1.AnnotationWebhookSecret] = "test"
	ret = listener.validateSecret("", annotations, "default", []byte("test"))
	g.Expect(ret).To(gomega.BeFalse())
}

func TestValidateChannel(t *testing.T) {
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
	err = yaml.Unmarshal([]byte(channelYAML), &channel)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ret := listener.validateChannel(channel, "", "", []byte(""))
	g.Expect(ret).To(gomega.BeFalse())

	channel.Spec.Type = chnv1alpha1.ChannelTypeHelmRepo
	ret = listener.validateChannel(channel, "", "", []byte(""))
	g.Expect(ret).To(gomega.BeFalse())

	channel.Spec.Type = chnv1alpha1.ChannelTypeGit
	newAnnotations := make(map[string]string)
	newAnnotations[appv1alpha1.AnnotationWebhookEnabled] = "false"
	channel.SetAnnotations(newAnnotations)
	ret = listener.validateChannel(channel, "", "", []byte(""))
	g.Expect(ret).To(gomega.BeFalse())

	newAnnotations[appv1alpha1.AnnotationWebhookEnabled] = "true"
	channel.SetAnnotations(newAnnotations)
	ret = listener.validateChannel(channel, "", "", []byte(""))
	g.Expect(ret).To(gomega.BeTrue())
}

func TestServiceCreation(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Sleep for the manager cache to start
	time.Sleep(3 * time.Second)

	os.Setenv("DEPLOYMENT_LABEL", "test-deployment")

	err = createWebhookListnerService(c, "default")
	// It will fail because the deployment resource for the owner reference is not found in the cluster.
	g.Expect(err).To(gomega.HaveOccurred())
}
