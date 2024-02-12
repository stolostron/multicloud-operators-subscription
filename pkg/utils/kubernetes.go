// Copyright 2021 The Kubernetes Authors.
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

package utils

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

func NamespacedNameFormat(str string) types.NamespacedName {
	nn := types.NamespacedName{}

	if str != "" {
		strs := strings.Split(str, "/")
		if len(strs) != 2 {
			errmsg := "Illegal string, want namespace/name, but get " + str
			klog.Error(errmsg)

			return nn
		}

		nn.Name = strs[1]
		nn.Namespace = strs[0]
	}

	return nn
}

// ConvertLabels coverts label selector to lables.Selector
func ConvertLabels(labelSelector *metav1.LabelSelector) (labels.Selector, error) {
	if labelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(labelSelector)

		if err != nil {
			return labels.Nothing(), err
		}

		return selector, nil
	}

	return labels.Everything(), nil
}

func GetComponentNamespace() (string, error) {
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "open-cluster-management-agent-addon", err
	}

	return string(nsBytes), nil
}

// GetCheckSum generates a checksum of a kube config file
func GetCheckSum(kubeconfigfile string) ([32]byte, error) {
	content, err := os.ReadFile(filepath.Clean(kubeconfigfile))
	if err != nil {
		return [32]byte{}, fmt.Errorf("read %s failed, %w", kubeconfigfile, err)
	}

	return sha256.Sum256(content), nil
}
