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
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	"helm.sh/helm/v3/pkg/postrender"
	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog"
)

// clusterScopeGate is a Helm PostRenderer that blocks a chart's rendered
// manifest from being applied if it contains cluster-scoped resources
// (e.g. ClusterRole, ClusterRoleBinding, Namespace, CustomResourceDefinition)
// and the owning Subscription was not created/updated by a subscription admin.
//
// Without this gate, a non-admin user could smuggle a cluster-scoped resource
// (such as a ClusterRoleBinding granting cluster-admin) inside a Helm chart
// and have the operator, which applies charts with elevated credentials,
// deploy it on their behalf.
type clusterScopeGate struct {
	restMapper meta.RESTMapper
	isAdmin    bool
}

// newClusterScopeGate returns a PostRenderer enforcing the cluster-scoped
// resource restriction described above. When isAdmin is true, it is a no-op.
func newClusterScopeGate(restMapper meta.RESTMapper, isAdmin bool) postrender.PostRenderer {
	return &clusterScopeGate{restMapper: restMapper, isAdmin: isAdmin}
}

func (g *clusterScopeGate) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	if g.isAdmin || renderedManifests == nil {
		return renderedManifests, nil
	}

	blocked, err := g.findClusterScopedResources(renderedManifests.String())
	if err != nil {
		return nil, err
	}

	if len(blocked) > 0 {
		return nil, fmt.Errorf("not deployed by a subscription admin. cluster-scoped resource(s) %s are not deployed",
			strings.Join(blocked, ", "))
	}

	return renderedManifests, nil
}

// findClusterScopedResources returns a human readable identifier for every
// cluster-scoped resource found in the manifest. If the scope of a resource
// kind cannot be determined (e.g. its CRD isn't registered with the API
// server yet), it is treated the same as an unresolvable GVK elsewhere in
// this codebase and reported as blocked, erring on the side of caution.
func (g *clusterScopeGate) findClusterScopedResources(manifest string) ([]string, error) {
	var blocked []string

	for _, doc := range releaseutil.SplitManifests(manifest) {
		if strings.TrimSpace(doc) == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), obj); err != nil {
			// Not a valid k8s object (e.g. a NOTES.txt fragment); let Helm's
			// own manifest handling surface any real errors downstream.
			continue
		}

		gvk := obj.GroupVersionKind()
		if gvk.Kind == "" {
			continue
		}

		mapping, err := g.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			klog.Warningf("cluster-scope gate: unable to determine scope of %s, blocking as a precaution: %v",
				gvk.String(), err)

			blocked = append(blocked, fmt.Sprintf("%s kind: %s name: %s (unknown scope)",
				obj.GetAPIVersion(), obj.GetKind(), obj.GetName()))

			continue
		}

		if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
			blocked = append(blocked, fmt.Sprintf("%s kind: %s name: %s",
				obj.GetAPIVersion(), obj.GetKind(), obj.GetName()))
		}
	}

	return blocked, nil
}
