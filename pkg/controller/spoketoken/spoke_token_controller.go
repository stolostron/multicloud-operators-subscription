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

package spoketoken

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	ocinfrav1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

const (
	secretSuffix             = "-cluster-secret"
	infrastructureConfigName = "cluster"
)

type ReconcileSpokeToken struct {
	client.Client
	hubclient client.Client
	Interval  int
	Syncid    *types.NamespacedName
}

func Add(mgr manager.Manager, interval int, hubconfig *rest.Config, syncid *types.NamespacedName, standalone bool) error {
	if !standalone {
		hubclient, err := client.New(hubconfig, client.Options{})

		if err != nil {
			klog.Error("Failed to generate client to hub cluster with error:", err)

			return err
		}

		dsRS := &ReconcileSpokeToken{
			Client:    mgr.GetClient(),
			hubclient: hubclient,
			Interval:  interval,
			Syncid:    syncid,
		}

		return mgr.Add(dsRS)
	}

	return fmt.Errorf("spoke token controller can't run in standalone mode")
}

func (r *ReconcileSpokeToken) Start(ctx context.Context) error {
	go wait.Until(func() {
		r.houseKeeping(ctx)
	}, time.Duration(r.Interval)*time.Second, ctx.Done())

	return nil
}

func (r *ReconcileSpokeToken) houseKeeping(ctx context.Context) {
	// possibly do something else here.
	r.doStuff(ctx)
}

func (r *ReconcileSpokeToken) doStuff(ctx context.Context) {
	appManager := types.NamespacedName{Name: "application-manager", Namespace: "open-cluster-management-agent-addon"}
	klog.Infof("Reconciling %s", appManager)

	appmgrsa := &corev1.ServiceAccount{}

	err := r.Client.Get(ctx, appManager, appmgrsa)

	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.Infof("%s is not found. Deleting the secret from the hub.", appManager)

			err := r.hubclient.Delete(ctx, r.prepareAgentTokenSecret(""))

			if err != nil {
				klog.Error("Failed to delete the secret from the hub.")

				return
			}

			return
		}

		klog.Errorf("Failed to get serviceaccount %v, error: %v", appManager, err)

		return
	}

	// Get the service account token from the service account's secret list
	token := r.getServiceAccountTokenSecret()

	if token == "" {
		klog.Error("Failed to find the service account token.")

		return
	}

	// Prepare the secret to be created/updated in the managed cluster namespace on the hub
	secret := r.prepareAgentTokenSecret(token)

	// Get the existing secret in the managed cluster namespace from the hub
	hubSecret := &corev1.Secret{}
	hubSecretName := types.NamespacedName{Namespace: r.Syncid.Namespace, Name: r.Syncid.Name + secretSuffix}
	err = r.hubclient.Get(ctx, hubSecretName, hubSecret)

	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.Info("Secret " + hubSecretName.String() + " not found on the hub.")

			err := r.hubclient.Create(ctx, secret)

			if err != nil {
				klog.Error(err.Error())

				return
			}

			klog.Info("The cluster secret " + secret.Name + " was created in " + secret.Namespace + " on the hub successfully.")
		} else {
			klog.Error("Failed to get secret from the hub: ", err)
		}
	} else {
		// Update
		err := r.hubclient.Update(ctx, secret)

		if err != nil {
			klog.Error("Failed to update secret : ", err)

			return
		}

		klog.Info("The cluster secret " + secret.Name + " was updated successfully in " + secret.Namespace + " on the hub.")
	}
}

type Config struct {
	BearerToken     string          `json:"bearerToken"`
	TLSClientConfig map[string]bool `json:"tlsClientConfig"`
}

func (r *ReconcileSpokeToken) prepareAgentTokenSecret(token string) *corev1.Secret {
	mcSecret := &corev1.Secret{}
	mcSecret.Name = r.Syncid.Name + secretSuffix
	mcSecret.Namespace = r.Syncid.Namespace

	labels := make(map[string]string)
	labels["argocd.argoproj.io/secret-type"] = "cluster"
	labels["apps.open-cluster-management.io/secret-type"] = "acm-cluster"

	configData := &Config{}
	configData.BearerToken = token
	tlsClientConfig := make(map[string]bool)
	tlsClientConfig["insecure"] = true
	configData.TLSClientConfig = tlsClientConfig

	jsonConfigData, err := json.MarshalIndent(configData, "", "  ")

	if err != nil {
		klog.Error(err)
	}

	apiServerURL, err := r.getKubeAPIServerAddress()

	if err != nil {
		klog.Error(err)
	}

	data := make(map[string]string)
	data["name"] = r.Syncid.Name
	data["server"] = apiServerURL
	data["config"] = string(jsonConfigData)

	mcSecret.StringData = data

	labels["apps.open-cluster-management.io/cluster-name"] = data["name"]

	u, err := url.Parse(data["server"])
	if err != nil {
		klog.Error(err)
	}

	truncatedServerURL := utils.ValidateK8sLabel(u.Hostname())

	if truncatedServerURL == "" {
		klog.Error("Invalid hostname in the API URL:", u)
	} else {
		labels["apps.open-cluster-management.io/cluster-server"] = truncatedServerURL
	}

	klog.Infof("managed cluster secret label: %v", labels)
	mcSecret.SetLabels(labels)

	return mcSecret
}

func (r *ReconcileSpokeToken) getServiceAccountTokenSecret() string {
	// Grab application-manager service account
	sa := &corev1.ServiceAccount{}

	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "application-manager", Namespace: "open-cluster-management-agent-addon"}, sa)
	if err != nil {
		klog.Error(err.Error())
		return ""
	}

	// loop through secrets to find application-manager-dockercfg secret
	for _, secret := range sa.Secrets {
		if strings.HasPrefix(secret.Name, "application-manager-dockercfg") {
			klog.Info("found the application-manager-dockercfg secret " + secret.Name)

			// application-manager-token secret is owned by the dockercfg secret
			dockerSecret := &corev1.Secret{}

			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: secret.Name, Namespace: "open-cluster-management-agent-addon"}, dockerSecret)
			if err != nil {
				klog.Error(err.Error())
				return ""
			}

			anno := dockerSecret.GetAnnotations()
			klog.Info("found the application-manager-token secret " + anno["openshift.io/token-secret.name"])

			return anno["openshift.io/token-secret.value"]
		}
	}

	return ""
}

// getKubeAPIServerAddress - Get the API server address from OpenShift kubernetes cluster. This does not work with other kubernetes.
func (r *ReconcileSpokeToken) getKubeAPIServerAddress() (string, error) {
	infraConfig := &ocinfrav1.Infrastructure{}

	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: infrastructureConfigName}, infraConfig); err != nil {
		return "", err
	}

	return infraConfig.Status.APIServerURL, nil
}
