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

package utils

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
)

var (
	nssubTest  = "ns-sub"
	chKey      = types.NamespacedName{Name: "target-ch", Namespace: "ns-ch"}
	refSrtName = "target-referred-sercet"
	srtGVK     = schema.GroupVersionKind{Group: "", Kind: SecretKindStr, Version: "v1"}
)

func TestIsOwnedBy(t *testing.T) {
	owerName := "sub-a"
	owerUID := types.UID("sub-uid")

	owner := &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owerName,
			Namespace: nssubTest,
			UID:       owerUID,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: chKey.String(),
		},
	}

	testCases := []struct {
		desc   string
		obj    referredObject
		wanted bool
	}{
		{
			desc: "not owned",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: srtGVK.String(),
					Name:            refSrtName,
				},
			},
			wanted: false,
		},
		{
			desc: "ownedbysub",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alreadyowned",
					Namespace: nssubTest,
					OwnerReferences: []metav1.OwnerReference{
						{Name: owerName, UID: owerUID},
					},
				},
			},
			wanted: true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := isObjectOwnedBySub(tC.obj, owner.GetName())
			if got != tC.wanted {
				t.Errorf("wanted %v, got %v", tC.wanted, got)
			}
		})
	}
}

func TestAddObjectOwnedBySub(t *testing.T) {
	owerName := "sub-a"
	owerUID := types.UID("sub-uid")

	owner := &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owerName,
			Namespace: nssubTest,
			UID:       owerUID,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: chKey.String(),
		},
	}

	testCases := []struct {
		desc         string
		obj          referredObject
		newowner     *appv1alpha1.Subscription
		newOwerships []metav1.OwnerReference
	}{
		{
			desc: "adding new",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: srtGVK.String(),
					Name:            refSrtName,
				},
			},
			newowner: owner,
			newOwerships: []metav1.OwnerReference{
				{Kind: SubscriptionGVK.Kind, APIVersion: SubscriptionGVK.Version, Name: owner.GetName(), UID: owner.GetUID()},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := addObjectOwnedBySub(tC.obj, tC.newowner)
			t.Logf("got %v", got)
			if !reflect.DeepEqual(got, tC.newOwerships) {
				t.Errorf("sub %v is not added as owner to %v", owner.GetName(), tC.obj.GetName())
			}
		})
	}
}

func TestDeleteSubFromObjectOwners(t *testing.T) {
	ownerName := "sub-a"
	ownerUID := types.UID("sub-uid")

	ownera := &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ownerName,
			Namespace: nssubTest,
			UID:       ownerUID,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: chKey.String(),
		},
	}

	ownerNameb := "sub-b"
	ownerUIDb := types.UID("sub-uid-b")

	ownerb := &appv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ownerNameb,
			Namespace: nssubTest,
			UID:       ownerUIDb,
		},
		Spec: appv1alpha1.SubscriptionSpec{
			Channel: chKey.String(),
		},
	}

	testCases := []struct {
		desc         string
		obj          referredObject
		ownername    string
		newOwerships []metav1.OwnerReference
	}{
		{
			desc: "deleting with only one owner",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: srtGVK.String(),
					Name:            refSrtName,
					OwnerReferences: []metav1.OwnerReference{
						{Kind: SubscriptionGVK.Kind, APIVersion: SubscriptionGVK.Version, Name: ownera.GetName(), UID: ownera.GetUID()},
					},
				},
			},
			ownername:    ownera.GetName(),
			newOwerships: []metav1.OwnerReference{},
		},

		{
			desc: "deleting with 2 owner",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: srtGVK.String(),
					Name:            refSrtName,
					OwnerReferences: []metav1.OwnerReference{
						{Kind: SubscriptionGVK.Kind, APIVersion: SubscriptionGVK.Version, Name: ownera.GetName(), UID: ownera.GetUID()},
						{Kind: SubscriptionGVK.Kind, APIVersion: SubscriptionGVK.Version, Name: ownerb.GetName(), UID: ownerb.GetUID()},
					},
				},
			},
			ownername: ownera.GetName(),
			newOwerships: []metav1.OwnerReference{
				{Kind: SubscriptionGVK.Kind, APIVersion: SubscriptionGVK.Version, Name: ownerb.GetName(), UID: ownerb.GetUID()},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := deleteSubFromObjectOwnersByName(tC.obj, tC.ownername)
			t.Logf("got %v", got)
			if !reflect.DeepEqual(got, tC.newOwerships) {
				t.Errorf("sub %v is not delete from owner list of %v", tC.ownername, tC.obj.GetName())
			}
		})
	}
}

func TestIsEqualObjectsDataOwnersLabels(t *testing.T) {
	ownerName := "sub-a"
	defaultLabel := "test_value"
	defaultOwner := []metav1.OwnerReference{
		{Kind: "Pod", APIVersion: "v1", Name: "poda", UID: "1"},
	}

	defaultObject := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      ownerName,
				"namespace": nssubTest,
				"labels": map[string]interface{}{
					"test_label": defaultLabel,
				},
			},
		},
	}
	// setting "ownerReferences" under metadata above does not seem to register
	defaultObject.SetOwnerReferences(defaultOwner)

	testCases := []struct {
		desc   string
		obj1   *unstructured.Unstructured
		obj2   referredObject
		wanted bool
	}{
		{
			desc: "comparison with same owner same label",
			obj1: &defaultObject,
			obj2: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownerName,
					Namespace: nssubTest,
					Labels: map[string]string{
						"test_label": defaultLabel,
					},
					OwnerReferences: defaultOwner,
				},
			},
			wanted: true,
		},
		{
			desc: "comparison with same owner different label",
			obj1: &defaultObject,
			obj2: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownerName,
					Namespace: nssubTest,
					Labels: map[string]string{
						"test_label": "test_different_value",
					},
					OwnerReferences: defaultOwner,
				},
			},
			wanted: false,
		},
		{
			desc: "comparison with different owner same label",
			obj1: &defaultObject,
			obj2: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownerName,
					Namespace: nssubTest,
					Labels: map[string]string{
						"test_label": defaultLabel,
					},
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Pod", APIVersion: "v1", Name: "podb", UID: "1001"},
					},
				},
			},
			wanted: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := isEqualObjectsDataOwnersLabels(tC.obj1, tC.obj2)
			if got != tC.wanted {
				t.Errorf("wanted %v, got %v", tC.wanted, got)
			}
		})
	}
}
