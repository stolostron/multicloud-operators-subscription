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
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/klog/v2"

	"helm.sh/helm/v3/pkg/action"
	cpb "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/kube"
	rpb "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"

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
