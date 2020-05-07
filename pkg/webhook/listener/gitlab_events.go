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
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ghodss/yaml"
	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

const (
	GitLabPushEvents         = "Push Hook"
	GitLabMergeRequestEvents = "Merge Request Hook"
	gitlabSignatureHeader    = "X-Gitlab-Token"
)

type GitLabPayload struct {
	Repository GitLabRepository `json:"repository"`
}

type GitLabRepository struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Homepage    string `json:"homepage"`
}

func (listener *WebhookListener) handleGitlabWebhook(r *http.Request) error {
	event := r.Header.Get(GitlabEventHeader) // has to have value. webhook_listner ensures.

	klog.Info("Handling GitLab webhook event: " + event)

	body, err := ioutil.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		klog.Error("Failed to parse the payload: ", err)
		return errors.New("failed to parse the payload")
	}

	var payload GitLabPayload
	err = json.Unmarshal(body, &payload)

	if err != nil {
		klog.Error("Failed to parse the webhook event payload. error: ", err)
		return err
	}

	secret := r.Header.Get(gitlabSignatureHeader)

	subList := &appv1alpha1.SubscriptionList{}
	listopts := &client.ListOptions{}

	err = listener.LocalClient.List(context.TODO(), subList, listopts)
	if err != nil {
		klog.Error("Failed to get subscriptions. error: ", err)
		return err
	}

	if strings.EqualFold(event, GitLabPushEvents) || strings.EqualFold(event, GitLabMergeRequestEvents) { // process only push or PR merge events
		// Loop through all subscriptions
		for _, sub := range subList.Items {
			if !listener.processGitLabEvent(sub, event, payload, secret) {
				continue
			}
		}
	} else {
		klog.Infof("Unhandled webhook event %s\n", event)
		return nil
	}

	return nil
}

func (listener *WebhookListener) processGitLabEvent(sub appv1alpha1.Subscription, event string, payload GitLabPayload, hookSecret string) bool {
	klog.Info("Evaluating subscription: " + sub.GetName())

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

	if !listener.validateChannel(chobj, "", chNamespace, []byte("")) {
		klog.Info("Failed to validate channel: ")
		return false
	}

	chnAnnotations := chobj.GetAnnotations()
	channelSecret := ""

	if chnAnnotations != nil {
		channelSecret = listener.getWebhookSecret(chnAnnotations[appv1alpha1.AnnotationWebhookSecret], chNamespace)
	}

	if (strings.EqualFold(chobj.Spec.Pathname, payload.Repository.Homepage) ||
		strings.Contains(chobj.Spec.Pathname, payload.Repository.Homepage)) &&
		strings.TrimSpace(payload.Repository.Homepage) != "" &&
		strings.EqualFold(channelSecret, hookSecret) {
		klog.Infof("Processing %s event from %s repository for subscription %s", event, payload.Repository.URL, sub.Name)
		listener.updateSubscription(sub)
	}

	return true
}

func (listener *WebhookListener) getWebhookSecret(channelSecret, channelNs string) string {
	secret := ""
	// Get WebHook secret from the channel annotations
	if channelSecret == "" {
		klog.Info("No webhook secret found in annotations")
	} else {
		seckey := types.NamespacedName{Name: channelSecret, Namespace: channelNs}
		secobj := &corev1.Secret{}

		err := listener.RemoteClient.Get(context.TODO(), seckey, secobj)
		if err != nil {
			klog.Info("Failed to get secret for channel webhook listener, error: ", err)
		}

		err = yaml.Unmarshal(secobj.Data["secret"], &secret)
		if err != nil {
			klog.Info("Failed to unmarshal secret from the webhook secret. Skip this subscription, error: ", err)
		}
	}

	return secret
}
