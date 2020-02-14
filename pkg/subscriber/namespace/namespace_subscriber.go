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

package namespace

import (
	"reflect"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	chnv1alpha1 "github.com/IBM/multicloud-operators-channel/pkg/apis/app/v1alpha1"
	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	dplutils "github.com/IBM/multicloud-operators-deployable/pkg/utils"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	kubesynchronizer "github.com/IBM/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
)

// SubscriberItem - defines the unit of namespace subscription
type NamespaceSubscriberItem struct {
	appv1alpha1.SubscriberItem
	cache                cache.Cache
	deployablecontroller controller.Controller
	secretcontroller     controller.Controller
	clusterscoped        bool
	stopch               chan struct{}
	dplreconciler        *DeployableReconciler
	srtrecondiler        *SecretReconciler
}

type itemmap map[types.NamespacedName]*NamespaceSubscriberItem

// Subscriber - information to run namespace subscription
type NamespaceSubscriber struct {
	itemmap
	// hub cluster
	config *rest.Config
	scheme *runtime.Scheme
	// endpoint cluster
	manager      manager.Manager
	synchronizer *kubesynchronizer.KubeSynchronizer
}

var (
	defaultNsSubscriber *NamespaceSubscriber
	defaultSubscription = &appv1alpha1.Subscription{}
	defaultChannel      = &chnv1alpha1.Channel{}
	defaultitem         = &appv1alpha1.SubscriberItem{
		Subscription: defaultSubscription,
		Channel:      defaultChannel,
	}
)

const (
	deployablesyncsource = "subnsdpl-"
	secretsyncsource     = "subnssec-"
)

// Add does nothing for namespace subscriber, it generates cache for each of the item
func Add(mgr manager.Manager, hubconfig *rest.Config, syncid *types.NamespacedName, syncinterval int) error {
	// No polling, use cache. Add default one for cluster namespace

	sync := kubesynchronizer.GetDefaultSynchronizer()
	if sync == nil {
		if err := kubesynchronizer.Add(mgr, hubconfig, syncid, syncinterval); err != nil {
			klog.Error("failed to initialize synchronizer for default namespace channel with error:", err)
			return err
		}

		sync = kubesynchronizer.GetDefaultSynchronizer()
	}

	nssubscriber, err := CreateNamespaceSubscriber(hubconfig, mgr.GetScheme(), mgr, sync)
	if err != nil {
		return errors.New("failed to create default namespace subscriber")
	}

	defaultNsSubscriber = nssubscriber

	if syncid.String() != "/" {
		defaultitem.Channel.Namespace = syncid.Namespace
		if err := defaultNsSubscriber.SubscribeNamespaceItem(defaultitem, true); err != nil {
			klog.Error("failed to initialize default channel to cluster namespace")
			return err
		}

		klog.Info("default namespace subscriber with id:", syncid)
	}

	klog.Info("Done setup namespace subscriber")

	return nil
}

// SubscribeNamespaceItem adds namespace subscribe item to subscriber
func (ns *NamespaceSubscriber) SubscribeNamespaceItem(subitem *appv1alpha1.SubscriberItem, isClusterScoped bool) error {
	if ns.itemmap == nil {
		ns.itemmap = make(map[types.NamespacedName]*NamespaceSubscriberItem)
	}

	itemkey := types.NamespacedName{Name: subitem.Subscription.Name, Namespace: subitem.Subscription.Namespace}
	klog.V(2).Info("subscribeItem ", itemkey)

	nssubitem, ok := ns.itemmap[itemkey]

	if !ok {
		if err := ns.initializeSubscriber(nssubitem, itemkey, subitem, isClusterScoped); err != nil {
			return err
		}
	} else if !reflect.DeepEqual(nssubitem.SubscriberItem, subitem) {
		subitem.DeepCopyInto(&nssubitem.SubscriberItem)
		ns.itemmap[itemkey] = nssubitem

		if err := syncUpWithChannel(nssubitem); err != nil {
			return err
		}
	}

	return nil
}

