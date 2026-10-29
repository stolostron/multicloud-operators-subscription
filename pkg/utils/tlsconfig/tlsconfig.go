// Copyright 2025 The Kubernetes Authors.
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

// Package tlsconfig builds TLS configuration from the OpenShift cluster's API Server.
//
// Usage:
//  1. At process startup (in main), call InitClusterTLSConfig(ctx, restConfig, scheme).
//     The scheme must have configv1.AddToScheme(scheme) called first.
//  2. Wherever you need a *tls.Config (servers or clients), call GetClusterTLSConfig().
//
// If the cluster is not OpenShift or the APIServer resource is missing, the default
// (Intermediate profile: TLS 1.2 min, safe ciphers) is used.
package tlsconfig

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	cachedTLSConfig *tls.Config
	initOnce        sync.Once
)

const (
	apiserverClusterName = "cluster"

	// apiserverGetRetryInterval/apiserverGetRetryTimeout bound how long we retry
	// reading the cluster's APIServer resource before falling back to Intermediate.
	apiserverGetRetryInterval = 10 * time.Second
	apiserverGetRetryTimeout  = 2 * time.Minute
)

// InitClusterTLSConfig loads the cluster's TLS profile once and caches it.
// Call this at startup (e.g. in each cmd's RunManager) after you have rest.Config.
// Later calls do nothing. Scheme must include OpenShift config types (configv1.AddToScheme).
func InitClusterTLSConfig(ctx context.Context, cfg *rest.Config, scheme *runtime.Scheme) {
	initOnce.Do(func() {
		if cfg != nil && scheme != nil {
			cachedTLSConfig = fetchTLSConfigFromCluster(ctx, cfg, scheme)
		}

		if cachedTLSConfig == nil {
			cachedTLSConfig = profileSpecToTLSConfig(configv1.TLSProfiles[configv1.TLSProfileIntermediateType])
		}
	})
}

// GetClusterTLSConfig returns the cached TLS config for the process.
// Use this for http.Server.TLSConfig, http.Transport.TLSClientConfig, webhook TLSOpts, etc.
// If InitClusterTLSConfig was never called, returns the default (Intermediate) profile.
func GetClusterTLSConfig() *tls.Config {
	if cachedTLSConfig != nil {
		return cachedTLSConfig
	}

	return profileSpecToTLSConfig(configv1.TLSProfiles[configv1.TLSProfileIntermediateType])
}

// isPermanentAPIServerError reports whether err reflects a condition that retrying will never
// resolve: the resource doesn't exist (e.g. the cluster is not OpenShift, so the APIServer CRD
// was never installed) or the API server has no route for its kind at all. Anything else
// (connection refused, timeout, RBAC not yet propagated, etc.) is treated as transient and
// worth retrying.
func isPermanentAPIServerError(err error) bool {
	return apierrors.IsNotFound(err) || meta.IsNoMatchError(err)
}

// retryAPIServerOp retries op every apiserverGetRetryInterval for up to apiserverGetRetryTimeout
// (trying immediately first), unless op fails with a permanent error (see
// isPermanentAPIServerError), in which case it gives up immediately instead of burning the whole
// retry window on a condition that will never change. desc is used only for logging. Returns
// op's last error, if any, once retrying stops.
func retryAPIServerOp(ctx context.Context, desc string, op func(ctx context.Context) error) error {
	var lastErr error

	pollErr := wait.PollUntilContextTimeout(ctx, apiserverGetRetryInterval, apiserverGetRetryTimeout, true,
		func(pollCtx context.Context) (bool, error) {
			lastErr = op(pollCtx)
			if lastErr == nil {
				return true, nil
			}

			if isPermanentAPIServerError(lastErr) {
				return false, lastErr
			}

			klog.Warningf("failed to %s, will retry: %v", desc, lastErr)

			return false, nil
		})

	if pollErr != nil {
		return lastErr
	}

	return nil
}

