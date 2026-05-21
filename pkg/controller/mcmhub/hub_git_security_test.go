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

package mcmhub

// Security tests for the path-traversal fix in sortClonedGitRepoGievnDestPath.

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
)

func TestSortClonedGitRepoGievnDestPath_PathTraversal(t *testing.T) {
	repoRoot := t.TempDir()

	traversalCases := []struct {
		name     string
		destPath string
	}{
		{name: "double dot-dot", destPath: "../../etc"},
		{name: "nested traversal", destPath: "a/../../../outside"},
		{name: "sibling", destPath: "../sibling"},
	}

	for _, tc := range traversalCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := sortClonedGitRepoGievnDestPath(repoRoot, tc.destPath, logr.Discard())
			if err == nil {
				t.Fatalf("sortClonedGitRepoGievnDestPath(%q, %q): expected traversal error, got nil",
					repoRoot, tc.destPath)
			}

			if !strings.Contains(err.Error(), "escapes the repository root") {
				t.Errorf("unexpected error (want 'escapes the repository root'): %v", err)
			}
		})
	}
}

func TestSortClonedGitRepoGievnDestPath_LegitimateSubPath(t *testing.T) {
	repoRoot := t.TempDir()

	// An empty sub-path (destPath == "") means "use repoRoot itself"; the path
	// equals the root so IsPathWithinRoot returns true and we proceed to
	// SortResources (which will succeed on an empty dir).
	_, err := sortClonedGitRepoGievnDestPath(repoRoot, "", logr.Discard())
	if err != nil && strings.Contains(err.Error(), "escapes the repository root") {
		t.Errorf("legitimate empty destPath should not trigger traversal error, got: %v", err)
	}
}
