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
	e "errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	manifestWorkV1 "open-cluster-management.io/api/work/v1"
	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
)

var (
	oldDecision = &clusterv1beta1.PlacementDecision{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PlacementDecision",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
		Status: clusterv1beta1.PlacementDecisionStatus{
			Decisions: []clusterv1beta1.ClusterDecision{{ClusterName: "cluster1", Reason: "running"}},
		},
	}

	fakeAppsubStatus = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"apps.open-cluster-management.io/cluster": "true",
			},
		},
	}

	oldAppsubStatus = &appv1alpha1.SubscriptionReport{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "default",
			Labels: map[string]string{
				"apps.open-cluster-management.io/cluster": "true",
			},
		},
	}

	newAppsubStatus = &appv1alpha1.SubscriptionReport{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: "cluster.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "default",
			Labels: map[string]string{
				"apps.open-cluster-management.io/cluster": "true",
			},
		},
	}

	oldChn = &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				appv1.AnnotationGitBranch: "master",
			},
		},
	}

	newChn = &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				appv1.AnnotationGitBranch: "main",
			},
		},
	}
	serviceProper = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      addonServiceAccountName,
			Namespace: addonServiceAccountNamespace,
		},
	}

	serviceImproer = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "application-manager-1",
			Namespace: "open-cluster-management-agent-addon-1",
		},
	}
)

func TestSubscriptionStatusLogic(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t,
		"SubscriptionStatus check")
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
	g := NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(HaveOccurred())

	c = mgr.GetClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	crdx, err := clientsetx.NewForConfig(cfg)
	g.Expect(err).NotTo(HaveOccurred())

	runtimeClient, err := client.New(cfg, client.Options{})
	g.Expect(err).NotTo(HaveOccurred())

	slist := &appv1.SubscriptionList{}
	err = runtimeClient.List(context.TODO(), slist, &client.ListOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	DeleteSubscriptionCRD(runtimeClient, crdx)

	slist = &appv1.SubscriptionList{}
	err = runtimeClient.List(context.TODO(), slist, &client.ListOptions{})
	g.Expect(!errors.IsNotFound(err)).To(BeTrue())

	slist = &appv1.SubscriptionList{}
	err = runtimeClient.List(context.TODO(), slist, &client.ListOptions{})
	g.Expect(!errors.IsNotFound(err)).To(BeTrue())
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

func TestGetHostSubscriptionFromObject(t *testing.T) {
	var tests = []struct {
		name     string
		expected *types.NamespacedName
		obj      metav1.Object
	}{
		{
			name:     "nil object",
			expected: nil,
			obj:      nil,
		},
		{
			name:     "nil annotations",
			expected: nil,
			obj: &metav1.ObjectMeta{
				Annotations: nil,
			},
		},
		{
			name:     "empty annotations value",
			expected: nil,
			obj: &metav1.ObjectMeta{
				Annotations: map[string]string{
					appv1.AnnotationHosting: "",
				},
			},
		},
		{
			name:     "invalid annotation",
			expected: nil,
			obj: &metav1.ObjectMeta{
				Annotations: map[string]string{
					appv1.AnnotationHosting: "no name slash namespace",
				},
			},
		},
		{
			name:     "valid annotation",
			expected: &types.NamespacedName{Name: "test", Namespace: "testnamespace"},
			obj: &metav1.ObjectMeta{
				Annotations: map[string]string{
					appv1.AnnotationHosting: "testnamespace/test",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := GetHostSubscriptionFromObject(tt.obj)
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("(%s): expected %v, actual %v", tt.name, tt.expected, actual)
			}
		})
	}
}

func TestGetPauseLabel(t *testing.T) {
	var tests = []struct {
		name     string
		expected bool
		appsub   *appv1.Subscription
	}{
		{
			name:     "no labels",
			expected: false,
			appsub: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Labels: nil,
				},
			},
		},
		{
			name:     "LabelSubscriptionPause is true",
			expected: true,
			appsub: &appv1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appv1.LabelSubscriptionPause: "true",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := GetPauseLabel(tt.appsub)
			if actual != tt.expected {
				t.Errorf("(%s): expected %v, actual %v", tt.name, tt.expected, actual)
			}
		})
	}
}

