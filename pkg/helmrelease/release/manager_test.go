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
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	cpb "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
)

// testActionConfig returns a Helm action.Configuration backed entirely by
// in-memory/no-op implementations (no real Kubernetes API server or network
// access required), mirroring the fixture Helm itself uses in
// pkg/action/action_test.go.
func testActionConfig(t *testing.T) *action.Configuration {
	t.Helper()

	registryClient, err := registry.NewClient()
	require.NoError(t, err)

	return &action.Configuration{
		Releases:       storage.Init(driver.NewMemory()),
		KubeClient:     &kubefake.PrintingKubeClient{Out: io.Discard},
		Capabilities:   chartutil.DefaultCapabilities,
		RegistryClient: registryClient,
		Log:            func(_ string, _ ...interface{}) {},
	}
}

func namespacedOnlyChart(name string) *cpb.Chart {
	return &cpb.Chart{
		Metadata: &cpb.Metadata{APIVersion: "v2", Name: name, Version: "0.1.0"},
		Templates: []*cpb.File{
			{Name: "templates/configmap.yaml", Data: []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
`)},
		},
	}
}

func clusterScopedChart(name string) *cpb.Chart {
	return &cpb.Chart{
		Metadata: &cpb.Metadata{APIVersion: "v2", Name: name, Version: "0.2.0"},
		Templates: []*cpb.File{
			{Name: "templates/configmap.yaml", Data: []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
`)},
			{Name: "templates/crb.yaml", Data: []byte(`
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: attacker-crb
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: User
  name: attacker
`)},
		},
	}
}

// TestManagerInstallRelease_NonAdminBlocksClusterScopedResource proves that
// manager.InstallRelease actually wires the clusterScopeGate PostRenderer in
// (via m.restMapper/m.isAdmin), not just that the gate works in isolation.
func TestManagerInstallRelease_NonAdminBlocksClusterScopedResource(t *testing.T) {
	m := &manager{
		actionConfig: testActionConfig(t),
		restMapper:   newTestRESTMapper(),
		isAdmin:      false,
		releaseName:  "test-release",
		namespace:    "ns1",
		chart:        clusterScopedChart("test-release"),
		values:       map[string]interface{}{},
	}

	_, err := m.InstallRelease(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not deployed by a subscription admin")
	assert.Contains(t, err.Error(), "ClusterRoleBinding")
}

// TestManagerInstallRelease_AdminAllowsClusterScopedResource is the admin
// counterpart: the same chart succeeds when isAdmin is true.
func TestManagerInstallRelease_AdminAllowsClusterScopedResource(t *testing.T) {
	m := &manager{
		actionConfig: testActionConfig(t),
		restMapper:   newTestRESTMapper(),
		isAdmin:      true,
		releaseName:  "test-release",
		namespace:    "ns1",
		chart:        clusterScopedChart("test-release"),
		values:       map[string]interface{}{},
	}

	rel, err := m.InstallRelease(context.Background())

	require.NoError(t, err)
	require.NotNil(t, rel)
	assert.Contains(t, rel.Manifest, "ClusterRoleBinding")
}

// TestManagerInstallRelease_NonAdminAllowsNamespacedOnlyChart verifies the
// gate doesn't get in the way of ordinary charts that contain no
// cluster-scoped resources at all.
func TestManagerInstallRelease_NonAdminAllowsNamespacedOnlyChart(t *testing.T) {
	m := &manager{
		actionConfig: testActionConfig(t),
		restMapper:   newTestRESTMapper(),
		isAdmin:      false,
		releaseName:  "test-release",
		namespace:    "ns1",
		chart:        namespacedOnlyChart("test-release"),
		values:       map[string]interface{}{},
	}

	rel, err := m.InstallRelease(context.Background())

	require.NoError(t, err)
	require.NotNil(t, rel)
	assert.Contains(t, rel.Manifest, "ConfigMap")
}

// TestManagerUpgradeRelease_NonAdminBlocksClusterScopedResource proves the
// gate is also wired into UpgradeRelease: an existing release may not be
// upgraded to a revision that introduces a cluster-scoped resource unless
// isAdmin is true.
func TestManagerUpgradeRelease_NonAdminBlocksClusterScopedResource(t *testing.T) {
	cfg := testActionConfig(t)

	installer := &manager{
		actionConfig: cfg,
		restMapper:   newTestRESTMapper(),
		isAdmin:      true,
		releaseName:  "test-release",
		namespace:    "ns1",
		chart:        namespacedOnlyChart("test-release"),
		values:       map[string]interface{}{},
	}
	_, err := installer.InstallRelease(context.Background())
	require.NoError(t, err)

	upgrader := &manager{
		actionConfig: cfg,
		restMapper:   newTestRESTMapper(),
		isAdmin:      false,
		releaseName:  "test-release",
		namespace:    "ns1",
		chart:        clusterScopedChart("test-release"),
		values:       map[string]interface{}{},
	}

	_, upgraded, err := upgrader.UpgradeRelease(context.Background())

	require.Error(t, err)
	assert.Nil(t, upgraded)
	assert.Contains(t, err.Error(), "not deployed by a subscription admin")
}

// TestManagerUpgradeRelease_AdminAllowsClusterScopedResource is the admin
// counterpart of the upgrade test above.
func TestManagerUpgradeRelease_AdminAllowsClusterScopedResource(t *testing.T) {
	cfg := testActionConfig(t)

	installer := &manager{
		actionConfig: cfg,
		restMapper:   newTestRESTMapper(),
		isAdmin:      true,
		releaseName:  "test-release",
		namespace:    "ns1",
		chart:        namespacedOnlyChart("test-release"),
		values:       map[string]interface{}{},
	}
	_, err := installer.InstallRelease(context.Background())
	require.NoError(t, err)

	upgrader := &manager{
		actionConfig: cfg,
		restMapper:   newTestRESTMapper(),
		isAdmin:      true,
		releaseName:  "test-release",
		namespace:    "ns1",
		chart:        clusterScopedChart("test-release"),
		values:       map[string]interface{}{},
	}

	_, upgraded, err := upgrader.UpgradeRelease(context.Background())

	require.NoError(t, err)
	require.NotNil(t, upgraded)
	assert.Contains(t, upgraded.Manifest, "ClusterRoleBinding")
}

// TestManagerGetCandidateRelease_EnforcesClusterScopeGate proves the private
// getCandidateRelease helper (used by Sync to compute isUpgradeRequired)
// also enforces the gate during its dry-run upgrade.
func TestManagerGetCandidateRelease_EnforcesClusterScopeGate(t *testing.T) {
	cfg := testActionConfig(t)

	installer := &manager{
		actionConfig: cfg,
		restMapper:   newTestRESTMapper(),
		isAdmin:      true,
		releaseName:  "test-release",
		namespace:    "ns1",
		chart:        namespacedOnlyChart("test-release"),
		values:       map[string]interface{}{},
	}
	_, err := installer.InstallRelease(context.Background())
	require.NoError(t, err)

	nonAdmin := manager{actionConfig: cfg, restMapper: newTestRESTMapper(), isAdmin: false}
	_, err = nonAdmin.getCandidateRelease("ns1", "test-release", clusterScopedChart("test-release"), map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not deployed by a subscription admin")

	admin := manager{actionConfig: cfg, restMapper: newTestRESTMapper(), isAdmin: true}
	candidate, err := admin.getCandidateRelease("ns1", "test-release", clusterScopedChart("test-release"), map[string]interface{}{})
	require.NoError(t, err)
	assert.Contains(t, candidate.Manifest, "ClusterRoleBinding")
}