func (ns *NamespaceSubscriber) initializeSubscriber(nssubitem *NamespaceSubscriberItem,
	itemkey types.NamespacedName,
	subitem *appv1alpha1.SubscriberItem,
	isClusterScoped bool) error {
	var err error

	klog.V(1).Info("Built cache for namespace: ", subitem.Channel.Namespace)

	nssubitem = &NamespaceSubscriberItem{}
	nssubitem.clusterscoped = isClusterScoped
	nssubitem.cache, err = cache.New(ns.config, cache.Options{Scheme: ns.scheme, Namespace: subitem.Channel.Spec.PathName})

	if err != nil {
		return errors.Wrap(err, "failed to create cache for namespace subscriber item")
	}

	hubclient, err := client.New(ns.config, client.Options{})

	if err != nil {
		return errors.Wrap(err, "failed to create client for namespace subscriber item")
	}

	reconciler := &DeployableReconciler{
		Client:     hubclient,
		subscriber: ns,
		itemkey:    itemkey,
	}
	nssubitem.deployablecontroller, err = controller.New("sub"+itemkey.String(), ns.manager, controller.Options{Reconciler: reconciler})

	if err != nil {
		return errors.Wrap(err, "failed to create deployable controller for namespace subscriber item")
	}

	ifm, err := nssubitem.cache.GetInformer(&dplv1alpha1.Deployable{})

	if err != nil {
		return errors.Wrap(err, "failed to get informer for deployable from cache")
	}

	src := &source.Informer{Informer: ifm}

	err = nssubitem.deployablecontroller.Watch(src, &handler.EnqueueRequestForObject{}, dplutils.DeployablePredicateFunc)

	if err != nil {
		return errors.Wrap(err, "failed to watch deployable")
	}

	// adding secret reconciler

	//	secretreconciler := &SecretReconciler{
	//		Clt:        hubclient,
	//		Subscriber: ns,
	//		Itemkey:    itemkey,
	//		Schema:     ns.scheme,
	//	}

	secretreconciler := newSecretReconciler(ns, ns.manager, itemkey, ns.synchronizer)

	nssubitem.secretcontroller, err = controller.New("sub"+itemkey.String(), ns.manager, controller.Options{Reconciler: secretreconciler})

	if err != nil {
		return errors.Wrap(err, "failed to create secret controller for namespace subscriber item")
	}

	sifm, err := nssubitem.cache.GetInformer(&v1.Secret{})

	if err != nil {
		return errors.Wrap(err, "failed to get informer for secret from cache")
	}

	ssrc := &source.Informer{Informer: sifm}

	err = nssubitem.secretcontroller.Watch(ssrc, &handler.EnqueueRequestForObject{})

	if err != nil {
		return errors.Wrap(err, "failed to watch secret")
	}

	nssubitem.stopch = make(chan struct{})

	go func() {
		err := nssubitem.cache.Start(nssubitem.stopch)
		if err != nil {
			klog.Error("failed to start cache for Namespace subscriber item with error: ", err)
		}
	}()

	go func() {
		err := nssubitem.deployablecontroller.Start(nssubitem.stopch)
		if err != nil {
			klog.Error("failed to start controller for Namespace subscriber item with error: ", err)
		}
	}()

	go func() {
		err := nssubitem.secretcontroller.Start(nssubitem.stopch)
		if err != nil {
			klog.Error("failed to start controller for Namespace subscriber item with error: ", err)
		}
	}()

	nssubitem.dplreconciler = reconciler
	nssubitem.srtrecondiler = secretreconciler

	subitem.DeepCopyInto(&nssubitem.SubscriberItem)
	ns.itemmap[itemkey] = nssubitem

	return nil
}

func syncUpWithChannel(nssubitem *NamespaceSubscriberItem) error {
	fakeKey := types.NamespacedName{Namespace: nssubitem.Subscription.GetNamespace()}
	rq := reconcile.Request{NamespacedName: fakeKey}

	_, err := nssubitem.dplreconciler.Reconcile(rq)

	if err != nil {
		return errors.Wrapf(err, "failed to do reconcile on deployable on subscription %v", nssubitem.Subscription.GetName())
	}

	fakeKey = types.NamespacedName{Namespace: nssubitem.Channel.GetNamespace()}
	rq = reconcile.Request{NamespacedName: fakeKey}

	_, err = nssubitem.srtrecondiler.Reconcile(rq)

	return errors.Wrapf(err, "failed to do subscription %v", nssubitem.Subscription.GetName())
}

// SubscribeItem subscribes a subscriber item with namespace channel
func (ns *NamespaceSubscriber) SubscribeItem(subitem *appv1alpha1.SubscriberItem) error {
	return ns.SubscribeNamespaceItem(subitem, false)
}

// UnsubscribeItem unsubscribes a namespace subscriber item
func (ns *NamespaceSubscriber) UnsubscribeItem(key types.NamespacedName) error {
	klog.V(2).Info("UnsubscribeItem ", key)

	nssubitem, ok := ns.itemmap[key]

	if ok {
		close(nssubitem.stopch)
		delete(ns.itemmap, key)
		ns.synchronizer.CleanupByHost(key, deployablesyncsource+key.String())
		ns.synchronizer.CleanupByHost(key, secretsyncsource+key.String())
	}

	return nil
}

// GetdefaultNsSubscriber - returns the default namespace subscriber
func GetdefaultNsSubscriber() appv1alpha1.Subscriber {
	if defaultNsSubscriber == nil {
		return nil
	}

	return defaultNsSubscriber
}

// CreateNamespaceSubscriber - create namespace subscriber with config to hub cluster, scheme of hub cluster and a syncrhonizer to local cluster
func CreateNamespaceSubscriber(config *rest.Config, scheme *runtime.Scheme, mgr manager.Manager, kubesync *kubesynchronizer.KubeSynchronizer) (*NamespaceSubscriber, error) {
	if config == nil || kubesync == nil {
		return nil, errors.Errorf("cant create namespace subscriber with config %v kubenetes synchronizer %v", config, kubesync)
	}

	nssubscriber := &NamespaceSubscriber{
		config:       config,
		scheme:       scheme,
		manager:      mgr,
		synchronizer: kubesync,
	}

	nssubscriber.itemmap = make(map[types.NamespacedName]*NamespaceSubscriberItem)

	return nssubscriber, nil
}
