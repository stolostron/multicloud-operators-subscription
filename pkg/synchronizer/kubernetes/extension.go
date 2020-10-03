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

package kubernetes

import (
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

// SubscriptionExtension provides default extension settings
type SubscriptionExtension struct {
	localClient         client.Client
	remoteClient        client.Client
	IngoredGroupKindMap map[schema.GroupKind]bool
}

// Extension defines the extension features of synchronizer
type Extension interface {
	UpdateHostStatus(error, *unstructured.Unstructured, interface{}, bool) error
	GetHostFromObject(metav1.Object) *types.NamespacedName
	SetSynchronizerToObject(metav1.Object, *types.NamespacedName) error
	SetHostToObject(metav1.Object, types.NamespacedName, *types.NamespacedName) error
	IsObjectOwnedByHost(metav1.Object, types.NamespacedName, *types.NamespacedName) bool
	IsObjectOwnedBySynchronizer(metav1.Object, *types.NamespacedName) bool
	IsIgnoredGroupKind(schema.GroupKind) bool
}

var (
	defaultExtension = &SubscriptionExtension{
		IngoredGroupKindMap: map[schema.GroupKind]bool{
			dplgk: true,
		},
	}
)

// UpdateHostSubscriptionStatus defines update host status function for deployable
func (se *SubscriptionExtension) UpdateHostStatus(actionerr error, tplunit *unstructured.Unstructured, status interface{}, deletePkg bool) error {
	host := se.GetHostFromObject(tplunit)
	// the tplunit is the root subscription on managed cluster
	if host == nil || host.String() == "/" {
		return utils.UpdateDeployableStatus(se.remoteClient, actionerr, tplunit, status)
	}

	//update managed cluster subscription status
	return utils.UpdateSubscriptionStatus(se.localClient, actionerr, tplunit, status, deletePkg)
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

	if objanno[appv1alpha1.AnnotationHosting] == "" {
		objanno[appv1alpha1.AnnotationHosting] = host.String()
		obj.SetAnnotations(objanno)
	}

	return se.SetSynchronizerToObject(obj, syncid)
}

// IsObjectOwnedBySynchronizer defines update host status function for deployable
func (se *SubscriptionExtension) IsObjectOwnedBySynchronizer(obj metav1.Object, syncid *types.NamespacedName) bool {
	if obj == nil {
		// return false to let caller skip
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
		klog.V(5).Info("Resource", obj, " is not owned by ", syncid)
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
