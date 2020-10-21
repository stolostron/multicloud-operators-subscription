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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

func TestSubscriptionStatusLogic(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"SubscriptionStatus check",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = Describe("subscription(s)", func() {
	Context("having 1 subscription", func() {
		It("should only detect certain empty fields", func() {
			a := &appv1.SubscriptionStatus{}

			Expect(isEmptySubscriptionStatus(a)).Should(BeTrue())

			a.LastUpdateTime = metav1.Now()
			Expect(isEmptySubscriptionStatus(a)).Should(BeTrue())

			a.Message = "test"
			Expect(isEmptySubscriptionStatus(a)).ShouldNot(BeTrue())

			a = &appv1.SubscriptionStatus{}
			a.Statuses = appv1.SubscriptionClusterStatusMap{}
			Expect(isEmptySubscriptionStatus(a)).Should(BeTrue())
		})
	})

	Context("having 2 subscriptions", func() {
		var (
			a = &appv1.SubscriptionStatus{
				Phase:             "a",
				Reason:            "",
				LastUpdateTime:    metav1.Now(),
				AnsibleJobsStatus: appv1.AnsibleJobsStatus{},
				Statuses:          appv1.SubscriptionClusterStatusMap{},
			}

			b = &appv1.SubscriptionStatus{}
		)

		It("should detect the difference on the top level while ignore time fields and ansiblejob", func() {
			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())

			b = &appv1.SubscriptionStatus{
				Phase:             "a",
				Reason:            "",
				LastUpdateTime:    metav1.Now(),
				AnsibleJobsStatus: appv1.AnsibleJobsStatus{},
				Statuses:          appv1.SubscriptionClusterStatusMap{},
			}

			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			a.AnsibleJobsStatus = appv1.AnsibleJobsStatus{
				LastPosthookJob: "aa",
			}

			b.AnsibleJobsStatus = appv1.AnsibleJobsStatus{
				LastPosthookJob: "b",
			}

			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())
		})

		It("should detect the difference on the top level while ignore time fields", func() {
			b = &appv1.SubscriptionStatus{
				Phase:             "a",
				Reason:            "",
				LastUpdateTime:    metav1.Now(),
				AnsibleJobsStatus: appv1.AnsibleJobsStatus{},
				Statuses:          appv1.SubscriptionClusterStatusMap{},
			}

			a = b.DeepCopy()

			a.Statuses = appv1.SubscriptionClusterStatusMap{
				"/": &appv1.SubscriptionPerClusterStatus{},
			}

			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())

			b.Statuses = appv1.SubscriptionClusterStatusMap{
				"/": &appv1.SubscriptionPerClusterStatus{},
			}

			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			// the managed cluster level have a none nil and nil
			a.Statuses["/"] = nil
			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())

			b.Statuses["/"] = nil
			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			a.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: nil,
			}

			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())
			b.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: nil,
			}

			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			//package level a nil against empty map
			a.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{},
			}

			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			// extra package info
			a.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {},
				},
			}

			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())

			b.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {},
				},
			}
			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			a.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {
						Phase:  "a",
						Reason: "ab",
					},
				},
			}

			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())

			b.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {
						Phase:  "a",
						Reason: "ab",
					},
				},
			}
			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			// having extra package
			a.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {
						Phase:  "a",
						Reason: "ab",
					},
					"srt": {},
				},
			}
			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())

			//should ignore the package level LastUpdateTime
			b.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {
						Phase:          "a",
						Reason:         "ab",
						LastUpdateTime: metav1.Now(),
					},
					"srt": {},
				},
			}
			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			// having extra package raw data
			a.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {
						Phase:          "a",
						Reason:         "ab",
						ResourceStatus: &runtime.RawExtension{},
					},
					"srt": {},
				},
			}
			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())

			b.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {
						Phase:          "a",
						Reason:         "ab",
						ResourceStatus: &runtime.RawExtension{},
					},
					"srt": {},
				},
			}
			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			a.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {
						Phase:  "a",
						Reason: "ab",
						ResourceStatus: &runtime.RawExtension{
							Raw: []byte("ad,12"),
						},
					},
					"srt": {},
				},
			}
			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())

			b.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {
						Phase:  "a",
						Reason: "ab",
						ResourceStatus: &runtime.RawExtension{
							Raw: []byte("ad,12"),
						},
					},
					"srt": {},
				},
			}
			Expect(isEqualSubscriptionStatus(a, b)).Should(BeTrue())

			//check the reflect.DeepEqual logic on the ResourceStatus
			b.Statuses["/"] = &appv1.SubscriptionPerClusterStatus{
				SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
					"cfg": {
						Phase:  "a",
						Reason: "ab",
						ResourceStatus: &runtime.RawExtension{
							Raw: []byte("ad,123"),
						},
					},
					"srt": {},
				},
			}
			Expect(isEqualSubscriptionStatus(a, b)).ShouldNot(BeTrue())
		})
	})
})

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

		{
			name: "should be different due to cluster key",
			givena: &appv1.SubscriptionStatus{
				Reason:         "test",
				LastUpdateTime: now,
				Statuses: appv1.SubscriptionClusterStatusMap{
					"/": &appv1.SubscriptionPerClusterStatus{
						SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
							"package-a": {
								Phase: appv1.SubscriptionSubscribed,
								ResourceStatus: &runtime.RawExtension{
									Raw: rawResStatus,
								},
							},

							"package-b": {
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
							"package-a": {
								Phase:          appv1.SubscriptionSubscribed,
								LastUpdateTime: metav1.NewTime(now.Add(5 * time.Second)),
								ResourceStatus: &runtime.RawExtension{
									Raw: rawResStatus,
								},
							},

							"package-b": {
								Phase:          appv1.SubscriptionSubscribed,
								LastUpdateTime: metav1.NewTime(now.Add(5 * time.Second)),
								ResourceStatus: &runtime.RawExtension{
									Raw: rawResStatus,
								},
							},
						},
					},
				},
			},
			expected: true,
		},

		{
			name: "should be different due to cluster key",
			givena: &appv1.SubscriptionStatus{
				Reason:         "test",
				LastUpdateTime: now,
				Statuses: appv1.SubscriptionClusterStatusMap{
					"/": &appv1.SubscriptionPerClusterStatus{
						SubscriptionPackageStatus: map[string]*appv1.SubscriptionUnitStatus{
							"package-a": {
								Phase: appv1.SubscriptionSubscribed,
								ResourceStatus: &runtime.RawExtension{
									Raw: rawResStatus,
								},
							},

							"package-b": {
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
							"package-a": {
								Phase:          appv1.SubscriptionSubscribed,
								LastUpdateTime: metav1.NewTime(now.Add(5 * time.Second)),
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
		{name: "should be true, default status", expected: false, given: &appv1.SubscriptionStatus{Message: "test"}},
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

func TestIsSubscriptionBasicChanged(t *testing.T) {
	var tests = []struct {
		name     string
		expected bool
		oldIns   *appv1.Subscription
		newIns   *appv1.Subscription
	}{
		{
			name:     "",
			expected: false,
			oldIns: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						//nolint
						"apps.open-cluster-management.io/deployables": "test/pacman-subscription-0-ansible-pacman-mongo-deployment,test/pacman-subscription-0-ansible-pacman-pacman-deployment,test/pacman-subscription-0-ansible-pacman-f5-gslb-pacman-route,test/pacman-subscription-0-ansible-pacman-pacman-route,test/pacman-subscription-0-ansible-pacman-mongo-service,test/pacman-subscription-0-ansible-pacman-pacman-service,test/pacman-subscription-0-ansible-pacman-mongo-storage-persistentvolumeclaim",
						"apps.open-cluster-management.io/git-branch":  "master",
						"apps.open-cluster-management.io/git-commit":  "389b2a1f023caa314a4a92c3831d86bbff0acf08",
						"apps.open-cluster-management.io/git-path":    "ansible/pacman",
						//nolint
						"apps.open-cluster-management.io/topo":     "deployable//Deployment//mongo/1,deployable//Deployment//pacman/1,deployable//Route//f5-gslb-pacman/0,deployable//Route//pacman/0,deployable//Service//mongo/0,deployable//Service//pacman/0,deployable//PersistentVolumeClaim//mongo-storage/0,hook//AnsibleJob/test/service-now-nginx-demo-1-389b2a/0,hook//AnsibleJob/test/f5-update-dns-load-balancer-1-389b2a/0",
						"open-cluster-management.io/user-group":    "c3lzdGVtOmNsdXN0ZXItYWRtaW5zLHN5c3RlbTphdXRoZW50aWNhdGVk",
						"open-cluster-management.io/user-identity": "a3ViZTphZG1pbg==",
					},
				},
			},

			newIns: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						//nolint
						"apps.open-cluster-management.io/deployables": "test/pacman-subscription-0-ansible-pacman-mongo-storage-persistentvolumeclaim,test/pacman-subscription-0-ansible-pacman-mongo-deployment,test/pacman-subscription-0-ansible-pacman-pacman-deployment,test/pacman-subscription-0-ansible-pacman-f5-gslb-pacman-route,test/pacman-subscription-0-ansible-pacman-pacman-route,test/pacman-subscription-0-ansible-pacman-mongo-service,test/pacman-subscription-0-ansible-pacman-pacman-service",
						"apps.open-cluster-management.io/git-branch":  "master",
						"apps.open-cluster-management.io/git-commit":  "389b2a1f023caa314a4a92c3831d86bbff0acf08",
						"apps.open-cluster-management.io/git-path":    "ansible/pacman",
						//nolint
						"apps.open-cluster-management.io/topo":     "deployable//Service//mongo/0,deployable//Service//pacman/0,deployable//PersistentVolumeClaim//mongo-storage/0,deployable//Deployment//mongo/1,deployable//Deployment//pacman/1,deployable//Route//f5-gslb-pacman/0,deployable//Route//pacman/0,hook//AnsibleJob/test/service-now-nginx-demo-1-389b2a/0,hook//AnsibleJob/test/f5-update-dns-load-balancer-1-389b2a/0",
						"open-cluster-management.io/user-group":    "c3lzdGVtOmNsdXN0ZXItYWRtaW5zLHN5c3RlbTphdXRoZW50aWNhdGVk",
						"open-cluster-management.io/user-identity": "a3ViZTphZG1pbg==",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			actual := IsSubscriptionBasicChanged(tt.oldIns, tt.newIns)
			if actual != tt.expected {
				t.Errorf("(%s): expected %v, actual %v", tt.name, tt.expected, actual)
			}
		})
	}
}
