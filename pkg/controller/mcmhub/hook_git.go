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

package mcmhub

import (
	"context"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type GitOps interface {
	//DownloadHookResource downloads the ansible job from the git and marshal
	//the resource to unstructured.Unstructured
	DownloadHookResource(*subv1.Subscription) ([]unstructured.Unstructured, error)
}

type HookGit struct {
	clt          client.Client
	logger       logr.Logger
	lastCommitID map[types.NamespacedName]string
}

var _ GitOps = (*HookGit)(nil)

func NewHookGit(clt client.Client, logger logr.Logger) *HookGit {
	return &HookGit{
		clt:          clt,
		logger:       logger,
		lastCommitID: make(map[types.NamespacedName]string),
	}
}

func (h *HookGit) DownloadHookResource(subIns *subv1.Subscription) ([]unstructured.Unstructured, error) {
	chn := &chnv1.Channel{}
	chnkey := utils.NamespacedNameFormat(subIns.Spec.Channel)
	if err := h.clt.Get(context.TODO(), chnkey, chn); err != nil {
		return []unstructured.Unstructured{}, err
	}

	return h.donwloadAnsibleJobFromGit(h.clt, chn, subIns, h.logger)
}

// get channel
func (h *HookGit) donwloadAnsibleJobFromGit(clt client.Client, chn *chnv1.Channel, sub *subv1.Subscription, logger logr.Logger) ([]unstructured.Unstructured, error) {
	repoRoot := utils.GetLocalGitFolder(chn, sub)
	commitID, err := cloneGitRepo(clt, repoRoot, chn, sub)
	if err != nil {
		return []unstructured.Unstructured{}, err
	}

	h.lastCommitID[types.NamespacedName{Name: sub.GetName(), Namespace: sub.GetNamespace()}] = commitID

	kubeRes, err := sortClonedGitRepo(repoRoot, sub, logger)
	if err != nil {
		return []unstructured.Unstructured{}, err
	}

	return parseAnsibleJobs(kubeRes, parseAnsibleJobResoures)
}

func cloneGitRepo(clt client.Client, repoRoot string, chn *chnv1.Channel, sub *subv1.Subscription) (commitID string, err error) {
	user, pwd, err := utils.GetChannelSecret(clt, chn)

	if err != nil {
		return "", err
	}

	return utils.CloneGitRepo(chn.Spec.Pathname, utils.GetSubscriptionBranch(sub), user, pwd, repoRoot)
}

func sortClonedGitRepo(repoRoot string, sub *subv1.Subscription, logger logr.Logger) ([]string, error) {
	resourcePath := repoRoot

	annotations := sub.GetAnnotations()

	if annotations[appv1.AnnotationGithubPath] != "" {
		resourcePath = filepath.Join(repoRoot, annotations[appv1.AnnotationGithubPath])
	} else if annotations[appv1.AnnotationGitPath] != "" {
		resourcePath = filepath.Join(repoRoot, annotations[appv1.AnnotationGitPath])
	}

	// chartDirs contains helm chart directories
	// crdsAndNamespaceFiles contains CustomResourceDefinition and Namespace Kubernetes resources file paths
	// rbacFiles contains ServiceAccount, ClusterRole and Role Kubernetes resource file paths
	// otherFiles contains all other Kubernetes resource file paths
	_, _, _, _, kubeRes, err := utils.SortResources(repoRoot, resourcePath)
	if err != nil {
		logger.Error(err, "failed to sort kubernetes resources.")
		return []string{}, err
	}

	return kubeRes, nil
}

func parseAnsibleJobResoures(file []byte) [][]byte {
	cond := func(t utils.KubeResource) bool {
		return t.APIVersion == "" || t.Kind == "" || !strings.EqualFold(t.Kind, AnsibleJobKind)
	}

	return utils.KubeResourcePaser(file, cond)
}

func parseAnsibleJobs(rscFiles []string, paser func([]byte) [][]byte) ([]unstructured.Unstructured, error) {
	jobs := []unstructured.Unstructured{}
	// sync kube resource deployables
	for _, rscFile := range rscFiles {
		file, err := ioutil.ReadFile(rscFile) // #nosec G304 rscFile is not user input

		if err != nil {
			return []unstructured.Unstructured{}, err
		}

		resources := paser(file)

		if len(resources) > 0 {
			for _, resource := range resources {
				job := &unstructured.Unstructured{}
				err := yaml.Unmarshal(resource, job)

				if err != nil {
					continue
				}

				jobs = append(jobs, *job)
			}
		}
	}

	return jobs, nil
}
