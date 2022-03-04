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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//GetAccessToken retrieve the accessToken
func GetAccessToken(secret *corev1.Secret) string {
	if accessToken, ok := secret.Data["accessToken"]; ok {
		return string(accessToken)
	}

	return ""
}

//GetPassword retrieve the password
func GetPassword(secret *corev1.Secret) string {
	if password, ok := secret.Data["password"]; ok {
		return string(password)
	}

	return ""
}

//GetConfigMap search the config map containing the helm repo client configuration.
func GetConfigMap(client client.Client, parentNamespace string, configMapRef *corev1.ObjectReference) (configMap *corev1.ConfigMap, err error) {
	if configMapRef != nil {
		klog.V(5).Info("Retrieve configMap ", parentNamespace, "/", configMapRef.Name)

		// The secret is copied into the subscription namespace on managed cluster.
		// If namespace is not specified, look for the secret in the parent (subscription) namespace.
		// If namespace is specified, FIRST look for the secret in the specified namespace.
		//     THEN look for the secret in the parent (subscription) namespace.
		usingParentNs := false

		ns := configMapRef.Namespace

		if ns == "" {
			ns = parentNamespace
			usingParentNs = true
		}

		configMap = &corev1.ConfigMap{}

		err = client.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: configMapRef.Name}, configMap)
		if err != nil {
			if !usingParentNs && errors.IsNotFound(err) {
				// Now try to find the config map in subscription namespace
				err = client.Get(context.TODO(), types.NamespacedName{Namespace: parentNamespace, Name: configMapRef.Name}, configMap)

				if err != nil {
					return nil, err
				}

				klog.Info("configMap found ", "Name: ", configMapRef.Name, " in namespace: ", parentNamespace)
			} else {
				return nil, err
			}
		} else {
			klog.Info("ConfigMap found ", "Name:", configMapRef.Name, " in namespace: ", ns)
		}
	} else {
		klog.V(5).Info("no configMapRef defined ", "parentNamespace", parentNamespace)
	}

	return configMap, err
}

//GetSecret returns the secret to access the helm-repo
func GetSecret(client client.Client, parentNamespace string, secretRef *corev1.ObjectReference) (secret *corev1.Secret, err error) {
	if secretRef != nil {
		klog.V(5).Info("retrieve secret :", parentNamespace, "/", secretRef)

		// The secret is copied into the subscription namespace on managed cluster.
		// If namespace is not specified, look for the secret in the parent (subscription) namespace.
		// If namespace is specified, FIRST look for the secret in the specified namespace.
		//     THEN look for the secret in the parent (subscription) namespace.
		usingParentNs := false

		ns := secretRef.Namespace
		if ns == "" {
			ns = parentNamespace
			usingParentNs = true
		}

		secret = &corev1.Secret{}

		err = client.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: secretRef.Name}, secret)
		if err != nil {
			if !usingParentNs && errors.IsNotFound(err) {
				// Now try to find the secret in subscription namespace
				err = client.Get(context.TODO(), types.NamespacedName{Namespace: parentNamespace, Name: secretRef.Name}, secret)

				if err != nil {
					return nil, err
				}

				klog.Info("Secret found ", "Name: ", secretRef.Name, " in namespace: ", parentNamespace)
			} else {
				return nil, err
			}
		} else {
			klog.Info("Secret found ", "Name: ", secretRef.Name, " in namespace: ", ns)
		}
	} else {
		klog.V(5).Info("No secret defined at ", "parentNamespace", parentNamespace)
	}

	return secret, err
}
