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
	"net/url"
	"strings"
	"time"

	ocinfrav1 "github.com/openshift/api/config/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	authv1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
)

const (
	secretSuffix             = "-cluster-secret"
	requeuAfter              = 1
	infrastructureConfigName = "cluster"
	appAddonName             = "application-manager"
	sevenDays                = 168
)

var (
	appAddonNS = utils.GetComponentNamespace()
)

// Add creates a new agent token controller and adds it to the Manager if standalone is false.
func Add(mgr manager.Manager, hubconfig *rest.Config, syncid *types.NamespacedName, standalone bool) error {
	if !standalone {
		hubclient, err := client.New(hubconfig, client.Options{})

		if err != nil {
			klog.Error("Failed to generate client to hub cluster with error:", err)
			return err
		}

		return add(mgr, newReconciler(mgr, hubclient, syncid, mgr.GetConfig().Host))
	}

	return nil
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, hubclient client.Client, syncid *types.NamespacedName, host string) reconcile.Reconciler {
	rec := &ReconcileAgentToken{
		Client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		hubclient: hubclient,
		syncid:    syncid,
		host:      host,
	}

	return rec
}

type applicationManagerSecretMapper struct {
	client.Client
}

func (mapper *applicationManagerSecretMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request

	// reconcile App addon application-manager SA if its associated secret changes
	requests = append(requests, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: appAddonNS,
			Name:      appAddonName,
		},
	})

	klog.Infof("app addon SA secret changed: %v/%v", appAddonNS, appAddonName)

	return requests
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	klog.Info("Adding klusterlet token controller.")
	// Create a new controller
	c, err := controller.New("klusterlet-token-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to  App Addon application-manager service account.
	err = c.Watch(source.Kind(mgr.GetCache(), &corev1.ServiceAccount{}), &handler.EnqueueRequestForObject{}, utils.ServiceAccountPredicateFunctions)
	if err != nil {
		return err
	}

	// watch for changes to the secrets associated to the App Addon application-manager SA
	saSecretMapper := &applicationManagerSecretMapper{mgr.GetClient()}
	err = c.Watch(
		source.Kind(mgr.GetCache(), &corev1.Secret{}),
		handler.EnqueueRequestsFromMapFunc(saSecretMapper.Map),
		applicationManagerSecretPredicateFunctions)

	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileSubscription implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAgentToken{}

// ReconcileAppMgrToken syncid is the namespaced name of this managed cluster.
// host is the API server URL of this managed cluster.
type ReconcileAgentToken struct {
	client.Client
	hubclient client.Client
	scheme    *runtime.Scheme
	syncid    *types.NamespacedName
	host      string
}

type Config struct {
	BearerToken     string          `json:"bearerToken"`
	TLSClientConfig map[string]bool `json:"tlsClientConfig"`
}

// Reconciles <clusterName>-cluster-secret secret in the managed cluster's namespace
// on the hub cluster to the klusterlet-addon-appmgr service account's token secret.
// If it is running on the hub, don't do anything.
func (r *ReconcileAgentToken) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	klog.Infof("Reconciling %s", request.NamespacedName)

	appmgrsa := &corev1.ServiceAccount{}

	err := r.Client.Get(context.TODO(), request.NamespacedName, appmgrsa)

	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.Infof("SA %s is not found. Deleting the secret from the hub.", request.NamespacedName)

			err := r.hubclient.Delete(context.TODO(), r.prepareAgentTokenSecret(""))

			if err != nil {
				klog.Errorf("Failed to delete the secret from the hub. err: %v", err)
				return reconcile.Result{RequeueAfter: requeuAfter * time.Minute}, err
			}

			return reconcile.Result{}, nil
		}

		klog.Errorf("Failed to get serviceaccount %v, error: %v", request.NamespacedName, err)

		return reconcile.Result{RequeueAfter: requeuAfter * time.Minute}, err
	}

	// Get the service account token from the service account's secret list
	token, skipCreateSecret := r.getServiceAccountTokenSecret()

	if token == "" && !skipCreateSecret {
		klog.Error("Failed to find the service account token.")
		return reconcile.Result{RequeueAfter: requeuAfter * time.Minute}, errors.New("failed to find the klusterlet agent addon service account token secret")
	}

	if !skipCreateSecret {
		// Prepare the secret to be created/updated in the managed cluster namespace on the hub
		secret := r.prepareAgentTokenSecret(token)

		// Get the existing secret in the managed cluster namespace from the hub
		hubSecret := &corev1.Secret{}
		hubSecretName := types.NamespacedName{Namespace: r.syncid.Namespace, Name: r.syncid.Name + secretSuffix}
		err = r.hubclient.Get(context.TODO(), hubSecretName, hubSecret)

		if err != nil {
			if kerrors.IsNotFound(err) {
				klog.Info("Secret " + hubSecretName.String() + " not found on the hub.")

				err := r.hubclient.Create(context.TODO(), secret)

				if err != nil {
					klog.Error(err.Error())
					return reconcile.Result{RequeueAfter: requeuAfter * time.Minute}, err
				}

				klog.Info("The cluster secret " + secret.Name + " was created in " + secret.Namespace + " on the hub successfully.")
			} else {
				klog.Error("Failed to get secret from the hub: ", err)
				return reconcile.Result{RequeueAfter: requeuAfter * time.Minute}, err
			}
		} else {
			// Update
			err := r.hubclient.Update(context.TODO(), secret)

			if err != nil {
				klog.Error("Failed to update secret : ", err)
				return reconcile.Result{RequeueAfter: time.Duration(requeuAfter * time.Minute.Milliseconds())}, err
			}

			klog.Info("The cluster secret " + secret.Name + " was updated successfully in " + secret.Namespace + " on the hub.")
		}
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileAgentToken) prepareAgentTokenSecret(token string) *corev1.Secret {
	mcSecret := &corev1.Secret{}
	mcSecret.Name = r.syncid.Name + secretSuffix
	mcSecret.Namespace = r.syncid.Namespace

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
	data["name"] = r.syncid.Name
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

func (r *ReconcileAgentToken) getServiceAccountTokenSecret() (string, bool) {
	// Grab application-manager service account
	sa := &corev1.ServiceAccount{}

	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: appAddonName, Namespace: appAddonNS}, sa)
	if err != nil {
		klog.Error(err.Error())
		return "", false
	}

	// first loop through secret list from the application-manager SA to find application-manager-dockercfg secret
	for _, secret := range sa.Secrets {
		if strings.HasPrefix(secret.Name, "application-manager-dockercfg") {
			klog.Info("found the application-manager-dockercfg secret " + secret.Name)

			// application-manager-token secret is owned by the dockercfg secret
			dockerSecret := &corev1.Secret{}

			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: secret.Name, Namespace: appAddonNS}, dockerSecret)
			if err != nil {
				klog.Errorf("secret not found: %v/%v, err: %v", appAddonNS, secret.Name, err.Error())
				return "", false
			}

			anno := dockerSecret.GetAnnotations()

			if anno["openshift.io/token-secret.value"] > "" {
				klog.Info("found the application-manager-token secret " + anno["openshift.io/token-secret.name"])
				return anno["openshift.io/token-secret.value"], false
			}
		}
	}

	// If not found, check the secret associated to the application-manager SA
	skipCreateSecret, err := r.createOrUpdateApplicationManagerSecret()
	if err != nil {
		klog.Error(err.Error())
		return "", false
	}

	return "", skipCreateSecret
}

