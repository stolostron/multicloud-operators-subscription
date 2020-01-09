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

package listener

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/go-github/v28/github"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chnv1alpha1 "github.com/IBM/multicloud-operators-channel/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

func (listner *WebhookListner) handleGithubWebhook(r *http.Request) {
	var body []byte

	var signature string

	var event interface{}

	var err error

	body, signature, event, err = parseRequest(r)
	if err != nil {
		klog.Error("Failed to parse the request. error:", err)
		return
	}

	subList := &appv1alpha1.SubscriptionList{}
	listopts := &client.ListOptions{}

	err = listner.LocalClient.List(context.TODO(), subList, listopts)
	if err != nil {
		klog.Error("Failed to get subscriptions. error: ", err)
		return
	}

	// Loop through all subscriptions
	for _, sub := range subList.Items {
		klog.V(2).Info("Evaluating subscription: " + sub.GetName())

		chNamespace := ""
		chName := ""
		chType := ""

		if sub.Spec.Channel != "" {
			strs := strings.Split(sub.Spec.Channel, "/")
			if len(strs) == 2 {
				chNamespace = strs[0]
				chName = strs[1]
			} else {
				klog.Info("Failed to get channel namespace and name.")
				continue
			}
		}

		chkey := types.NamespacedName{Name: chName, Namespace: chNamespace}
		chobj := &chnv1alpha1.Channel{}
		err := listner.RemoteClient.Get(context.TODO(), chkey, chobj)

		if err == nil {
			chType = string(chobj.Spec.Type)
		} else {
			klog.Error("Failed to get subscription's channel. error: ", err)
			return
		}

		// This WebHook event is applicable for this subscription if:
		// 		1. channel type is github
		// 		2. AND ValidateSignature is true with the channel's secret token
		// 		3. AND channel path contains the repo full name from the event
		// If these conditions are not met, skip to the next subscription.

		if !strings.EqualFold(chType, chnv1alpha1.ChannelTypeGitHub) {
			klog.V(2).Infof("The channel type is %s. Skipping to process this subscription.", chType)
			continue
		}

		if signature != "" {
			if !listner.validateSecret(signature, chobj.GetAnnotations(), chNamespace, body) {
				continue
			}
		}

		switch e := event.(type) {
		case *github.PullRequestEvent:
			if chobj.Spec.PathName == e.GetRepo().GetCloneURL() ||
				chobj.Spec.PathName == e.GetRepo().GetHTMLURL() ||
				chobj.Spec.PathName == e.GetRepo().GetURL() ||
				strings.Contains(chobj.Spec.PathName, e.GetRepo().GetFullName()) {
				klog.Info("Processing PUSH event from " + e.GetRepo().GetHTMLURL())
				listner.updateSubscription(sub)
			}
		case *github.PushEvent:
			if chobj.Spec.PathName == e.GetRepo().GetCloneURL() ||
				chobj.Spec.PathName == e.GetRepo().GetHTMLURL() ||
				chobj.Spec.PathName == e.GetRepo().GetURL() ||
				strings.Contains(chobj.Spec.PathName, e.GetRepo().GetFullName()) {
				klog.Info("Processing PUSH event from " + e.GetRepo().GetHTMLURL())
				listner.updateSubscription(sub)
			}
		default:
			klog.Infof("Unhandled event type %s\n", github.WebHookType(r))
			continue
		}
	}
}
