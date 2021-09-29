// Copyright 2019 The Kubernetes Authors.
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

package exec

import (
	pflag "github.com/spf13/pflag"
)

// SubscriptionCMDOptions for command line flag parsing
type SubscriptionCMDOptions struct {
	MetricsAddr           string
	ClusterName           string
	HubConfigFilePathName string
	TLSKeyFilePathName    string
	TLSCrtFilePathName    string
	SyncInterval          int
	DisableTLS            bool
	Standalone            bool
	DeployAgent           bool
	AgentImage            string
	LeaseDurationSeconds  int
}

var Options = SubscriptionCMDOptions{
	MetricsAddr:          "",
	SyncInterval:         60,
	LeaseDurationSeconds: 60,
	Standalone:           false,
	DeployAgent:          false,
	AgentImage:           "quay.io/open-cluster-management/multicloud-operators-subscription:latest",
}

// ProcessFlags parses command line parameters into Options
func ProcessFlags() {
	flag := pflag.CommandLine
	// add flags
	flag.StringVar(
		&Options.MetricsAddr,
		"metrics-addr",
		Options.MetricsAddr,
		"The address the metric endpoint binds to.",
	)

	flag.StringVar(
		&Options.HubConfigFilePathName,
		"hub-cluster-configfile",
		Options.HubConfigFilePathName,
		"Configuration file pathname to hub kubernetes cluster",
	)

	flag.StringVar(
		&Options.ClusterName,
		"cluster-name",
		Options.ClusterName,
		"Name of this endpoint.",
	)

	flag.IntVar(
		&Options.SyncInterval,
		"sync-interval",
		Options.SyncInterval,
		"The interval of housekeeping in seconds.",
	)

	flag.IntVar(
		&Options.LeaseDurationSeconds,
		"lease-duration",
		Options.LeaseDurationSeconds,
		"The lease duration in seconds.",
	)

	flag.BoolVar(
		&Options.Standalone,
		"standalone",
		Options.Standalone,
		"Standalone mode.",
	)

	flag.BoolVar(
		&Options.DeployAgent,
		"deploy-agent",
		Options.DeployAgent,
		"Deploy agent by hub controller.",
	)

	flag.StringVar(
		&Options.AgentImage,
		"agent-image",
		Options.AgentImage,
		"Image of the agent to be deployed on managed cluster.",
	)

	flag.StringVar(
		&Options.TLSKeyFilePathName,
		"tls-key-file",
		Options.TLSKeyFilePathName,
		"WebHook event listener TLS key file path.",
	)

	flag.StringVar(
		&Options.TLSCrtFilePathName,
		"tls-crt-file",
		Options.TLSCrtFilePathName,
		"WebHook event listener TLS cert file path.",
	)

	flag.BoolVar(
		&Options.DisableTLS,
		"disable-tls",
		Options.DisableTLS,
		"Disable TLS on WebHook event listener.",
	)
}
