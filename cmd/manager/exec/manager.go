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
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	addonV1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	ocinfrav1 "github.com/openshift/api/config/v1"
	addonutils "open-cluster-management.io/addon-framework/pkg/utils"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	manifestWorkV1 "open-cluster-management.io/api/work/v1"
	agentaddon "open-cluster-management.io/multicloud-operators-subscription/addon"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/apis"
	ansiblejob "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/ansible/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/controller"
	leasectrl "open-cluster-management.io/multicloud-operators-subscription/pkg/controller/subscription"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/subscriber"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/webhook"
	k8swebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost         = "0.0.0.0"
	metricsPort         = 8381
	operatorMetricsPort = 8684
)

const (
	AddonName               = "application-manager"
	leaseUpdateJitterFactor = 0.25
)

func RunManager() {
	enableLeaderElection := false

	if _, err := rest.InClusterConfig(); err == nil {
		klog.Info("LeaderElection enabled as running in a cluster")

		enableLeaderElection = true
	} else {
		klog.Info("LeaderElection disabled as not running in a cluster")
	}

	// for hub subcription pod
	leaderElectionID := "multicloud-operators-hub-subscription-leader.open-cluster-management.io"

	if Options.Standalone {
		// for standalone subcription pod
		leaderElectionID = "multicloud-operators-standalone-subscription-leader.open-cluster-management.io"
		metricsPort = 8389
	} else if !strings.EqualFold(Options.ClusterName, "") {
		// for managed cluster pod appmgr. It could run on hub if hub is self-managed cluster
		metricsPort = 8388
		leaderElectionID = "multicloud-operators-remote-subscription-leader.open-cluster-management.io"
	}

	klog.Info("kubeconfig:" + Options.KubeConfig)

	// increase the dafault QPS(5) to 100, only sends 5 requests to API server
	// seems to be unrealistic. Reading some other projects, it seems QPS 100 is
	// a pretty common practice
	var err error

	cfg := ctrl.GetConfigOrDie()

	if Options.KubeConfig != "" {
		cfg, err = utils.GetClientConfigFromKubeConfig(Options.KubeConfig)

		if err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}
	}

	cfg.QPS = 100.0
	cfg.Burst = 200

	leaderElectionLeaseDuration := time.Duration(Options.LeaderElectionLeaseDurationSeconds) * time.Second
	renewDeadline := time.Duration(Options.RenewDeadlineSeconds) * time.Second
	retryPeriod := time.Duration(Options.RetryPeriodSeconds) * time.Second
	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		MetricsBindAddress:      fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		Port:                    operatorMetricsPort,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        leaderElectionID,
		LeaderElectionNamespace: "kube-system",
		LeaseDuration:           &leaderElectionLeaseDuration,
		RenewDeadline:           &renewDeadline,
		RetryPeriod:             &retryPeriod,
		WebhookServer:           k8swebhook.NewServer(k8swebhook.Options{TLSMinVersion: "1.2"}),
		ClientDisableCacheFor:   []client.Object{&corev1.Secret{}, &corev1.ServiceAccount{}},
	})

	if err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}

	// id is the namespacedname of this cluster in hub
	var id = &types.NamespacedName{
		Name:      Options.ClusterName,
		Namespace: Options.ClusterName,
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

	if !Options.Standalone && Options.ClusterName == "" {
		// Setup managedCluster Scheme for manager
		if err := spokeClusterV1.AddToScheme(mgr.GetScheme()); err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}

		// Setup manifestWork Scheme for manager
		if err := manifestWorkV1.AddToScheme(mgr.GetScheme()); err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}

		// Setup cluster management addon Scheme for manager
		if err := addonV1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}

		// Setup all Hub Controllers
		if err := controller.AddHubToManager(mgr); err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}

		if !Options.Debug {
			// Setup Webhook listner
			if err := webhook.AddToManager(mgr, hubconfig, Options.TLSKeyFilePathName, Options.TLSCrtFilePathName, Options.DisableTLS, true); err != nil {
				klog.Error("Failed to initialize WebHook listener with error:", err)
				os.Exit(1)
			}
		}
	} else if !strings.EqualFold(Options.ClusterName, "") {
		// Setup ocinfrav1 Scheme for manager
		if err := ocinfrav1.AddToScheme(mgr.GetScheme()); err != nil {
			klog.Error(err, "")
			os.Exit(1)
		}

		if err := setupStandalone(mgr, hubconfig, id, false); err != nil {
			klog.Error("Failed to setup managed subscription, error:", err)
			os.Exit(1)
		}

		// set up lease controller for updating the application-manager lease in agent addon namespace on managed cluster
		// The application-manager lease resource is jitter updated every 60 seconds by default.
		managedClusterKubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
		if err != nil {
			klog.Error("Unable to create managed cluster kube client.", err)
			os.Exit(1)
		}

		hubKubeClient, err := kubernetes.NewForConfig(hubconfig)
		if err != nil {
			klog.Error("Failed to create hub cluster kube client.", err)
			os.Exit(1)
		}

		hubKubeConfigCheckSum, err := utils.GetCheckSum(Options.HubConfigFilePathName)
		if err != nil {
			klog.Error("Failed to get the checksum of the hub kubeconfig file. ", err)
			os.Exit(1)
		}

		leaseReconciler := leasectrl.LeaseReconciler{
			HubKubeClient:         hubKubeClient,
			HubConfigFilePathName: Options.HubConfigFilePathName,
			HubConfigCheckSum:     hubKubeConfigCheckSum,
			KubeClient:            managedClusterKubeClient,
			ClusterName:           Options.ClusterName,
			LeaseName:             AddonName,
			LeaseDurationSeconds:  int32(Options.LeaseDurationSeconds),
		}

		go wait.JitterUntilWithContext(context.TODO(), leaseReconciler.Reconcile,
			time.Duration(Options.LeaseDurationSeconds)*time.Second, leaseUpdateJitterFactor, true)

		// add liveness probe server
		cc, err := addonutils.NewConfigChecker("managed-serviceaccount-agent", "/var/run/klusterlet/kubeconfig")
		if err != nil {
			klog.Fatalf("unable to create config checker for application-manager addon")
		}
		go func() {
			if err = serveHealthProbes(":8000", cc.Check); err != nil {
				klog.Fatal(err)
			}
		}()
	} else if err := setupStandalone(mgr, hubconfig, id, true); err != nil {
		klog.Error("Failed to setup standalone subscription, error:", err)
		os.Exit(1)
	}

	sig := signals.SetupSignalHandler()

	// Only detect if the placementDecsion API is ready on the hub cluster
	if !Options.Standalone && Options.ClusterName == "" {
		klog.Info("Detecting ACM Placement Decision API on the hub...")
		utils.DetectPlacementDecision(sig, mgr.GetAPIReader(), mgr.GetClient())
	}

	// Detect if the subscription API is ready
	isHubCluster := false
	if Options.ClusterName == "" {
		isHubCluster = true
	}

	if !utils.IsReadySubscription(mgr.GetAPIReader(), isHubCluster) {
		for {
			if !utils.IsReadySubscription(mgr.GetAPIReader(), isHubCluster) {
				time.Sleep(10 * time.Second)
			} else { // restart controller when CRDs are found.
				klog.Info("Subscription API is ready.")
				os.Exit(1)
			}
		}
	}

	klog.Info("Starting the Cmd.")

	// Start addon manager
	if !Options.Standalone && Options.ClusterName == "" {
		klog.Info("Starting addon manager")

		agentImage, err := agentaddon.GetMchImage(cfg)
		if err != nil {
			klog.Error("Failed to get MCH image config map, error:", err)
		}

		if agentImage == "" {
			var found bool
			agentImage, found = os.LookupEnv(agentaddon.AgentImageEnv)

			if !found {
				agentImage = Options.AgentImage
			}
		}

		klog.Infof("Agent image: %v", agentImage)

		adddonmgr, err := agentaddon.NewAddonManager(cfg, agentImage, Options.AgentInstallAll)
		if err != nil {
			klog.Error("Failed to setup addon manager, error:", err)
			os.Exit(1)
		}

		go func() {
			<-mgr.Elected()

			if err := adddonmgr.Start(sig); err != nil {
				klog.Error("Failed to start addon manager, error:", err)
				os.Exit(1)
			}
		}()
	}

	// Start the Cmd
	if err := mgr.Start(sig); err != nil {
		klog.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}

