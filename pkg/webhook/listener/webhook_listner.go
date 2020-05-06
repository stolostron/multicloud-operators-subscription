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
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"

	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

const (
	GithubEventHeader    = "X-Github-Event"
	BitbucketEventHeader = "X-Event-Key"
	GitlabEventHeader    = "X-Gitlab-Event"
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

	dir := "/root/certs"
	if strings.EqualFold(tlsKeyFile, "") || strings.EqualFold(tlsCrtFile, "") {
		err := utils.GenerateServerCerts(dir)

		if err != nil {
			klog.Error("Failed to generate a self signed certificate. error: ", err)
			return err
		}

		tlsKeyFile = filepath.Join(dir, "tls.key")
		tlsCrtFile = filepath.Join(dir, "tls.crt")
	}

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

	http.HandleFunc("/webhook", listener.HandleWebhook)

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

// HandleWebhook handles incoming webhook events
func (listener *WebhookListener) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	klog.Info("handleWebhook headers: ", r.Header)

	if r.Header.Get(GithubEventHeader) != "" {
		// This is an event from a GitHub repository.
		err := listener.handleGithubWebhook(r)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, err = w.Write([]byte(err.Error()))

			if err != nil {
				klog.Error(err.Error())
			}
		}
	} else if r.Header.Get(BitbucketEventHeader) != "" {
		// This is an event from a BitBucket repository.
		err := listener.handleBitbucketWebhook(r)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, err = w.Write([]byte(err.Error()))

			if err != nil {
				klog.Error(err.Error())
			}
		}
	} else if r.Header.Get(GitlabEventHeader) != "" {
		// This is an event from a GitLab repository.
		err := listener.handleGitlabWebhook(r)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, err = w.Write([]byte(err.Error()))

			if err != nil {
				klog.Error(err.Error())
			}
		}
	} else {
		klog.Info("handleWebhook headers: ", r.Header)
		klog.Info("Unsupported webhook event type.")
		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte("Unsupported webhook event type."))
		if err != nil {
			klog.Error(err.Error())
		}
	}
}

func (listener *WebhookListener) updateSubscription(sub appv1alpha1.Subscription) *appv1alpha1.Subscription {
	klog.V(2).Info("Updating annotations in subscription: " + sub.GetName())
	subAnnotations := sub.GetAnnotations()

	if subAnnotations == nil {
		subAnnotations = make(map[string]string)
		subAnnotations[appv1alpha1.AnnotationWebhookEventCount] = "0"
	} else if subAnnotations[appv1alpha1.AnnotationWebhookEventCount] == "" {
		subAnnotations[appv1alpha1.AnnotationWebhookEventCount] = "0"
	} else {
		eventCounter, err := strconv.Atoi(subAnnotations[appv1alpha1.AnnotationWebhookEventCount])
		if err != nil {
			subAnnotations[appv1alpha1.AnnotationWebhookEventCount] = "0"
		} else {
			subAnnotations[appv1alpha1.AnnotationWebhookEventCount] = strconv.Itoa(eventCounter + 1)
		}
	}

	sub.SetAnnotations(subAnnotations)
	newsub := sub.DeepCopy()

	err := listener.LocalClient.Update(context.TODO(), newsub)
	if err != nil {
		klog.Error("Failed to update subscription annotations. error: ", err)
	}

	return newsub
}
