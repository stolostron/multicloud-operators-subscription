// Copyright 2018 The Operator-SDK Authors
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

// copied and modified from github.com/operator-framework/operator-sdk/internal/helm/release

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/klog/v2"

	jsonpatch "gomodules.xyz/jsonpatch/v3"
	"helm.sh/helm/v3/pkg/action"
	cpb "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/kube"
	rpb "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
)

// Manager manages a Helm release. It can install, upgrade, reconcile,
// and uninstall a release.
type Manager interface {
	ReleaseName() string
	IsInstalled() bool
	IsUpgradeRequired() bool
	Sync(context.Context) error
	InstallRelease(context.Context, ...InstallOption) (*rpb.Release, error)
	UpgradeRelease(context.Context, ...UpgradeOption) (*rpb.Release, *rpb.Release, error)
	UninstallRelease(context.Context, ...UninstallOption) (*rpb.Release, error)
	RollbackRelease(context.Context) error
	GetDeployedRelease() (*rpb.Release, error)
	GetActionConfig() *action.Configuration
	ReconcileRelease(context.Context) (*rpb.Release, error)
}

type manager struct {
	actionConfig   *action.Configuration
	storageBackend *storage.Storage
	kubeClient     kube.Interface

	releaseName string
	namespace   string

	values map[string]interface{}
	status *appv1.HelmAppStatus

	isInstalled       bool
	isUpgradeRequired bool
	deployedRelease   *rpb.Release
	chart             *cpb.Chart
}

type InstallOption func(*action.Install) error
type UpgradeOption func(*action.Upgrade) error
type UninstallOption func(*action.Uninstall) error

func (m manager) GetActionConfig() *action.Configuration {
	return m.actionConfig
}

// ReleaseName returns the name of the release.
func (m manager) ReleaseName() string {
	return m.releaseName
}

func (m manager) IsInstalled() bool {
	return m.isInstalled
}

func (m manager) IsUpgradeRequired() bool {
	return m.isUpgradeRequired
}

// Sync ensures the Helm storage backend is in sync with the status of the
// custom resource.
func (m *manager) Sync(ctx context.Context) error {
	// Get release history for this release name
	releases, err := m.storageBackend.History(m.releaseName)
	if err != nil && !notFoundErr(err) {
		return fmt.Errorf("failed to retrieve release history: %w", err)
	}

	// Cleanup non-deployed release versions. If all release versions are
	// non-deployed, this will ensure that failed installations are correctly
	// retried.
	for _, rel := range releases {
		if rel.Info != nil && rel.Info.Status != rpb.StatusDeployed {
			klog.Info("Helm storage backend deleting: ", rel.Name, "/", rel.Version, "/", rel.Info.Status)
			_, err := m.storageBackend.Delete(rel.Name, rel.Version)
			if err != nil && !notFoundErr(err) {
				return fmt.Errorf("failed to delete stale release version: %w", err)
			}
		}
	}

	// Load the most recently deployed release from the storage backend.
	deployedRelease, err := m.GetDeployedRelease()
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get deployed release: %w", err)
	}
	m.deployedRelease = deployedRelease
	m.isInstalled = true

	// Get the next candidate release to determine if an upgrade is necessary.
	candidateRelease, err := m.getCandidateRelease(m.namespace, m.releaseName, m.chart, m.values)
	if err != nil {
		return fmt.Errorf("failed to get the next candidate release to determine if an upgrade is necessary: %w", err)
	}
	if deployedRelease.Manifest != candidateRelease.Manifest {
		m.isUpgradeRequired = true
	}

	return nil
}

func notFoundErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
}

func (m manager) GetDeployedRelease() (*rpb.Release, error) {
	deployedRelease, err := m.storageBackend.Deployed(m.releaseName)
	if err != nil {
		if strings.Contains(err.Error(), "has no deployed releases") {
			return nil, driver.ErrReleaseNotFound
		}
		return nil, err
	}
	return deployedRelease, nil
}

