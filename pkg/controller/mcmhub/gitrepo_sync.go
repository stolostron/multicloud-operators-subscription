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

package mcmhub

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	gerr "github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	helmops "github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber/helmrepo"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

type kubeResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   *kubeResourceMetadata
}

type kubeResourceMetadata struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

// UpdateGitDeployablesAnnotation clones the git repo and regenerate deployables and update annotation if needed
func (r *ReconcileSubscription) UpdateGitDeployablesAnnotation(sub *appv1.Subscription) (bool, error) {
	updated := false
	origsub := &appv1.Subscription{}
	sub.DeepCopyInto(origsub)

	channel, err := r.getChannel(sub)

	if err != nil {
		klog.Errorf("Failed to find a channel for subscription: %s", sub.GetName())
		return false, err
	}

	if utils.IsGitChannel(string(channel.Spec.Type)) {
		klog.Infof("Subscription %s has Git type channel.", sub.GetName())

		//making sure the commit id is coming from the same source
		commit, err := r.hubGitOps.GetLatestCommitID(sub)
		if err != nil {
			klog.Error(err.Error())
			return false, err
		}

		annotations := sub.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
			sub.SetAnnotations(annotations)
		}

		subKey := types.NamespacedName{Name: sub.GetName(), Namespace: sub.GetNamespace()}
		// Compare the commit to the Git repo and update deployables only if the commit has changed
		// If subscription does not have commit annotation, it needs to be generated in this block.
		oldCommit := getCommitID(sub)

		if oldCommit == "" || !strings.EqualFold(oldCommit, commit) ||
			r.isHookUpdate(annotations, subKey) {
			if oldCommit == "" || !strings.EqualFold(oldCommit, commit) {
				klog.Infof("The Git commit has changed since the last reconcile. last: %s, new: %s", oldCommit, commit)
			}

			if r.isHookUpdate(annotations, subKey) {
				klog.Info("The topo annotation does not have applied hooks. Adding it.")
			}
			// Delete the existing deployables that meets the subscription
			// selector and recreate them
			r.deleteSubscriptionDeployables(sub)

			baseDir := r.hubGitOps.GetRepoRootDirctory(sub)
			resourcePath := getResourcePath(r.hubGitOps.ResolveLocalGitFolder, channel, sub)

			err = r.processRepo(channel, sub, r.hubGitOps.ResolveLocalGitFolder(channel, sub), resourcePath, baseDir)

			if err != nil {
				klog.Error(err.Error())
				return false, err
			}

			setCommitID(sub, commit)

			r.updateGitSubDeployablesAnnotation(sub)

			// Check and add cluster-admin annotation for multi-namepsace application
			r.AddClusterAdminAnnotation(sub)

			if annotations[appv1.AnnotationDeployables] == "" {
				// this might have failed previously. Try again.
				r.updateGitSubDeployablesAnnotation(sub)
			}

			if ifUpdateGitSubscriptionAnnotation(origsub, sub) {
				updated = true
			}
		} else {
			klog.Infof("The Git commit has not changed since the last reconcile. last: %s, new: %s", annotations[appv1.AnnotationGitCommit], commit)
		}
	}

	return updated, nil
}

func (r *ReconcileSubscription) isHookUpdate(a map[string]string, subKey types.NamespacedName) bool {
	applied := r.hooks.GetLastAppliedInstance(subKey)

	if len(applied.pre) != 0 && !strings.Contains(a[subv1.AnnotationTopo], applied.pre) {
		return true
	}

	if len(applied.post) != 0 && !strings.Contains(a[subv1.AnnotationTopo], applied.post) {
		return true
	}

	return false
}

