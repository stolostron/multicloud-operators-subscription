// Copyright 220The Kubernetes Authors.
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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/kustomize"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/kustomize/pkg/fs"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
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
func (r *ReconcileSubscription) UpdateGitDeployablesAnnotation(sub *appv1.Subscription) bool {
	updated := false

	channel, err := r.getChannel(sub)

	if err != nil {
		klog.Errorf("Failed to find a channel for subscription: %s", sub.GetName())
		return false
	}

	if strings.EqualFold(string(channel.Spec.Type), chnv1.ChannelTypeGitHub) {
		klog.Infof("Subscription %s has GitHub type channel.", sub.GetName())

		user, pwd, err := utils.GetChannelSecret(r.Client, channel)

		if err != nil {
			klog.Errorf("Failed to get secret for channel: %s", channel.GetName())
		}

		commit, err := utils.CloneGitRepo(channel.Spec.Pathname,
			utils.GetSubscriptionBranch(sub),
			user,
			pwd,
			utils.GetLocalGitFolder(channel, sub))

		if err != nil {
			klog.Error(err.Error())
			return false
		}

		annotations := sub.GetAnnotations()
		if !strings.EqualFold(annotations[appv1.AnnotationGithubCommit], commit) {
			klog.Info("Repo commit = " + commit)
			klog.Info("Subscription commit = " + annotations[appv1.AnnotationGithubCommit])
			klog.Info("The repo has changed. Update deployables.")

			// Delete the existing deployables and recreate them
			r.deleteSubscriptionDeployables(sub)

			annotations[appv1.AnnotationGithubCommit] = commit
			sub.SetAnnotations(annotations)

			resourcePath := utils.GetLocalGitFolder(channel, sub)
			baseDir := resourcePath

			if annotations[appv1.AnnotationGithubPath] != "" {
				resourcePath = filepath.Join(utils.GetLocalGitFolder(channel, sub), annotations[appv1.AnnotationGithubPath])
			}

			err = r.processRepo(channel, sub, utils.GetLocalGitFolder(channel, sub), resourcePath, baseDir)

			if err != nil {
				klog.Error(err.Error())
				return false
			}

			r.updateGitSubDeployablesAnnotation(sub)

			updated = true
		}

		if annotations[appv1alpha1.AnnotationDeployables] == "" {
			// this might have failed previously. Try again.
			r.updateGitSubDeployablesAnnotation(sub)

			updated = true
		}
	}

	return updated
}

// updateGitSubDeployablesAnnotation set all deployables subscribed by github subscription to the apps.open-cluster-management.io/deployables annotation
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

	subanno[appv1alpha1.AnnotationDeployables] = dplstr
	sub.SetAnnotations(subanno)
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

	// Create deployables for kube resources and helm charts from the github repo
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
		klog.Info("Processing ... " + rscFile)
		file, err := ioutil.ReadFile(rscFile) // #nosec G304 rscFile is not user input

		if err != nil {
			klog.Error(err, "Failed to read YAML file "+rscFile)
			continue
		}

		dir, _ := filepath.Split(rscFile)
		fmt.Println("Dir: ", dir)

		resourceDir := strings.Split(dir, baseDir)[1]
		resourceDir = strings.Trim(resourceDir, "/")
		fmt.Println("resourceDir: ", resourceDir)

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
			if !strings.EqualFold(ovKustomizeDir, relativePath) {
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

		fSys := fs.MakeRealFS()

		var out bytes.Buffer

		err := kustomize.RunKustomizeBuild(&out, fSys, kustomizeDir)
		if err != nil {
			klog.Error("Failed to applying kustomization, error: ", err.Error())
		}
		// Split the output of kustomize build output into individual kube resource YAML files
		resources := strings.Split(out.String(), "---")
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

	klog.Info(string(filecontent))

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
	GitHub   *sourceURLs `json:"github,omitempty"`
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

		src.Type = chnv1.ChannelTypeGitHub
		src.GitHub = sourceurls
		chartVersion, _ := indexFile.Get(packageName, chartVersions[0].Version)
		src.GitHub.ChartPath = chartVersion.URLs[0]

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
