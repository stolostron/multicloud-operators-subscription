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

package helmrepo

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	chnv1alpha1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
)

var (
	repourl   = "https://charts.helm.sh/stable/"
	sharedkey = types.NamespacedName{
		Name:      "test",
		Namespace: "default",
	}
	helmchn = &chnv1alpha1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: chnv1alpha1.ChannelSpec{
			Type:     chnv1alpha1.ChannelTypeHelmRepo,
			Pathname: repourl,
		},
	}
	helmsub = &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sharedkey.Name,
			Namespace: sharedkey.Namespace,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: sharedkey.String(),
			Package: "nginx-ingress",
		},
	}
	subitem = &appv1alpha1.SubscriberItem{
		Subscription: helmsub,
		Channel:      helmchn,
	}
)

func TestHelmSubscriber(t *testing.T) {
}

var _ = Describe("", func() {
	It("", func() {
		Expect(defaultSubscriber.SubscribeItem(subitem)).NotTo(HaveOccurred())

		time.Sleep(k8swait)

		Expect(defaultSubscriber.UnsubscribeItem(sharedkey)).NotTo(HaveOccurred())
	})

	// This covers the privilege de-escalation fix in SubscribeItem: clusterAdmin
	// must track the live cluster-admin annotation on every call, including
	// being reset to false when the annotation is removed on a later update.
	It("sets and resets clusterAdmin on the cached SubscriberItem based on the live annotation", func() {
		adminKey := types.NamespacedName{
			Name:      "cluster-admin-deescalation-test",
			Namespace: "default",
		}

		adminSub := &appv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      adminKey.Name,
				Namespace: adminKey.Namespace,
				Annotations: map[string]string{
					appv1alpha1.AnnotationClusterAdmin: "true",
				},
			},
			Spec: appv1alpha1.SubscriptionSpec{
				Channel: sharedkey.String(),
				Package: "nginx-ingress",
			},
		}

		adminSubitem := &appv1alpha1.SubscriberItem{
			Subscription: adminSub,
			Channel:      helmchn,
		}

		Expect(defaultSubscriber.SubscribeItem(adminSubitem)).NotTo(HaveOccurred())
		Expect(defaultSubscriber.itemmap[adminKey].clusterAdmin).To(BeTrue())

		// Remove the cluster-admin annotation and re-subscribe: the cached
		// item's clusterAdmin flag must be de-escalated back to false rather
		// than retaining its previous elevated value.
		nonAdminSub := adminSub.DeepCopy()
		nonAdminSub.Annotations = map[string]string{}

		nonAdminSubitem := &appv1alpha1.SubscriberItem{
			Subscription: nonAdminSub,
			Channel:      helmchn,
		}

		Expect(defaultSubscriber.SubscribeItem(nonAdminSubitem)).NotTo(HaveOccurred())
		Expect(defaultSubscriber.itemmap[adminKey].clusterAdmin).To(BeFalse())

		// Avoid UnsubscribeItem here: Stop() blocks on a WaitGroup until the
		// background goroutine's in-flight doSubscription attempt against
		// this (deliberately unreachable) test channel finishes, which can
		// take up to the subscriber's retry interval (~90s). Since this test
		// only cares about the clusterAdmin bookkeeping done synchronously
		// inside SubscribeItem, just drop the cached item and let the
		// goroutine exit on its own once its current attempt completes.
		item := defaultSubscriber.itemmap[adminKey]
		delete(defaultSubscriber.itemmap, adminKey)

		if item != nil && item.stopch != nil {
			close(item.stopch)
		}
	})
})