// ifUpdateGitSubscriptionAnnotation compare given annoations between the two subscriptions. return true if no the same
func ifUpdateGitSubscriptionAnnotation(origsub, newsub *appv1.Subscription) bool {
	origanno := origsub.GetAnnotations()
	newanno := newsub.GetAnnotations()

	// 1. compare deployables list annoation
	if ifUpdateAnnotation(origanno, newanno, appv1.AnnotationDeployables) {
		return true
	}

	// 2. compare git-commit annoation
	origGitCommit, origok := origanno[appv1.AnnotationGitCommit]
	newGitCommit, newok := newanno[appv1.AnnotationGitCommit]

	if (!origok && newok) || (origok && !newok) || (origGitCommit != newGitCommit) {
		klog.V(1).Infof("different Git Subscription git-commit annotations. origGitCommit: %v, newGitCommit: %v",
			origGitCommit, newGitCommit)
		return true
	}

	// 3. compare cluster-admin annoation
	origClusterAdmin, origok := origanno[appv1.AnnotationClusterAdmin]
	newClusterAdmin, newok := newanno[appv1.AnnotationClusterAdmin]

	if (!origok && newok) || (origok && !newok) || origClusterAdmin != newClusterAdmin {
		klog.V(1).Infof("different Git Subscription cluster-admin annotations. origClusterAdmin: %v, newClusterAdmin: %v",
			origClusterAdmin, newClusterAdmin)
		return true
	}

	// 4. compare topo annoation
	if ifUpdateAnnotation(origanno, newanno, appv1.AnnotationTopo) {
		return true
	}

	return false
}

func ifUpdateAnnotation(origanno, newanno map[string]string, annoString string) bool {
	origdplmap := make(map[string]bool)

	if origanno != nil {
		dpls := origanno[annoString]
		if dpls != "" {
			dplkeys := strings.Split(dpls, ",")
			for _, dplkey := range dplkeys {
				origdplmap[dplkey] = true
			}
		}

		klog.V(1).Infof("orig dpl map: %v", origdplmap)
	}

	newdplmap := make(map[string]bool)

	if newanno != nil {
		dpls := newanno[annoString]
		if dpls != "" {
			dplkeys := strings.Split(dpls, ",")
			for _, dplkey := range dplkeys {
				newdplmap[dplkey] = true
			}
		}

		klog.V(1).Infof("new dpl map: %v", newdplmap)
	}

	if !reflect.DeepEqual(origdplmap, newdplmap) {
		klog.V(1).Infof("different Git Subscription deployable annotations. origdplmap: %v, newdplmap: %v",
			origdplmap, newdplmap)
		return true
	}

	return false
}

// AddClusterAdminAnnotation adds cluster-admin annotation if conditions are met
func (r *ReconcileSubscription) AddClusterAdminAnnotation(sub *appv1.Subscription) bool {
	annotations := sub.GetAnnotations()
	if annotations[appv1.AnnotationHosting] == "" {
		// if there is hosting subscription, the cluster admin annotation must have been inherited. Don't remove.
		delete(annotations, appv1.AnnotationClusterAdmin) // make sure cluster-admin annotation is removed to begin with
	}

	if utils.IsClusterAdmin(r.Client, sub, r.eventRecorder) {
		annotations[appv1.AnnotationClusterAdmin] = "true"
		sub.SetAnnotations(annotations)

		return true
	}

	return false
}

func getResourcePath(localFolderFunc func(*chnv1.Channel, *appv1.Subscription) string, chn *chnv1.Channel, sub *appv1.Subscription) string {
	resourcePath := localFolderFunc(chn, sub)

	annotations := sub.GetAnnotations()
	if annotations[appv1.AnnotationGithubPath] != "" {
		resourcePath = filepath.Join(localFolderFunc(chn, sub), annotations[appv1.AnnotationGithubPath])
	} else if annotations[appv1.AnnotationGitPath] != "" {
		resourcePath = filepath.Join(localFolderFunc(chn, sub), annotations[appv1.AnnotationGitPath])
	}

	return resourcePath
}

func getGitChart(sub *appv1.Subscription, localRepoRoot, subPath string) (*repo.IndexFile, error) {
	chartDirs, a, b, c, d, err := utils.SortResources(localRepoRoot, subPath)
	if err != nil {
		return nil, gerr.Wrap(err, "failed to get helm index for topo annotation")
	}

	//to pass the linter without changing the utils.SortResources() API
	_ = fmt.Sprint(a, b, c, d)

	// Build a helm repo index file
	indexFile, err := utils.GenerateHelmIndexFile(sub, localRepoRoot, chartDirs)

	if err != nil {
		// If package name is not specified in the subscription, filterCharts throws an error. In this case, just return the original index file.
		return nil, gerr.Wrap(err, "failed to get helm index file")
	}

	return indexFile, nil
}

