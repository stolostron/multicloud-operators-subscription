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
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v32/github"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

const (
	payloadFormParam      = "payload"
	githubSignatureHeader = "X-Hub-Signature"
)

func (listener *WebhookListener) handleGithubWebhook(r *http.Request) error {
	var body []byte

	var signature string

	var event interface{}

	var err error

	eventType := github.WebHookType(r)

	body, signature, event, err = listener.ParseRequest(r)
	if err != nil {
		klog.Error("Failed to parse the request. error:", err)
		return err
	}

	subList := &appv1alpha1.SubscriptionList{}
	listopts := &client.ListOptions{}

	err = listener.LocalClient.List(context.TODO(), subList, listopts)
	if err != nil {
		klog.Error("Failed to get subscriptions. error: ", err)
		return err
	}

	if strings.EqualFold(eventType, "push") || strings.EqualFold(eventType, "pull") {
		// Loop through all subscriptions
		for _, sub := range subList.Items {
			if !listener.processSubscription(sub, event, signature, eventType, body) {
				continue
			}
		}
	} else {
		klog.V(2).Infof("Unhandled webhook event type %s\n", eventType)
	}

	return nil
}

func (listener *WebhookListener) processSubscription(sub appv1alpha1.Subscription, event interface{}, signature, eventType string, body []byte) bool {
	klog.V(2).Info("Evaluating subscription: " + sub.GetName())

	chNamespace := ""
	chName := ""

	if sub.Spec.Channel != "" {
		strs := strings.Split(sub.Spec.Channel, "/")
		if len(strs) == 2 {
			chNamespace = strs[0]
			chName = strs[1]
		} else {
			klog.Error("Failed to get channel namespace and name.")
			return false
		}
	}

	chkey := types.NamespacedName{Name: chName, Namespace: chNamespace}
	chobj := &chnv1alpha1.Channel{}
	err := listener.RemoteClient.Get(context.TODO(), chkey, chobj)

	if err != nil {
		klog.Error("Failed to get subscription's channel. error: ", err)
		return false
	}

	if !listener.validateChannel(chobj, signature, chNamespace, body) {
		return false
	}

	switch e := event.(type) {
	case *github.PullRequestEvent:
		if chobj.Spec.Pathname == e.GetRepo().GetCloneURL() ||
			chobj.Spec.Pathname == e.GetRepo().GetHTMLURL() ||
			chobj.Spec.Pathname == e.GetRepo().GetURL() ||
			strings.Contains(chobj.Spec.Pathname, e.GetRepo().GetFullName()) {
			klog.Info("Processing PR event from " + e.GetRepo().GetHTMLURL())
			listener.updateSubscription(sub)
		}
	case *github.PushEvent:
		if chobj.Spec.Pathname == e.GetRepo().GetCloneURL() ||
			chobj.Spec.Pathname == e.GetRepo().GetHTMLURL() ||
			chobj.Spec.Pathname == e.GetRepo().GetURL() ||
			strings.Contains(chobj.Spec.Pathname, e.GetRepo().GetFullName()) {
			klog.Info("Processing PUSH event from " + e.GetRepo().GetHTMLURL())
			listener.updateSubscription(sub)
		}
	default:
		klog.Infof("Unhandled webhook event type %s\n", eventType)
		return false
	}

	return true
}

func (listener *WebhookListener) validateChannel(chobj *chnv1alpha1.Channel, signature, chNamespace string, body []byte) bool {
	// This WebHook event is applicable for this subscription if:
	// 		1. channel type is github
	// 		2. AND ValidateSignature is true with the channel's secret token
	// 		3. AND channel path contains the repo full name from the event (this is verified in the actual event processing)
	//      4. AND channel has annotation webhookenabled="true"
	// If these conditions are not met, skip to the next subscription.
	chType := string(chobj.Spec.Type)

	if !utils.IsGitChannel(chType) {
		klog.V(2).Infof("The channel type is %s. Skipping to process this subscription.", chType)
		return false
	}

	if !strings.EqualFold(chobj.GetAnnotations()[appv1alpha1.AnnotationWebhookEnabled], "true") {
		klog.V(2).Infof("WebHook event listening is not enabled on the channel. Skipping to process this subscription.")
		return false
	}

	if signature != "" {
		if !listener.validateSecret(signature, chobj.GetAnnotations(), chNamespace, body) {
			klog.V(2).Infof("WebHook secret validation failed. Skipping to process this subscription.")
			return false
		}
	}

	return true
}

// ParseRequest parses incoming WebHook event request
func (listener *WebhookListener) ParseRequest(r *http.Request) (body []byte, signature string, event interface{}, err error) {
	var payload []byte

	switch contentType := r.Header.Get("Content-Type"); contentType {
	case "application/json":
		if body, err = ioutil.ReadAll(r.Body); err != nil {
			klog.Error("Failed to read the request body. error: ", err)
			return nil, "", nil, err
		}

		payload = body //the JSON payload
	case "application/x-www-form-urlencoded":
		if body, err = ioutil.ReadAll(r.Body); err != nil {
			klog.Error("Failed to read the request body. error: ", err)
			return nil, "", nil, err
		}

		form, err := url.ParseQuery(string(body))
		if err != nil {
			klog.Error("Failed to parse the request body. error: ", err)
			return nil, "", nil, err
		}

		payload = []byte(form.Get(payloadFormParam))
	default:
		klog.Warningf("Webhook request has unsupported Content-Type %q", contentType)
		return nil, "", nil, errors.New("Unsupported Content-Type: " + contentType)
	}

	defer r.Body.Close()

	signature = r.Header.Get(githubSignatureHeader)

	event, err = github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		klog.Error("could not parse webhook. error:", err)
		return nil, "", nil, err
	}

	return body, signature, event, nil
}

func (listener *WebhookListener) validateSecret(signature string, annotations map[string]string, chNamespace string, body []byte) (ret bool) {
	secret := listener.getWebhookSecret(annotations[appv1alpha1.AnnotationWebhookSecret], chNamespace)

	// Using the channel's webhook secret, validate it against the request's body
	if err := github.ValidateSignature(signature, body, []byte(secret)); err != nil {
		klog.Info("Failed to validate webhook event signature, error: ", err)
		// If validation fails, this webhook event is not for this subscription. Skip.
		ret = false
	}

	return ret
}