func TestRemoveSubAnnotations(t *testing.T) {
	var tests = []struct {
		name     string
		obj      *unstructured.Unstructured
		expected *unstructured.Unstructured
	}{
		{
			name: "No annotations",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": nil,
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{},
				},
			},
		},
		{
			name: "objanno = 0",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							appv1.AnnotationClusterAdmin:      "true",
							appv1.AnnotationHosting:           "",
							appv1.AnnotationSyncSource:        "subnsdpl",
							appv1.AnnotationHostingDeployable: "",
							appv1.AnnotationChannelType:       "",
						},
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{},
				},
			},
		},
		{
			name: "objanno > 0",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							appv1.AnnotationClusterAdmin:           "true",
							appv1.AnnotationHosting:                "",
							appv1.AnnotationSyncSource:             "subnsdpl",
							appv1.AnnotationHostingDeployable:      "",
							appv1.AnnotationChannelType:            "",
							appv1.AnnotationResourceReconcileLevel: "med",
						},
					},
				},
			},
			expected: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							appv1.AnnotationResourceReconcileLevel: "med",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := RemoveSubAnnotations(tt.obj)
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("(%s): expected %v, actual %v", tt.name, tt.expected, actual)
			}
		})
	}
}

func TestRemoveSubOwnerRef(t *testing.T) {
	g := NewGomegaWithT(t)

	// newOwnerRefs = 0
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"OwnerReferences": []metav1.OwnerReference{},
			},
		},
	}

	expected := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"OwnerReferences": []metav1.OwnerReference{},
			},
		},
	}

	g.Expect(RemoveSubOwnerRef(obj)).To(Equal(expected))

	// newOwnerRefs > 0
	obj2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"OwnerReferences": []metav1.OwnerReference{
					{Kind: "newOwnerRefs > 0"},
				},
			},
		},
	}

	expected2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"OwnerReferences": []metav1.OwnerReference{
					{Kind: "newOwnerRefs > 0"},
				},
			},
		},
	}

	obj2.SetOwnerReferences([]metav1.OwnerReference{{Kind: "newOwnerRefs > 0"}})
	expected2.SetOwnerReferences([]metav1.OwnerReference{{Kind: "newOwnerRefs > 0"}})

	g.Expect(*RemoveSubOwnerRef(obj2)).To(Equal(*expected2))
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
						"apps.open-cluster-management.io/deployables":        "test/pacman-subscription-0-ansible-pacman-mongo-deployment,test/pacman-subscription-0-ansible-pacman-pacman-deployment,test/pacman-subscription-0-ansible-pacman-f5-gslb-pacman-route,test/pacman-subscription-0-ansible-pacman-pacman-route,test/pacman-subscription-0-ansible-pacman-mongo-service,test/pacman-subscription-0-ansible-pacman-pacman-service,test/pacman-subscription-0-ansible-pacman-mongo-storage-persistentvolumeclaim",
						"apps.open-cluster-management.io/git-branch":         "main",
						"apps.open-cluster-management.io/git-current-commit": "389b2a1f023caa314a4a92c3831d86bbff0acf08",
						"apps.open-cluster-management.io/git-path":           "ansible/pacman",
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
						"apps.open-cluster-management.io/deployables":        "test/pacman-subscription-0-ansible-pacman-mongo-storage-persistentvolumeclaim,test/pacman-subscription-0-ansible-pacman-mongo-deployment,test/pacman-subscription-0-ansible-pacman-pacman-deployment,test/pacman-subscription-0-ansible-pacman-f5-gslb-pacman-route,test/pacman-subscription-0-ansible-pacman-pacman-route,test/pacman-subscription-0-ansible-pacman-mongo-service,test/pacman-subscription-0-ansible-pacman-pacman-service",
						"apps.open-cluster-management.io/git-branch":         "main",
						"apps.open-cluster-management.io/git-current-commit": "389b2a1f023caa314a4a92c3831d86bbff0acf08",
						"apps.open-cluster-management.io/git-path":           "ansible/pacman",
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

func TestGetReconcileRate(t *testing.T) {
	g := NewGomegaWithT(t)

	chnAnnotations := make(map[string]string)
	subAnnotations := make(map[string]string)

	// defaut medium
	g.Expect(GetReconcileRate(chnAnnotations, subAnnotations)).To(Equal("medium"))

	chnAnnotations[appv1.AnnotationResourceReconcileLevel] = "off"
	g.Expect(GetReconcileRate(chnAnnotations, subAnnotations)).To(Equal("off"))

	chnAnnotations[appv1.AnnotationResourceReconcileLevel] = "low"
	g.Expect(GetReconcileRate(chnAnnotations, subAnnotations)).To(Equal("low"))

	chnAnnotations[appv1.AnnotationResourceReconcileLevel] = "medium"
	g.Expect(GetReconcileRate(chnAnnotations, subAnnotations)).To(Equal("medium"))

	chnAnnotations[appv1.AnnotationResourceReconcileLevel] = "high"
	g.Expect(GetReconcileRate(chnAnnotations, subAnnotations)).To(Equal("high"))

	// Subscription can override reconcile level to be off
	subAnnotations[appv1.AnnotationResourceReconcileLevel] = "off"
	g.Expect(GetReconcileRate(chnAnnotations, subAnnotations)).To(Equal("off"))

	// Subscription can not override reconcile level to be something other than off
	subAnnotations[appv1.AnnotationResourceReconcileLevel] = "low"
	g.Expect(GetReconcileRate(chnAnnotations, subAnnotations)).To(Equal("high"))

	// If annotation has unknown value, default to medium
	chnAnnotations[appv1.AnnotationResourceReconcileLevel] = "mediumhigh"
	g.Expect(GetReconcileRate(chnAnnotations, subAnnotations)).To(Equal("medium"))
}

