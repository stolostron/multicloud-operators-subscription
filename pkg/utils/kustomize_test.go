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
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func Test_RunKustomizeBuild(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	out, err := RunKustomizeBuild("../../test/github/kustomize/overlays/inlinePatch")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Split the output of kustomize build output into individual kube resource YAML files
	resources := ParseYAML(out)
	for _, resource := range resources {
		resourceFile := []byte(strings.Trim(resource, "\t \n"))

		t := kubeResource{}
		err := yaml.Unmarshal(resourceFile, &t)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Check the kustomization was applied
		if t.Kind == "ConfigMap" {
			cm := &corev1.ConfigMap{}
			err = yaml.Unmarshal(resourceFile, &cm)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(cm.Data["altGreeting"]).To(gomega.Equal("Good Evening!"))
		}
	}
}
