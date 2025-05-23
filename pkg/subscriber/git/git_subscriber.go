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

package git

import (
	"errors"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

type itemmap map[types.NamespacedName]*SubscriberItem

type SyncSource interface {
	GetInterval() int
	GetLocalClient() client.Client
	GetLocalNonCachedClient() client.Client
	GetRemoteClient() client.Client
	GetRemoteNonCachedClient() client.Client
	IsResourceNamespaced(*unstructured.Unstructured) bool
	ProcessSubResources(*appv1.Subscription, []kubesynchronizer.ResourceUnit,
		map[string]map[string]string, map[string]map[string]string, bool, bool) error
	PurgeAllSubscribedResources(*appv1.Subscription) error
	UpdateAppsubOverallStatus(*appv1.Subscription, bool, string) error
}

// Subscriber - information to run namespace subscription
type Subscriber struct {
	itemmap
	manager      manager.Manager
	synchronizer SyncSource
	syncinterval int
}

var defaultSubscriber *Subscriber

// Add does nothing for namespace subscriber, it generates cache for each of the item.
func Add(mgr manager.Manager, hubconfig *rest.Config, syncid *types.NamespacedName, syncinterval int, hub, standalone bool) error {
	// No polling, use cache. Add default one for cluster namespace
	var err error

	klog.V(5).Info("Setting up default github subscriber on ", syncid)

	sync := kubesynchronizer.GetDefaultSynchronizer()
	if sync == nil {
		err = kubesynchronizer.Add(mgr, hubconfig, syncid, syncinterval, hub, standalone)
		if err != nil {
			klog.Error("Failed to initialize synchronizer for default namespace channel with error:", err)

			return err
		}

		sync = kubesynchronizer.GetDefaultSynchronizer()
	}

	if err != nil {
		klog.Error("Failed to create synchronizer for subscriber with error:", err)

		return err
	}

	sync.SkipAppSubStatusResDel = false

	defaultSubscriber = CreateGitHubSubscriber(hubconfig, mgr.GetScheme(), mgr, sync, syncinterval)
	if defaultSubscriber == nil {
		errmsg := "failed to create default namespace subscriber"

		return errors.New(errmsg)
	}

	return nil
}

// SubscribeItem subscribes a subscriber item with namespace channel.
func (ghs *Subscriber) SubscribeItem(subitem *appv1.SubscriberItem) error {
	if ghs.itemmap == nil {
		ghs.itemmap = make(map[types.NamespacedName]*SubscriberItem)
	}

	itemkey := types.NamespacedName{Name: subitem.Subscription.Name, Namespace: subitem.Subscription.Namespace}
	klog.Info("subscribeItem ", itemkey)

	ghssubitem, ok := ghs.itemmap[itemkey]

	if !ok {
		ghssubitem = &SubscriberItem{}
		ghssubitem.syncinterval = ghs.syncinterval
		ghssubitem.synchronizer = ghs.synchronizer
	}

	subitem.DeepCopyInto(&ghssubitem.SubscriberItem)

	ghs.itemmap[itemkey] = ghssubitem

	previousReconcileLevel := ghssubitem.reconcileRate

	previousDesiredCommit := ghssubitem.desiredCommit

	previousDesiredTag := ghssubitem.desiredTag

	previousSyncTime := ghssubitem.syncTime

	chnAnnotations := ghssubitem.Channel.GetAnnotations()

	subAnnotations := ghssubitem.Subscription.GetAnnotations()

	ghssubitem.reconcileRate = utils.GetReconcileRate(chnAnnotations, subAnnotations)

	// Reconcile level can be overridden to be
	if strings.EqualFold(subAnnotations[appv1.AnnotationResourceReconcileLevel], "off") {
		klog.Infof("Overriding channel's reconcile rate %s to turn it off", ghssubitem.reconcileRate)
		ghssubitem.reconcileRate = "off"
	}

	if strings.EqualFold(subAnnotations[appv1.AnnotationClusterAdmin], "true") {
		klog.Info("Cluster admin role enabled on SubscriberItem ", ghssubitem.Subscription.Name)
		ghssubitem.clusterAdmin = true
	} else {
		ghssubitem.clusterAdmin = false
	}

	if strings.EqualFold(subAnnotations[appv1.AnnotationCurrentNamespaceScoped], "true") {
		klog.Info("CurrentNamespaceScoped enabled on SubscriberItem ", ghssubitem.Subscription.Name)
		ghssubitem.currentNamespaceScoped = true
	} else {
		ghssubitem.currentNamespaceScoped = false
	}

	ghssubitem.desiredCommit = subAnnotations[appv1.AnnotationGitTargetCommit]
	ghssubitem.desiredTag = subAnnotations[appv1.AnnotationGitTag]
	ghssubitem.syncTime = subAnnotations[appv1.AnnotationManualReconcileTime]
	ghssubitem.userID = strings.Trim(subAnnotations[appv1.AnnotationUserIdentity], "")
	ghssubitem.userGroup = strings.Trim(subAnnotations[appv1.AnnotationUserGroup], "")

	// If the channel has annotation webhookenabled="true", do not poll the repo.
	// Do subscription only on webhook events.
	if strings.EqualFold(ghssubitem.Channel.GetAnnotations()[appv1.AnnotationWebhookEnabled], "true") {
		klog.Info("Webhook enabled on SubscriberItem ", ghssubitem.Subscription.Name)
		ghssubitem.webhookEnabled = true
		// Set successful to false so that the subscription keeps trying until all resources are successfully
		// applied until the next webhook event.
		ghssubitem.successful = false

		ghssubitem.doSubscriptionWithRetries(time.Hour*3, 10)

		klog.Info("Webhook event processed")

		return nil
	}

	klog.Info("Polling enabled on SubscriberItem ", ghssubitem.Subscription.Name)
	ghssubitem.webhookEnabled = false

	var restart = false

	if strings.EqualFold(previousReconcileLevel, ghssubitem.reconcileRate) && strings.EqualFold(ghssubitem.reconcileRate, "off") {
		// auto reconcile off but something changed in subscription. restart to reconcile resources
		klog.Info("auto reconcile off but something changed in subscription. restart to reconcile resources")

		restart = true
	}

	if previousReconcileLevel != "" && !strings.EqualFold(previousReconcileLevel, ghssubitem.reconcileRate) {
		// reconcile frequency has changed. restart the go routine
		klog.Infof("reconcile rate has changed from %s to %s. restart to reconcile resources", previousReconcileLevel, ghssubitem.reconcileRate)

		restart = true
	}

	// If desired commit or tag has changed, we want to restart the reconcile cycle and deploy the new commit immediately
	if !strings.EqualFold(previousDesiredCommit, ghssubitem.desiredCommit) {
		klog.Infof("desired commit hash has changed from %s to %s. restart to reconcile resources", previousDesiredCommit, ghssubitem.desiredCommit)

		restart = true
	}

	// If desired commit or tag has changed, we want to restart the reconcile cycle and deploy the new commit immediately
	if !strings.EqualFold(previousDesiredTag, ghssubitem.desiredTag) {
		klog.Infof("desired tag has changed from %s to %s. restart to reconcile resources", previousDesiredTag, ghssubitem.desiredTag)

		restart = true
	}

	// If manual sync time is updated, we want to restart the reconcile cycle and deploy the new commit immediately
	if !strings.EqualFold(previousSyncTime, ghssubitem.syncTime) {
		klog.Infof("Manual reconcile time has changed from %s to %s. restart to reconcile resources", previousSyncTime, ghssubitem.syncTime)

		// reset commit ID to force sync
		ghssubitem.commitID = ""

		restart = true
	}

	ghssubitem.Start(restart)

	return nil
}

// UnsubscribeItem uhrsubscribes a namespace subscriber item.
func (ghs *Subscriber) UnsubscribeItem(key types.NamespacedName) error {
	klog.Info("git UnsubscribeItem ", key)

	subitem, ok := ghs.itemmap[key]

	if ok {
		subitem.Stop()
		delete(ghs.itemmap, key)

		if err := ghs.synchronizer.PurgeAllSubscribedResources(subitem.Subscription); err != nil {
			klog.Errorf("failed to unsubscribe  %v, err: %v", key.String(), err)

			return err
		}
	}

	return nil
}

// GetDefaultSubscriber - returns the default git subscriber.
func GetDefaultSubscriber() appv1.Subscriber {
	return defaultSubscriber
}

// CreateGitHubSubscriber - create github subscriber with config to hub cluster, scheme of hub cluster and a syncrhonizer to local cluster
func CreateGitHubSubscriber(config *rest.Config, scheme *runtime.Scheme, mgr manager.Manager,
	kubesync SyncSource, syncinterval int) *Subscriber {
	if config == nil || kubesync == nil {
		klog.Error("Can not create github subscriber with config: ", config, " kubenetes synchronizer: ", kubesync)

		return nil
	}

	githubsubscriber := &Subscriber{
		manager:      mgr,
		synchronizer: kubesync,
	}

	githubsubscriber.itemmap = make(map[types.NamespacedName]*SubscriberItem)
	githubsubscriber.syncinterval = syncinterval

	return githubsubscriber
}
