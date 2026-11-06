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
	"crypto/tls"
	"errors"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
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

// TestTLSGroupsToCurveIDs covers the group -> curve mapping: recognized groups map in order,
// unrecognized ones are collected (and skipped), and an empty input yields no curves.
func TestTLSGroupsToCurveIDs(t *testing.T) {
	tests := []struct {
		name             string
		groups           []configv1.TLSGroup
		wantCurves       []tls.CurveID
		wantUnrecognized []configv1.TLSGroup
	}{
		{
			name:       "empty groups yields no curves",
			groups:     nil,
			wantCurves: nil,
		},
		{
			name: "all recognized classical and post-quantum groups map in order",
			groups: []configv1.TLSGroup{
				configv1.TLSGroupX25519MLKEM768,
				configv1.TLSGroupX25519,
				configv1.TLSGroupSecP256r1,
				configv1.TLSGroupSecP384r1,
				configv1.TLSGroupSecP521r1,
				configv1.TLSGroupSecP256r1MLKEM768,
				configv1.TLSGroupSecP384r1MLKEM1024,
			},
			wantCurves: []tls.CurveID{
				tls.X25519MLKEM768,
				tls.X25519,
				tls.CurveP256,
				tls.CurveP384,
				tls.CurveP521,
				tls.SecP256r1MLKEM768,
				tls.SecP384r1MLKEM1024,
			},
		},
		{
			name:             "unrecognized groups are skipped while recognized ones keep their order",
			groups:           []configv1.TLSGroup{configv1.TLSGroupSecP256r1, "BogusGroup", configv1.TLSGroupSecP384r1},
			wantCurves:       []tls.CurveID{tls.CurveP256, tls.CurveP384},
			wantUnrecognized: []configv1.TLSGroup{"BogusGroup"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			curves, unrecognized := tlsGroupsToCurveIDs(tc.groups)
			assert.Equal(t, tc.wantCurves, curves)
			assert.Equal(t, tc.wantUnrecognized, unrecognized)
		})
	}
}

// TestProfileSpecToTLSConfig_CurvePreferences verifies that the profile's groups flow into
// tls.Config.CurvePreferences, and that an absent groups list leaves it nil (Go defaults).
func TestProfileSpecToTLSConfig_CurvePreferences(t *testing.T) {
	intermediateCiphers := configv1.TLSProfiles[configv1.TLSProfileIntermediateType].Ciphers

	t.Run("predefined Intermediate carries its default groups", func(t *testing.T) {
		cfg := profileSpecToTLSConfig(configv1.TLSProfiles[configv1.TLSProfileIntermediateType])
		assert.Equal(t, []tls.CurveID{
			tls.X25519MLKEM768,
			tls.X25519,
			tls.CurveP256,
			tls.CurveP384,
		}, cfg.CurvePreferences)
	})

	t.Run("custom groups are honored", func(t *testing.T) {
		spec := &configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Ciphers:       intermediateCiphers,
			Groups:        []configv1.TLSGroup{configv1.TLSGroupSecP256r1, configv1.TLSGroupSecP384r1},
		}
		cfg := profileSpecToTLSConfig(spec)
		assert.Equal(t, []tls.CurveID{tls.CurveP256, tls.CurveP384}, cfg.CurvePreferences)
	})

	t.Run("no groups leaves CurvePreferences nil for Go defaults", func(t *testing.T) {
		spec := &configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Ciphers:       intermediateCiphers,
		}
		cfg := profileSpecToTLSConfig(spec)
		assert.Nil(t, cfg.CurvePreferences)
	})

	t.Run("nil spec falls back to Intermediate which has groups", func(t *testing.T) {
		cfg := profileSpecToTLSConfig(nil)
		assert.NotEmpty(t, cfg.CurvePreferences)
	})
}
