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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

type itemmap map[types.NamespacedName]appv1alpha1.SubsriberItem

type namespaceSubscriber struct {
	itemmap
}

var defaultSubscriber = &namespaceSubscriber{}

// Add does nothing for namespace subscriber, it generates cache for each of the item
func Add(mgr manager.Manager, syncinterval int) error {
	// No polling, so no runnable function to add for Namespace subscriber
	return nil
}

func (ns *namespaceSubscriber) SubscriberItem(item *appv1alpha1.SubsriberItem) error {
	if ns.itemmap == nil {
		ns.itemmap = make(map[types.NamespacedName]appv1alpha1.SubsriberItem)
	}

	return nil
}

func (ns *namespaceSubscriber) UnsubscribeItem(key types.NamespacedName) error {
	return nil
}

// GetSubscriberItemMap - returns the item map for all
func GetDefaultSubscriber() appv1alpha1.Subscriber {
	return defaultSubscriber
}
