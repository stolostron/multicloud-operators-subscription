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
	"crypto/tls"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/apis"
	appsubv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/placementrule/controller"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/placementrule/utils"
	appsubutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	k8swebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost = "0.0.0.0"
	metricsPort = 8383
)

// RunManager starts the actual manager
func RunManager() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	enableLeaderElection := false

	if _, err := rest.InClusterConfig(); err == nil {
		klog.Info("LeaderElection enabled as running in a cluster")

		enableLeaderElection = true
	} else {
		klog.Info("LeaderElection disabled as not running in cluster")
	}

	klog.Info("kubeconfig:" + options.KubeConfig)

	// Create a new Cmd to provide shared dependencies and start components
	var err error

	cfg := ctrl.GetConfigOrDie()

	if options.KubeConfig != "" {
		cfg, err = appsubutils.GetClientConfigFromKubeConfig(options.KubeConfig)

		if err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}
	}

	cfg.QPS = 30.0
	cfg.Burst = 60

	klog.Info("Leader election settings",
		"leaseDuration", options.LeaderElectionLeaseDuration,
		"renewDeadline", options.LeaderElectionRenewDeadline,
		"retryPeriod", options.LeaderElectionRetryPeriod)

	webhookOption := k8swebhook.Options{}
	webhookOption.TLSOpts = append(webhookOption.TLSOpts, func(config *tls.Config) {
		config.MinVersion = appsubv1.TLSMinVersionInt
	})
	webhookServer := k8swebhook.NewServer(webhookOption)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Metrics: metricsserver.Options{
			BindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		},
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "multicloud-operators-placementrule-leader.open-cluster-management.io",
		LeaderElectionNamespace: "kube-system",
		LeaseDuration:           &options.LeaderElectionLeaseDuration,
		RenewDeadline:           &options.LeaderElectionRenewDeadline,
		RetryPeriod:             &options.LeaderElectionRetryPeriod,
		WebhookServer:           webhookServer,
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{&corev1.Secret{}, &corev1.ServiceAccount{}, &corev1.ConfigMap{}},
			},
		},
	})

	if err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	klog.Infof("server endpoint: %v", mgr.GetConfig().Host)

	klog.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	sig := signals.SetupSignalHandler()

	klog.Info("Detecting ACM managed cluster API ...")
	utils.DetectClusterRegistry(sig, mgr.GetAPIReader())

	klog.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(sig); err != nil {
		klog.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}