func setupStandalone(mgr manager.Manager, hubconfig *rest.Config, id *types.NamespacedName, standalone bool) error {
	// Setup Synchronizer
	isHub := utils.IsHub(mgr.GetConfig())
	if err := synchronizer.AddToManager(mgr, hubconfig, id, Options.SyncInterval, isHub, standalone); err != nil {
		klog.Error("Failed to initialize synchronizer with error:", err)

		return err
	}

	// Setup Subscribers
	if err := subscriber.AddToManager(mgr, hubconfig, id, Options.SyncInterval, isHub, standalone); err != nil {
		klog.Error("Failed to initialize subscriber with error:", err)

		return err
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr, hubconfig, id, isHub, standalone); err != nil {
		klog.Error("Failed to initialize controller with error:", err)

		return err
	}

	if standalone && !Options.Debug {
		// Setup Webhook listner
		if err := webhook.AddToManager(mgr, hubconfig, Options.TLSKeyFilePathName, Options.TLSCrtFilePathName, Options.DisableTLS, false); err != nil {
			klog.Error("Failed to initialize WebHook listener with error:", err)

			return err
		}
	}

	return nil
}

// serveHealthProbes serves health probes and configchecker.
func serveHealthProbes(healthProbeBindAddress string, configCheck healthz.Checker) error {
	mux := http.NewServeMux()
	mux.Handle("/healthz", http.StripPrefix("/healthz", &healthz.Handler{Checks: map[string]healthz.Checker{
		"healthz-ping": healthz.Ping,
		"configz-ping": configCheck,
	}}))

	server := http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		Addr:              healthProbeBindAddress,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	klog.Infof("heath probes server is running...")

	return server.ListenAndServe()
}
