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
	"encoding/json"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

func TestDeleteSubscriptionCRD(t *testing.T) {
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

	slist := &appv1.SubscriptionList{}
	err = runtimeClient.List(context.TODO(), slist, &client.ListOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	DeleteSubscriptionCRD(runtimeClient, crdx)

	slist = &appv1.SubscriptionList{}
	err = runtimeClient.List(context.TODO(), slist, &client.ListOptions{})
	g.Expect(!errors.IsNotFound(err)).To(gomega.BeTrue())

	slist = &appv1.SubscriptionList{}
	err = runtimeClient.List(context.TODO(), slist, &client.ListOptions{})
	g.Expect(!errors.IsNotFound(err)).To(gomega.BeTrue())
}

func TestIsEqaulSubscriptionStatus(t *testing.T) {
	now := metav1.Now()
	resStatus := corev1.PodStatus{
		Reason: "ok",
	}

	rawResStatus, _ := json.Marshal(resStatus)

	var tests = []struct {
		name     string
		givenb   *appv1.SubscriptionStatus
		givena   *appv1.SubscriptionStatus
		expected bool
	}{

		{
			name:     "empty status should be equal",
			givena:   &appv1.SubscriptionStatus{},
			givenb:   &appv1.SubscriptionStatus{},
			expected: true,
		},

		{
			name: "should be equal even lastUpdateTime is different",
			givena: &appv1.SubscriptionStatus{
				Reason:         "test",
				LastUpdateTime: now,
				Statuses: appv1.SubscriptionClusterStatusMap{
					"/": &appv1.SubscriptionPerClusterStatus{
						SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
							"resource": {
								Phase: appv1.SubscriptionSubscribed,
							},
						},
					},
				},
			},
			givenb: &appv1.SubscriptionStatus{
				Reason:         "test",
				LastUpdateTime: metav1.NewTime(now.Add(5 * time.Second)),
				Statuses: appv1.SubscriptionClusterStatusMap{
					"/": &appv1.SubscriptionPerClusterStatus{
						SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
							"resource": {
								Phase: appv1.SubscriptionSubscribed,
							},
						},
					},
				},
			},
			expected: true,
		},

		{
			name: "should be different due to resource status",
			givena: &appv1.SubscriptionStatus{
				Reason:         "test",
				LastUpdateTime: now,
				Statuses: appv1.SubscriptionClusterStatusMap{
					"/": &appv1.SubscriptionPerClusterStatus{
						SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
							"resource": {
								Phase: appv1.SubscriptionSubscribed,
								ResourceStatus: &runtime.RawExtension{
									Raw: rawResStatus,
								},
							},
						},
					},
				},
			},
			givenb: &appv1.SubscriptionStatus{
				Reason:         "test",
				LastUpdateTime: now,
				Statuses: appv1.SubscriptionClusterStatusMap{
					"/": &appv1.SubscriptionPerClusterStatus{
						SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
							"resource": {
								Phase: appv1.SubscriptionSubscribed,
							},
						},
					},
				},
			},
			expected: false,
		},

		{
			name: "should be different due to cluster key",
			givena: &appv1.SubscriptionStatus{
				Reason:         "test",
				LastUpdateTime: now,
				Statuses: appv1.SubscriptionClusterStatusMap{
					"/": &appv1.SubscriptionPerClusterStatus{
						SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
							"resource": {
								Phase: appv1.SubscriptionSubscribed,
								ResourceStatus: &runtime.RawExtension{
									Raw: rawResStatus,
								},
							},
						},
					},
				},
			},
			givenb: &appv1.SubscriptionStatus{
				Reason:         "test",
				LastUpdateTime: now,
				Statuses: appv1.SubscriptionClusterStatusMap{
					"a": &appv1.SubscriptionPerClusterStatus{
						SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
							"resource": {
								Phase: appv1.SubscriptionSubscribed,
								ResourceStatus: &runtime.RawExtension{
									Raw: rawResStatus,
								},
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			actual := isEqualSubscriptionStatus(tt.givena, tt.givenb)

			if actual != tt.expected {
				t.Errorf("given (a: %#v b: %#v): expected %v", tt.givena, tt.givenb, tt.expected)
			}
		})
	}
}

func TestIsEmptySubscriptionStatus(t *testing.T) {
	var tests = []struct {
		name     string
		expected bool
		given    *appv1.SubscriptionStatus
	}{
		{name: "should be true, nil pointer", expected: true, given: nil},
		{name: "should be true, default status", expected: true, given: &appv1.SubscriptionStatus{}},
		{name: "should be true, default status", expected: true, given: &appv1.SubscriptionStatus{Statuses: appv1.SubscriptionClusterStatusMap{}}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			actual := isEmptySubscriptionStatus(tt.given)
			if actual != tt.expected {
				t.Errorf("(%v): expected %v, actual %v", tt.given, tt.expected, actual)
			}

		})
	}

}
