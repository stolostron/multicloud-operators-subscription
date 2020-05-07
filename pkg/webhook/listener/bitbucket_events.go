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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

const (
	RepoPushEvent          = "repo:push"
	PullRequestMergedEvent = "pullrequest:fulfilled"
)

type BitBucketPayload struct {
	Repository BitBucketRepository `json:"repository"`
}

type BitBucketRepository struct {
	Type  string `json:"type"`
	Links struct {
		Self struct {
			Href string `json:"href"`
		} `json:"self"`
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
		Avatar struct {
			Href string `json:"href"`
		} `json:"avatar"`
	} `json:"links"`
	FullName string `json:"full_name"`
	Name     string `json:"name"`
	Website  string `json:"website"`
}

func (listener *WebhookListener) handleBitbucketWebhook(r *http.Request) error {
	event := r.Header.Get(BitbucketEventHeader) // has to have value. webhook_listner ensures.

	klog.Info("Handling BitBucket webhook event: " + event)

	body, err := ioutil.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		klog.Error("Failed to parse the payload: ", err)
		return errors.New("failed to parse the payload")
	}

	var payload BitBucketPayload
	err = json.Unmarshal(body, &payload)

	if err != nil {
		klog.Error("Failed to parse the webhook event payload. error: ", err)
		return err
	}

	subList := &appv1alpha1.SubscriptionList{}
	listopts := &client.ListOptions{}

	err = listener.LocalClient.List(context.TODO(), subList, listopts)
	if err != nil {
		klog.Error("Failed to get subscriptions. error: ", err)
		return err
	}

	if strings.EqualFold(event, RepoPushEvent) || strings.EqualFold(event, PullRequestMergedEvent) { // process only push or PR merge events
		// Loop through all subscriptions
		for _, sub := range subList.Items {
			if !listener.processBitbucketEvent(sub, event, payload) {
				continue
			}
		}
	} else {
		klog.Infof("Unhandled webhook event %s\n", event)
		return nil
	}

	return nil
}

func (listener *WebhookListener) processBitbucketEvent(sub appv1alpha1.Subscription, event string, payload BitBucketPayload) bool {
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

	if !listener.validateChannel(chobj, "", chNamespace, []byte("")) {
		return false
	}

	if chobj.Spec.Pathname == payload.Repository.FullName ||
		chobj.Spec.Pathname == payload.Repository.Links.HTML.Href ||
		strings.Contains(chobj.Spec.Pathname, payload.Repository.Links.HTML.Href) {
		klog.Infof("Processing %s event from %s repository for subscription %s", event, payload.Repository.FullName, sub.Name)
		listener.updateSubscription(sub)
	}

	return true
}