func TestGetReconcileInterval(t *testing.T) {
	g := NewGomegaWithT(t)

	// these intervals are not used if off. Just default values
	loopPeriod, retryInterval, retries := GetReconcileInterval("off", chnv1.ChannelTypeGit)

	g.Expect(loopPeriod).To(Equal(3 * time.Minute))
	g.Expect(retryInterval).To(Equal(90 * time.Second))
	g.Expect(retries).To(Equal(1))

	// if reconcile rate is unknown, just default values
	loopPeriod, retryInterval, retries = GetReconcileInterval("unknown", chnv1.ChannelTypeGit)

	g.Expect(loopPeriod).To(Equal(3 * time.Minute))
	g.Expect(retryInterval).To(Equal(90 * time.Second))
	g.Expect(retries).To(Equal(1))

	loopPeriod, retryInterval, retries = GetReconcileInterval("low", chnv1.ChannelTypeGit)

	g.Expect(loopPeriod).To(Equal(1 * time.Hour))
	g.Expect(retryInterval).To(Equal(3 * time.Minute))
	g.Expect(retries).To(Equal(3))

	loopPeriod, retryInterval, retries = GetReconcileInterval("medium", chnv1.ChannelTypeGit)

	g.Expect(loopPeriod).To(Equal(3 * time.Minute))
	g.Expect(retryInterval).To(Equal(90 * time.Second))
	g.Expect(retries).To(Equal(1))

	loopPeriod, retryInterval, retries = GetReconcileInterval("medium", chnv1.ChannelTypeHelmRepo)

	g.Expect(loopPeriod).To(Equal(15 * time.Minute))
	g.Expect(retryInterval).To(Equal(90 * time.Second))
	g.Expect(retries).To(Equal(1))

	loopPeriod, retryInterval, retries = GetReconcileInterval("medium", chnv1.ChannelTypeObjectBucket)

	g.Expect(loopPeriod).To(Equal(15 * time.Minute))
	g.Expect(retryInterval).To(Equal(90 * time.Second))
	g.Expect(retries).To(Equal(1))

	loopPeriod, retryInterval, retries = GetReconcileInterval("high", chnv1.ChannelTypeGit)

	g.Expect(loopPeriod).To(Equal(2 * time.Minute))
	g.Expect(retryInterval).To(Equal(60 * time.Second))
	g.Expect(retries).To(Equal(1))
}

func TestIsSameUnstructured(t *testing.T) {
	g := NewGomegaWithT(t)

	// Both empty unstructured objects
	obj1 := &unstructured.Unstructured{}
	obj2 := &unstructured.Unstructured{}

	g.Expect(isSameUnstructured(obj1, obj2)).To(BeTrue())

	// Differing GVK
	obj1, obj2 = &unstructured.Unstructured{}, &unstructured.Unstructured{}
	obj1.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Kind: "kind", Version: "v1"})
	obj2.SetGroupVersionKind(schema.GroupVersionKind{Group: "Different group", Kind: "kind", Version: "v1"})

	g.Expect(isSameUnstructured(obj1, obj2)).To(BeFalse())

	// Differing Name
	obj1, obj2 = &unstructured.Unstructured{}, &unstructured.Unstructured{}
	obj1.SetName("obj1")
	obj2.SetName("Obj2")

	g.Expect(isSameUnstructured(obj1, obj2)).To(BeFalse())

	// Differing Namespace
	obj1, obj2 = &unstructured.Unstructured{}, &unstructured.Unstructured{}
	obj1.SetNamespace("namespace1")
	obj2.SetNamespace("namespace2")

	g.Expect(isSameUnstructured(obj1, obj2)).To(BeFalse())

	// Differing Labels
	obj1, obj2 = &unstructured.Unstructured{}, &unstructured.Unstructured{}
	obj1.SetLabels(map[string]string{"local-cluster": "true"})
	obj2.SetLabels(map[string]string{"local-cluster": "false"})

	g.Expect(isSameUnstructured(obj1, obj2)).To(BeFalse())

	// Differing Annotations
	obj1, obj2 = &unstructured.Unstructured{}, &unstructured.Unstructured{}
	obj1.SetAnnotations(map[string]string{appv1.AnnotationGitBranch: "main"})
	obj2.SetAnnotations(map[string]string{appv1.AnnotationGitBranch: "master"})

	g.Expect(isSameUnstructured(obj1, obj2)).To(BeFalse())
}

