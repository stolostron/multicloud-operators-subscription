// Copyright 2018 The Operator-SDK Authors
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

// copied from github.com/operator-framework/operator-sdk/internal/util/k8sutil

package k8sutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestSupportsOwnerReference(t *testing.T) {
	type testcase struct {
		name       string
		restMapper meta.RESTMapper
		owner      runtime.Object
		dependent  runtime.Object
		result     bool
	}

	var defaultVersion []schema.GroupVersion
	restMapper := meta.NewDefaultRESTMapper(defaultVersion)

	GVK1 := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1alpha1",
		Kind:    "MyNamespaceKind",
	}
	GVK2 := schema.GroupVersionKind{
		Group:   "rbac",
		Version: "v1alpha1",
		Kind:    "MyClusterKind",
	}

	restMapper.Add(GVK1, meta.RESTScopeNamespace)
	restMapper.Add(GVK2, meta.RESTScopeRoot)

	cases := []testcase{
		{
			name:       "Returns false when owner is Namespaced and dependent resource is Clusterscoped.",
			restMapper: restMapper,
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyNamespaceKind",
					"apiVersion": "apps/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-controller",
						"namespace": "ns",
					},
				},
			},
			dependent: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyClusterKind",
					"apiVersion": "rbac/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-role",
						"namespace": "ns",
					},
				},
			},
			result: false,
		},
		{
			name:       "Returns true for owner and dependant are both ClusterScoped.",
			restMapper: restMapper,
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyClusterKind",
					"apiVersion": "rbac/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-controller",
						"namespace": "ns",
					},
				},
			},
			dependent: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyClusterKind",
					"apiVersion": "rbac/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-role",
						"namespace": "ns",
					},
				},
			},
			result: true,
		},
		{
			name:       "Returns true when owner and dependant are Namespaced with in same namespace.",
			restMapper: restMapper,
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyNamespaceKind",
					"apiVersion": "apps/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-controller",
						"namespace": "ns",
					},
				},
			},
			dependent: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyNamespaceKind",
					"apiVersion": "apps/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-role",
						"namespace": "ns",
					},
				},
			},
			result: true,
		},
		{
			name:       "Returns false when owner,and dependant are Namespaced, with different namespaces.",
			restMapper: restMapper,
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyNamespaceKind",
					"apiVersion": "apps/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-controller",
						"namespace": "ns1",
					},
				},
			},
			dependent: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyNamespaceKind",
					"apiVersion": "apps/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-role",
						"namespace": "ns",
					},
				},
			},
			result: false,
		},
		{
			name:       "Returns false for invalid Owner Kind.",
			restMapper: restMapper,
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "Dummy",
					"apiVersion": "apps/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-controller",
						"namespace": "ns1",
					},
				},
			},
			dependent: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyNamespaceKind",
					"apiVersion": "apps/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-role",
						"namespace": "ns",
					},
				},
			},
			result: false,
		},
		{
			name:       "Returns false for invalid dependant Kind.",
			restMapper: restMapper,
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "MyNamespaceKind",
					"apiVersion": "apps/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-controller",
						"namespace": "ns1",
					},
				},
			},
			dependent: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "Dummy",
					"apiVersion": "apps/v1alpha1",
					"metadata": map[string]interface{}{
						"name":      "example-nginx-role",
						"namespace": "ns",
					},
				},
			},
			result: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useOwner, err := SupportsOwnerReference(c.restMapper, c.owner, c.dependent)
			if err != nil {
				assert.Error(t, err)
			}
			assert.Equal(t, c.result, useOwner)
		})
	}
}