func (m manager) getCandidateRelease(namespace, name string, chart *cpb.Chart,
	values map[string]interface{}) (*rpb.Release, error) {
	upgrade := action.NewUpgrade(m.actionConfig)
	upgrade.Namespace = namespace
	upgrade.DryRun = true
	return upgrade.Run(name, chart, values)
}

// InstallRelease performs a Helm release install.
func (m manager) InstallRelease(ctx context.Context, opts ...InstallOption) (*rpb.Release, error) {
	install := action.NewInstall(m.actionConfig)
	install.ReleaseName = m.releaseName
	install.Namespace = m.namespace
	for _, o := range opts {
		if err := o(install); err != nil {
			return nil, fmt.Errorf("failed to apply install option: %w", err)
		}
	}

	return install.Run(m.chart, m.values)
}

func ForceUpgrade(force bool) UpgradeOption {
	return func(u *action.Upgrade) error {
		u.Force = force
		return nil
	}
}

// UpgradeRelease performs a Helm release upgrade.
func (m manager) UpgradeRelease(ctx context.Context, opts ...UpgradeOption) (*rpb.Release, *rpb.Release, error) {
	upgrade := action.NewUpgrade(m.actionConfig)
	upgrade.Namespace = m.namespace
	for _, o := range opts {
		if err := o(upgrade); err != nil {
			return nil, nil, fmt.Errorf("failed to apply upgrade option: %w", err)
		}
	}

	upgradedRelease, err := upgrade.Run(m.releaseName, m.chart, m.values)
	if err != nil {
		return nil, nil, err
	}
	return m.deployedRelease, upgradedRelease, err
}

// UninstallRelease performs a Helm release uninstall.
func (m manager) UninstallRelease(ctx context.Context, opts ...UninstallOption) (*rpb.Release, error) {
	uninstall := action.NewUninstall(m.actionConfig)
	for _, o := range opts {
		if err := o(uninstall); err != nil {
			return nil, fmt.Errorf("failed to apply uninstall option: %w", err)
		}
	}

	uninstallResponse, err := uninstall.Run(m.releaseName)
	if uninstallResponse == nil {
		return nil, err
	}

	return uninstallResponse.Release, err
}

// RollbackRelease performs a Helm release rollback.
func (m manager) RollbackRelease(ctx context.Context) error {
	rollback := action.NewRollback(m.actionConfig)
	rollback.Force = true

	return rollback.Run(m.releaseName)
}

// ReconcileRelease creates or patches resources as necessary to match the
// deployed release's manifest.
// copied from https://github.com/operator-framework/operator-sdk/blob/v1.22.0/internal/helm/release/manager.go
func (m manager) ReconcileRelease(ctx context.Context) (*rpb.Release, error) {
	err := reconcileRelease(ctx, m.kubeClient, m.deployedRelease.Manifest)
	return m.deployedRelease, err
}

