// Copyright 2026 The Kubernetes Authors.
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

package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	appsubv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
)

func newHelmReleaseCR(annotations map[string]string) *unstructured.Unstructured {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "apps.open-cluster-management.io", Version: "v1", Kind: "HelmRelease",
	})
	cr.SetName("test-release")
	cr.SetNamespace("test-ns")
	cr.SetAnnotations(annotations)

	return cr
}

// TestIsClusterAdminCR verifies that the manager factory correctly derives
// isAdmin from the apps.open-cluster-management.io/cluster-admin annotation
// copied onto the HelmRelease CR (see utils.CreateHelmCRManifest), which is
// what NewManager passes to the clusterScopeGate PostRenderer.
func TestIsClusterAdminCR(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{name: "annotation true", annotations: map[string]string{appsubv1.AnnotationClusterAdmin: "true"}, want: true},
		{name: "annotation True mixed case", annotations: map[string]string{appsubv1.AnnotationClusterAdmin: "True"}, want: true},
		{name: "annotation TRUE upper case", annotations: map[string]string{appsubv1.AnnotationClusterAdmin: "TRUE"}, want: true},
		{name: "annotation false", annotations: map[string]string{appsubv1.AnnotationClusterAdmin: "false"}, want: false},
		{name: "annotation missing", annotations: map[string]string{"other": "value"}, want: false},
		{name: "no annotations at all", annotations: nil, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cr := newHelmReleaseCR(tc.annotations)
			assert.Equal(t, tc.want, isClusterAdminCR(cr))
		})
	}
}
