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
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	kubemetrics "github.com/operator-framework/operator-sdk/pkg/kube-metrics"
	"github.com/operator-framework/operator-sdk/pkg/leader"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	sdkVersion "github.com/operator-framework/operator-sdk/version"

	"github.com/prometheus/common/log"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/controller"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/webhook"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost               = "0.0.0.0"
	metricsPort         int32 = 8383
	operatorMetricsPort int32 = 8686
)

func printVersion() {
	klog.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	klog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	klog.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion.Version))
}

func RunManager(sig <-chan struct{}) {
	printVersion()

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

	ctx := context.TODO()

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
		// Become the leader before proceeding
		err = leader.Become(ctx, "multicloud-operators-subscription-lock")
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

	if err = serveCRMetrics(cfg); err != nil {
		klog.Info("Could not generate and serve custom resource metrics", "error", err.Error())
	}

	// Add to the below struct any other metrics ports you want to expose.
	servicePorts := []v1.ServicePort{
		{
			Port:       metricsPort,
			Name:       metrics.OperatorPortName,
			Protocol:   v1.ProtocolTCP,
			TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: metricsPort},
		},
		{
			Port:       operatorMetricsPort,
			Name:       metrics.CRPortName,
			Protocol:   v1.ProtocolTCP,
			TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: operatorMetricsPort},
		},
	}
	// Create Service object to expose the metrics port(s).
	service, err := metrics.CreateMetricsService(ctx, cfg, servicePorts)
	if err != nil {
		klog.Info("Could not create metrics Service", "error", err.Error())
	}

	// CreateServiceMonitors will automatically create the prometheus-operator ServiceMonitor resources
	// necessary to configure Prometheus to scrape metrics from this operator.
	services := []*v1.Service{service}
	_, err = metrics.CreateServiceMonitors(cfg, "", services)

	if err != nil {
		log.Info("Could not create ServiceMonitor object", "error", err.Error())
		// If this operator is deployed to a cluster without the prometheus-operator running, it will return
		// ErrServiceMonitorNotPresent, which can be used to safely skip ServiceMonitor creation.
		if err == metrics.ErrServiceMonitorNotPresent {
			klog.Info("Install prometheus-operator in your cluster to create ServiceMonitor objects", "error", err.Error())
		}
	}

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
	if err := controller.AddToManager(mgr, hubconfig, id, standalone); err != nil {
		klog.Error("Failed to initialize controller with error:", err)
		return err
	}

	if standalone {
		// Setup Webhook listner.
		if err := webhook.AddToManager(mgr, hubconfig, Options.TLSKeyFilePathName, Options.TLSCrtFilePathName, Options.DisableTLS, false); err != nil {
			klog.Error("Failed to initialize WebHook listener with error:", err)
			return err
		}
	}

	return nil
}

// serveCRMetrics gets the Operator/CustomResource GVKs and generates metrics based on those types.
// It serves those metrics on "http://metricsHost:operatorMetricsPort".
func serveCRMetrics(cfg *rest.Config) error {
	// Below function returns filtered operator/CustomResource specific GVKs.
	// For more control override the below GVK list with your own custom logic.
	filteredGVK, err := k8sutil.GetGVKsFromAddToScheme(apis.AddToScheme)
	if err != nil {
		return err
	}
	// Get the namespace the operator is currently deployed in.
	operatorNs, err := k8sutil.GetOperatorNamespace()
	if err != nil {
		return err
	}
	// To generate metrics in other namespaces, add the values below.
	ns := []string{operatorNs}
	// Generate and serve custom resource specific metrics.
	return kubemetrics.GenerateAndServeCRMetrics(cfg, ns, filteredGVK, metricsHost, operatorMetricsPort)
}
