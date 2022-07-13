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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
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