func TestIsHostingAppsub(t *testing.T) {
	g := NewGomegaWithT(t)

	// nil appsub
	g.Expect(IsHostingAppsub(nil)).To(BeFalse())

	// nil annotations
	appsub := &appv1.Subscription{}
	appsub.SetAnnotations(nil)

	g.Expect(IsHostingAppsub(appsub)).To(BeFalse())

	// Contains annotation AnnotationHosting
	appsub = &appv1.Subscription{}
	appsub.SetAnnotations(map[string]string{appv1.AnnotationHosting: "testnamespace/testname"})

	g.Expect(IsHostingAppsub(appsub)).To(BeTrue())
}

func TestParseAPIVersion(t *testing.T) {
	g := NewGomegaWithT(t)

	// missing "/"
	apiVersion := "v1"

	group, version := ParseAPIVersion(apiVersion)
	g.Expect(group).To(Equal(""))
	g.Expect(version).To(Equal("v1"))

	// Group & version
	apiVersion = "apps.open-cluster-management.io/v1"

	group, version = ParseAPIVersion(apiVersion)
	g.Expect(group).To(Equal("apps.open-cluster-management.io"))
	g.Expect(version).To(Equal("v1"))

	// More than just group & version
	apiVersion = "apps.open-cluster-management.io/v1/v2/v3"

	group, version = ParseAPIVersion(apiVersion)
	g.Expect(group).To(Equal(""))
	g.Expect(version).To(Equal(""))
}

func TestParseNamespacedName(t *testing.T) {
	g := NewGomegaWithT(t)

	// missing "/" invalid namespace
	namespacedName := "mynamespacename"

	namespace, name := ParseNamespacedName(namespacedName)
	g.Expect(namespace).To(Equal(""))
	g.Expect(name).To(Equal(""))

	// valid namespace & name
	namespacedName = "mynamespace/name"

	namespace, name = ParseNamespacedName(namespacedName)
	g.Expect(namespace).To(Equal("mynamespace"))
	g.Expect(name).To(Equal("name"))
}
func TestIsResourceAllowed(t *testing.T) {
	g := NewGomegaWithT(t)

	// Not admin
	resource := unstructured.Unstructured{}
	allowlist := make(map[string]map[string]string)

	g.Expect(IsResourceAllowed(resource, allowlist, false)).To(BeTrue())

	// Not admin but policy
	resource = unstructured.Unstructured{}
	resource.SetAPIVersion("policy.open-cluster-management.io/v1")

	allowlist = make(map[string]map[string]string)

	g.Expect(IsResourceAllowed(resource, allowlist, false)).To(BeFalse())

	// Admin, len(allowlist) = 0
	resource = unstructured.Unstructured{}
	allowlist = make(map[string]map[string]string)

	g.Expect(IsResourceAllowed(resource, allowlist, true)).To(BeTrue())

	// Admin, allowlist has Kind
	resource = unstructured.Unstructured{}
	resource.SetAPIVersion("policy.open-cluster-management.io/v1")
	resource.SetKind("Deployment")

	allowlist = make(map[string]map[string]string)
	allowlist[resource.GetAPIVersion()] = make(map[string]string)
	allowlist[resource.GetAPIVersion()][resource.GetKind()] = "true"

	g.Expect(IsResourceAllowed(resource, allowlist, true)).To(BeTrue())
}

func TestIsResourceDenied(t *testing.T) {
	g := NewGomegaWithT(t)

	// Not admin
	resource := unstructured.Unstructured{}
	allowlist := make(map[string]map[string]string)

	g.Expect(IsResourceDenied(resource, allowlist, false)).To(BeFalse())

	// Admin, len(allowlist) = 0
	resource = unstructured.Unstructured{}
	allowlist = make(map[string]map[string]string)

	g.Expect(IsResourceDenied(resource, allowlist, true)).To(BeFalse())

	// Admin, denylist has Kind
	resource = unstructured.Unstructured{}
	resource.SetAPIVersion("policy.open-cluster-management.io/v1")
	resource.SetKind("Deployment")

	allowlist = make(map[string]map[string]string)
	allowlist[resource.GetAPIVersion()] = make(map[string]string)
	allowlist[resource.GetAPIVersion()][resource.GetKind()] = "true"

	g.Expect(IsResourceDenied(resource, allowlist, true)).To(BeTrue())
}

