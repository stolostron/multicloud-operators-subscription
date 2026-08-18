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
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func newTestRESTMapper() meta.RESTMapper {
	var defaultVersion []schema.GroupVersion
	restMapper := meta.NewDefaultRESTMapper(defaultVersion)

	restMapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	restMapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	restMapper.Add(
		schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		meta.RESTScopeRoot)
	restMapper.Add(
		schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		meta.RESTScopeRoot)
	restMapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)

	return restMapper
}

const namespacedManifest = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
  namespace: ns1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy1
  namespace: ns1
`

const clusterScopedManifest = namespacedManifest + `
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: attacker-crb
`

func TestClusterScopeGate_AdminAllowsClusterScopedResources(t *testing.T) {
	gate := newClusterScopeGate(newTestRESTMapper(), true)

	out, err := gate.Run(bytes.NewBufferString(clusterScopedManifest))

	assert.NoError(t, err)
	assert.Equal(t, clusterScopedManifest, out.String())
}

func TestClusterScopeGate_NonAdminBlocksClusterScopedResource(t *testing.T) {
	gate := newClusterScopeGate(newTestRESTMapper(), false)

	out, err := gate.Run(bytes.NewBufferString(clusterScopedManifest))

	assert.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "not deployed by a subscription admin")
	assert.Contains(t, err.Error(), "ClusterRoleBinding")
	assert.Contains(t, err.Error(), "attacker-crb")
}

func TestClusterScopeGate_NonAdminAllowsNamespacedOnlyManifest(t *testing.T) {
	gate := newClusterScopeGate(newTestRESTMapper(), false)

	out, err := gate.Run(bytes.NewBufferString(namespacedManifest))

	assert.NoError(t, err)
	assert.Equal(t, namespacedManifest, out.String())
}

func TestClusterScopeGate_NonAdminBlocksExplicitNamespaceResource(t *testing.T) {
	manifest := `
apiVersion: v1
kind: Namespace
metadata:
  name: attacker-ns
`
	gate := newClusterScopeGate(newTestRESTMapper(), false)

	out, err := gate.Run(bytes.NewBufferString(manifest))

	assert.Error(t, err)
	assert.Nil(t, out)
	assert.True(t, strings.Contains(err.Error(), "Namespace"))
}

func TestClusterScopeGate_UnresolvableKindIsBlockedAsPrecaution(t *testing.T) {
	manifest := `
apiVersion: example.com/v1
kind: SomeUnknownCRD
metadata:
  name: mystery
`
	gate := newClusterScopeGate(newTestRESTMapper(), false)

	out, err := gate.Run(bytes.NewBufferString(manifest))

	assert.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "unknown scope")
}

func TestClusterScopeGate_NilManifestIsNoop(t *testing.T) {
	gate := newClusterScopeGate(newTestRESTMapper(), false)

	out, err := gate.Run(nil)

	assert.NoError(t, err)
	assert.Nil(t, out)
}
