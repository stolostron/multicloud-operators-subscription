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

package mcmhub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	manifestWorkV1 "open-cluster-management.io/api/work/v1"
	placementV1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	appSubV1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appSubStatusV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var manifestNSString string
var manifestAppsubString string

func (r *ReconcileSubscription) PropagateAppSubManifestWork(instance *appSubV1.Subscription, clusters []ManageClusters) error {
	// try to find all children manifestworks
	children, err := r.getManifestWorkFamily(instance)

	if err != nil {
		klog.Error("Failed to get children manifeworks with err:", err)
	}

	// prepare map to delete expired children
	expiredManifestWorkmap := make(map[string]*manifestWorkV1.ManifestWork)

	for _, manifestWork := range children {
		expiredManifestWorkmap[manifestWork.GetNamespace()+"-"+manifestWork.GetName()] = manifestWork
	}

	klog.V(1).Infof("expiredManifestWorkmap: %#v", expiredManifestWorkmap)

	// propagate template
	expiredManifestWorkmap, err = r.propagateManifestWorks(clusters, instance, expiredManifestWorkmap)
	if err != nil {
		klog.Error("Error in propagating to clusters:", err)
		return err
	}

	// delete expired appsub manifestWork
	klog.Info("Expired manifestWork map:", expiredManifestWorkmap)

	for _, manifestWork := range expiredManifestWorkmap {
		mainfestWorkKey := types.NamespacedName{Namespace: manifestWork.GetNamespace(), Name: manifestWork.GetName()}
		err = r.Delete(context.TODO(), manifestWork)

		addtionalMsg := "Delete Expired ManifestWork " + mainfestWorkKey.String()
		r.eventRecorder.RecordEvent(instance, "Delete", addtionalMsg, err)

		if err != nil {
			klog.Errorf("Failed to delete Expired ManifestWork: %v/%v, err: %v", manifestWork.GetNamespace(), manifestWork.GetName(), err)
		}

		// remove relative appSubPakcageStatus CRs from the expired manifestWork cluster NS
		cleanupErr := r.cleanupAppSubStatus(instance, manifestWork.GetNamespace())
		if cleanupErr != nil {
			klog.Warning("error while clean up app sub status: ", cleanupErr)
		}
	}

	return err
}

func (r *ReconcileSubscription) getManifestWorkFamily(instance *appSubV1.Subscription) ([]*manifestWorkV1.ManifestWork, error) {
	// get all existing manifestworks
	exlist := &manifestWorkV1.ManifestWorkList{}
	exlabel := make(map[string]string)
	exlabel[appSubV1.AnnotationHosting] = fmt.Sprintf("%.63s", instance.GetNamespace()+"."+instance.GetName())
	err := r.List(context.TODO(), exlist, client.MatchingLabels(exlabel))

	if err != nil && !errors.IsNotFound(err) {
		klog.Error("Trying to list existing manifestWorks ", instance.GetNamespace(), "/", instance.GetName(), " with error:", err)

		return nil, err
	}

	manifestWorkList := []*manifestWorkV1.ManifestWork{}

	for _, manifestWork := range exlist.Items {
		// Due to k8s label max length of 63 characters, the manifestworks containing the same label could belong to different appsubs
		// need to further check the maniefstwork Name to make sure the manifestWork does belong to the appsub
		// e.g. The following 2 appsubs are deployed to the same NS `fo-monitoring-incluster`
		// appsub1: prometheus-stack-incluster-platform-engineering-westeurope01
		// appsub2: prometheus-stack-incluster-platform-engineering-eastus02
		// The generated manifestwork labels are exactly the same regardless of the different endings after the first 63 characters
		// fo-monitoring-incluster.prometheus-stack-incluster-platform-engineering-westeurope01
		// fo-monitoring-incluster.prometheus-stack-incluster-platform-engineering-eastus02
		if manifestWork.GetName() != instance.Namespace+"-"+instance.Name {
			klog.Infof("Skip the manifestWork %v/%v as it doesn't belong to the app %v/%v", manifestWork.Namespace, manifestWork.Name, instance.Namespace, instance.Name)

			continue
		}

		manifestWorkList = append(manifestWorkList, manifestWork.DeepCopy())
		klog.Infof("The manifestWork %v/%v added to the app %v/%v", manifestWork.Namespace, manifestWork.Name, instance.Namespace, instance.Name)
	}

	klog.Infof("Total children manifestWorks: %v for the app: %v/%v", len(manifestWorkList), instance.Namespace, instance.Name)

	return manifestWorkList, nil
}

