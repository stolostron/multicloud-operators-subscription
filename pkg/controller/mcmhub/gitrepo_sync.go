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
	"io/ioutil"
	"path/filepath"
	"strings"

	chnv1alpha1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	"gopkg.in/yaml.v2"
	"k8s.io/klog"
)

type kubeResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// UpdateGitDeployablesAnnotation clones the git repo and regenerate deployables and update annotation if needed
func (r *ReconcileSubscription) UpdateGitDeployablesAnnotation(sub *appv1alpha1.Subscription) bool {

	updated := false

	channel, err := r.getChannel(sub)

	if err != nil {
		klog.Errorf("Failed to find a channel for subscription: %s", sub.GetName())
		return false
	}

	if strings.EqualFold(string(channel.Spec.Type), chnv1alpha1.ChannelTypeGitHub) {
		klog.Infof("Subscription %s has GitHub type channel.", sub.GetName())

		user, pwd, err := utils.GetChannelSecret(r.Client, channel)

		if err != nil {
			klog.Errorf("Failed to get secret for channel: %s", channel.GetName())
		}

		commit, err := utils.CloneGitRepo(string(channel.Spec.Pathname),
			utils.GetSubscriptionBranch(sub),
			user,
			pwd,
			utils.GetLocalGitFolder(channel, sub))

		klog.Info("Cloned into " + utils.GetLocalGitFolder(channel, sub))
		klog.Info("Commit ID = " + commit)

		annotations := sub.GetAnnotations()
		if !strings.EqualFold(annotations[appv1alpha1.AnnotationGithubCommit], commit) {
			klog.Info("Repo commit = " + commit)
			klog.Info("Subscription commit = " + annotations[appv1alpha1.AnnotationGithubCommit])
			klog.Info("The repo has changed. Update deployables.")
			annotations[appv1alpha1.AnnotationGithubCommit] = commit
			sub.SetAnnotations(annotations)

			resourcePath := utils.GetLocalGitFolder(channel, sub)

			if annotations[appv1alpha1.AnnotationGithubPath] != "" {
				resourcePath = filepath.Join(utils.GetLocalGitFolder(channel, sub), annotations[appv1alpha1.AnnotationGithubPath])
			}

			r.createDeployables(sub, utils.GetLocalGitFolder(channel, sub), resourcePath)

			updated = true
		}

	}

	return updated
}

func (r *ReconcileSubscription) createDeployables(sub *appv1alpha1.Subscription, localRepoRoot, subPath string) error {

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

	r.subscribeResources(crdsAndNamespaceFiles)
	r.subscribeResources(rbacFiles)
	r.subscribeResources(otherFiles)
	r.subscribeKustomizations(kustomizeDirs)

	return nil
}

func (r *ReconcileSubscription) subscribeResources(rscFiles []string) {
	// sync kube resource deployables
	for _, rscFile := range rscFiles {
		klog.Info("Processing ... " + rscFile)
		file, err := ioutil.ReadFile(rscFile) // #nosec G304 rscFile is not user input

		if err != nil {
			klog.Error(err, "Failed to read YAML file "+rscFile)
			continue
		}

		resources := utils.ParseKubeResoures(file)

		if len(resources) > 0 {
			for _, resource := range resources {
				t := kubeResource{}
				err := yaml.Unmarshal(resource, &t)

				if err != nil {
					// Ignore if it does not have apiVersion or kind fields in the YAML
					continue
				}

				klog.Info("Creating a deployable of kind ", t.Kind)

				//err = ghsi.subscribeResourceFile(hostkey, syncsource, kvalid, resource, pkgMap)
				//if err != nil {
				//		klog.Error("Failed to apply a resource, error: ", err)
				//}
			}
		}
	}
}

func (r *ReconcileSubscription) subscribeKustomizations(kustomizeDirs map[string]string) {
	for _, kustomizeDir := range kustomizeDirs {
		klog.Info("Applying kustomization ", kustomizeDir)
	}
}
