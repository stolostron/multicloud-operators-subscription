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

package tlsconfig

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsPermanentAPIServerError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "not found is permanent",
			err:  apierrors.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "cluster"),
			want: true,
		},
		{
			name: "no kind match is permanent",
			err: &meta.NoKindMatchError{
				GroupKind:        schema.GroupKind{Group: "config.openshift.io", Kind: "APIServer"},
				SearchedVersions: []string{"v1"},
			},
			want: true,
		},
		{
			name: "generic/transient error is not permanent",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "forbidden is not permanent (RBAC may not have propagated yet)",
			err:  apierrors.NewForbidden(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "cluster", errors.New("denied")),
			want: false,
		},
		{
			name: "nil error is not permanent",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isPermanentAPIServerError(tc.err))
		})
	}
}

// TestRetryAPIServerOp_PermanentErrorStopsImmediately proves the fix: a permanent error (e.g.
// the APIServer CRD doesn't exist because the cluster isn't OpenShift) must not burn the whole
// apiserverGetRetryTimeout retry window. op is called exactly once and the call returns quickly.
func TestRetryAPIServerOp_PermanentErrorStopsImmediately(t *testing.T) {
	callCount := 0
	permanentErr := apierrors.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "cluster")

	start := time.Now()
	err := retryAPIServerOp(context.Background(), "test op", func(_ context.Context) error {
		callCount++
		return permanentErr
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Equal(t, permanentErr, err)
	assert.Equal(t, 1, callCount, "op should not be retried for a permanent error")
	assert.Less(t, elapsed, apiserverGetRetryInterval, "should give up well before the first retry interval elapses")
}

// TestRetryAPIServerOp_TransientErrorRetriesUntilSuccess proves transient errors are still
// retried (unlike permanent errors) until op eventually succeeds.
func TestRetryAPIServerOp_TransientErrorRetriesUntilSuccess(t *testing.T) {
	callCount := 0

	err := retryAPIServerOp(context.Background(), "test op", func(_ context.Context) error {
		callCount++
		if callCount < 2 {
			return errors.New("connection refused")
		}

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "op should be retried after a transient error")
}

// TestRetryAPIServerOp_SuccessOnFirstTry covers the common case with no errors at all.
func TestRetryAPIServerOp_SuccessOnFirstTry(t *testing.T) {
	callCount := 0

	err := retryAPIServerOp(context.Background(), "test op", func(_ context.Context) error {
		callCount++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, callCount)
}