func (r *ReconcileSubscription) propagateManifestWorks(clusters []ManageClusters, instance *appSubV1.Subscription,
	familymap map[string]*manifestWorkV1.ManifestWork) (map[string]*manifestWorkV1.ManifestWork, error) {
	var err error

	hosting := types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}

	// prepare appsub namespace manifest
	manifestNSString, err = prepareManifestWorkNS(instance.GetNamespace(), hosting)
	if err != nil {
		return nil, err
	}

	// prepare appsub manifest
	manifestAppsubString, err = r.prepareManifestWorkAppsub(instance, hosting)
	if err != nil {
		return nil, err
	}

	for _, cluster := range clusters {
		familymap, err = r.createManifestWork(cluster, hosting, instance, familymap)
		if err != nil {
			klog.Errorf("Error in propagating to cluster: %v, error:%v", cluster.Cluster, err)

			err = utils.CreateFailedAppsubReportResult(r.Client, cluster.Cluster, instance.Namespace, instance.Name, err.Error())
			if err != nil {
				klog.Error("Error create cluster appsubReport: ", err)
			}
		}
	}

	return familymap, nil
}

func (r *ReconcileSubscription) createManifestWork(cluster ManageClusters, hosting types.NamespacedName, instance *appSubV1.Subscription,
	familymap map[string]*manifestWorkV1.ManifestWork) (map[string]*manifestWorkV1.ManifestWork, error) {
	var err error

	klog.V(1).Infof("Creating Managed manifestWork for appsub: %v/%v, cluster: %v", instance.GetNamespace(), instance.GetName(), cluster)

	truekey := cluster.Cluster + "-" + instance.GetNamespace() + "-" + instance.GetName()

	klog.V(1).Infof("truekey: %v, familymap: %#v", truekey, familymap)

	var existingManifestWork *manifestWorkV1.ManifestWork
	existingManifestWork, ok := familymap[truekey]

	if !ok {
		existingManifestWork = &manifestWorkV1.ManifestWork{}
	}

	original := existingManifestWork.DeepCopy()

	existingManifestWork, err = r.setLocalManifestWork(cluster, hosting, instance, existingManifestWork)
	if err != nil {
		klog.Error("Failed to set local manifestwork. error:", err)
		return nil, err
	}

	if !ok {
		err = r.Create(context.TODO(), existingManifestWork)
		klog.Infof("Creating new local ManifestWork: %v/%v, err: %v",
			existingManifestWork.GetNamespace(), existingManifestWork.GetName(), err)
	} else {
		if !utils.CompareManifestWork(original, existingManifestWork) {
			err = r.Update(context.TODO(), existingManifestWork)
			klog.Infof("Updating existing local ManifestWork: %v/%v err: %v",
				existingManifestWork.GetNamespace(), existingManifestWork.GetName(), err)
		} else {
			klog.Infof("Same existing local ManifestWork, no need to update: %v/%v ",
				existingManifestWork.GetNamespace(), existingManifestWork.GetName())
		}
	}

	if err != nil {
		klog.Error("Failed in processing local ManifestWork with error:", err)

		return nil, err
	}

	// remove it from to-be deleted map
	klog.V(1).Info("Removing ", truekey, " from ", familymap)
	delete(familymap, truekey)

	return familymap, nil
}

