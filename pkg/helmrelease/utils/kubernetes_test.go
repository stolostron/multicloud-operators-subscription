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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetAccessToken(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"accessToken": []byte("accessToken"),
		},
	}
	pw := GetAccessToken(secret)
	assert.Equal(t, "accessToken", pw)

	secret = &corev1.Secret{
		Data: map[string][]byte{},
	}
	pw = GetAccessToken(secret)
	assert.Equal(t, "", pw)
}

func TestGetPassword(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"password": []byte("password"),
		},
	}
	pw := GetPassword(secret)
	assert.Equal(t, "password", pw)

	secret = &corev1.Secret{
		Data: map[string][]byte{},
	}
	pw = GetPassword(secret)
	assert.Equal(t, "", pw)
}

// TestGetSecretIgnoresCrossNamespaceRef verifies that secretRef.Namespace is
// ignored and the Secret is always read from the HelmRelease (parent)
// namespace, preventing cross-namespace Secret exfiltration via a
// tenant-authored HelmRelease.
func TestGetSecretIgnoresCrossNamespaceRef(t *testing.T) {
	victimSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "victim-ns"},
		Data:       map[string][]byte{"password": []byte("victim-password")},
	}
	tenantSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "tenant-ns"},
		Data:       map[string][]byte{"password": []byte("tenant-password")},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(victimSecret, tenantSecret).Build()

	// secretRef points at victim-ns, but parentNamespace is tenant-ns.
	ref := &corev1.ObjectReference{Name: "creds", Namespace: "victim-ns"}

	got, err := GetSecret(cl, "tenant-ns", ref)
	assert.NoError(t, err)
	assert.Equal(t, "tenant-ns", got.Namespace, "secret must be read from parent namespace")
	assert.Equal(t, "tenant-password", string(got.Data["password"]), "must not return cross-namespace secret data")

	// When no Secret exists in the parent namespace the lookup must fail
	// rather than fall through to the cross-namespace ref.
	cl2 := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(victimSecret).Build()
	_, err = GetSecret(cl2, "tenant-ns", ref)
	assert.Error(t, err, "must not fall back to cross-namespace lookup")
}
