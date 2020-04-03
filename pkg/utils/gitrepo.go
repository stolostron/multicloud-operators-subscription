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

package utils

import (
	"k8s.io/klog"

	"strings"

	"github.com/ghodss/yaml"
)

type kubeResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// ParseKubeResoures parses a YAML content and returns kube resources in byte array from the file
func ParseKubeResoures(file []byte) [][]byte {
	var ret [][]byte

	items := strings.Split(string(file), "---")

	for _, i := range items {
		item := []byte(strings.Trim(i, "\t \n"))

		t := kubeResource{}
		err := yaml.Unmarshal(item, &t)

		if err != nil {
			// Ignore item that cannot be unmarshalled..
			klog.Warning(err, "Failed to unmarshal YAML content")
			continue
		}

		if t.APIVersion == "" || t.Kind == "" {
			// Ignore item that does not have apiVersion or kind.
			klog.Warning("Not a Kubernetes resource", i)
		} else {
			ret = append(ret, item)
		}
	}

	return ret
}