func (r *ReconcileSubscription) setLocalManifestWork(cluster ManageClusters, hosting types.NamespacedName,
	appsub *appSubV1.Subscription, localManifestWork *manifestWorkV1.ManifestWork) (*manifestWorkV1.ManifestWork, error) {
	newManifestAppsubByte := []byte(manifestAppsubString)

	// if target cluster is local-cluster, append -local suffix to the appsub name to avoid subscription name collision in the same namespace
	if cluster.IsLocalCluster {
		klog.Info("This is local-cluster, Appending -local to the subscription name")

		sub := &unstructured.Unstructured{}

		err := json.Unmarshal(newManifestAppsubByte, sub)
		if err != nil {
			klog.Info("Failed to unmarshall manifestAppsub, err:", err, " |template: ", string(newManifestAppsubByte))
		} else {
			sub.SetName(sub.GetName() + "-local")
		}

		newManifestAppsubByte, err = json.Marshal(sub)
		if err != nil {
			klog.Info("Error in mashalling obj ", sub, err)
			return nil, err
		}
	}

	localManifestWork.APIVersion = "work.open-cluster-management.io/v1"
	localManifestWork.Kind = "ManifestWork"

	localManifestWork.SetName(appsub.GetNamespace() + "-" + appsub.GetName())
	localManifestWork.SetNamespace(cluster.Cluster)

	localLabels := localManifestWork.GetLabels()

	if localLabels == nil {
		localLabels = make(map[string]string)
	}

	localLabels[appSubV1.AnnotationHosting] = fmt.Sprintf("%.63s", hosting.Namespace+"."+hosting.Name)
	localManifestWork.SetLabels(localLabels)

	localManifestWork.Spec.Workload.Manifests = []manifestWorkV1.Manifest{
		{
			RawExtension: runtime.RawExtension{
				Raw: []byte(manifestNSString),
			},
		},
		{
			RawExtension: runtime.RawExtension{
				Raw: newManifestAppsubByte,
			},
		},
	}

	localManifestWork.Spec.DeleteOption = &manifestWorkV1.DeleteOption{
		PropagationPolicy: manifestWorkV1.DeletePropagationPolicyTypeSelectivelyOrphan,
		SelectivelyOrphan: &manifestWorkV1.SelectivelyOrphan{
			OrphaningRules: []manifestWorkV1.OrphaningRule{
				{
					Group:     "",
					Namespace: "",
					Resource:  "namespaces",
					Name:      appsub.GetNamespace(),
				},
			},
		},
	}

	for i := 0; i < len(localManifestWork.Spec.Workload.Manifests); i++ {
		klog.V(1).Infof("workload manifest: %#v", string(localManifestWork.Spec.Workload.Manifests[i].Raw))
	}

	return localManifestWork, nil
}

func (r *ReconcileSubscription) prepareManifestWorkAppsub(appsub *appSubV1.Subscription, hosting types.NamespacedName) (string, error) {
	var err error

	b := true
	subep := &appSubV1.Subscription{
		TypeMeta: metaV1.TypeMeta{
			Kind:       "Subscription",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Name:      appsub.GetName(),
			Namespace: appsub.GetNamespace(),
			Annotations: map[string]string{
				appSubV1.AnnotationHosting: hosting.String(),
			},
		},
		Spec: appSubV1.SubscriptionSpec{
			Placement: &placementV1.Placement{
				Local: &b,
			},
		},
	}

	subep.Spec.Channel = appsub.Spec.Channel
	subep.Spec.Package = appsub.Spec.Package
	subep.Spec.PackageFilter = appsub.Spec.PackageFilter
	subep.Spec.PackageOverrides = appsub.Spec.PackageOverrides
	subep.Spec.Overrides = appsub.Spec.Overrides
	subep.Spec.TimeWindow = appsub.Spec.TimeWindow
	subep.Spec.HookSecretRef = appsub.Spec.HookSecretRef
	subep.Spec.Allow = appsub.Spec.Allow
	subep.Spec.Deny = appsub.Spec.Deny
	subep.Spec.WatchHelmNamespaceScopedResources = appsub.Spec.WatchHelmNamespaceScopedResources
	subep.Spec.SecondaryChannel = appsub.Spec.SecondaryChannel

	subepanno := r.updateSubAnnotations(appsub, hosting)
	subep.SetAnnotations(subepanno)

	subepLabels := appsub.GetLabels()
	subep.SetLabels(subepLabels)

	klog.V(1).Infof("new local subep: %#v", subep)

	manifestAppsubByte, err := json.Marshal(subep)
	if err != nil {
		klog.Info("Error in mashalling subep obj ", err)
		return "", err
	}

	return string(manifestAppsubByte), nil
}

func prepareManifestWorkNS(appsubNS string, hosting types.NamespacedName) (string, error) {
	var err error

	endpointNS := &coreV1.Namespace{
		TypeMeta: metaV1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Name: appsubNS,
			Annotations: map[string]string{
				appSubV1.AnnotationHosting: hosting.String(),
			},
		},
	}

	klog.V(1).Infof("new local endpointNS: %#v", endpointNS)

	manifestNSByte, err := json.Marshal(endpointNS)
	if err != nil {
		klog.Info("Error in mashalling endpointNS obj ", err)
		return "", err
	}

	return string(manifestNSByte), nil
}