// copied from https://github.com/operator-framework/operator-sdk/blob/v1.22.0/internal/helm/release/manager.go
func reconcileRelease(_ context.Context, kubeClient kube.Interface, expectedManifest string) error {
	expectedInfos, err := kubeClient.Build(bytes.NewBufferString(expectedManifest), false)
	if err != nil {
		return err
	}
	return expectedInfos.Visit(func(expected *resource.Info, err error) error {
		if err != nil {
			return fmt.Errorf("visit error: %w", err)
		}

		helper := resource.NewHelper(expected.Client, expected.Mapping)
		existing, err := helper.Get(expected.Namespace, expected.Name)
		if apierrors.IsNotFound(err) {
			if _, err := helper.Create(expected.Namespace, true, expected.Object); err != nil {
				return fmt.Errorf("create error: %s", err)
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("could not get object: %w", err)
		}

		// Replicate helm's patch creation, which will create a Three-Way-Merge patch for
		// native kubernetes Objects and fall back to a JSON merge patch for unstructured Objects such as CRDs
		// We also extend the JSON merge patch by ignoring "remove" operations for fields added by kubernetes
		// Reference in the helm source code:
		// https://github.com/helm/helm/blob/1c9b54ad7f62a5ce12f87c3ae55136ca20f09c98/pkg/kube/client.go#L392
		patch, patchType, err := createPatch(existing, expected)
		if err != nil {
			return fmt.Errorf("error creating patch: %w", err)
		}

		if patch == nil {
			// nothing to do
			return nil
		}

		_, err = helper.Patch(expected.Namespace, expected.Name, patchType, patch,
			&metav1.PatchOptions{})
		if err != nil {
			return fmt.Errorf("patch error: %w", err)
		}
		return nil
	})
}

// copied from https://github.com/operator-framework/operator-sdk/blob/v1.22.0/internal/helm/release/manager.go
// which operator-sdk copied from https://github.com/helm/helm
func createPatch(existing runtime.Object, expected *resource.Info) ([]byte, apitypes.PatchType, error) {
	existingJSON, err := json.Marshal(existing)
	if err != nil {
		return nil, apitypes.StrategicMergePatchType, err
	}
	expectedJSON, err := json.Marshal(expected.Object)
	if err != nil {
		return nil, apitypes.StrategicMergePatchType, err
	}

	// Get a versioned object
	versionedObject := kube.AsVersioned(expected)

	// Unstructured objects, such as CRDs, may not have an not registered error
	// returned from ConvertToVersion. Anything that's unstructured should
	// use the jsonpatch.CreateMergePatch. Strategic Merge Patch is not supported
	// on objects like CRDs.
	_, isUnstructured := versionedObject.(runtime.Unstructured)

	// On newer K8s versions, CRDs aren't unstructured but have a dedicated type
	_, isV1CRD := versionedObject.(*apiextv1.CustomResourceDefinition)
	_, isV1beta1CRD := versionedObject.(*apiextv1beta1.CustomResourceDefinition)
	isCRD := isV1CRD || isV1beta1CRD

	if isUnstructured || isCRD {
		// fall back to generic JSON merge patch
		patch, err := createJSONMergePatch(existingJSON, expectedJSON)
		return patch, apitypes.JSONPatchType, err
	}

	patchMeta, err := strategicpatch.NewPatchMetaFromStruct(versionedObject)
	if err != nil {
		return nil, apitypes.StrategicMergePatchType, err
	}

	patch, err := strategicpatch.CreateThreeWayMergePatch(expectedJSON, expectedJSON, existingJSON, patchMeta, true)
	if err != nil {
		return nil, apitypes.StrategicMergePatchType, err
	}
	// An empty patch could be in the form of "{}" which represents an empty map out of the 3-way merge;
	// filter them out here too to avoid sending the apiserver empty patch requests.
	if len(patch) == 0 || bytes.Equal(patch, []byte("{}")) {
		return nil, apitypes.StrategicMergePatchType, nil
	}
	return patch, apitypes.StrategicMergePatchType, nil
}

// copied from https://github.com/operator-framework/operator-sdk/blob/v1.22.0/internal/helm/release/manager.go
// which operator-sdk copied from https://github.com/helm/helm
func createJSONMergePatch(existingJSON, expectedJSON []byte) ([]byte, error) {
	ops, err := jsonpatch.CreatePatch(existingJSON, expectedJSON)
	if err != nil {
		return nil, err
	}

	// We ignore the "remove" operations from the full patch because they are
	// fields added by Kubernetes or by the user after the existing release
	// resource has been applied. The goal for this patch is to make sure that
	// the fields managed by the Helm chart are applied.
	// All "add" operations without a value (null) can be ignored
	patchOps := make([]jsonpatch.JsonPatchOperation, 0)
	for _, op := range ops {
		if op.Operation != "remove" && !(op.Operation == "add" && op.Value == nil) {
			patchOps = append(patchOps, op)
		}
	}

	// If there are no patch operations, return nil. Callers are expected
	// to check for a nil response and skip the patch operation to avoid
	// unnecessary chatter with the API server.
	if len(patchOps) == 0 {
		return nil, nil
	}

	return json.Marshal(patchOps)
}