func TestGetAllowDenyLists(t *testing.T) {
	g := NewGomegaWithT(t)

	// Empty allowed, denied lists
	sub := appv1.Subscription{}

	allowedResources, deniedResources := GetAllowDenyLists(sub)
	g.Expect(allowedResources).To(Equal(make(map[string]map[string]string)))
	g.Expect(deniedResources).To(Equal(make(map[string]map[string]string)))

	// both allowed & denied Resources, nil APIVersion
	sub = appv1.Subscription{}
	sub.Spec.Allow = append(sub.Spec.Allow,
		&appv1.AllowDenyItem{APIVersion: "", Kinds: []string{"Git"}})
	sub.Spec.Deny = append(sub.Spec.Deny,
		&appv1.AllowDenyItem{APIVersion: "", Kinds: []string{"Objectstore"}})

	// Function returns two map[string]map[string]string
	// Need to make the expected maps beforehand
	expectedAllowedResources := make(map[string]map[string]string)
	expectedAllowedResources[""] = make(map[string]string)
	expectedAllowedResources[""]["Git"] = "Git"
	expectedDeniedResources := make(map[string]map[string]string)
	expectedDeniedResources[""] = make(map[string]string)
	expectedDeniedResources[""]["Objectstore"] = "Objectstore"

	allowedResources, deniedResources = GetAllowDenyLists(sub)
	g.Expect(allowedResources).To(Equal(expectedAllowedResources))
	g.Expect(deniedResources).To(Equal(expectedDeniedResources))
}

func TestCompareManifestWork(t *testing.T) {
	g := NewGomegaWithT(t)

	// Empty manifestWorks
	oldManifestWork, newManifestWork := &manifestWorkV1.ManifestWork{}, &manifestWorkV1.ManifestWork{}

	g.Expect(CompareManifestWork(oldManifestWork, newManifestWork)).To(BeTrue())

	// Differing sizes
	oldManifestWork = &manifestWorkV1.ManifestWork{}
	newManifestWork = &manifestWorkV1.ManifestWork{}

	oldManifestWork.Spec.Workload.Manifests = append(oldManifestWork.Spec.Workload.Manifests, manifestWorkV1.Manifest{})

	g.Expect(CompareManifestWork(oldManifestWork, newManifestWork)).To(BeFalse())

	// Equal manifestWorks
	oldManifestWork = &manifestWorkV1.ManifestWork{}
	newManifestWork = &manifestWorkV1.ManifestWork{}

	// Need json formatted
	endpointNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "appsubNS",
			Annotations: map[string]string{
				appv1.AnnotationHosting: types.NamespacedName{Name: "test", Namespace: "testnamespace"}.String(),
			},
		},
	}
	manifestNSByte, _ := json.Marshal(endpointNS)

	oldManifestWork.Spec.Workload.Manifests = append(oldManifestWork.Spec.Workload.Manifests,
		manifestWorkV1.Manifest{RawExtension: runtime.RawExtension{Raw: manifestNSByte}})

	newManifestWork.Spec.Workload.Manifests = append(newManifestWork.Spec.Workload.Manifests,
		manifestWorkV1.Manifest{RawExtension: runtime.RawExtension{Raw: manifestNSByte}})

	g.Expect(CompareManifestWork(oldManifestWork, newManifestWork)).To(BeTrue())

	// Differing manifestWorks
	oldManifestWork = &manifestWorkV1.ManifestWork{}
	newManifestWork = &manifestWorkV1.ManifestWork{}

	oldManifestWork.Spec.Workload.Manifests = append(oldManifestWork.Spec.Workload.Manifests,
		manifestWorkV1.Manifest{RawExtension: runtime.RawExtension{Raw: manifestNSByte}})
	// Need json formatted
	endpointNS = &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "appsubNS",
			Annotations: map[string]string{
				appv1.AnnotationHosting: types.NamespacedName{Name: "test-different", Namespace: "testnamespace"}.String(),
			},
		},
	}
	manifestNSByte, _ = json.Marshal(endpointNS)

	newManifestWork.Spec.Workload.Manifests = append(newManifestWork.Spec.Workload.Manifests,
		manifestWorkV1.Manifest{RawExtension: runtime.RawExtension{Raw: manifestNSByte}})

	g.Expect(CompareManifestWork(oldManifestWork, newManifestWork)).To(BeFalse())

	// Fail to unmarshal new manifestwork
	oldManifestWork = &manifestWorkV1.ManifestWork{}
	newManifestWork = &manifestWorkV1.ManifestWork{}

	oldManifestWork.Spec.Workload.Manifests = append(oldManifestWork.Spec.Workload.Manifests,
		manifestWorkV1.Manifest{RawExtension: runtime.RawExtension{Raw: manifestNSByte}})

	newManifestWork.Spec.Workload.Manifests = append(newManifestWork.Spec.Workload.Manifests,
		manifestWorkV1.Manifest{RawExtension: runtime.RawExtension{Raw: []byte("fail to unmarshal me >;(")}})

	g.Expect(CompareManifestWork(oldManifestWork, newManifestWork)).To(BeFalse())

	// Fail to unmarshal old manifestwork
	oldManifestWork = &manifestWorkV1.ManifestWork{}
	newManifestWork = &manifestWorkV1.ManifestWork{}

	oldManifestWork.Spec.Workload.Manifests = append(oldManifestWork.Spec.Workload.Manifests,
		manifestWorkV1.Manifest{RawExtension: runtime.RawExtension{Raw: []byte("fail to unmarshal me >:(")}})

	newManifestWork.Spec.Workload.Manifests = append(newManifestWork.Spec.Workload.Manifests,
		manifestWorkV1.Manifest{RawExtension: runtime.RawExtension{Raw: manifestNSByte}})

	g.Expect(CompareManifestWork(oldManifestWork, newManifestWork)).To(BeFalse())
}

