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

package kubernetes

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

type ResourceUnit struct {
	Resource *unstructured.Unstructured
	Gvk      schema.GroupVersionKind
}

type SubscriptionUnitStatus struct {
	Name       string
	Namespace  string
	APIVersion string
	Kind       string
	Phase      string
	Message    string
}

type SubscriptionClusterStatus struct {
	Cluster                   string
	AppSub                    types.NamespacedName /* hosting appsub */
	Action                    string               /* "APPLY" or "DELETE" */
	SubscriptionPackageStatus []SubscriptionUnitStatus
}

// KubeSynchronizer handles resources to a kube endpoint.
type KubeSynchronizer struct {
	Interval               int
	localCachedClient      *cachedClient
	remoteCachedClient     *cachedClient
	LocalClient            client.Client
	LocalNonCachedClient   client.Client
	RemoteClient           client.Client
	RemoteNonCachedClient  client.Client
	localConfig            *rest.Config
	hub                    bool
	standalone             bool
	DynamicClient          dynamic.Interface
	RestMapper             meta.RESTMapper
	kmtx                   sync.Mutex            // lock the kubeResource
	SynchronizerID         *types.NamespacedName // managed cluster Namespaced name
	Extension              Extension
	eventrecorder          *utils.EventRecorder
	dmtx                   sync.Mutex //this lock protect the dynamicFactory and stopCh
	SkipAppSubStatusResDel bool       // used by helm subscriber to skip resource delete based on AppSubStatus
}

var defaultSynchronizer *KubeSynchronizer

// Add creates the default syncrhonizer and add the start function as runnable into manager.
func Add(mgr manager.Manager, hubconfig *rest.Config, syncid *types.NamespacedName, interval int, hub, standalone bool) error {
	var err error
	defaultSynchronizer, err = CreateSynchronizer(mgr.GetConfig(), hubconfig, mgr.GetScheme(), syncid, interval, defaultExtension, hub, standalone)

	if err != nil {
		klog.Error("Failed to create synchronizer with error: ", err)

		return err
	}

	startCleanup(defaultSynchronizer)

	return mgr.Add(defaultSynchronizer)
}

// GetDefaultSynchronizer - return the default kubernetse synchronizer.
func GetDefaultSynchronizer() *KubeSynchronizer {
	return defaultSynchronizer
}

