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
	"fmt"
	"os"
	"strings"

	"github.com/operator-framework/operator-sdk/pkg/k8sutil"

	"github.com/prometheus/common/log"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/controller"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/webhook"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost             = "0.0.0.0"
	metricsPort         int = 8381
	operatorMetricsPort int = 8684
)

func RunManager() {

	// Get watch namespace setting of controller
	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		log.Error(err, " - Failed to get watch namespace")
		os.Exit(1)
	}
	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		Namespace:          namespace,
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
	})
	if err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// id is the namespacedname of this cluster in hub
	var id = &types.NamespacedName{
		Name:      Options.ClusterName,
		Namespace: Options.ClusterNamespace,
	}

	// generate config to hub cluster
	hubconfig := mgr.GetConfig()
	if Options.HubConfigFilePathName != "" {
		hubconfig, err = clientcmd.BuildConfigFromFlags("", Options.HubConfigFilePathName)

		if err != nil {
			klog.Error("Failed to build config to hub cluster with the pathname provided ", Options.HubConfigFilePathName, " err:", err)
			os.Exit(1)
		}
	}

	klog.Info("Starting ... Registering Components for cluster: ", id)

	// Setup ansibleJob Scheme for manager
	if err := ansiblejob.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	if !Options.Standalone && Options.ClusterName == "" && Options.ClusterNamespace == "" {
		// Setup all Hub Controllers
		if err := controller.AddHubToManager(mgr); err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}

		enableLeaderElection := false
		if _, err := rest.InClusterConfig(); err == nil {
			klog.Info("LeaderElection enabled as running in a cluster")
			enableLeaderElection = true
		} else {
			klog.Info("LeaderElection disabled as not running in a cluster")
		}

		// Create a new Cmd to provide shared dependencies and start components
		mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
			MetricsBindAddress:      fmt.Sprintf("%s:%d", metricsHost, metricsPort),
			Port:                    operatorMetricsPort,
			LeaderElection:          enableLeaderElection,
			LeaderElectionID:        "multicloud-operators-subscription-leader.open-cluster-management.io",
			LeaderElectionNamespace: "kube-system",
		})

		if err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}
		// Setup Webhook listner
		if err := webhook.AddToManager(mgr, hubconfig, Options.TLSKeyFilePathName, Options.TLSCrtFilePathName, Options.DisableTLS, true); err != nil {
			klog.Error("Failed to initialize WebHook listener with error:", err)
			os.Exit(1)
		}
	} else if !strings.EqualFold(Options.ClusterName, "") && !strings.EqualFold(Options.ClusterNamespace, "") {
		if err := setupStandalone(mgr, hubconfig, id, false); err != nil {
			klog.Error("Failed to setup standalone subscription, error:", err)
			os.Exit(1)
		}
	} else if err := setupStandalone(mgr, hubconfig, id, true); err != nil {
		klog.Error("Failed to setup standalone subscription, error:", err)
		os.Exit(1)
	}

	sig := signals.SetupSignalHandler()

	klog.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(sig); err != nil {
		klog.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}

func setupStandalone(mgr manager.Manager, hubconfig *rest.Config, id *types.NamespacedName, standalone bool) error {
	// Setup Synchronizer
	if err := synchronizer.AddToManager(mgr, hubconfig, id, Options.SyncInterval); err != nil {
		klog.Error("Failed to initialize synchronizer with error:", err)
		return err
	}

	// Setup Subscribers
	if err := subscriber.AddToManager(mgr, hubconfig, id, Options.SyncInterval); err != nil {
		klog.Error("Failed to initialize subscriber with error:", err)
		return err
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr, hubconfig, standalone); err != nil {
		klog.Error("Failed to initialize controller with error:", err)
		return err
	}

	// Setup Webhook listner
	if err := webhook.AddToManager(mgr, hubconfig, Options.TLSKeyFilePathName, Options.TLSCrtFilePathName, Options.DisableTLS, false); err != nil {
		klog.Error("Failed to initialize WebHook listener with error:", err)
		return err
	}

	return nil
}