func (r *ReconcileAgentToken) createOrUpdateApplicationManagerSecret() (bool, error) {
	// For OCP >= 4.16 only as they stopped creating tokens for service accounts
	// We will create a short lived token(7 days) instead
	ManagedServiceAccount := &authv1beta1.ManagedServiceAccount{}
	ManagedServiceAccountName := types.NamespacedName{Namespace: r.syncid.Name, Name: appAddonName}
	err := r.hubclient.Get(context.TODO(), ManagedServiceAccountName, ManagedServiceAccount)

	// create the ManagedServiceAccount with the application-manager name if it doesn't exist
	if err != nil && kerrors.IsNotFound(err) {
		ManagedServiceAccount = &authv1beta1.ManagedServiceAccount{
			ObjectMeta: v1.ObjectMeta{
				Name:      appAddonName,
				Namespace: r.syncid.Name,
			},
			Spec: authv1beta1.ManagedServiceAccountSpec{
				Rotation: authv1beta1.ManagedServiceAccountRotation{
					Enabled: true,
					Validity: v1.Duration{
						Duration: time.Hour * sevenDays,
					},
				},
			},
		}

		err = r.hubclient.Create(context.TODO(), ManagedServiceAccount)
		if err != nil {
			klog.Errorf("failed to create the new application-manager managedserviceaccount, err: %v", err)
			return false, err
		}

		klog.Infof("Application-manager ManagedSeviceAccount %v created on the hub", appAddonName)

		// delete existing long-lived token if found on managed cluster
		ApplicationManagerSecret := &corev1.Secret{}
		ApplicationManagerSecretName := types.NamespacedName{Namespace: appAddonNS, Name: appAddonName}
		err = r.Client.Get(context.TODO(), ApplicationManagerSecretName, ApplicationManagerSecret)

		if err == nil {
			err = r.Client.Delete(context.TODO(), ApplicationManagerSecret)
			if err != nil {
				klog.Errorf("failed to delete existing application-manager secret, err: %v", err)
				return false, err
			}

			klog.Infof("Application-manager long lived token deleted on the managed cluster")
		} else {
			klog.Errorf("failed to get existing application-manager secret, err: %v", err) // no long lived token to clean up
			err = nil
		}

		// delete existing long-lived token if found on hub
		ApplicationManagerHubSecret := &corev1.Secret{}
		ApplicationManagerHubSecretName := types.NamespacedName{Namespace: r.syncid.Name, Name: r.syncid.Name + "-cluster-secret"}
		err = r.hubclient.Get(context.TODO(), ApplicationManagerHubSecretName, ApplicationManagerHubSecret)

		if err == nil {
			err = r.hubclient.Delete(context.TODO(), ApplicationManagerHubSecret)
			if err != nil {
				klog.Errorf("failed to delete existing %v secret on the hub, err: %v", r.syncid.Name+"-cluster-secret", err)
				return false, err
			}

			klog.Infof("Application-manager long lived token deleted on the hub cluster")
		} else {
			klog.Errorf("failed to get existing %v secret on the hub, err: %v", r.syncid.Name+"-cluster-secret", err) // no long lived token to clean up
			err = nil
		}
	} else if err != nil && !kerrors.IsNotFound(err) {
		klog.Errorf("failed to get managedserviceaccount, err: %v", err)
		return false, err
	}

	return true, err
}