// CreateSynchronizer createa an instance of synchrizer with give api-server config.
func CreateSynchronizer(config, remoteConfig *rest.Config, scheme *runtime.Scheme, syncid *types.NamespacedName,
	interval int, ext Extension, hub, standalone bool) (*KubeSynchronizer, error) {
	if klog.V(utils.QuiteLogLel).Enabled() {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	var err error

	dynamicClient := dynamic.NewForConfigOrDie(config)

	restMapper, err := apiutil.NewDynamicRESTMapper(config, apiutil.WithLazyDiscovery)
	if err != nil {
		return nil, err
	}

	s := &KubeSynchronizer{
		Interval:       interval,
		SynchronizerID: syncid,
		DynamicClient:  dynamicClient,
		RestMapper:     restMapper,
		localConfig:    config,
		hub:            hub,
		standalone:     standalone,
		kmtx:           sync.Mutex{},
		Extension:      ext,
		dmtx:           sync.Mutex{},
	}

	s.localCachedClient, err = newCachedClient(config, &types.NamespacedName{Name: "local"})
	if err != nil {
		klog.Error("Failed to initialize client to update local status. err: ", err)

		return nil, err
	}

	s.LocalClient = s.localCachedClient.clt

	// set up non chanced local client
	s.LocalNonCachedClient, err = client.New(config, client.Options{})
	if err != nil {
		klog.Error("Failed to generate client to local cluster with error: ", err)

		return nil, err
	}

	s.RemoteClient = s.LocalClient
	if remoteConfig != nil {
		s.remoteCachedClient, err = newCachedClient(remoteConfig, syncid)
		if err != nil {
			klog.Error("Failed to initialize client to update remote status. err: ", err)

			return nil, err
		}

		s.RemoteClient = s.remoteCachedClient.clt

		// set up non chanced hub client
		s.RemoteNonCachedClient, err = client.New(remoteConfig, client.Options{})
		if err != nil {
			klog.Error("Failed to generate client to hub cluster with error: ", err)

			return nil, err
		}
	}

	defaultExtension.localClient = s.LocalClient
	defaultExtension.remoteClient = s.RemoteClient

	if ext == nil {
		s.Extension = defaultExtension
	}

	s.eventrecorder, err = utils.NewEventRecorder(config, scheme)

	if err != nil {
		klog.Error("Failed to create event recorder. err: ", err)

		return nil, err
	}

	return s, nil
}

// Start caches, this will be triggered by the manager.
func (sync *KubeSynchronizer) Start(ctx context.Context) error {
	klog.Info("start synchronizer")
	defer klog.Info("stop synchronizer")

	go func() {
		if err := sync.localCachedClient.clientCache.Start(ctx); err != nil {
			klog.Error(err, "failed to start up cache")
		}
	}()

	go func() {
		if err := sync.remoteCachedClient.clientCache.Start(ctx); err != nil {
			klog.Error(err, "failed to start up cache")
		}
	}()

	if !sync.localCachedClient.clientCache.WaitForCacheSync(ctx) {
		return fmt.Errorf("failed to start up local cache")
	}

	klog.Info("local config cache started")

	if !sync.remoteCachedClient.clientCache.WaitForCacheSync(ctx) {
		return fmt.Errorf("failed to start up remote cache")
	}

	klog.Info("remote config cache started")

	return nil
}

func (sync *KubeSynchronizer) GetInterval() int {
	return sync.Interval
}

func (sync *KubeSynchronizer) GetLocalClient() client.Client {
	return sync.LocalClient
}

func (sync *KubeSynchronizer) GetLocalNonCachedClient() client.Client {
	return sync.LocalNonCachedClient
}

func (sync *KubeSynchronizer) GetRemoteClient() client.Client {
	return sync.RemoteClient
}

func (sync *KubeSynchronizer) GetRemoteNonCachedClient() client.Client {
	return sync.RemoteNonCachedClient
}

// startCleanup looks up all the subscriptionstatuses
// For each subscriptionstatus that doesn't have a subscription
// Delete the resources listed inside the subscriptionstatus
// If all the resources are deleted successfully then delete the subscriptionstatus
func startCleanup(synchronizer *KubeSynchronizer) {
	go wait.Until(func() {
		klog.Info("Starting cleanup")

		ctx := context.Background()

		clt := synchronizer.LocalNonCachedClient

		appsubStatusList := &appv1alpha1.SubscriptionStatusList{}

		if err := clt.List(ctx, appsubStatusList, &client.ListOptions{}); err != nil {
			klog.Error(err, "failed to list SubscriptionStatus")
		}

		if appsubStatusList != nil && len(appsubStatusList.Items) > 0 {
			for _, appsubStatus := range appsubStatusList.Items {
				appsub := &appv1.Subscription{}

				nsn := types.NamespacedName{Namespace: appsubStatus.Namespace, Name: appsubStatus.Name}

				if err := clt.Get(ctx, nsn, appsub); err != nil {
					if errors.IsNotFound(err) {
						klog.Infof("cannot find Subscription namespace: %s , name: %s , deleting resources in SubscriptionStatus",
							nsn.Namespace, nsn.Name)

						if len(appsubStatus.Statuses.SubscriptionStatus) > 0 {
							foundErr := false

							for _, unitStatus := range appsubStatus.Statuses.SubscriptionStatus {
								if err = synchronizer.DeleteSingleSubscribedResource(nsn, unitStatus); err != nil {
									klog.Error(err, "failed to delete resource")
									foundErr = true
								}
							}

							if !foundErr {
								if err = clt.Delete(ctx, &appsubStatus, &client.DeleteOptions{}); err != nil {
									klog.Error(err, "failed to delete SubscriptionStatus")
								}
							}
						}
					} else {
						klog.Error(err, "unable to get Subscription")
					}
				}
			}
		}
	}, time.Hour*1, make(chan struct{}))
}