// fetchTLSConfigFromCluster reads the APIServer "cluster" resource and converts its
// tlsSecurityProfile into a *tls.Config. If creating the client or reading the resource fails
// with a transient error (e.g. RBAC not yet propagated, API server temporarily unreachable), it
// retries every apiserverGetRetryInterval for up to apiserverGetRetryTimeout. If the failure is
// permanent instead (the cluster is not OpenShift, or the resource genuinely doesn't exist), it
// gives up immediately rather than blocking for the whole retry window. Either way, once it
// gives up the error is logged and nil is returned so the caller falls back to Intermediate.
func fetchTLSConfigFromCluster(ctx context.Context, cfg *rest.Config, scheme *runtime.Scheme) *tls.Config {
	var kubeClient client.Client

	if err := retryAPIServerOp(ctx, "create client to read APIServer for TLS security profile",
		func(pollCtx context.Context) error {
			var err error
			kubeClient, err = client.New(cfg, client.Options{Scheme: scheme})

			return err
		}); err != nil {
		klog.Errorf("giving up creating client to read APIServer %q for TLS security profile, "+
			"falling back to %s profile: %v", apiserverClusterName, configv1.TLSProfileIntermediateType, err)

		return nil
	}

	var apiServer configv1.APIServer

	if err := retryAPIServerOp(ctx, fmt.Sprintf("get APIServer %q for TLS security profile", apiserverClusterName),
		func(pollCtx context.Context) error {
			return kubeClient.Get(pollCtx, types.NamespacedName{Name: apiserverClusterName}, &apiServer)
		}); err != nil {
		klog.Errorf("giving up reading APIServer %q for TLS security profile, falling back to %s profile: %v",
			apiserverClusterName, configv1.TLSProfileIntermediateType, err)

		return nil
	}

	spec := getEffectiveProfileSpec(apiServer.Spec.TLSSecurityProfile)

	profileType := configv1.TLSProfileIntermediateType
	if p := apiServer.Spec.TLSSecurityProfile; p != nil {
		profileType = p.Type
	}

	klog.Infof("TLS security profile used: profileType=%s minTLSVersion=%s cipherSuiteCount=%d groupCount=%d",
		profileType, spec.MinTLSVersion, len(spec.Ciphers), len(spec.Groups))

	return profileSpecToTLSConfig(spec)
}

// getEffectiveProfileSpec turns a TLSSecurityProfile (Old/Intermediate/Modern/Custom)
// into a concrete TLSProfileSpec (min version + cipher list). Nil or unknown → Intermediate.
func getEffectiveProfileSpec(profile *configv1.TLSSecurityProfile) *configv1.TLSProfileSpec {
	if profile == nil {
		return configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	if profile.Type == configv1.TLSProfileCustomType && profile.Custom != nil {
		return &profile.Custom.TLSProfileSpec
	}

	if spec := configv1.TLSProfiles[profile.Type]; spec != nil {
		return spec
	}

	return configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
}

// curveIDByTLSGroup maps an OpenShift TLSSecurityProfile group (supported group,
// formerly known as an elliptic curve) to Go's crypto/tls CurveID. It mirrors
// library-go's crypto.TLSGroupsToCurveIDs mapping, kept local so we do not have to
// bump library-go (the pinned version predates that helper). Groups absent from this
// map are ones Go's crypto/tls does not implement and are skipped.
var curveIDByTLSGroup = map[configv1.TLSGroup]tls.CurveID{
	configv1.TLSGroupX25519:             tls.X25519,
	configv1.TLSGroupSecP256r1:          tls.CurveP256,
	configv1.TLSGroupSecP384r1:          tls.CurveP384,
	configv1.TLSGroupSecP521r1:          tls.CurveP521,
	configv1.TLSGroupX25519MLKEM768:     tls.X25519MLKEM768,
	configv1.TLSGroupSecP256r1MLKEM768:  tls.SecP256r1MLKEM768,
	configv1.TLSGroupSecP384r1MLKEM1024: tls.SecP384r1MLKEM1024,
}

// tlsGroupsToCurveIDs converts the profile's groups into Go CurveIDs, returning the
// recognized curves (in the profile's order) and the group names Go does not support
// (for logging). Unrecognized groups are skipped rather than failing.
func tlsGroupsToCurveIDs(groups []configv1.TLSGroup) ([]tls.CurveID, []configv1.TLSGroup) {
	var (
		curves       []tls.CurveID
		unrecognized []configv1.TLSGroup
	)

	for _, g := range groups {
		if id, ok := curveIDByTLSGroup[g]; ok {
			curves = append(curves, id)
			continue
		}

		unrecognized = append(unrecognized, g)
	}

	return curves, unrecognized
}

// profileSpecToTLSConfig converts a TLSProfileSpec (min version + ciphers + groups) into Go's
// *tls.Config. OpenShift uses OpenSSL-style cipher names; we convert to IANA then to Go constants.
// Groups (supported curves) are mapped to CurvePreferences; when the profile lists none, we leave
// CurvePreferences nil so Go uses its own defaults (backward compatible). FIPS filtering of groups
// Go cannot use in FIPS mode (e.g. X25519MLKEM768) is handled by crypto/tls at handshake time, so
// we do not special-case it here.
func profileSpecToTLSConfig(spec *configv1.TLSProfileSpec) *tls.Config {
	if spec == nil {
		spec = configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}
	minVersion := crypto.TLSVersionOrDie(string(spec.MinTLSVersion))
	ianaCiphers := crypto.OpenSSLToIANACipherSuites(spec.Ciphers)
	cipherIDs := crypto.CipherSuitesOrDie(ianaCiphers)
	config := &tls.Config{
		MinVersion:   minVersion,
		CipherSuites: cipherIDs,
	}

	curves, unrecognized := tlsGroupsToCurveIDs(spec.Groups)
	if len(unrecognized) > 0 {
		klog.Warningf("ignoring TLS groups from cluster profile not supported by this build: %v", unrecognized)
	}

	if len(curves) > 0 {
		config.CurvePreferences = curves
	}

	return crypto.SecureTLSConfig(config)
}