// getKubeAPIServerAddress - Get the API server address from OpenShift kubernetes cluster. This does not work with other kubernetes.
func (r *ReconcileAgentToken) getKubeAPIServerAddress() (string, error) {
	infraConfig := &ocinfrav1.Infrastructure{}

	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: infrastructureConfigName}, infraConfig); err != nil {
		return "", err
	}

	return infraConfig.Status.APIServerURL, nil
}

// detect if there is any change to the secret associated to the App Addon application-manager SA.
var applicationManagerSecretPredicateFunctions = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		newSecret, ok := e.ObjectNew.(*corev1.Secret)
		if !ok {
			return false
		}

		if strings.EqualFold(newSecret.Namespace, appAddonNS) && newSecret.Type == "kubernetes.io/service-account-token" &&
			newSecret.GetAnnotations()["kubernetes.io/service-account.name"] == appAddonName {
			klog.Infof("secret updated: %v/%v", appAddonNS, appAddonName)

			return true
		}

		return false
	},
	CreateFunc: func(e event.CreateEvent) bool {
		newSecret, ok := e.Object.(*corev1.Secret)
		if !ok {
			return false
		}

		if strings.EqualFold(newSecret.Namespace, appAddonNS) && newSecret.Type == "kubernetes.io/service-account-token" &&
			newSecret.GetAnnotations()["kubernetes.io/service-account.name"] == appAddonName {
			klog.Infof("secret created: %v/%v", appAddonNS, appAddonName)

			return true
		}

		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		newSecret, ok := e.Object.(*corev1.Secret)
		if !ok {
			return false
		}

		if strings.EqualFold(newSecret.Namespace, appAddonNS) && newSecret.Type == "kubernetes.io/service-account-token" &&
			newSecret.GetAnnotations()["kubernetes.io/service-account.name"] == appAddonName {
			klog.Infof("secret deleted: %v/%v", appAddonNS, appAddonName)

			return true
		}

		return false
	},
}
