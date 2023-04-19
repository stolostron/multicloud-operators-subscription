// Copyright 2021 The Kubernetes Authors.
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
	"time"

	pflag "github.com/spf13/pflag"
)

// SubscriptionCMDOptions for command line flag parsing
type SubscriptionCMDOptions struct {
	MetricsAddr                 string
	KubeConfig                  string
	ClusterName                 string
	HubConfigFilePathName       string
	TLSKeyFilePathName          string
	TLSCrtFilePathName          string
	SyncInterval                int
	DisableTLS                  bool
	Standalone                  bool
	AgentImage                  string
	LeaseDurationSeconds        int
	LeaderElectionLeaseDuration time.Duration
	LeaderElectionRenewDeadline time.Duration
	LeaderElectionRetryPeriod   time.Duration
	Debug                       bool
	AgentInstallAll             bool
}

var Options = SubscriptionCMDOptions{
	MetricsAddr:                 "",
	KubeConfig:                  "",
	SyncInterval:                60,
	LeaseDurationSeconds:        60,
	LeaderElectionLeaseDuration: 137 * time.Second,
	LeaderElectionRenewDeadline: 107 * time.Second,
	LeaderElectionRetryPeriod:   26 * time.Second,
	Standalone:                  false,
	AgentImage:                  "quay.io/open-cluster-management/multicloud-operators-subscription:latest",
	Debug:                       false,
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
		&Options.KubeConfig,
		"kubeconfig",
		Options.KubeConfig,
		"The kube config that points to a external api server.",
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

	flag.DurationVar(
		&Options.LeaderElectionLeaseDuration,
		"leader-election-lease-duration",
		Options.LeaderElectionLeaseDuration,
		"The duration that non-leader candidates will wait after observing a leadership "+
			"renewal until attempting to acquire leadership of a led but unrenewed leader "+
			"slot. This is effectively the maximum duration that a leader can be stopped "+
			"before it is replaced by another candidate. This is only applicable if leader "+
			"election is enabled.",
	)

	flag.DurationVar(
		&Options.LeaderElectionRenewDeadline,
		"leader-election-renew-deadline",
		Options.LeaderElectionRenewDeadline,
		"The interval between attempts by the acting master to renew a leadership slot "+
			"before it stops leading. This must be less than or equal to the lease duration. "+
			"This is only applicable if leader election is enabled.",
	)

	flag.DurationVar(
		&Options.LeaderElectionRetryPeriod,
		"leader-election-retry-period",
		Options.LeaderElectionRetryPeriod,
		"The duration the clients should wait between attempting acquisition and renewal "+
			"of a leadership. This is only applicable if leader election is enabled.",
	)

	flag.BoolVar(
		&Options.Standalone,
		"standalone",
		Options.Standalone,
		"Standalone mode.",
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
		&Options.Debug,
		"debug",
		false,
		"if debug is true, hub github webhook listener will be disabled",
	)

	flag.BoolVar(
		&Options.DisableTLS,
		"disable-tls",
		Options.DisableTLS,
		"Disable TLS on WebHook event listener.",
	)

	flag.BoolVar(
		&Options.AgentInstallAll,
		"agent-install-all",
		false,
		"Configure the install strategy of agent on managed clusters. "+
			"Enabling this will automatically install agent on all managed cluster.")
}
