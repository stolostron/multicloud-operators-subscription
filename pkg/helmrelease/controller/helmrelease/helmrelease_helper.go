/*
Copyright 2020 Red Hat

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helmrelease

import (
	"context"
	"fmt"
	"strings"

	"helm.sh/helm/v3/pkg/action"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"

	"helm.sh/helm/v3/pkg/chartutil"
	rspb "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/releaseutil"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
)

// nameFilter filters a set of Helm storage releases by name.
func nameFilter(name string) releaseutil.FilterFunc {
	return releaseutil.FilterFunc(func(rls *rspb.Release) bool {
		if rls == nil {
			return true
		}
		return rls.Name == name
	})
}

// determines if this HelmRelease is owned by Subscription which is owned by MultiClusterHub
func (r *ReconcileHelmRelease) isMultiClusterHubOwnedResource(hr *appv1.HelmRelease) (bool, error) {
	klog.V(3).Info("Running isMultiClusterHubOwnedResource on ", hr.GetNamespace(), "/", hr.GetName())

	if hr.OwnerReferences == nil {
		return false, nil
	}

	for _, hrOwner := range hr.OwnerReferences {
		if hrOwner.Kind == "Subscription" {
			appsubGVK := schema.FromAPIVersionAndKind(hrOwner.APIVersion, hrOwner.Kind)
			appsubNsn := types.NamespacedName{Namespace: hr.GetNamespace(), Name: hrOwner.Name}

			appsub := &unstructured.Unstructured{}
			appsub.SetGroupVersionKind(appsubGVK)
			appsub.SetNamespace(appsubNsn.Namespace)
			appsub.SetName(appsubNsn.Name)

			err := r.GetClient().Get(context.TODO(), appsubNsn, appsub)
			if err != nil {
				if errors.IsNotFound(err) {
					klog.Info("Failed to find the parent (already deleted?), won't be able to determine if it's an ACM's HelmRelease: ",
						appsubNsn, " ", err)

					return false, nil
				}

				klog.Error("Failed to lookup HelmRelease's parent Subscription: ", appsubNsn, " ", err)

				return false, err
			}

			if appsub.GetOwnerReferences() != nil {
				for _, appsubOwner := range appsub.GetOwnerReferences() {
					if appsubOwner.Kind == "MultiClusterHub" &&
						strings.Contains(appsubOwner.APIVersion, "open-cluster-management") {
						return true, nil
					}
				}
			}
		}
	}

	return false, nil
}

// Remove CRD references from Helm storage and HelmRelease's Status.DeployedRelease.Manifest
// TODO add an annotation to trigger this feature instead of triggering on MultiClusterHub owned resource
func (r *ReconcileHelmRelease) hackMultiClusterHubRemoveCRDReferences(hr *appv1.HelmRelease, c *action.Configuration) error {
	klog.V(3).Info("Running hackMultiClusterHubRemoveCRDReferences on ", hr.GetNamespace(), "/", hr.GetName())

	isOwnedByMCH, err := r.isMultiClusterHubOwnedResource(hr)
	if err != nil {
		klog.Error("Failed to determine if HelmRelease is owned a MultiClusterHub resource: ",
			hr.GetNamespace(), "/", hr.GetName())

		return err
	}

	if !isOwnedByMCH {
		klog.Info("HelmRelease is not owned by a MultiClusterHub resource: ",
			hr.GetNamespace(), "/", hr.GetName())

		return nil
	}

	klog.Info("HelmRelease is owned by a MultiClusterHub resource proceed with the removal of all CRD references: ",
		hr.GetNamespace(), "/", hr.GetName())

	clientv1, err := v1.NewForConfig(r.GetConfig())
	if err != nil {
		klog.Error("Failed create client for HelmRelease: ", hr.GetNamespace(), "/", hr.GetName())

		return err
	}

	storageBackend := storage.Init(driver.NewSecrets(clientv1.Secrets(hr.GetNamespace())))

	storageReleases, err := storageBackend.List(
		func(rls *rspb.Release) bool {
			return nameFilter(hr.GetName()).Check(rls)
		})
	if err != nil {
		klog.Error("Failed list all storage releases for HelmRelease: ", hr.GetNamespace(), "/", hr.GetName())

		return err
	}

	if storageReleases == nil {
		klog.Info("HelmRelease does not have any matching Helm storage releases: ",
			hr.GetNamespace(), "/", hr.GetName())
	} else {
		klog.Info("HelmRelease contains storage releases, attempting to strip CRDs from them: ",
			hr.GetNamespace(), "/", hr.GetName())
	}

	for _, storageRelease := range storageReleases {
		klog.Info("Release: ", storageRelease.Name)

		if storageRelease.Info != nil {
			klog.Info("Release: ", storageRelease.Name, " Status: ", storageRelease.Info.Status.String())
		}

		newManifest, changed, err := stripCRDs(storageRelease.Manifest, c)
		if err != nil {
			return err
		}
		if changed {
			klog.Info("Release: ", storageRelease.Name, " needs updating")

			storageRelease.Manifest = newManifest

			err = storageBackend.Update(storageRelease)
			if err != nil {
				klog.Error("Failed update storage release for HelmRelease: ", hr.GetNamespace(), "/", hr.GetName())

				return err
			}

		} else {
			klog.Info("Release: ", storageRelease.Name, " is unchanged")
		}
	}

	if hr.Status.DeployedRelease == nil {
		klog.Info("HelmRelease does not have any Status.DeployedRelease: ",
			hr.GetNamespace(), "/", hr.GetName())

		return nil
	}

	klog.Info("HelmRelease contains Status.DeployedRelease, attempting to strip CRDs from it: ",
		hr.GetNamespace(), "/", hr.GetName())

	newManifest, changed, err := stripCRDs(hr.Status.DeployedRelease.Manifest, c)
	if err != nil {
		return err
	}
	if changed {
		klog.Info("Status release: ", hr.GetName(), " needs updating")

		hr.Status.DeployedRelease.Manifest = newManifest

		err = r.updateResourceStatus(hr)
		if err != nil {
			klog.Error("Failed to update Status.DeployedRelease.Manifest for HelmRelease: ",
				hr.GetNamespace(), "/", hr.GetName())

			return err
		}

	} else {
		klog.Info("Status release: ", hr.GetName(), " is unchanged")
	}

	return nil
}

func stripCRDs(bigFile string, c *action.Configuration) (string, bool, error) {
	changed := false

	caps, err := getCapabilities(c)
	if err != nil {
		return "", false, err
	}

	manifests := releaseutil.SplitManifests(bigFile)
	_, files, err := releaseutil.SortManifests(manifests, caps.APIVersions, releaseutil.InstallOrder)
	if err != nil {
		return "", false, fmt.Errorf("corrupted release record. %w", err)
	}

	var builder strings.Builder
	for _, file := range files {
		if file.Head != nil && file.Head.Kind == "CustomResourceDefinition" {
			if file.Head.Metadata != nil {
				klog.Info("CRD detected: ", file.Head.Metadata.Name)
			}
			changed = true
		} else {
			builder.WriteString("\n---\n" + file.Content)
		}
	}

	return builder.String(), changed, nil
}

// capabilities builds a Capabilities from discovery information. Took from https://github.com/helm/helm/blob/v3.4.2/pkg/action/action.go
func getCapabilities(c *action.Configuration) (*chartutil.Capabilities, error) {
	if c.Capabilities != nil {
		return c.Capabilities, nil
	}
	dc, err := c.RESTClientGetter.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	// force a discovery cache invalidation to always fetch the latest server version/capabilities.
	dc.Invalidate()
	kubeVersion, err := dc.ServerVersion()
	if err != nil {
		return nil, err
	}
	// Issue #6361:
	// Client-Go emits an error when an API service is registered but unimplemented.
	// We trap that error here and print a warning. But since the discovery client continues
	// building the API object, it is correctly populated with all valid APIs.
	// See https://github.com/kubernetes/kubernetes/issues/72051#issuecomment-521157642
	apiVersions, err := action.GetVersionSet(dc)
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) {
			klog.Warning("The Kubernetes server has an orphaned API service. Server reports: ", err)
			klog.Warning("To fix this, kubectl delete apiservice <service-name>")
		} else {
			return nil, err
		}
	}

	c.Capabilities = &chartutil.Capabilities{
		APIVersions: apiVersions,
		KubeVersion: chartutil.KubeVersion{
			Version: kubeVersion.GitVersion,
			Major:   kubeVersion.Major,
			Minor:   kubeVersion.Minor,
		},
	}
	return c.Capabilities, nil
}
