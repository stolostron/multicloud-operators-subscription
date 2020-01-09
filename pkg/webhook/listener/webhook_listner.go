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
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/ghodss/yaml"
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

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

const (
	defaultKeyFile   = "/etc/subscription/tls.key"
	defaultCrtFile   = "/etc/subscription/tls.crt"
	payloadFormParam = "payload"
	signatureHeader  = "X-Hub-Signature"
)

// WebhookListener is a generic webhook event listener
type WebhookListener struct {
	localConfig   *rest.Config
	LocalClient   client.Client
	RemoteClient  client.Client
	DynamicClient dynamic.Interface
	TLSKeyFile    string
	TLSCrtFile    string
}

var webhookListener *WebhookListener

// Add does nothing for namespace subscriber, it generates cache for each of the item
func Add(mgr manager.Manager, hubconfig *rest.Config, tlsKeyFile, tlsCrtFile string) error {
	klog.V(2).Info("Setting up webhook listener ...")

	var err error
	webhookListener, err = CreateWebhookListener(mgr.GetConfig(), hubconfig, mgr.GetScheme(), tlsKeyFile, tlsCrtFile)

	if err != nil {
		klog.Error("Failed to create synchronizer. error: ", err)
		return err
	}

	return mgr.Add(webhookListener)
}

// Start the GutHub WebHook event listener
func (listener *WebhookListener) Start(l <-chan struct{}) error {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	http.HandleFunc("/webhook", listener.handleWebhook)

	if listener.TLSKeyFile != "" && listener.TLSCrtFile != "" {
		klog.Info("Starting the WebHook listener on port 8443 with TLS key and cert files: " + listener.TLSKeyFile + " " + listener.TLSCrtFile)
		klog.Fatal(http.ListenAndServeTLS(":8443", listener.TLSCrtFile, listener.TLSKeyFile, nil))
	} else {
		klog.Info("Starting the WebHook listener on port 8443 with no TLS.")
		klog.Fatal(http.ListenAndServe(":8443", nil))
	}

	klog.Info("the WebHook listener started on port 8443.")

	<-l

	return nil
}

// CreateWebhookListener creates a WebHook listener instance
func CreateWebhookListener(config, remoteConfig *rest.Config, scheme *runtime.Scheme, tlsKeyFile, tlsCrtFile string) (*WebhookListener, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	var err error

	dynamicClient := dynamic.NewForConfigOrDie(config)

	l := &WebhookListener{
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

func (listener *WebhookListener) handleWebhook(w http.ResponseWriter, r *http.Request) {
	klog.V(2).Info("handleWebhook headers: ", r.Header)

	if r.Header.Get("X-Github-Event") != "" {
		// This is an event from a GitHub repository.
		listener.handleGithubWebhook(r)
	} else {
		klog.Info("handleWebhook headers: ", r.Header)
		klog.Info("Unsupported webhook event type.")
	}
}

func (listener *WebhookListener) updateSubscription(sub appv1alpha1.Subscription) {
	klog.V(2).Info("Updating annotations in subscription: " + sub.GetName())
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

	err := listener.LocalClient.Update(context.TODO(), newsub)
	if err != nil {
		klog.Error("Failed to update subscription annotations. error: ", err)
	}
}

func (listener *WebhookListener) validateSecret(signature string, annotations map[string]string, chNamespace string, body []byte) (ret bool) {
	secret := ""
	ret = true
	// Get GitHub WebHook secret from the channel annotations
	if annotations["webhook-secret"] == "" {
		klog.Info("No webhook secret found in annotations")

		ret = false
	} else {
		seckey := types.NamespacedName{Name: annotations["webhook-secret"], Namespace: chNamespace}
		secobj := &corev1.Secret{}

		err := listener.RemoteClient.Get(context.TODO(), seckey, secobj)
		if err != nil {
			klog.Info("Failed to get secret for channel webhook listener, error: ", err)
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
