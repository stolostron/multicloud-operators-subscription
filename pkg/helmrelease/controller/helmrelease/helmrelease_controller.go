/*
Copyright 2019 The Kubernetes Authors.

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

// install/upgrade/uninstall code ported and modified from:
// github.com/operator-framework/operator-sdk/internal/helm/controller/reconcile.go

//Package helmrelease controller manages the helmrelease CR
package helmrelease

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/releaseutil"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	helmoperator "open-cluster-management.io/multicloud-operators-subscription/pkg/helmrelease/release"
)

const (
	finalizer = "uninstall-helm-release"

	defaultMaxConcurrent = 10
)

// Add creates a new HelmRelease Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileHelmRelease{mgr}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	chartsDir := os.Getenv(appv1.ChartsDir)
	if chartsDir == "" {
		chartsDir, err := ioutil.TempDir("/tmp", "charts")
		if err != nil {
			return err
		}

		err = os.Setenv(appv1.ChartsDir, chartsDir)
		if err != nil {
			return err
		}
	}

	klog.Info("The MaxConcurrentReconciles is set to: ", defaultMaxConcurrent)

	// Create a new controller
	c, err := controller.New("helmrelease-controller", mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: defaultMaxConcurrent})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource HelmRelease
	if err := c.Watch(&source.Kind{Type: &appv1.HelmRelease{}}, &handler.EnqueueRequestForObject{},
		predicate.GenerationChangedPredicate{}); err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileHelmRelease implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileHelmRelease{}

// ReconcileHelmRelease reconciles a HelmRelease object
type ReconcileHelmRelease struct {
	manager.Manager
}

// Reconcile reads that state of the cluster for a HelmRelease object and makes changes based on the state read
// and what is in the HelmRelease.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileHelmRelease) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	klog.V(1).Info("Reconciling HelmRelease: ", request.Namespace, "/", request.Name)

	// Fetch the HelmRelease instance
	instance := &appv1.HelmRelease{}

	err := r.GetClient().Get(ctx, request.NamespacedName, instance)
	if apierrors.IsNotFound(err) {
		klog.Info("Ignorable error. Failed to find HelmRelease, most likely it has been uninstalled: ",
			helmreleaseNsn(instance), " ", err)

		return reconcile.Result{}, nil
	}
	if err != nil {
		klog.Error("Failed to lookup resource ", request.Namespace, "/", request.Name, " ", err)
		return reconcile.Result{}, err
	}

	if instance.Repo.Source == nil {
		klog.Error("Failed to detect Repo.Source from HelmRelease ", helmreleaseNsn(instance), ". Setting requeue to false.")
		//TODO set error status here

		return reconcile.Result{Requeue: false}, nil
	}

	// setting the nil spec to "":"" allows helmrelease to reconcile with default chart values.
	if instance.Spec == nil && instance.GetDeletionTimestamp() == nil {
		spec := make(map[string]interface{})

		err := yaml.Unmarshal([]byte("{\"\":\"\"}"), &spec)
		if err != nil {
			klog.Error("Failed to unmarshal default spec: ",
				helmreleaseNsn(instance), " ", err)

			return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
		}

		instance.Spec = spec

		err = r.GetClient().Update(ctx, instance)
		if err != nil {
			klog.Error("Failed to update HelmRelease with default spec: ",
				helmreleaseNsn(instance), " ", err)

			return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
		}
	}

	// handles the download of the chart as well
	helmOperatorManagerFactory, err := r.newHelmOperatorManagerFactory(instance)
	if err != nil {
		altSourcePassed := false

		if instance.Repo.AltSource != nil {
			klog.Warning("Attempting AltSource to create new HelmOperatorManagerFactory because Source failed: ",
				helmreleaseNsn(instance), " ", err)

			repoClone := instance.Repo.Clone()
			instance.Repo = instance.Repo.AltSourceToSource()

			helmOperatorManagerFactory, err = r.newHelmOperatorManagerFactory(instance)
			if err == nil {
				altSourcePassed = true
				instance.Repo = repoClone
			}
		}

		if !altSourcePassed {
			klog.Error("Failed to create new HelmOperatorManagerFactory: ",
				helmreleaseNsn(instance), " ", err)

			instance.Status.SetCondition(appv1.HelmAppCondition{
				Type:    appv1.ConditionIrreconcilable,
				Status:  appv1.StatusTrue,
				Reason:  appv1.ReasonReconcileError,
				Message: err.Error(),
			})
			_ = r.updateResourceStatus(instance)

			return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
		}
	}

	manager, err := r.newHelmOperatorManager(instance, request, helmOperatorManagerFactory)
	if err != nil {
		klog.Error("Failed to create new HelmOperatorManager: ",
			helmreleaseNsn(instance), " ", err)

		instance.Status.SetCondition(appv1.HelmAppCondition{
			Type:    appv1.ConditionIrreconcilable,
			Status:  appv1.StatusTrue,
			Reason:  appv1.ReasonReconcileError,
			Message: err.Error(),
		})
		_ = r.updateResourceStatus(instance)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	// hack for MultiClusterHub to remove CRD outside of Helm/HelmRelease's control
	// TODO introduce a generic annotation to trigger this feature
	if err := r.hackMultiClusterHubRemoveCRDReferences(instance, manager.GetActionConfig()); err != nil {
		klog.Error("Failed to hackMultiClusterHubRemoveCRDReferences: ", err)

		return reconcile.Result{}, err
	}

	instance.Status.RemoveCondition(appv1.ConditionIrreconcilable)

	if instance.GetDeletionTimestamp() != nil {
		return r.uninstall(instance, manager)
	}

	instance.Status.SetCondition(appv1.HelmAppCondition{
		Type:   appv1.ConditionInitialized,
		Status: appv1.StatusTrue,
	})

	klog.Info("Sync Release ", helmreleaseNsn(instance))

	if err := manager.Sync(context.TODO()); err != nil {
		klog.Error("Failed to sync HelmRelease ", helmreleaseNsn(instance), " ", err)

		instance.Status.SetCondition(appv1.HelmAppCondition{
			Type:    appv1.ConditionIrreconcilable,
			Status:  appv1.StatusTrue,
			Reason:  appv1.ReasonReconcileError,
			Message: err.Error(),
		})
		_ = r.updateResourceStatus(instance)

		klog.Info("Requeue HelmRelease after one minute ")

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	instance.Status.RemoveCondition(appv1.ConditionIrreconcilable)

	if !manager.IsInstalled() {
		return r.install(instance, manager)
	}

	if !contains(instance.GetFinalizers(), finalizer) {
		klog.V(1).Info("Adding finalizer (", finalizer, ") to ", helmreleaseNsn(instance))
		controllerutil.AddFinalizer(instance, finalizer)
		if err := r.updateResource(instance); err != nil {
			klog.Error("Failed to add uninstall finalizer to ", helmreleaseNsn(instance))
			return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
		}
	}

	if manager.IsUpgradeRequired() {
		return r.upgrade(instance, manager)
	}

	// If a change is made to the CR spec that causes a release failure, a
	// ConditionReleaseFailed is added to the status conditions. If that change
	// is then reverted to its previous state, the operator will stop
	// attempting the release and will resume reconciling. In this case, we
	// need to remove the ConditionReleaseFailed because the failing release is
	// no longer being attempted.
	instance.Status.RemoveCondition(appv1.ConditionReleaseFailed)

	return r.ensureStatusReasonPopulated(instance, manager)
}

func (r ReconcileHelmRelease) updateResourceStatus(hr *appv1.HelmRelease) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.GetClient().Status().Update(context.TODO(), hr)
	})
}

func (r ReconcileHelmRelease) updateResource(hr *appv1.HelmRelease) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.GetClient().Update(context.TODO(), hr)
	})
}

func contains(l []string, s string) bool {
	for _, elem := range l {
		if elem == s {
			return true
		}
	}
	return false
}

// returns the boolean representation of the annotation string
// will return false if annotation is not set
func hasHelmUpgradeForceAnnotation(hr *appv1.HelmRelease) bool {
	const helmUpgradeForceAnnotation = "helm.sdk.operatorframework.io/upgrade-force"
	force := hr.GetAnnotations()[helmUpgradeForceAnnotation]
	if force == "" {
		return false
	}
	value := false
	if i, err := strconv.ParseBool(force); err != nil {
		klog.Info("Could not parse annotation as a boolean ",
			"annotation=", helmUpgradeForceAnnotation, " value informed ", force,
			" for ", hr.GetNamespace(), "/", hr.GetName())
	} else {
		value = i
	}
	return value
}

func (r *ReconcileHelmRelease) install(instance *appv1.HelmRelease, manager helmoperator.Manager) (reconcile.Result, error) {
	// If all the Helm release records are deleted, then the Helm operator will try to install the release again.
	// In that case, if the install errors, then don't perform the uninstall rollback because it might lead to unintended data loss.
	// See: https://github.com/operator-framework/operator-sdk/issues/4296
	rollbackByUninstall := true
	if instance.Status.DeployedRelease != nil {
		klog.Info("Release is not installed but status.DeployedRelease is populated. If the install error, skip rollback(uninstall)")
		rollbackByUninstall = false
	}

	klog.Info("Installing Release ", helmreleaseNsn(instance))

	installedRelease, err := manager.InstallRelease(context.TODO())
	if err != nil {
		klog.Error("Failed to install HelmRelease ",
			helmreleaseNsn(instance), " ", err)
		instance.Status.SetCondition(appv1.HelmAppCondition{
			Type:    appv1.ConditionReleaseFailed,
			Status:  appv1.StatusTrue,
			Reason:  appv1.ReasonInstallError,
			Message: err.Error(),
		})
		_ = r.updateResourceStatus(instance)

		if rollbackByUninstall && installedRelease != nil {
			// hack for MultiClusterHub to remove CRD outside of Helm/HelmRelease's control
			// TODO introduce a generic annotation to trigger this feature
			if errRemoveCRDs := r.hackMultiClusterHubRemoveCRDReferences(instance, manager.GetActionConfig()); errRemoveCRDs != nil {
				klog.Error("Failed to hackMultiClusterHubRemoveCRDReferences: ", errRemoveCRDs)

				return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
			}

			klog.Info("Failed to install HelmRelease and the installedRelease response is not nil. Proceed to uninstall ",
				helmreleaseNsn(instance))

			_, errUninstall := manager.UninstallRelease(context.TODO())
			if errUninstall != nil && !errors.Is(errUninstall, driver.ErrReleaseNotFound) {
				klog.Error("Failed to uninstall HelmRelease for install rollback",
					helmreleaseNsn(instance), " ", errUninstall)

				instance.Status.SetCondition(appv1.HelmAppCondition{
					Type:    appv1.ConditionReleaseFailed,
					Status:  appv1.StatusTrue,
					Reason:  appv1.ReasonInstallError,
					Message: "failed installation " + err.Error() + " and failed uninstall rollback " + errUninstall.Error(),
				})
				_ = r.updateResourceStatus(instance)

				return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
			}

			klog.Info("Uninstalled Release for install failure ", helmreleaseNsn(instance))
		}

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	instance.Status.RemoveCondition(appv1.ConditionReleaseFailed)

	klog.V(1).Info("Adding finalizer (", finalizer, ") to ", helmreleaseNsn(instance))
	controllerutil.AddFinalizer(instance, finalizer)
	if err := r.updateResource(instance); err != nil {
		klog.Error("Failed to add uninstall finalizer to ", helmreleaseNsn(instance), " ", err)
		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	klog.Info("Installed HelmRelease ", helmreleaseNsn(instance))

	message := ""
	if installedRelease.Info != nil {
		message = installedRelease.Info.Notes
	}
	instance.Status.SetCondition(appv1.HelmAppCondition{
		Type:    appv1.ConditionDeployed,
		Status:  appv1.StatusTrue,
		Reason:  appv1.ReasonInstallSuccessful,
		Message: message,
	})
	instance.Status.DeployedRelease = &appv1.HelmAppRelease{
		Name:     installedRelease.Name,
		Manifest: installedRelease.Manifest,
	}
	err = r.updateResourceStatus(instance)
	if err != nil {
		klog.Error("Failed to update resource status for HelmRelease ",
			helmreleaseNsn(instance), " ", err)
	}

	return reconcile.Result{}, err
}

func (r *ReconcileHelmRelease) upgrade(instance *appv1.HelmRelease, manager helmoperator.Manager) (reconcile.Result, error) {
	klog.Info("Upgrading Release ", helmreleaseNsn(instance))

	force := hasHelmUpgradeForceAnnotation(instance)
	_, upgradedRelease, err := manager.UpgradeRelease(context.TODO(), helmoperator.ForceUpgrade(force))
	if err != nil {
		klog.Error("Failed to upgrade HelmRelease ", helmreleaseNsn(instance), " ", err)
		instance.Status.SetCondition(appv1.HelmAppCondition{
			Type:    appv1.ConditionReleaseFailed,
			Status:  appv1.StatusTrue,
			Reason:  appv1.ReasonUpgradeError,
			Message: err.Error(),
		})
		_ = r.updateResourceStatus(instance)

		// hack for MultiClusterHub to remove CRD outside of Helm/HelmRelease's control
		// TODO introduce a generic annotation to trigger this feature
		if errRemoveCRDs := r.hackMultiClusterHubRemoveCRDReferences(instance, manager.GetActionConfig()); errRemoveCRDs != nil {
			klog.Error("Failed to hackMultiClusterHubRemoveCRDReferences: ", errRemoveCRDs)

			return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
		}

		if upgradedRelease != nil {
			klog.Info("Failed to upgrade HelmRelease and the upgradedRelease response is not nil. Proceed to rollback ",
				helmreleaseNsn(instance))

			errRollback := manager.RollbackRelease(context.TODO())
			if errRollback != nil && !errors.Is(errRollback, driver.ErrReleaseNotFound) {
				klog.Error("Failed to rollback HelmRelease ",
					helmreleaseNsn(instance), " ", err)

				instance.Status.SetCondition(appv1.HelmAppCondition{
					Type:    appv1.ConditionReleaseFailed,
					Status:  appv1.StatusTrue,
					Reason:  appv1.ReasonUpgradeError,
					Message: "failed upgrade " + err.Error() + " and failed rollback: " + errRollback.Error(),
				})
				_ = r.updateResourceStatus(instance)

				return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
			}

			klog.Info("Rollbacked Release for upgrade failure ", helmreleaseNsn(instance))
		}

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}
	instance.Status.RemoveCondition(appv1.ConditionReleaseFailed)

	klog.Info("Upgraded HelmRelease ", "force=", force, " for ", helmreleaseNsn(instance))
	message := ""
	if upgradedRelease.Info != nil {
		message = upgradedRelease.Info.Notes
	}
	instance.Status.SetCondition(appv1.HelmAppCondition{
		Type:    appv1.ConditionDeployed,
		Status:  appv1.StatusTrue,
		Reason:  appv1.ReasonUpgradeSuccessful,
		Message: message,
	})
	instance.Status.DeployedRelease = &appv1.HelmAppRelease{
		Name:     upgradedRelease.Name,
		Manifest: upgradedRelease.Manifest,
	}
	err = r.updateResourceStatus(instance)
	if err != nil {
		klog.Error("Failed to update resource status for HelmRelease ",
			helmreleaseNsn(instance), " ", err)
	}

	return reconcile.Result{}, err
}

func (r *ReconcileHelmRelease) uninstall(instance *appv1.HelmRelease, manager helmoperator.Manager) (reconcile.Result, error) {
	if !contains(instance.GetFinalizers(), finalizer) {
		klog.Info("HelmRelease is terminated, skipping reconciliation ", helmreleaseNsn(instance))

		return reconcile.Result{}, nil
	}

	klog.Info("Uninstalling Release ", helmreleaseNsn(instance))

	_, err := manager.UninstallRelease(context.TODO())
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		klog.Error("Failed to uninstall HelmRelease ", helmreleaseNsn(instance), " ", err)
		r.updateUninstallResourceErrorStatus(instance, err)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	klog.Info("Uninstalled HelmRelease ", helmreleaseNsn(instance))

	// no need to check for remaining resources when there is no DeployedRelease
	// skip ahead to removing the finalizer and let the helmrelease terminate
	if instance.Status.DeployedRelease == nil || instance.Status.DeployedRelease.Manifest == "" {
		controllerutil.RemoveFinalizer(instance, finalizer)

		if err := r.updateResource(instance); err != nil {
			klog.Error("Failed to strip HelmRelease uninstall finalizer ", helmreleaseNsn(instance), " ", err)

			return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
		}

		klog.Info("Removed finalizer from HelmRelease ", helmreleaseNsn(instance), " requeue after 1 minute")

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	klog.Info("Checking to see if all the resources in Status.DeployedRelease.Manifest are deleted ",
		helmreleaseNsn(instance))

	instance.Status.RemoveCondition(appv1.ConditionReleaseFailed)

	caps, err := getCapabilities(manager.GetActionConfig())
	if err != nil {
		klog.Error("Failed to get API Capabilities to perform cleanup check ", helmreleaseNsn(instance), " ", err)
		r.updateUninstallResourceErrorStatus(instance, err)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	manifests := releaseutil.SplitManifests(instance.Status.DeployedRelease.Manifest)

	_, files, err := releaseutil.SortManifests(manifests, caps.APIVersions, releaseutil.UninstallOrder)
	if err != nil {
		klog.Error("Corrupted release record for ", helmreleaseNsn(instance), " ", err)
		r.updateUninstallResourceErrorStatus(instance, err)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	// do not delete resources that are annotated with the Helm resource policy 'keep'
	_, filesToDelete := filterManifestsToKeep(files)
	var builder strings.Builder
	for _, file := range filesToDelete {
		builder.WriteString("\n---\n" + file.Content)
	}
	resources, err := manager.GetActionConfig().KubeClient.Build(strings.NewReader(builder.String()), false)
	if err != nil {
		klog.Error("Unable to build kubernetes objects for delete ", helmreleaseNsn(instance), " ", err)
		r.updateUninstallResourceErrorStatus(instance, err)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	if len(resources) > 0 {
		for _, resource := range resources {
			err = resource.Get()
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue // resource is already delete, check the next one.
				}
				klog.Error("Unable to get resource ", resource.Namespace, "/", resource.Name,
					" for ", helmreleaseNsn(instance), " ", err)
				r.updateUninstallResourceErrorStatus(instance, err)

				return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
			}

			// found at least one resource that is not deleted then just delete everything again.
			_, errs := manager.GetActionConfig().KubeClient.Delete(resources)
			if errs != nil {
				klog.Error("Errors caught while trying to delete resources ", joinErrors(errs))
			}

			gvk := ""
			if resource.Mapping != nil {
				gvk = resource.Mapping.GroupVersionKind.String()
			}

			message := "Failed to delete HelmRelease due to resource: " + gvk + " " +
				resource.Namespace + "/" + resource.Name +
				" is not deleted yet. Checking again after one minute."
			klog.Error(message)
			instance.Status.SetCondition(appv1.HelmAppCondition{
				Type:    appv1.ConditionReleaseFailed,
				Status:  appv1.StatusTrue,
				Reason:  appv1.ReasonUninstallError,
				Message: message,
			})
			_ = r.updateResourceStatus(instance)

			return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
		}
	}

	klog.Info("HelmRelease ", helmreleaseNsn(instance),
		" all Status.DeployedRelease.Manifest resources are deleted/terminating")

	instance.Status.RemoveCondition(appv1.ConditionReleaseFailed)
	instance.Status.SetCondition(appv1.HelmAppCondition{
		Type:   appv1.ConditionDeployed,
		Status: appv1.StatusFalse,
		Reason: appv1.ReasonUninstallSuccessful,
	})
	_ = r.updateResourceStatus(instance)

	controllerutil.RemoveFinalizer(instance, finalizer)

	if err := r.updateResource(instance); err != nil {
		klog.Error("Failed to strip HelmRelease uninstall finalizer ",
			helmreleaseNsn(instance), " ", err)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	// if everything goes well the next time the reconcile won't find the helmrelease anymore
	// which will end the reconcile loop
	return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
}

func (r *ReconcileHelmRelease) updateUninstallResourceErrorStatus(instance *appv1.HelmRelease, err error) {
	instance.Status.SetCondition(appv1.HelmAppCondition{
		Type:    appv1.ConditionReleaseFailed,
		Status:  appv1.StatusTrue,
		Reason:  appv1.ReasonUninstallError,
		Message: err.Error(),
	})
	_ = r.updateResourceStatus(instance)
}

func (r *ReconcileHelmRelease) ensureStatusReasonPopulated(
	instance *appv1.HelmRelease, manager helmoperator.Manager) (reconcile.Result, error) {
	expectedRelease, err := manager.GetDeployedRelease()
	if err != nil {
		klog.Error(err, "Failed to get deployed release for HelmRelease ",
			helmreleaseNsn(instance))
		instance.Status.SetCondition(appv1.HelmAppCondition{
			Type:    appv1.ConditionIrreconcilable,
			Status:  appv1.StatusTrue,
			Reason:  appv1.ReasonReconcileError,
			Message: err.Error(),
		})
		_ = r.updateResourceStatus(instance)
		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}
	instance.Status.RemoveCondition(appv1.ConditionIrreconcilable)

	reason := appv1.ReasonUpgradeSuccessful
	if expectedRelease.Version == 1 {
		reason = appv1.ReasonInstallSuccessful
	}
	message := ""
	if expectedRelease.Info != nil {
		message = expectedRelease.Info.Notes
	}
	instance.Status.SetCondition(appv1.HelmAppCondition{
		Type:    appv1.ConditionDeployed,
		Status:  appv1.StatusTrue,
		Reason:  reason,
		Message: message,
	})
	instance.Status.DeployedRelease = &appv1.HelmAppRelease{
		Name:     expectedRelease.Name,
		Manifest: expectedRelease.Manifest,
	}
	err = r.updateResourceStatus(instance)
	if err != nil {
		klog.Error("Failed to update resource status for HelmRelease ",
			helmreleaseNsn(instance), " ", err)
	}

	return reconcile.Result{}, err
}

func helmreleaseNsn(hr *appv1.HelmRelease) string {
	return fmt.Sprintf("%s/%s", hr.GetNamespace(), hr.GetName())
}

// Source from https://github.com/helm/helm/blob/v3.4.2/pkg/action/resource_policy.go
func filterManifestsToKeep(manifests []releaseutil.Manifest) (keep, remaining []releaseutil.Manifest) {
	for _, m := range manifests {
		if m.Head.Metadata == nil || m.Head.Metadata.Annotations == nil || len(m.Head.Metadata.Annotations) == 0 {
			remaining = append(remaining, m)
			continue
		}

		resourcePolicyType, ok := m.Head.Metadata.Annotations[kube.ResourcePolicyAnno]
		if !ok {
			remaining = append(remaining, m)
			continue
		}

		resourcePolicyType = strings.ToLower(strings.TrimSpace(resourcePolicyType))
		if resourcePolicyType == kube.KeepPolicy {
			keep = append(keep, m)
		}

	}
	return keep, remaining
}

func joinErrors(errs []error) string {
	es := make([]string, 0, len(errs))
	for _, e := range errs {
		es = append(es, e.Error())
	}
	return strings.Join(es, "; ")
}
