/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetAccessToken retrieve the accessToken
func GetAccessToken(secret *corev1.Secret) string {
	if accessToken, ok := secret.Data["accessToken"]; ok {
		return string(accessToken)
	}

	return ""
}

// GetPassword retrieve the password
func GetPassword(secret *corev1.Secret) string {
	if password, ok := secret.Data["password"]; ok {
		return string(password)
	}

	return ""
}

// GetConfigMap search the config map containing the helm repo client configuration.
// The configMapRef.Namespace field is ignored: the ConfigMap is always read from
// the parent (HelmRelease) namespace to prevent a tenant from referencing a
// ConfigMap in another tenant's namespace via a directly-created HelmRelease.
func GetConfigMap(client client.Client, parentNamespace string, configMapRef *corev1.ObjectReference) (configMap *corev1.ConfigMap, err error) {
	if configMapRef != nil {
		klog.V(5).Info("Retrieve configMap ", parentNamespace, "/", configMapRef.Name)

		if configMapRef.Namespace != "" && configMapRef.Namespace != parentNamespace {
			klog.Warningf("ignoring configMapRef.namespace %q; reading ConfigMap %q from HelmRelease namespace %q",
				configMapRef.Namespace, configMapRef.Name, parentNamespace)
		}

		configMap = &corev1.ConfigMap{}

		err = client.Get(context.TODO(), types.NamespacedName{Namespace: parentNamespace, Name: configMapRef.Name}, configMap)
		if err != nil {
			return nil, err
		}

		klog.Info("ConfigMap found ", "Name:", configMapRef.Name, " in namespace: ", parentNamespace)
	} else {
		klog.V(5).Info("no configMapRef defined ", "parentNamespace", parentNamespace)
	}

	return configMap, err
}

// GetSecret returns the secret to access the helm-repo.
// The secretRef.Namespace field is ignored: the Secret is always read from the
// parent (HelmRelease) namespace to prevent a tenant from exfiltrating a Secret
// from another namespace by setting repo.secretRef.namespace on a
// directly-created HelmRelease (the controller fetches with cluster-admin and
// would otherwise transmit the Secret as Basic-Auth to a tenant-controlled URL).
func GetSecret(client client.Client, parentNamespace string, secretRef *corev1.ObjectReference) (secret *corev1.Secret, err error) {
	if secretRef != nil {
		klog.V(5).Info("retrieve secret :", parentNamespace, "/", secretRef)

		if secretRef.Namespace != "" && secretRef.Namespace != parentNamespace {
			klog.Warningf("ignoring secretRef.namespace %q; reading Secret %q from HelmRelease namespace %q",
				secretRef.Namespace, secretRef.Name, parentNamespace)
		}

		secret = &corev1.Secret{}

		err = client.Get(context.TODO(), types.NamespacedName{Namespace: parentNamespace, Name: secretRef.Name}, secret)
		if err != nil {
			return nil, err
		}

		klog.Info("Secret found ", "Name: ", secretRef.Name, " in namespace: ", parentNamespace)
	} else {
		klog.V(5).Info("No secret defined at ", "parentNamespace", parentNamespace)
	}

	return secret, err
}
