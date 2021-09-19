/*
Copyright 2019 The Kubernetes Authors.

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
