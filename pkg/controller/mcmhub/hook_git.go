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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type GitOps interface {
	//DownloadAnsibleHookResource downloads the ansible job from the git and marshal
	//the resource to
	DownloadAnsibleHookResource(*subv1.Subscription) error

	// GetHooks returns the ansiblejob from a given folder, if the folder is
	// inaccessible, then os.Error is returned
	GetHooks(*subv1.Subscription, string) ([]ansiblejob.AnsibleJob, error)
}

type HookGit struct {
	clt          client.Client
	logger       logr.Logger
	lastCommitID map[types.NamespacedName]string
	localDir     string
}

var _ GitOps = (*HookGit)(nil)

func NewHookGit(clt client.Client, logger logr.Logger) *HookGit {
	return &HookGit{
		clt:          clt,
		logger:       logger,
		lastCommitID: make(map[types.NamespacedName]string),
	}
}

func (h *HookGit) DownloadAnsibleHookResource(subIns *subv1.Subscription) error {
	chn := &chnv1.Channel{}
	chnkey := utils.NamespacedNameFormat(subIns.Spec.Channel)

	if err := h.clt.Get(context.TODO(), chnkey, chn); err != nil {
		return err
	}

	return h.donwloadAnsibleJobFromGit(h.clt, chn, subIns, h.logger)
}

func (h *HookGit) donwloadAnsibleJobFromGit(clt client.Client, chn *chnv1.Channel, sub *subv1.Subscription, logger logr.Logger) error {
	logger.V(DebugLog).Info("entry donwloadAnsibleJobFromGit")
	defer logger.V(DebugLog).Info("exit donwloadAnsibleJobFromGit")

	subKey := types.NamespacedName{Name: sub.GetName(), Namespace: sub.GetNamespace()}
	repoRoot := utils.GetLocalGitFolder(chn, sub)
	h.localDir = repoRoot

	if _, ok := h.lastCommitID[subKey]; ok {
		logger.Info("repo already exist")
		return nil
	}

	commitID, err := cloneGitRepo(clt, repoRoot, chn, sub)

	if err != nil {
		return err
	}

	h.lastCommitID[subKey] = commitID

	return nil
}

func cloneGitRepo(clt client.Client, repoRoot string, chn *chnv1.Channel, sub *subv1.Subscription) (commitID string, err error) {
	user, pwd, err := utils.GetChannelSecret(clt, chn)

	if err != nil {
		return "", err
	}

	return utils.CloneGitRepo(chn.Spec.Pathname, utils.GetSubscriptionBranch(sub), user, pwd, repoRoot)
}

//repoRoot is the local path of the git
// destPath will specify a sub-directory for finding all the relative resource
func sortClonedGitRepoGievnDestPath(repoRoot string, destPath string, logger logr.Logger) ([]string, error) {
	resourcePath := filepath.Join(repoRoot, destPath)

	sortWrapper := func() ([]string, error) {
		_, _, _, _, kubeRes, err := utils.SortResources(repoRoot, resourcePath)

		return kubeRes, err
	}

	kubeRes, err := sortWrapper()

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

	return utils.KubeResourceParser(file, cond)
}

func parseAsAnsibleJobs(rscFiles []string, paser func([]byte) [][]byte) ([]ansiblejob.AnsibleJob, error) {
	jobs := []ansiblejob.AnsibleJob{}
	// sync kube resource deployables
	for _, rscFile := range rscFiles {
		file, err := ioutil.ReadFile(rscFile) // #nosec G304 rscFile is not user input

		if err != nil {
			return []ansiblejob.AnsibleJob{}, err
		}

		resources := paser(file)

		if len(resources) > 0 {
			for _, resource := range resources {
				job := &ansiblejob.AnsibleJob{}
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

//GetHooks will provided the ansibleJobs at the given hookPath(if given a
//posthook path, then posthook ansiblejob is returned)
func (h *HookGit) GetHooks(subIns *subv1.Subscription, hookPath string) ([]ansiblejob.AnsibleJob, error) {
	fullPath := fmt.Sprintf("%v/%v", h.localDir, hookPath)
	if _, err := os.Stat(fullPath); err != nil {
		h.logger.Error(err, "fail to access the hook path")
		return []ansiblejob.AnsibleJob{}, err
	}

	kubeRes, err := sortClonedGitRepoGievnDestPath(h.localDir, hookPath, h.logger)
	if err != nil {
		return []ansiblejob.AnsibleJob{}, err
	}

	return parseAsAnsibleJobs(kubeRes, parseAnsibleJobResoures)
}