func (r *ReconcileSubscription) gitHelmResourceString(sub *appv1.Subscription, chn *chnv1.Channel) string {
	idxFile, err := getGitChart(sub, utils.GetLocalGitFolder(chn, sub), getResourcePath(r.hubGitOps.ResolveLocalGitFolder, chn, sub))
	if err != nil {
		klog.Error(err.Error())
		return ""
	}

	_ = idxFile

	if len(idxFile.Entries) != 0 {
		rls, err := helmops.ChartIndexToHelmReleases(r.Client, chn, sub, idxFile)
		if err != nil {
			klog.Error(err.Error())
			return ""
		}

		res, err := generateResrouceList(r.cfg, rls)
		if err != nil {
			klog.Error(err.Error())
			return ""
		}

		return res
	}

	return ""
}

// updateGitSubDeployablesAnnotation set all deployables subscribed by git subscription to the apps.open-cluster-management.io/deployables annotation
func (r *ReconcileSubscription) updateGitSubDeployablesAnnotation(sub *appv1.Subscription) {
	allDpls := r.getSubscriptionDeployables(sub)

	dplstr := ""
	for dplkey := range allDpls {
		if dplstr != "" {
			dplstr += ","
		}

		dplstr += dplkey
	}

	klog.Info("subscription updated for ", sub.Namespace, "/", sub.Name, " new deployables:", dplstr)

	subanno := sub.GetAnnotations()
	if subanno == nil {
		subanno = make(map[string]string)
	}

	subanno[appv1.AnnotationDeployables] = dplstr

	if err := r.updateAnnotationTopo(sub, allDpls); err != nil {
		klog.Errorf("failed to update topo annotation for git sub %v, err: %v", sub.Name, err)
	}

	sub.SetAnnotations(subanno)
}

func (r *ReconcileSubscription) updateAnnotationTopo(sub *subv1.Subscription, allDpls map[string]*dplv1alpha1.Deployable) error {
	dplStr, err := updateResourceListViaDeployableMap(allDpls)
	if err != nil {
		return gerr.Wrap(err, "failed to parse deployable template")
	}

	chn, err := r.getChannel(sub)
	if err != nil {
		return gerr.Wrap(err, "fail to get channel info")
	}

	subanno := sub.GetAnnotations()
	if subanno == nil {
		subanno = make(map[string]string)
	}

	chartRes := r.gitHelmResourceString(sub, chn)
	tpStr := dplStr

	if len(chartRes) != 0 {
		tpStr = fmt.Sprintf("%v,%v", tpStr, chartRes)
	}

	klog.V(3).Infof("dplStr string: %v\n chartStr %v", tpStr, chartRes)

	subanno[appv1.AnnotationTopo] = tpStr

	k := types.NamespacedName{Name: sub.GetName(), Namespace: sub.GetNamespace()}
	subanno = r.appendAnsiblejobToSubsriptionAnnotation(subanno, k)

	sub.SetAnnotations(subanno)

	klog.V(3).Infof("topo string: %v", tpStr)

	return nil
}

