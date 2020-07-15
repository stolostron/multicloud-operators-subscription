// Copyright 2020 The Kubernetes Authors.
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

package utils

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
)

func TestDeleteDeployableCRD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	crdx, err := clientsetx.NewForConfig(cfg)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	runtimeClient, err := client.New(cfg, client.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	dlist := &dplv1.DeployableList{}
	err = runtimeClient.List(context.TODO(), dlist, &client.ListOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	DeleteDeployableCRD(runtimeClient, crdx)

	dlist = &dplv1.DeployableList{}
	err = runtimeClient.List(context.TODO(), dlist, &client.ListOptions{})
	g.Expect(!errors.IsNotFound(err)).To(gomega.BeTrue())

	DeleteDeployableCRD(runtimeClient, crdx)
	err = runtimeClient.List(context.TODO(), dlist, &client.ListOptions{})
	g.Expect(!errors.IsNotFound(err)).To(gomega.BeTrue())
}

func TestIsUpdateStatus(t *testing.T) {
	var now = metav1.Now()

	var tests = []struct {
		name     string
		expected bool
		old      dplv1.DeployableStatus
		cur      dplv1.DeployableStatus
	}{
		{name: "equal",
			expected: false,
			old:      dplv1.DeployableStatus{},
			cur:      dplv1.DeployableStatus{},
		},

		{name: "shouldn't check propagateStatus",
			expected: false,
			old: dplv1.DeployableStatus{
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{},
			},
			cur: dplv1.DeployableStatus{
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{
					"a": {Reason: "no"},
				},
			},
		},

		{name: "shouldn't check lastupdatetime",
			expected: false,
			old: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					LastUpdateTime: &now,
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{},
			},
			cur: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					LastUpdateTime: &metav1.Time{},
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{
					"a": {Reason: "no"},
				},
			},
		},

		{name: "should check same static filed",
			expected: true,
			old: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason:         "no",
					LastUpdateTime: &now,
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{},
			},
			cur: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason:         "yes",
					LastUpdateTime: &metav1.Time{},
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{
					"a": {Reason: "no"},
				},
			},
		},

		{name: "should check static missing",
			expected: true,
			old: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason:         "no",
					LastUpdateTime: &now,
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{},
			},
			cur: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					LastUpdateTime: &metav1.Time{},
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{
					"a": {Reason: "no"},
				},
			},
		},

		{name: "should be false when resource b status is the same string",
			expected: false,
			old: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason:         "yes",
					LastUpdateTime: &now,
					ResourceStatus: &runtime.RawExtension{
						Raw: []byte(`
  resourceStatus:
    lastUpdateTime: "2020-07-14T14:01:32Z"
    message: Active
    phase: Subscribed
    statuses:
      /:
        packages:
          github-redhat-sa-brazil-demo-summitgov-cy20-Deployment-muyi:
            lastUpdateTime: "2020-07-14T14:00:43Z"
            phase: Subscribed
            resourceStatus:
              observedGeneration: 7658
              replicas: 1
              unavailableReplicas: 1
              updatedReplicas: 1`),
					},
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{},
			},
			cur: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason:         "yes",
					LastUpdateTime: &metav1.Time{},
					ResourceStatus: &runtime.RawExtension{
						Raw: []byte(`
  resourceStatus:
    lastUpdateTime: "2020-07-14T14:01:32Z"
    message: Active
    phase: Subscribed
    statuses:
      /:
        packages:
          github-redhat-sa-brazil-demo-summitgov-cy20-Deployment-muyi:
            lastUpdateTime: "2020-07-14T14:00:43Z"
            phase: Subscribed
            resourceStatus:
              observedGeneration: 7658
              replicas: 1
              unavailableReplicas: 1
              updatedReplicas: 1`),
					},
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{
					"a": {Reason: "no"},
				},
			},
		},

		{name: "should be false resource a status is not the same string time",
			expected: false,
			old: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason: "yes",
					ResourceStatus: &runtime.RawExtension{
						Raw: []byte(`
  resourceStatus:
    message: Active
    phase: Subscribed
    resourceStatus:
      lastUpdateTime: "2020-07-14T15:09:43Z"
      phase: Subscribed
      resourceStatus:
        observedGeneration: 7658
        replicas: 1
        unavailableReplicas: 1
        updatedReplicas: 1`),
					},
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{},
			},
			cur: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason: "yes",

					ResourceStatus: &runtime.RawExtension{
						Raw: []byte(`
  resourceStatus:
    message: Active
    phase: Subscribed
    resourceStatus:
      lastUpdateTime: "2020-07-14T14:00:43Z"
      phase: Subscribed
      resourceStatus:
        observedGeneration: 7658
        replicas: 1
        unavailableReplicas: 1
        updatedReplicas: 1`),
					},
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{
					"a": {Reason: "no"},
				},
			},
		},

		{name: "should be false resource status is compare nil ",
			expected: false,
			old: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason:         "yes",
					ResourceStatus: nil,
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{},
			},

			cur: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason:         "yes",
					ResourceStatus: nil,
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{
					"a": {Reason: "no"},
				},
			},
		},

		// this could be the edge case
		{name: "should be false resource status is not the same string compare nil with empty",
			expected: true,
			old: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason: "yes",
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{},
			},

			cur: dplv1.DeployableStatus{
				ResourceUnitStatus: dplv1.ResourceUnitStatus{
					Reason:         "yes",
					ResourceStatus: &runtime.RawExtension{},
				},
				PropagatedStatus: map[string]*dplv1.ResourceUnitStatus{
					"a": {Reason: "no"},
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			actual := isStatusUpdated(tt.old, tt.cur)
			if actual != tt.expected {
				t.Errorf("(%v, %v): expected %v, actual %v", tt.old, tt.cur, tt.expected, actual)
			}
		})
	}
}