func TestIsSubscriptionResourceChanged(t *testing.T) {
	g := NewGomegaWithT(t)

	// phase = ""
	oSub, nSub := &appv1.Subscription{}, &appv1.Subscription{}

	g.Expect(IsSubscriptionResourceChanged(oSub, nSub)).To(BeTrue())

	// phase != "" & oSub phase == nsub phase
	oSub.Status.Phase = "same"
	nSub.Status.Phase = "same"

	g.Expect(IsSubscriptionResourceChanged(oSub, nSub)).To(BeFalse())
}

func TestIsHubRelatedStatusChanged(t *testing.T) {
	g := NewGomegaWithT(t)

	old, nnew := &appv1.SubscriptionStatus{}, &appv1.SubscriptionStatus{}

	g.Expect(IsHubRelatedStatusChanged(old, nnew)).To(BeFalse())

	// differing phases
	old = &appv1.SubscriptionStatus{Phase: "a"}
	nnew = &appv1.SubscriptionStatus{Phase: "b"}

	g.Expect(IsHubRelatedStatusChanged(old, nnew)).To(BeTrue())

	// differing PosthookJob
	old = &appv1.SubscriptionStatus{}
	nnew = &appv1.SubscriptionStatus{}

	old.AnsibleJobsStatus = appv1.AnsibleJobsStatus{LastPosthookJob: "aa"}
	nnew.AnsibleJobsStatus = appv1.AnsibleJobsStatus{LastPosthookJob: "b"}

	g.Expect(IsHubRelatedStatusChanged(old, nnew)).To(BeTrue())

	// differing PrehookJob
	old = &appv1.SubscriptionStatus{}
	nnew = &appv1.SubscriptionStatus{}

	old.AnsibleJobsStatus = appv1.AnsibleJobsStatus{LastPrehookJob: "aa"}
	nnew.AnsibleJobsStatus = appv1.AnsibleJobsStatus{LastPrehookJob: "b"}

	g.Expect(IsHubRelatedStatusChanged(old, nnew)).To(BeTrue())

	// differing statuses
	old = &appv1.SubscriptionStatus{
		Statuses: appv1.SubscriptionClusterStatusMap{"/": &appv1.SubscriptionPerClusterStatus{}}}
	nnew = &appv1.SubscriptionStatus{}

	g.Expect(IsHubRelatedStatusChanged(old, nnew)).To(BeTrue())
}

func TestGetReleaseName(t *testing.T) {
	g := NewGomegaWithT(t)

	base, err := GetReleaseName("shorter than max length")

	g.Expect(base).To(Equal("shorter than max length"))
	g.Expect(err).NotTo(HaveOccurred())

	base, err = GetReleaseName("larger than max length (52 - len('-delete-registrations')")
	g.Expect(base[:25]).To(Equal("larger than max length (5"))
	g.Expect(err).NotTo(HaveOccurred())
}

func TestSetPartOfLabel(t *testing.T) {
	g := NewGomegaWithT(t)

	// No app label in subscription
	sub := &appv1.Subscription{}
	obj := &unstructured.Unstructured{}
	SetPartOfLabel(sub, obj)
	labels := obj.GetLabels()
	g.Expect(labels).To(BeNil())

	// Has app label in subscription
	subLabels := make(map[string]string)
	subLabels["app.kubernetes.io/part-of"] = "testApp"
	sub.Labels = subLabels
	SetPartOfLabel(sub, obj)
	labels = obj.GetLabels()
	g.Expect(labels).NotTo(BeNil())
	g.Expect(labels["app.kubernetes.io/part-of"]).To(Equal("testApp"))
}

