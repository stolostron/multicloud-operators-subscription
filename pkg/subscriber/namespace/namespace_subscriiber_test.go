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
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	kubesynchronizer "github.com/IBM/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
)

var c client.Client

var id = types.NamespacedName{
	Name:      "endpoint",
	Namespace: "enpoint-ns",
}

func TestSubscriber(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()
	sync, err := kubesynchronizer.CreateSynchronizer(cfg, cfg, &id, 10, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sub := CreateNamespaceSubsriber(cfg, mgr.GetScheme(), mgr, sync)

	sub.SubscribeItem(defaultitem)

	g.Expect(err).NotTo(gomega.HaveOccurred())
}
