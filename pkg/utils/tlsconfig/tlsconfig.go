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
	"sync"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	cachedTLSConfig *tls.Config
	initOnce        sync.Once
)

const apiserverClusterName = "cluster"

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

// fetchTLSConfigFromCluster reads the APIServer "cluster" resource and converts its
// tlsSecurityProfile into a *tls.Config. Returns nil on any error (e.g. not OpenShift, NotFound).
func fetchTLSConfigFromCluster(ctx context.Context, cfg *rest.Config, scheme *runtime.Scheme) *tls.Config {
	kubeClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil
	}

	var apiServer configv1.APIServer
	if err := kubeClient.Get(ctx, types.NamespacedName{Name: apiserverClusterName}, &apiServer); err != nil {
		return nil
	}

	spec := getEffectiveProfileSpec(apiServer.Spec.TLSSecurityProfile)

	profileType := configv1.TLSProfileIntermediateType
	if p := apiServer.Spec.TLSSecurityProfile; p != nil {
		profileType = p.Type
	}

	klog.Infof("TLS security profile used: profileType=%s minTLSVersion=%s cipherSuiteCount=%d",
		profileType, spec.MinTLSVersion, len(spec.Ciphers))

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

// profileSpecToTLSConfig converts a TLSProfileSpec (min version + ciphers) into Go's *tls.Config.
// OpenShift uses OpenSSL-style cipher names; we convert to IANA then to Go constants.
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

	return crypto.SecureTLSConfig(config)
}