func TestSetInClusterPackageStatus(t *testing.T) {
	g := NewGomegaWithT(t)

	var pkgErr error

	substatus := &appv1.SubscriptionStatus{}

	// nil status
	err := SetInClusterPackageStatus(substatus, "foo", pkgErr, nil)
	g.Expect(err).NotTo(HaveOccurred())

	type fakeStatus struct {
		name string
	}

	status := fakeStatus{name: "fakeStatus"}
	pkgErr = e.New("Fake error")

	err = SetInClusterPackageStatus(substatus, "foo", pkgErr, status)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestIsHub(t *testing.T) {
	g := NewGomegaWithT(t)

	g.Expect(IsHub(cfg)).To(BeFalse())
}

func TestAllowApplyTemplate(t *testing.T) {
	g := NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(HaveOccurred())

	c = mgr.GetClient()

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	runtimeClient, err := client.New(cfg, client.Options{})
	g.Expect(err).NotTo(HaveOccurred())

	// Template with subscription kind
	templateSub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "Subscription",
		},
	}
	g.Expect(AllowApplyTemplate(runtimeClient, templateSub)).To(BeTrue())

	// Template without subscription kind
	templateEmpty := &unstructured.Unstructured{
		Object: map[string]interface{}{},
	}
	g.Expect(AllowApplyTemplate(runtimeClient, templateEmpty)).To(BeTrue())

	// Fail to get subscription obj
	templateFail := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "example",
				"namespace": "ns",
				"annotations": map[string]interface{}{
					appv1.AnnotationClusterAdmin: "true",
					appv1.AnnotationHosting:      "ns/example",
				},
			},
		},
	}
	g.Expect(AllowApplyTemplate(runtimeClient, templateFail)).To(BeTrue())
}

func TestOverrideResourceBySubscription(t *testing.T) {
	g := NewGomegaWithT(t)

	templateSub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "Subscription",
		},
	}
	i := &appv1.Subscription{}

	returnedTemplate, err := OverrideResourceBySubscription(templateSub, "foo", i)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(returnedTemplate).To(Equal(templateSub))

	i.Spec.PackageOverrides = append(i.Spec.PackageOverrides, &appv1.Overrides{PackageName: "foo"})

	returnedTemplate, err = OverrideResourceBySubscription(templateSub, "foodiff", i)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(returnedTemplate).To(Equal(templateSub))

	returnedTemplate, err = OverrideResourceBySubscription(templateSub, "foo", i)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(returnedTemplate).To(Equal(templateSub))
}