func (r *ReconcileSubscription) cleanupManifestWork(appsub types.NamespacedName) error {
	manifestWorkList := &manifestWorkV1.ManifestWorkList{}
	listopts := &client.ListOptions{
		Limit: 1000,
	}

	manifestWorkSelector := &metaV1.LabelSelector{
		MatchLabels: map[string]string{
			"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appsub.Namespace+"."+appsub.Name),
		},
	}

	manifestWorkLabels, err := utils.ConvertLabels(manifestWorkSelector)
	if err != nil {
		klog.Error("Failed to convert managed manifestWork label selector, err:", err)

		return err
	}

	listopts.LabelSelector = manifestWorkLabels

	klog.Infof("start listing manifestWorks for appsub: %v", appsub)

	for {
		err = r.List(context.TODO(), manifestWorkList, listopts)

		if err != nil {
			klog.Error("Failed to list managed manifestWorks, err:", err)

			return err
		}

		klog.Infof("Listed manifestWorks for appsub: %v, total: %v", appsub, len(manifestWorkList.Items))

		for _, manifestWork := range manifestWorkList.Items {
			curManifesWork := manifestWork.DeepCopy()
			err := r.Delete(context.TODO(), curManifesWork)

			if err != nil {
				klog.Warningf("Error in deleting existing manifestWork key: %v/%v, err: %v ",
					curManifesWork.GetNamespace(), curManifesWork.GetName(), err)

				return err
			}

			klog.Infof("manifestWork deleted: %v/%v", curManifesWork.GetNamespace(), curManifesWork.GetName())
		}

		if manifestWorkList.GetContinue() == "" {
			klog.Infof("failed to continue to list manifestworks")
			break
		}

		// Set the continue field for the next request
		listopts.Continue = manifestWorkList.GetContinue()

		klog.Infof("continue listing manifestWorks for appsub: %v", appsub)
	}

	klog.Infof("finish listing manifestWorks for appsub: %v", appsub)

	return nil
}

func (r *ReconcileSubscription) cleanupAppSubStatus(appsub *appSubV1.Subscription, manifestWorkNS string) error {
	managedSubPackageStatusList := &appSubStatusV1alpha1.SubscriptionStatusList{}
	listopts := &client.ListOptions{}

	managedSubStatusSelector := &metaV1.LabelSelector{
		MatchLabels: map[string]string{
			"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appsub.GetNamespace()+"."+appsub.GetName()),
		},
	}

	managedSubStatusLabels, err := utils.ConvertLabels(managedSubStatusSelector)
	if err != nil {
		klog.Error("Failed to convert managed appsubstatus label selector, err:", err)

		return err
	}

	listopts.LabelSelector = managedSubStatusLabels
	listopts.Namespace = manifestWorkNS

	err = r.List(context.TODO(), managedSubPackageStatusList, listopts)

	if err != nil {
		klog.Error("Failed to list managed appsubstatus, err:", err)

		return err
	}

	if len(managedSubPackageStatusList.Items) == 0 {
		klog.Infof("No managed appsubstatus with labels %v found", managedSubStatusSelector)

		return nil
	}

	for _, managedSubStatus := range managedSubPackageStatusList.Items {
		curManagedSubStatus := managedSubStatus.DeepCopy()
		err := r.Delete(context.TODO(), curManagedSubStatus)

		if err != nil {
			klog.Warningf("Error in deleting existing appsubstatus key: %v in cluster NS: %v, err: %v ",
				appsub.GetNamespace()+"."+appsub.GetName(), manifestWorkNS, err)

			return err
		}

		klog.Infof("managedSubStatus deleted: %v/%v", curManagedSubStatus.GetNamespace(), curManagedSubStatus.GetName())
	}

	return nil
}

