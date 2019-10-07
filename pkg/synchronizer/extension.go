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

package synchronizer

import (
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

// SubscriptionExtension provides default extension settings
type SubscriptionExtension struct {
	IngoredGroupKindMap map[schema.GroupKind]bool
}

// Extension defines the extension features of synchronizer
type Extension interface {
	UpdateHostStatus(client.Client, error, *unstructured.Unstructured, interface{}) error
	GetHostFromObject(metav1.Object) *types.NamespacedName
	SetSynchronizerToObject(metav1.Object, *types.NamespacedName) error
	SetHostToObject(metav1.Object, types.NamespacedName, *types.NamespacedName) error
	IsObjectOwnedByHost(metav1.Object, types.NamespacedName, *types.NamespacedName) bool
	IsObjectOwnedBySynchronizer(metav1.Object, *types.NamespacedName) bool
	IsIgnoredGroupKind(schema.GroupKind) bool
}

var (
	defaultExtension = SubscriptionExtension{
		IngoredGroupKindMap: map[schema.GroupKind]bool{
			dplgk: true,
		},
	}
)

// UpdateHostStatus defines update host status function for deployable
func (se *SubscriptionExtension) UpdateHostStatus(statusClient client.Client, actionerr error, tplunit *unstructured.Unstructured, status interface{}) error {
	return utils.UpdateSubscriptionStatus(statusClient, actionerr, tplunit, status)
}

// GetHostFromObject defines update host status function for deployable
func (se *SubscriptionExtension) GetHostFromObject(obj metav1.Object) *types.NamespacedName {
	return utils.GetHostSubscriptionFromObject(obj)
}

// SetSynchronizerToObject defines update host status function for deployable
func (se *SubscriptionExtension) SetSynchronizerToObject(obj metav1.Object, syncid *types.NamespacedName) error {
	if obj == nil {
		return errors.New("trying to set host to nil object")
	}

	objanno := obj.GetAnnotations()
	if objanno != nil {
		delete(objanno, dplv1alpha1.AnnotationManagedCluster)
		obj.SetAnnotations(objanno)
	}

	return nil
}

// SetHostToObject defines update host status function for deployable
func (se *SubscriptionExtension) SetHostToObject(obj metav1.Object, host types.NamespacedName, syncid *types.NamespacedName) error {
	if obj == nil {
		return errors.New("trying to set host to nil object")
	}

	objanno := obj.GetAnnotations()
	if objanno == nil {
		objanno = make(map[string]string)
	}
	objanno[appv1alpha1.AnnotationHosting] = host.String()
	obj.SetAnnotations(objanno)

	return se.SetSynchronizerToObject(obj, syncid)
}

// IsObjectOwnedBySynchronizer defines update host status function for deployable
func (se *SubscriptionExtension) IsObjectOwnedBySynchronizer(obj metav1.Object, syncid *types.NamespacedName) bool {
	if obj == nil {
		// return false to let caller skip
		return false
	}

	if syncid != nil {
		// subscription does not introduce id yet.
		return false
	}

	objanno := obj.GetAnnotations()
	if objanno != nil {
		_, ok := objanno[dplv1alpha1.AnnotationManagedCluster]
		return !ok
	}

	// nil annotation owned by sub sync, though not by any sub
	return true
}

// IsObjectOwnedByHost defines update host status function for deployable
func (se *SubscriptionExtension) IsObjectOwnedByHost(obj metav1.Object, host types.NamespacedName, syncid *types.NamespacedName) bool {
	owned := se.IsObjectOwnedBySynchronizer(obj, syncid)

	if !owned {
		return owned
	}

	objhost := utils.GetHostSubscriptionFromObject(obj)
	if objhost == nil {
		return false
	}

	return objhost.Namespace == host.Namespace && objhost.Name == host.Name
}

// IsIgnoredGroupKind defines update host status function for deployable
func (se *SubscriptionExtension) IsIgnoredGroupKind(gk schema.GroupKind) bool {
	return se.IngoredGroupKindMap[gk]
}
