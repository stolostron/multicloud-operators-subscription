/*
Copyright 2021 The Kubernetes Authors.

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
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	libpredicate "github.com/operator-framework/operator-lib/predicate"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/kube"
	rpb "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/releaseutil"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	appsubv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	helmoperator "open-cluster-management.io/multicloud-operators-subscription/pkg/helmrelease/release"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
)

const (
	finalizer = "uninstall-helm-release"

	defaultMaxConcurrent = 10
)

// Add creates a new HelmRelease Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	synchronizer := kubesynchronizer.GetDefaultSynchronizer()

	if synchronizer == nil {
		err := fmt.Errorf("failed to get default synchronizer for HelmRelease controller")
		klog.Error(err)

		return err
	}

	chartsDir := os.Getenv(appv1.ChartsDir)
	if chartsDir == "" {
		chartsDir = "/tmp/hr-charts"

		err := os.Setenv(appv1.ChartsDir, chartsDir)
		if err != nil {
			return err
		}
	}

	r := &ReconcileHelmRelease{mgr, synchronizer, nil}

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

	watchDependentResources(mgr, r, c)

	return nil
}

// blank assignment to verify that ReconcileHelmRelease implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileHelmRelease{}

// ReleaseHookFunc defines a function signature for release hooks.
type ReleaseHookFunc func(*rpb.Release) error

// ReconcileHelmRelease reconciles a HelmRelease object
type ReconcileHelmRelease struct {
	manager.Manager
	synchronizer *kubesynchronizer.KubeSynchronizer
	releaseHook  ReleaseHookFunc
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

	err := r.GetClient().Get(context.TODO(), request.NamespacedName, instance)
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

		err = r.GetClient().Update(context.TODO(), instance)
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
			r.populateErrorAppSubStatus(err.Error(), instance)

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
		r.populateErrorAppSubStatus(err.Error(), instance)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	dryRunManager, err := r.newHelmOperatorManager(instance, request, helmOperatorManagerFactory)
	if err != nil {
		klog.Error("Failed to create new dry-run HelmOperatorManager: ",
			helmreleaseNsn(instance), " ", err)

		instance.Status.SetCondition(appv1.HelmAppCondition{
			Type:    appv1.ConditionIrreconcilable,
			Status:  appv1.StatusTrue,
			Reason:  appv1.ReasonReconcileError,
			Message: err.Error(),
		})
		_ = r.updateResourceStatus(instance)
		r.populateErrorAppSubStatus(err.Error(), instance)

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
		r.deleteAppSubStatus(instance)

		return r.uninstall(instance, manager, dryRunManager)
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
		r.populateErrorAppSubStatus(err.Error(), instance)

		klog.Info("Requeue HelmRelease after one minute ")

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	instance.Status.RemoveCondition(appv1.ConditionIrreconcilable)

	if !contains(instance.GetFinalizers(), finalizer) {
		klog.V(1).Info("Adding finalizer (", finalizer, ") to ", helmreleaseNsn(instance))
		controllerutil.AddFinalizer(instance, finalizer)
		if err := r.updateResource(instance); err != nil {
			klog.Error("Failed to add uninstall finalizer to ", helmreleaseNsn(instance))
			return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
		}
	}

	if !manager.IsInstalled() {
		return r.install(instance, manager, dryRunManager)
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

	if instance.Repo.WatchNamespaceScopedResources {
		klog.Info("Reapplying Release ", helmreleaseNsn(instance))

		expectedRelease, err := manager.ReconcileRelease(ctx)
		if err != nil {
			klog.Error(err, "Failed to reconcile release")
			instance.Status.SetCondition(appv1.HelmAppCondition{
				Type:    appv1.ConditionReleaseFailed,
				Status:  appv1.StatusTrue,
				Reason:  appv1.ReasonReconcileError,
				Message: err.Error(),
			})
			_ = r.updateResourceStatus(instance)
			return reconcile.Result{}, err
		}

		if err := r.runReleaseHook(instance, expectedRelease); err != nil {
			klog.Error(err, "Failed to run release hook")

			return reconcile.Result{}, err
		}
	}

	return r.ensureStatusReasonPopulated(instance, manager)
}

func (r ReconcileHelmRelease) runReleaseHook(hr *appv1.HelmRelease, release *rpb.Release) error {
	if hr.Repo.WatchNamespaceScopedResources && r.releaseHook != nil {
		return r.releaseHook(release)
	}

	return nil
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

func (r *ReconcileHelmRelease) install(instance *appv1.HelmRelease, manager helmoperator.Manager,
	dryRunManager helmoperator.Manager) (reconcile.Result, error) {
	// If all the Helm release records are deleted, then the Helm operator will try to install the release again.
	// In that case, if the install errors, then don't perform the uninstall rollback because it might lead to unintended data loss.
	// See: https://github.com/operator-framework/operator-sdk/issues/4296
	rollbackByUninstall := true
	if instance.Status.DeployedRelease != nil {
		klog.Info("Release is not installed but status.DeployedRelease is populated. If the install error, skip rollback(uninstall)")
		rollbackByUninstall = false
	}

	klog.Info("Installing (dry-run) Release ", helmreleaseNsn(instance))

	installDryRun := func(install *action.Install) error {
		install.DryRun = true
		install.ClientOnly = true

		return nil
	}

	installedRelease, err := dryRunManager.InstallRelease(context.TODO(), installDryRun)
	if err != nil {
		klog.Error("Failed to install (dry-run) HelmRelease ",
			helmreleaseNsn(instance), " ", err)
		instance.Status.SetCondition(appv1.HelmAppCondition{
			Type:    appv1.ConditionReleaseFailed,
			Status:  appv1.StatusTrue,
			Reason:  appv1.ReasonInstallError,
			Message: err.Error(),
		})
		_ = r.updateResourceStatus(instance)
		r.populateErrorAppSubStatus(string(appv1.ReasonInstallError)+" "+err.Error(), instance)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	if installedRelease != nil && installedRelease.Manifest != "" && instance.OwnerReferences != nil {
		r.populateAppSubStatus(installedRelease.Manifest, instance, manager, string(appSubStatusV1alpha1.PackageUnknown), "", nil)
	}

	klog.Info("Installing Release ", helmreleaseNsn(instance))

	installOpt := func(install *action.Install) error {
		install.DryRun = false
		install.ClientOnly = false

		return nil
	}

	installedRelease, err = manager.InstallRelease(context.TODO(), installOpt)
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
		r.populateErrorAppSubStatus(string(appv1.ReasonInstallError)+" "+err.Error(), instance)

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

				errMsg := "failed installation " + err.Error() + " and failed uninstall rollback " + errUninstall.Error()

				instance.Status.SetCondition(appv1.HelmAppCondition{
					Type:    appv1.ConditionReleaseFailed,
					Status:  appv1.StatusTrue,
					Reason:  appv1.ReasonInstallError,
					Message: errMsg,
				})
				_ = r.updateResourceStatus(instance)
				r.populateErrorAppSubStatus(string(appv1.ReasonInstallError)+" "+errMsg, instance)

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

	if err := r.runReleaseHook(instance, installedRelease); err != nil {
		klog.Error(err, "Failed to run release hook")

		return reconcile.Result{}, err
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

	if installedRelease != nil && installedRelease.Manifest != "" && instance.OwnerReferences != nil {
		r.populateAppSubStatus(installedRelease.Manifest, instance, manager, string(appSubStatusV1alpha1.PackageDeployed),
			instance.Repo.Version+" "+string(appv1.ReasonInstallSuccessful), nil)
	}

	return reconcile.Result{}, err
}

func (r *ReconcileHelmRelease) upgrade(instance *appv1.HelmRelease, manager helmoperator.Manager) (reconcile.Result, error) {
	klog.Info("Upgrading Release ", helmreleaseNsn(instance))
	force := hasHelmUpgradeForceAnnotation(instance)
	upgradeOpt := func(upgrade *action.Upgrade) error {
		upgrade.DryRun = false
		upgrade.Force = force

		return nil
	}
	_, upgradedRelease, err := manager.UpgradeRelease(context.TODO(), upgradeOpt)
	if err != nil {
		klog.Error("Failed to upgrade HelmRelease ", helmreleaseNsn(instance), " ", err)
		instance.Status.SetCondition(appv1.HelmAppCondition{
			Type:    appv1.ConditionReleaseFailed,
			Status:  appv1.StatusTrue,
			Reason:  appv1.ReasonUpgradeError,
			Message: err.Error(),
		})
		_ = r.updateResourceStatus(instance)
		r.populateErrorAppSubStatus(string(appv1.ReasonUpgradeError)+" "+err.Error(), instance)

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

				errMsg := "failed upgrade " + err.Error() + " and failed rollback: " + errRollback.Error()

				instance.Status.SetCondition(appv1.HelmAppCondition{
					Type:    appv1.ConditionReleaseFailed,
					Status:  appv1.StatusTrue,
					Reason:  appv1.ReasonUpgradeError,
					Message: errMsg,
				})
				_ = r.updateResourceStatus(instance)
				r.populateErrorAppSubStatus(string(appv1.ReasonUpgradeError)+" "+errMsg, instance)

				return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
			}

			klog.Info("Rollbacked Release for upgrade failure ", helmreleaseNsn(instance))
		}

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}
	instance.Status.RemoveCondition(appv1.ConditionReleaseFailed)

	if err := r.runReleaseHook(instance, upgradedRelease); err != nil {
		klog.Error(err, "Failed to run release hook")

		return reconcile.Result{}, err
	}

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

	if upgradedRelease != nil && upgradedRelease.Manifest != "" && instance.OwnerReferences != nil {
		r.populateAppSubStatus(upgradedRelease.Manifest, instance, manager, string(appSubStatusV1alpha1.PackageDeployed),
			instance.Repo.Version+" "+string(appv1.ReasonUpgradeSuccessful), nil)
	}

	return reconcile.Result{}, err
}

func (r *ReconcileHelmRelease) uninstall(instance *appv1.HelmRelease, manager helmoperator.Manager,
	dryRunManager helmoperator.Manager) (reconcile.Result, error) {
	if !contains(instance.GetFinalizers(), finalizer) {
		klog.Info("HelmRelease is terminated, skipping reconciliation ", helmreleaseNsn(instance))

		return reconcile.Result{}, nil
	}

	klog.Info("Uninstalling (dry-run) Release ", helmreleaseNsn(instance))

	uninstallDryRun := func(uninstall *action.Uninstall) error {
		uninstall.DryRun = true

		return nil
	}

	_, err := dryRunManager.UninstallRelease(context.TODO(), uninstallDryRun)
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		klog.Error("Failed to uninstall (dry-run) HelmRelease ", helmreleaseNsn(instance), " ", err)
		r.updateUninstallResourceErrorStatus(instance, err)
		r.populateErrorAppSubStatus(string(appv1.ReasonUninstallError)+" "+err.Error(), instance)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	klog.Info("Uninstalling Release ", helmreleaseNsn(instance))

	uninstallOpt := func(uninstall *action.Uninstall) error {
		uninstall.DryRun = false

		return nil
	}

	_, err = manager.UninstallRelease(context.TODO(), uninstallOpt)
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		klog.Error("Failed to uninstall HelmRelease ", helmreleaseNsn(instance), " ", err)
		r.updateUninstallResourceErrorStatus(instance, err)
		r.populateErrorAppSubStatus(string(appv1.ReasonUninstallError)+" "+err.Error(), instance)

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

	caps, err := GetCapabilities(manager.GetActionConfig())
	if err != nil {
		klog.Error("Failed to get API Capabilities to perform cleanup check ", helmreleaseNsn(instance), " ", err)
		r.updateUninstallResourceErrorStatus(instance, err)
		r.populateErrorAppSubStatus(string(appv1.ReasonUninstallError)+" "+err.Error(), instance)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}

	manifests := releaseutil.SplitManifests(instance.Status.DeployedRelease.Manifest)

	_, files, err := releaseutil.SortManifests(manifests, caps.APIVersions, releaseutil.UninstallOrder)
	if err != nil {
		klog.Error("Corrupted release record for ", helmreleaseNsn(instance), " ", err)
		r.updateUninstallResourceErrorStatus(instance, err)
		r.populateErrorAppSubStatus(string(appv1.ReasonUninstallError)+" "+err.Error(), instance)

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
		r.populateErrorAppSubStatus(string(appv1.ReasonUninstallError)+" "+err.Error(), instance)

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
				r.populateErrorAppSubStatus(string(appv1.ReasonUninstallError)+" "+err.Error(), instance)

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
			r.populateErrorAppSubStatus(string(appv1.ReasonUninstallError)+" "+message, instance)

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
		r.populateErrorAppSubStatus(err.Error(), instance)

		return reconcile.Result{RequeueAfter: time.Minute * 1}, nil
	}
	instance.Status.RemoveCondition(appv1.ConditionIrreconcilable)

	if err := r.runReleaseHook(instance, expectedRelease); err != nil {
		klog.Error(err, "Failed to run release hook")

		return reconcile.Result{}, err
	}

	// ensure the appsub status resource exist
	skipUpdate := true
	r.populateAppSubStatus(expectedRelease.Manifest, instance, manager, string(appSubStatusV1alpha1.PackageDeployed),
		instance.Repo.Version, &skipUpdate)

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

func (r *ReconcileHelmRelease) populateAppSubStatus(
	manifest string, instance *appv1.HelmRelease, manager helmoperator.Manager, packagePhase string, helmReleaseMessage string,
	skipUpdate *bool) {
	for _, hrOwner := range instance.OwnerReferences {
		if strings.EqualFold(hrOwner.APIVersion, "apps.open-cluster-management.io/v1") &&
			strings.EqualFold(hrOwner.Kind, "Subscription") {

			caps, err := GetCapabilities(manager.GetActionConfig())
			if err != nil {
				klog.Error("Failed to get capabilities ", helmreleaseNsn(instance), " ", err)
			} else {
				manifests := releaseutil.SplitManifests(manifest)
				_, files, err := releaseutil.SortManifests(manifests, caps.APIVersions, releaseutil.InstallOrder)
				if err != nil {
					klog.Error("Failed to sort manifest ", helmreleaseNsn(instance), " ", err)
				} else {
					appSubUnitStatuses := []kubesynchronizer.SubscriptionUnitStatus{}

					for _, file := range files {
						if file.Head != nil && file.Head.Metadata != nil {
							appSubUnitStatus := kubesynchronizer.SubscriptionUnitStatus{}
							appSubUnitStatus.APIVersion = file.Head.Version
							appSubUnitStatus.Kind = file.Head.Kind
							appSubUnitStatus.Name = file.Head.Metadata.Name
							appSubUnitStatus.Namespace = instance.Namespace
							appSubUnitStatus.Phase = packagePhase
							appSubUnitStatus.Message = ""
							appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)
						}
					}

					appSubUnitStatus := kubesynchronizer.SubscriptionUnitStatus{}
					appSubUnitStatus.APIVersion = instance.APIVersion
					appSubUnitStatus.Kind = instance.Kind
					appSubUnitStatus.Name = instance.Name
					appSubUnitStatus.Namespace = instance.Namespace
					appSubUnitStatus.Phase = packagePhase
					appSubUnitStatus.Message = helmReleaseMessage
					appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)

					appsubClusterStatus := kubesynchronizer.SubscriptionClusterStatus{
						Cluster:                   r.synchronizer.SynchronizerID.Name,
						AppSub:                    types.NamespacedName{Name: hrOwner.Name, Namespace: instance.GetNamespace()},
						Action:                    "APPLY",
						SubscriptionPackageStatus: appSubUnitStatuses,
					}

					// get the parent appsub
					appsub := &appsubv1.Subscription{}
					appsubKey := types.NamespacedName{
						Namespace: instance.GetNamespace(),
						Name:      hrOwner.Name,
					}
					err := r.GetClient().Get(context.TODO(), appsubKey, appsub)
					if err != nil {
						klog.Warning("failed to get parent appsub, err: ", err)
					}

					skipOrphanDelete := true
					err = r.synchronizer.SyncAppsubClusterStatus(appsub, appsubClusterStatus, &skipOrphanDelete, skipUpdate)
					if err != nil {
						klog.Warning("error while sync app sub cluster status: ", err)
					}
				}
			}
		}
	}
}

func (r *ReconcileHelmRelease) populateErrorAppSubStatus(
	errMsg string, instance *appv1.HelmRelease) {
	for _, hrOwner := range instance.OwnerReferences {
		if hrOwner.Kind == "Subscription" {

			appSubUnitStatuses := []kubesynchronizer.SubscriptionUnitStatus{}

			appSubUnitStatus := kubesynchronizer.SubscriptionUnitStatus{}
			appSubUnitStatus.APIVersion = instance.APIVersion
			appSubUnitStatus.Kind = instance.Kind
			appSubUnitStatus.Name = instance.Name
			appSubUnitStatus.Namespace = instance.Namespace

			appSubUnitStatus.Phase = string(appSubStatusV1alpha1.PackageDeployFailed)
			appSubUnitStatus.Message = errMsg
			appSubUnitStatuses = append(appSubUnitStatuses, appSubUnitStatus)

			appsubClusterStatus := kubesynchronizer.SubscriptionClusterStatus{
				Cluster:                   r.synchronizer.SynchronizerID.Name,
				AppSub:                    types.NamespacedName{Name: hrOwner.Name, Namespace: instance.GetNamespace()},
				Action:                    "APPLY",
				SubscriptionPackageStatus: appSubUnitStatuses,
			}

			// get the parent appsub
			appsub := &appsubv1.Subscription{}
			appsubKey := types.NamespacedName{
				Namespace: instance.GetNamespace(),
				Name:      hrOwner.Name,
			}
			err := r.GetClient().Get(context.TODO(), appsubKey, appsub)
			if err != nil {
				klog.Warning("failed to get parent appsub, err: ", err)
			}

			skipOrphanDelete := true
			err = r.synchronizer.SyncAppsubClusterStatus(appsub, appsubClusterStatus, &skipOrphanDelete, nil)
			if err != nil {
				klog.Warning("error while sync app sub cluster status: ", err)
			}
		}
	}
}

func (r *ReconcileHelmRelease) deleteAppSubStatus(instance *appv1.HelmRelease) {
	for _, hrOwner := range instance.OwnerReferences {
		if strings.EqualFold(hrOwner.APIVersion, "apps.open-cluster-management.io/v1") &&
			strings.EqualFold(hrOwner.Kind, "Subscription") {

			appSubUnitStatuses := []kubesynchronizer.SubscriptionUnitStatus{}

			appsubClusterStatus := kubesynchronizer.SubscriptionClusterStatus{
				Cluster:                   r.synchronizer.SynchronizerID.Name,
				AppSub:                    types.NamespacedName{Name: hrOwner.Name, Namespace: instance.GetNamespace()},
				Action:                    "DELETE",
				SubscriptionPackageStatus: appSubUnitStatuses,
			}

			skipOrphanDelete := true
			err := r.synchronizer.SyncAppsubClusterStatus(nil, appsubClusterStatus, &skipOrphanDelete, nil)
			if err != nil {
				klog.Warning("error while sync app sub cluster status: ", err)
			}
		}
	}
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

// coped and modified from https://github.com/operator-framework/operator-sdk/blob/v1.22.0/internal/helm/controller/controller.go
func watchDependentResources(mgr manager.Manager, r *ReconcileHelmRelease, c controller.Controller) {
	owner := &appv1.HelmRelease{}

	var m sync.RWMutex
	watches := map[schema.GroupVersionKind]struct{}{}
	releaseHook := func(release *rpb.Release) error {
		resources := releaseutil.SplitManifests(release.Manifest)

		if len(resources) == 0 {
			klog.Warning("Failed to find resources for release ", release.Name)
		}

		for _, resource := range resources {
			var u unstructured.Unstructured
			if err := yaml.Unmarshal([]byte(resource), &u); err != nil {
				return err
			}

			gvk := u.GroupVersionKind()
			if gvk.Empty() {
				continue
			}

			var setWatchOnResource = func(dependent runtime.Object) error {
				unstructuredObj := dependent.(*unstructured.Unstructured)

				gvkDependent := unstructuredObj.GroupVersionKind()
				if gvkDependent.Empty() {
					return nil
				}

				m.RLock()
				_, ok := watches[gvkDependent]
				m.RUnlock()
				if ok {
					return nil
				}

				err := c.Watch(&source.Kind{Type: unstructuredObj}, &handler.EnqueueRequestForOwner{OwnerType: owner},
					libpredicate.DependentPredicate{})
				if err != nil {
					return err
				}

				m.Lock()
				watches[gvkDependent] = struct{}{}
				m.Unlock()
				klog.Info("Watching dependent resource ownerApiVersion ", owner.GroupVersionKind().GroupVersion(),
					" ownerKind ", owner.GroupVersionKind().Kind, " apiVersion ", gvkDependent.GroupVersion(), " kind ", gvkDependent.Kind)
				return nil
			}

			// List is not actually a resource and therefore cannot have a
			// watch on it. The watch will be on the kinds listed in the list
			// and will therefore need to be handled individually.
			listGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "List"}
			if gvk == listGVK {
				errListItem := u.EachListItem(func(obj runtime.Object) error {
					return setWatchOnResource(obj)
				})
				if errListItem != nil {
					return errListItem
				}
			} else {
				err := setWatchOnResource(&u)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}
	r.releaseHook = releaseHook
}
