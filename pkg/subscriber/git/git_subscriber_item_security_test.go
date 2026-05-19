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

package git

// Security tests for the path-traversal fix in sortClonedGitRepo.
//
// These tests verify that a Subscription whose git-path (or github-path)
// annotation contains ".." components is rejected with an error before
// SortResources ever walks the filesystem, and that a legitimate sub-path
// is accepted.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
)

// minimalSecurityItem builds a SubscriberItem with repoRoot set to a real
// temporary directory so that the legitimate-path test can also pass the
// IsPathWithinRoot guard. The synchronizer is a noopSyncSource (already
// defined in git_subscriber_item_lifecycle_test.go).
func minimalSecurityItem(t *testing.T, annotations map[string]string) *SubscriberItem {
	t.Helper()

	root := t.TempDir()

	return &SubscriberItem{
		SubscriberItem: appv1.SubscriberItem{
			Subscription: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "security-test-sub",
					Namespace:   "default",
					Annotations: annotations,
				},
			},
			Channel: &chnv1.Channel{
				Spec: chnv1.ChannelSpec{
					Pathname: "https://localhost:1/nonexistent.git",
				},
			},
		},
		repoRoot:     root,
		synchronizer: &noopSyncSource{fakeClient: fake.NewClientBuilder().Build()},
	}
}

func TestSortClonedGitRepo_PathTraversal(t *testing.T) {
	traversalPaths := []string{
		"../../etc",
		"../sibling",
		"subdir/../../..",
		"a/b/../../../outside",
	}

	for _, p := range traversalPaths {
		p := p
		t.Run("git-path="+p, func(t *testing.T) {
			item := minimalSecurityItem(t, map[string]string{
				appv1.AnnotationGitPath: p,
			})

			err := item.sortClonedGitRepo()
			if err == nil {
				t.Fatalf("sortClonedGitRepo with git-path=%q: expected traversal error, got nil", p)
			}

			if !strings.Contains(err.Error(), "escapes the repository root") {
				t.Errorf("sortClonedGitRepo with git-path=%q: unexpected error: %v", p, err)
			}
		})

		t.Run("github-path="+p, func(t *testing.T) {
			item := minimalSecurityItem(t, map[string]string{
				appv1.AnnotationGithubPath: p,
			})

			err := item.sortClonedGitRepo()
			if err == nil {
				t.Fatalf("sortClonedGitRepo with github-path=%q: expected traversal error, got nil", p)
			}

			if !strings.Contains(err.Error(), "escapes the repository root") {
				t.Errorf("sortClonedGitRepo with github-path=%q: unexpected error: %v", p, err)
			}
		})
	}
}

func TestSortClonedGitRepo_LegitimateSubPath(t *testing.T) {
	item := minimalSecurityItem(t, map[string]string{
		appv1.AnnotationGitPath: "charts/myapp",
	})

	// Create the subdirectory inside repoRoot so SortResources can walk it.
	subdir := filepath.Join(item.repoRoot, "charts", "myapp")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	err := item.sortClonedGitRepo()
	// SortResources may return an error for other reasons (empty dir, etc.)
	// but it must NOT return a path-traversal error.
	if err != nil && strings.Contains(err.Error(), "escapes the repository root") {
		t.Errorf("sortClonedGitRepo with legitimate git-path should not return traversal error, got: %v", err)
	}
}

func TestSortClonedGitRepo_FilterRefConfigMap_PathTraversal(t *testing.T) {
	// The FilterRef configmap data["path"] is the third input vector.
	// When PackageFilter.FilterRef is set and the configmap cannot be fetched,
	// the error surfaces from the API call, not the path check. We verify that
	// even when the SubscriptionConfigMap is pre-populated with a traversal
	// path, the containment guard fires.
	item := minimalSecurityItem(t, nil) // no annotations

	// Inject a pre-populated SubscriptionConfigMap (bypassing the k8s GET).
	item.SubscriberItem.SubscriptionConfigMap = &corev1.ConfigMap{
		Data: map[string]string{
			"path": "../../etc",
		},
	}

	err := item.sortClonedGitRepo()
	if err == nil {
		t.Fatal("sortClonedGitRepo with configmap path=../../etc: expected traversal error, got nil")
	}

	if !strings.Contains(err.Error(), "escapes the repository root") {
		t.Errorf("unexpected error: %v", err)
	}
}
