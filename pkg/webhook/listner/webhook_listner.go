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

package listner

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"

	"github.com/google/go-github/v28/github"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/IBM/multicloud-operators-subscription/pkg/utils"

	chnv1alpha1 "github.com/IBM/multicloud-operators-channel/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

const (
	defaultKeyFile   = "/etc/subscription/tls.key"
	defaultCrtFile   = "/etc/subscription/tls.crt"
	payloadFormParam = "payload"
	signatureHeader  = "X-Hub-Signature"
)

// WebhookListner is a generic webhook event listner
type WebhookListner struct {
	localConfig   *rest.Config
	LocalClient   client.Client
	RemoteClient  client.Client
	DynamicClient dynamic.Interface
	TLSKeyFile    string
	TLSCrtFile    string
}

var webhookListner *WebhookListner

// Add does nothing for namespace subscriber, it generates cache for each of the item
func Add(mgr manager.Manager, hubconfig *rest.Config, tlsKeyFile, tlsCrtFile string) error {
	klog.V(4).Info("Setting up webhook listner ...")

	var err error
	webhookListner, err = CreateWebhookListner(mgr.GetConfig(), hubconfig, mgr.GetScheme(), tlsKeyFile, tlsCrtFile)

	if err != nil {
		klog.Error("Failed to create synchronizer. error: ", err)
		return err
	}

	return mgr.Add(webhookListner)
}

// Start the GutHub WebHook event listner
func (listner *WebhookListner) Start(l <-chan struct{}) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	http.HandleFunc("/webhook", listner.handleWebhook)

	if listner.TLSKeyFile != "" && listner.TLSCrtFile != "" {
		klog.Info("Starting the WebHook listner on port 8443 with TLS key and cert files: " + listner.TLSKeyFile + " " + listner.TLSCrtFile)
		klog.Fatal(http.ListenAndServeTLS(":8443", listner.TLSCrtFile, listner.TLSKeyFile, nil))
	} else {
		klog.Info("Starting the WebHook listner on port 8443 with no TLS.")
		klog.Fatal(http.ListenAndServe(":8443", nil))
	}

	klog.Info("the WebHook listner started on port 8443.")

	<-l

	return nil
}

// CreateWebhookListner creates a WebHook Listner instance
func CreateWebhookListner(config, remoteConfig *rest.Config, scheme *runtime.Scheme, tlsKeyFile, tlsCrtFile string) (*WebhookListner, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	var err error

	dynamicClient := dynamic.NewForConfigOrDie(config)

	l := &WebhookListner{
		DynamicClient: dynamicClient,
		localConfig:   config,
	}

	// The user-provided key and cert files take precedence over the default provided files if both sets exist.
	if _, err := os.Stat(defaultKeyFile); err == nil {
		l.TLSKeyFile = defaultKeyFile
	}

	if _, err := os.Stat(defaultCrtFile); err == nil {
		l.TLSCrtFile = defaultCrtFile
	}

	if _, err := os.Stat(tlsKeyFile); err == nil {
		l.TLSKeyFile = tlsKeyFile
	}

	if _, err := os.Stat(tlsCrtFile); err == nil {
		l.TLSCrtFile = tlsCrtFile
	}

	l.LocalClient, err = client.New(config, client.Options{})

	if err != nil {
		klog.Error("Failed to initialize client to update local status. error: ", err)
		return nil, err
	}

	l.RemoteClient = l.LocalClient
	if remoteConfig != nil {
		l.RemoteClient, err = client.New(remoteConfig, client.Options{})

		if err != nil {
			klog.Error("Failed to initialize client to update remote status. error: ", err)
			return nil, err
		}
	}

	return l, err
}

func (listner *WebhookListner) handleWebhook(w http.ResponseWriter, r *http.Request) {
	klog.V(4).Info("handleWebhook headers: ", r.Header)

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
		klog.V(4).Info("Evaluating subscription: " + sub.GetName())

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
			klog.V(4).Infof("The channel type is %s. Skipping to process this subscription.", chType)
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

func (listner *WebhookListner) updateSubscription(sub appv1alpha1.Subscription) {
	subAnnotations := sub.GetAnnotations()

	if subAnnotations["webhook-event"] == "" {
		subAnnotations["webhook-event"] = "0"
	} else {
		eventCounter, err := strconv.Atoi(subAnnotations["webhook-event"])
		if err != nil {
			subAnnotations["webhook-event"] = "0"
		} else {
			subAnnotations["webhook-event"] = strconv.Itoa(eventCounter + 1)
		}
	}

	sub.SetAnnotations(subAnnotations)
	newsub := sub.DeepCopy()

	err := listner.LocalClient.Update(context.TODO(), newsub)
	if err != nil {
		klog.Error("Failed to update subscription annotations. error: ", err)
	}
}

func (listner *WebhookListner) validateSecret(signature string, annotations map[string]string, chNamespace string, body []byte) (ret bool) {
	secret := ""
	ret = true
	// Get GitHub WebHook secret from the channel annotations
	if annotations["webhook-secret"] == "" {
		klog.Info("No webhook secret found in annotations")

		ret = false
	} else {
		seckey := types.NamespacedName{Name: annotations["webhook-secret"], Namespace: chNamespace}
		secobj := &corev1.Secret{}

		err := listner.RemoteClient.Get(context.TODO(), seckey, secobj)
		if err != nil {
			klog.Info("Failed to get secret for channel webhook listner, error: ", err)
			ret = false
		}

		err = yaml.Unmarshal(secobj.Data["secret"], &secret)
		if err != nil {
			klog.Info("Failed to unmarshal secret from the webhook secret. Skip this subscription, error: ", err)
			ret = false
		} else if secret == "" {
			klog.Info("Failed to get secret from the webhook secret. Skip this subscription, error: ", err)
			ret = false
		}
	}
	// Using the channel's webhook secret, validate it against the request's body
	if err := github.ValidateSignature(signature, body, []byte(secret)); err != nil {
		klog.Info("Failed to validate webhook event signature, error: ", err)
		// If validation fails, this webhook event is not for this subscription. Skip.
		ret = false
	}

	return ret
}

func parseRequest(r *http.Request) (body []byte, signature string, event interface{}, err error) {
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
		return
	}

	defer r.Body.Close()

	signature = r.Header.Get(signatureHeader)

	event, err = github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		klog.Error("could not parse webhook. error:", err)
		return nil, "", nil, err
	}

	return body, signature, event, nil
}