func TestPredicate(t *testing.T) {
	g := NewGomegaWithT(t)

	// Test PlacementDecisionPredicateFunctions
	instance := PlacementDecisionPredicateFunctions

	updateEvt := event.UpdateEvent{
		ObjectOld: oldDecision,
		ObjectNew: oldDecision,
	}
	ret := instance.Update(updateEvt)
	g.Expect(ret).To(BeFalse())

	createEvt := event.CreateEvent{}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(BeTrue())

	deleteEvt := event.DeleteEvent{}
	ret = instance.Delete(deleteEvt)
	g.Expect(ret).To(BeTrue())

	// Test AppSubSummaryPredicateFunc
	instance = AppSubSummaryPredicateFunc

	updateEvt = event.UpdateEvent{
		ObjectOld: oldAppsubStatus,
		ObjectNew: newAppsubStatus,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(BeFalse())

	newApp := newAppsubStatus.DeepCopy()
	newApp.Labels = map[string]string{}
	updateEvt = event.UpdateEvent{
		ObjectOld: oldAppsubStatus,
		ObjectNew: newApp,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(BeFalse())

	updateEvt = event.UpdateEvent{
		ObjectOld: fakeAppsubStatus,
		ObjectNew: newAppsubStatus,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(BeFalse())

	updateEvt = event.UpdateEvent{
		ObjectOld: oldAppsubStatus,
		ObjectNew: fakeAppsubStatus,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(BeFalse())
	// Create event
	createEvt = event.CreateEvent{
		Object: newApp,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(BeFalse())

	createEvt = event.CreateEvent{
		Object: newAppsubStatus,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(BeTrue())
	// Delete event
	deleteEvt = event.DeleteEvent{
		Object: newApp,
	}
	ret = instance.Delete(deleteEvt)
	g.Expect(ret).To(BeFalse())

	deleteEvt = event.DeleteEvent{
		Object: newAppsubStatus,
	}
	ret = instance.Delete(deleteEvt)
	g.Expect(ret).To(BeTrue())

	// Test SubscriptionPredicateFunctions
	instance = SubscriptionPredicateFunctions

	updateEvt = event.UpdateEvent{
		ObjectOld: &appv1.Subscription{},
		ObjectNew: &appv1.Subscription{},
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).NotTo(BeTrue())

	// Test ChannelPredicateFunctions
	instance = ChannelPredicateFunctions

	updateEvt = event.UpdateEvent{
		ObjectOld: oldChn,
		ObjectNew: newChn,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(BeTrue())

	updateEvt = event.UpdateEvent{
		ObjectOld: oldChn,
		ObjectNew: oldChn,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(BeFalse())

	createEvt = event.CreateEvent{}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(BeTrue())

	deleteEvt = event.DeleteEvent{}
	ret = instance.Delete(deleteEvt)
	g.Expect(ret).To(BeTrue())

	// Test ServiceAccountPredicateFunctions
	instance = ServiceAccountPredicateFunctions

	updateEvt = event.UpdateEvent{
		ObjectNew: serviceProper,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(BeTrue())

	updateEvt = event.UpdateEvent{
		ObjectNew: serviceImproer,
	}
	ret = instance.Update(updateEvt)
	g.Expect(ret).To(BeFalse())

	createEvt = event.CreateEvent{
		Object: serviceProper,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(BeTrue())

	createEvt = event.CreateEvent{
		Object: serviceImproer,
	}
	ret = instance.Create(createEvt)
	g.Expect(ret).To(BeFalse())

	deleteEvt = event.DeleteEvent{
		Object: serviceProper,
	}
	ret = instance.Delete(deleteEvt)
	g.Expect(ret).To(BeTrue())

	deleteEvt = event.DeleteEvent{
		Object: serviceImproer,
	}
	ret = instance.Delete(deleteEvt)
	g.Expect(ret).To(BeFalse())
}

func TestIsReadyManagedClusterView(t *testing.T) {
	g := NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Managed Cluster View should NOT be ready.
	ret := IsReadyManagedClusterView(mgr.GetAPIReader())
	g.Expect(ret).To(BeFalse())
}

func TestIsReadyPlacementDecision(t *testing.T) {
	g := NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Cluster Management Addon API should NOT be ready.
	ret := IsReadyPlacementDecision(mgr.GetAPIReader())
	g.Expect(ret).To(BeFalse())
}

func TestIsReadySubscription(t *testing.T) {
	g := NewGomegaWithT(t)

	tEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "deploy", "crds"),
			filepath.Join("..", "..", "hack", "test"),
		},
	}

	appv1.SchemeBuilder.AddToScheme(scheme.Scheme)
	appv1alpha1.AddToScheme(scheme.Scheme)

	var (
		err    error
		cfgSub *rest.Config
	)

	if cfgSub, err = tEnv.Start(); err != nil {
		log.Fatal(fmt.Errorf("got error while start up the envtest, err: %w", err))
	}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfgSub, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	// Subscription API should BE ready on the hub
	ret := IsReadySubscription(mgr.GetAPIReader(), true)
	g.Expect(ret).To(BeTrue())

	// Subscription API should BE ready on the managed cluster.
	ret = IsReadySubscription(mgr.GetAPIReader(), false)
	g.Expect(ret).To(BeTrue())
}

func TestFetchChannelReferences(t *testing.T) {
	g := NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	runtimeClient, err := client.New(cfg, client.Options{})
	g.Expect(err).NotTo(HaveOccurred())

	chn := chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "chntest",
			Namespace: "default",
		},
		Spec: chnv1.ChannelSpec{
			SecretRef: &corev1.ObjectReference{
				Name:      "chntestSecret",
				Namespace: "default",
			},
			ConfigMapRef: &corev1.ObjectReference{
				Name:      "chntestConfigMap",
				Namespace: "default",
			},
		},
	}

	cs, cm := FetchChannelReferences(runtimeClient, chn)
	g.Expect(cs).To(BeNil()) // Fail to get reference secret
	g.Expect(cm).To(BeNil()) // Fail to get reference configmap
}

func TestGetClientConfigFromKubeConfig(t *testing.T) {
	g := NewGomegaWithT(t)

	// no kubeconfigFile
	_, err := GetClientConfigFromKubeConfig("")
	g.Expect(err).To(HaveOccurred())

	// fake kubconfigFile
	_, err = GetClientConfigFromKubeConfig("fakekubeconfig.fake")
	g.Expect(err).To(HaveOccurred())
}

func TestGetCheckSum(t *testing.T) {
	g := NewGomegaWithT(t)

	tmpFile, err := ioutil.TempFile("", "temptest")
	g.Expect(err).ShouldNot(HaveOccurred())

	_, err = tmpFile.WriteString("fake kubeconfig data")
	g.Expect(err).ShouldNot(HaveOccurred())

	defer os.Remove(tmpFile.Name()) // clean up the temp fake kubeconfig file

	// test 1: pass a non-existing file name, expect error
	_, err = GetCheckSum("non_existing_file_name")
	g.Expect(err).Should(HaveOccurred())

	// test 2: pass a valid file name, expect no error
	_, err = GetCheckSum(tmpFile.Name())
	g.Expect(err).ShouldNot(HaveOccurred())
}