func (r *ReconcileSubscription) processRepo(chn *chnv1.Channel, sub *appv1.Subscription, localRepoRoot, subPath, baseDir string) error {
	chartDirs, kustomizeDirs, crdsAndNamespaceFiles, rbacFiles, otherFiles, err := utils.SortResources(localRepoRoot, subPath)

	if err != nil {
		klog.Error(err, "Failed to sort kubernetes resources and helm charts.")
		return err
	}

	// Build a helm repo index file
	indexFile, err := utils.GenerateHelmIndexFile(sub, localRepoRoot, chartDirs)

	if err != nil {
		// If package name is not specified in the subscription, filterCharts throws an error. In this case, just return the original index file.
		klog.Error(err, "Failed to generate helm index file.")
		return err
	}

	b, _ := yaml.Marshal(indexFile)
	klog.Info("New index file ", string(b))

	// Create deployables for kube resources and helm charts from the git repo
	r.subscribeResources(chn, sub, crdsAndNamespaceFiles, baseDir)
	r.subscribeResources(chn, sub, rbacFiles, baseDir)
	r.subscribeResources(chn, sub, otherFiles, baseDir)
	r.subscribeKustomizations(chn, sub, kustomizeDirs, baseDir)
	err = r.subscribeHelmCharts(chn, sub, indexFile)

	if err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

// clearSubscriptionTargetDpls clear the subscription target deployable if exists.
func (r *ReconcileSubscription) deleteSubscriptionDeployables(sub *appv1.Subscription) {
	klog.Info("Deleting sbscription deploaybles")

	//get deployables that meet the subscription selector
	subDeployables := r.getSubscriptionDeployables(sub)

	// delete subscription deployables if exists.
	for dplName, dpl := range subDeployables {
		klog.Info("deleting deployable: " + dplName)

		err := r.Delete(context.TODO(), dpl)

		if err != nil {
			klog.Errorf("Error in deleting sbuscription target deploayble: %#v, err: %#v ", dpl, err)
		}
	}
}

func (r *ReconcileSubscription) subscribeResources(chn *chnv1.Channel, sub *appv1.Subscription, rscFiles []string, baseDir string) {
	// sync kube resource deployables
	for _, rscFile := range rscFiles {
		file, err := ioutil.ReadFile(rscFile) // #nosec G304 rscFile is not user input

		if err != nil {
			klog.Error(err, "Failed to read YAML file "+rscFile)
			continue
		}

		//skip pre/posthook folder
		dir, _ := filepath.Split(rscFile)

		if strings.HasSuffix(dir, PrehookDirSuffix) || strings.HasSuffix(dir, PosthookDirSuffix) {
			continue
		}

		klog.Info("Processing ... " + rscFile)

		resourceDir := strings.TrimPrefix(dir, baseDir)
		resourceDir = strings.Trim(resourceDir, "/")

		resources := utils.ParseKubeResoures(file)

		if len(resources) > 0 {
			for _, resource := range resources {
				err = r.createDeployable(chn, sub, resourceDir, resource)
				if err != nil {
					klog.Error(err.Error())
				}
			}
		}
	}
}

func (r *ReconcileSubscription) subscribeKustomizations(chn *chnv1.Channel, sub *appv1.Subscription, kustomizeDirs map[string]string, baseDir string) {
	for _, kustomizeDir := range kustomizeDirs {
		klog.Info("Applying kustomization ", kustomizeDir)

		relativePath := kustomizeDir

		if len(strings.SplitAfter(kustomizeDir, baseDir+"/")) > 1 {
			relativePath = strings.SplitAfter(kustomizeDir, baseDir+"/")[1]
		}

		for _, ov := range sub.Spec.PackageOverrides {
			ovKustomizeDir := strings.Split(ov.PackageName, "kustomization")[0]

			//If the full kustomization.yaml path is specified but different than the current kustomize dir, egnore
			if !strings.EqualFold(ovKustomizeDir, relativePath) && !strings.EqualFold(ovKustomizeDir, "") {
				continue
			} else {
				klog.Info("Overriding kustomization ", kustomizeDir)
				pov := ov.PackageOverrides[0] // there is only one override for kustomization.yaml
				err := utils.OverrideKustomize(pov, kustomizeDir)
				if err != nil {
					klog.Error("Failed to override kustomization.")
					break
				}
			}
		}

		out, err := utils.RunKustomizeBuild(kustomizeDir)

		if err != nil {
			klog.Error("Failed to applying kustomization, error: ", err.Error())
		}

		// Split the output of kustomize build output into individual kube resource YAML files
		resources := utils.ParseYAML(out)
		for _, resource := range resources {
			resourceFile := []byte(strings.Trim(resource, "\t \n"))

			t := kubeResource{}
			err := yaml.Unmarshal(resourceFile, &t)

			if err != nil {
				klog.Error(err, "Failed to unmarshal YAML file")
				continue
			}

			if t.APIVersion == "" || t.Kind == "" {
				klog.Info("Not a Kubernetes resource")
			} else {
				err := r.createDeployable(chn, sub, strings.Trim(relativePath, "/"), resourceFile)
				if err != nil {
					klog.Error("Failed to apply a resource, error: ", err)
				}
			}
		}
	}
}

func (r *ReconcileSubscription) createDeployable(
	chn *chnv1.Channel,
	sub *appv1.Subscription,
	dir string,
	filecontent []byte) error {
	obj := &unstructured.Unstructured{}

	if err := yaml.Unmarshal(filecontent, &obj); err != nil {
		klog.Error("Failed to unmarshal resource YAML.")
		return err
	}

	dpl := &dplv1.Deployable{}
	prefix := ""

	if dir != "" {
		prefix = strings.ReplaceAll(dir, "/", "-") + "-"
	}

	dpl.Name = strings.ToLower(sub.Name + "-" + prefix + obj.GetName() + "-" + obj.GetKind())
	klog.Info("Creating a deployable " + dpl.Name)

	if len(dpl.Name) > 252 { // kubernetest resource name length limit
		dpl.Name = dpl.Name[0:251]
	}

	dpl.Namespace = sub.Namespace

	if err := controllerutil.SetControllerReference(sub, dpl, r.scheme); err != nil {
		return errors.Wrap(err, "failed to set controller reference")
	}

	dplanno := make(map[string]string)
	dplanno[chnv1.KeyChannel] = chn.Name
	dplanno[dplv1.AnnotationExternalSource] = dir
	dplanno[dplv1.AnnotationLocal] = "false"
	dplanno[dplv1.AnnotationDeployableVersion] = obj.GetAPIVersion()
	dpl.SetAnnotations(dplanno)

	subscriptionNameLabel := types.NamespacedName{
		Name:      sub.Name,
		Namespace: sub.Namespace,
	}
	subscriptionNameLabelStr := strings.ReplaceAll(subscriptionNameLabel.String(), "/", "-")

	dplLabels := make(map[string]string)
	dplLabels[chnv1.KeyChannel] = chn.Name
	dplLabels[chnv1.KeyChannelType] = string(chn.Spec.Type)
	dplLabels[appv1.LabelSubscriptionName] = subscriptionNameLabelStr
	dpl.SetLabels(dplLabels)

	dpl.Spec.Template = &runtime.RawExtension{}

	var err error
	dpl.Spec.Template.Raw, err = json.Marshal(obj)

	if err != nil {
		klog.Error("failed to marshal resource to template")
		return err
	}

	if err := r.Client.Create(context.TODO(), dpl); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.Info("deployable already exists. Updating it.")

			if err := r.Client.Update(context.TODO(), dpl); err != nil {
				klog.Error("Failed to update deployable.")
				return err
			}

			return nil
		}

		return err
	}

	return nil
}