func (r *ReconcileSubscription) updateSubAnnotations(sub *appSubV1.Subscription, hosting types.NamespacedName) map[string]string {
	// Check and add cluster-admin annotation for multi-namepsace application
	r.AddClusterAdminAnnotation(sub)

	subepanno := make(map[string]string)

	origsubanno := sub.GetAnnotations()

	// User and Group annotations
	subepanno[appSubV1.AnnotationUserIdentity] = strings.Trim(origsubanno[appSubV1.AnnotationUserIdentity], "")
	subepanno[appSubV1.AnnotationUserGroup] = strings.Trim(origsubanno[appSubV1.AnnotationUserGroup], "")

	// hosting subscription annotation
	subepanno[appSubV1.AnnotationHosting] = hosting.String()

	// Keep Git related annotations from the source subscription.
	if !strings.EqualFold(origsubanno[appSubV1.AnnotationWebhookEventCount], "") {
		subepanno[appSubV1.AnnotationWebhookEventCount] = origsubanno[appSubV1.AnnotationWebhookEventCount]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationGitBranch], "") {
		subepanno[appSubV1.AnnotationGitBranch] = origsubanno[appSubV1.AnnotationGitBranch]
	} else if !strings.EqualFold(origsubanno[appSubV1.AnnotationGithubBranch], "") {
		subepanno[appSubV1.AnnotationGitBranch] = origsubanno[appSubV1.AnnotationGithubBranch]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationGitPath], "") {
		subepanno[appSubV1.AnnotationGitPath] = origsubanno[appSubV1.AnnotationGitPath]
	} else if !strings.EqualFold(origsubanno[appSubV1.AnnotationGithubPath], "") {
		subepanno[appSubV1.AnnotationGitPath] = origsubanno[appSubV1.AnnotationGithubPath]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationBucketPath], "") {
		subepanno[appSubV1.AnnotationBucketPath] = origsubanno[appSubV1.AnnotationBucketPath]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationClusterAdmin], "") && r.AddClusterAdminAnnotation(sub) {
		subepanno[appSubV1.AnnotationClusterAdmin] = origsubanno[appSubV1.AnnotationClusterAdmin]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationCurrentNamespaceScoped], "") {
		subepanno[appSubV1.AnnotationCurrentNamespaceScoped] = origsubanno[appSubV1.AnnotationCurrentNamespaceScoped]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationResourceReconcileOption], "") {
		subepanno[appSubV1.AnnotationResourceReconcileOption] = origsubanno[appSubV1.AnnotationResourceReconcileOption]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationGitTargetCommit], "") {
		subepanno[appSubV1.AnnotationGitTargetCommit] = origsubanno[appSubV1.AnnotationGitTargetCommit]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationGitTag], "") {
		subepanno[appSubV1.AnnotationGitTag] = origsubanno[appSubV1.AnnotationGitTag]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationGitCloneDepth], "") {
		subepanno[appSubV1.AnnotationGitCloneDepth] = origsubanno[appSubV1.AnnotationGitCloneDepth]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationResourceReconcileLevel], "") {
		subepanno[appSubV1.AnnotationResourceReconcileLevel] = origsubanno[appSubV1.AnnotationResourceReconcileLevel]
	}

	if !strings.EqualFold(origsubanno[appSubV1.AnnotationManualReconcileTime], "") {
		subepanno[appSubV1.AnnotationManualReconcileTime] = origsubanno[appSubV1.AnnotationManualReconcileTime]
	}

	// Keep cluster admin annotation from the source subscription.
	if !strings.EqualFold(origsubanno[appSubV1.AnnotationClusterAdmin], "") {
		subepanno[appSubV1.AnnotationClusterAdmin] = origsubanno[appSubV1.AnnotationClusterAdmin]
	}

	// Add annotation for git path and branch
	// It is recommended to define Git path and branch in subscription annotations but
	// this code is to support those that already use ConfigMap.
	if sub.Spec.PackageFilter != nil && sub.Spec.PackageFilter.FilterRef != nil {
		subscriptionConfigMap := &coreV1.ConfigMap{}
		subcfgkey := types.NamespacedName{
			Name:      sub.Spec.PackageFilter.FilterRef.Name,
			Namespace: sub.Namespace,
		}

		err := r.Get(context.TODO(), subcfgkey, subscriptionConfigMap)
		if err != nil {
			klog.Error("Failed to get PackageFilter.FilterRef of subsciption, error: ", err)
		} else {
			gitPath := subscriptionConfigMap.Data["path"]
			if gitPath != "" {
				subepanno[appSubV1.AnnotationGitPath] = gitPath
			}

			gitBranch := subscriptionConfigMap.Data["branch"]

			if gitBranch != "" {
				subepanno[appSubV1.AnnotationGitBranch] = gitBranch
			}
		}
	}

	return subepanno
}