type helmSpec struct {
	ChartName   string      `json:"chartName,omitempty"`
	ReleaseName string      `json:"releaseName,omitempty"`
	Version     string      `json:"version,omitempty"`
	Source      *helmSource `json:"source,omitempty"`
}

type helmSource struct {
	HelmRepo *sourceURLs `json:"helmRepo,omitempty"`
	Git      *sourceURLs `json:"git,omitempty"`
	Type     string      `json:"type,omitempty"`
}

type sourceURLs struct {
	URLs      []string `json:"urls,omitempty"`
	ChartPath string   `json:"chartPath,omitempty"`
}

func (r *ReconcileSubscription) subscribeHelmCharts(chn *chnv1.Channel, sub *appv1.Subscription, indexFile *repo.IndexFile) (err error) {
	for packageName, chartVersions := range indexFile.Entries {
		klog.Infof("chart: %s\n%v", packageName, chartVersions)

		obj := &unstructured.Unstructured{}
		obj.SetKind("HelmRelease")
		obj.SetAPIVersion("apps.open-cluster-management.io/v1")
		obj.SetName(packageName + "-" + chartVersions[0].Version)

		spec := &helmSpec{}
		spec.ChartName = packageName
		spec.ReleaseName = packageName
		spec.Version = chartVersions[0].Version

		sourceurls := &sourceURLs{}
		sourceurls.URLs = []string{chn.Spec.Pathname}

		src := &helmSource{}

		src.Type = chnv1.ChannelTypeGit
		src.Git = sourceurls
		chartVersion, _ := indexFile.Get(packageName, chartVersions[0].Version)
		src.Git.ChartPath = chartVersion.URLs[0]

		spec.Source = src

		obj.Object["spec"] = spec

		dplSpec, err := json.Marshal(obj)

		if err != nil {
			klog.Error("failed to marshal helmrelease spec")
			continue
		}

		klog.Info("generating deployable")

		err = r.createDeployable(chn, sub, "", dplSpec)

		if err != nil {
			klog.Error("failed to create deployable for helmrelease: " + packageName + "-" + chartVersions[0].Version)
			continue
		}
	}

	return nil
}
