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
	"time"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	"github.com/google/go-github/v32/github"
	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	hookInterval = time.Minute * 1
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

// the git watch will go to each subscription download the repo and compare the
// commit id, it's the commit id is different, then update the commit id to
// subscription
func (a *AnsibleHooks) Start(stop <-chan struct{}) error {
	a.logger.Info("entry StartGitWatch")
	defer a.logger.Info("exit StartGitWatch")

	go wait.Until(a.GitWatch, hookInterval, stop)

	return nil
}

func (a *AnsibleHooks) GitWatch() {
	a.logger.V(DebugLog).Info("entry GitWatch")
	defer a.logger.V(DebugLog).Info("exit GitWatch")

	ctx := context.TODO()

	a.mtx.Lock()
	defer a.mtx.Unlock()

	for subKey := range a.registry {
		subIns := &subv1.Subscription{}
		if err := a.clt.Get(ctx, subKey, subIns); err != nil {
			if !k8serrors.IsNotFound(err) {
				a.logger.Error(err, "failed to get ")
			}

			continue
		}

		if newCommit, ok := a.IsGitUpdate(subIns); ok {
			anno := subIns.GetAnnotations()
			anno[subv1.AnnotationGitCommit] = newCommit
			subIns.SetAnnotations(anno)

			if err := a.clt.Update(ctx, subIns); err != nil {
				a.logger.Error(err, "failed to update subscription from GitWatch")
			}

			a.logger.Info(fmt.Sprintf("updated the commit annotation of subscrption %s", subKey))
		}
	}
}

func (a *AnsibleHooks) IsGitUpdate(nSubIns *subv1.Subscription) (string, bool) {
	subKey := types.NamespacedName{Name: nSubIns.GetName(), Namespace: nSubIns.GetNamespace()}
	oldCommit := nSubIns.GetAnnotations()[subv1.AnnotationGitCommit]
	newCommit, err := GetLatestRemoteGitCommitID(a.clt, nSubIns)

	if err != nil {
		a.logger.Error(err, fmt.Sprintf("failed to get the new commit id for subscription %s", subKey))
		return "", false
	}

	if !strings.EqualFold(oldCommit, newCommit) {
		return newCommit, true
	}

	return "", false
}

func GetLatestRemoteGitCommitID(clt client.Client, subIns *subv1.Subscription) (string, error) {
	channel, err := GetSubscriptionRefChannel(clt, subIns)

	if err != nil {
		return "", err
	}

	user, pwd, err := utils.GetChannelSecret(clt, channel)

	if err != nil {
		return "", err
	}

	an := subIns.GetAnnotations()

	branch := ""
	if an[subv1.AnnotationGithubBranch] != "" {
		branch = an[subv1.AnnotationGithubBranch]
	}

	if an[subv1.AnnotationGitBranch] != "" {
		branch = an[subv1.AnnotationGitBranch]
	}

	if branch == "" {
		branch = "master"
	}

	tp := github.BasicAuthTransport{
		Username: strings.TrimSpace(user),
		Password: strings.TrimSpace(pwd),
	}

	return utils.GetLatestCommitID(channel.Spec.Pathname, branch, github.NewClient(tp.Client()))
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

type gitSortResult struct {
	kustomized [][]byte
	kubRes     []string
}

//repoRoot is the local path of the git
// destPath will specify a sub-directory for finding all the relative resource
func sortClonedGitRepoGievnDestPath(repoRoot string, destPath string, logger logr.Logger) (gitSortResult, error) {
	resourcePath := filepath.Join(repoRoot, destPath)

	sortWrapper := func() (gitSortResult, error) {
		_, kustomizeDirs, _, _, kubeRes, err := utils.SortResources(repoRoot, resourcePath)
		if len(kustomizeDirs) != 0 {
			out := [][]byte{}

			for _, dir := range kustomizeDirs {
				//this will return an []byte
				r, err := utils.RunKustomizeBuild(dir)
				if err != nil {
					return gitSortResult{}, err
				}

				out = append(out, r)
			}

			return gitSortResult{kustomized: out}, nil
		}

		return gitSortResult{kubRes: kubeRes}, err
	}

	kubeRes, err := sortWrapper()

	if err != nil {
		logger.Error(err, "failed to sort kubernetes resources.")
		return gitSortResult{}, err
	}

	return kubeRes, nil
}

func parseAnsibleJobResoures(file []byte) [][]byte {
	cond := func(t utils.KubeResource) bool {
		return t.APIVersion == "" || t.Kind == "" || !strings.EqualFold(t.Kind, AnsibleJobKind)
	}

	return utils.KubeResourceParser(file, cond)
}

func parseFromKutomizedAsAnsibleJobs(kustomizes [][]byte, parser func([]byte) [][]byte, logger logr.Logger) ([]ansiblejob.AnsibleJob, error) {
	jobs := []ansiblejob.AnsibleJob{}
	// sync kube resource deployables
	for _, kus := range kustomizes {
		resources := parser(kus)

		if len(resources) > 0 {
			for _, resource := range resources {
				job := &ansiblejob.AnsibleJob{}
				err := yaml.Unmarshal(resource, job)

				if err != nil {
					logger.Error(err, "failed to parse a resource")
					continue
				}

				jobs = append(jobs, *job)
			}
		}
	}

	return jobs, nil
}

func parseAsAnsibleJobs(rscFiles []string, parser func([]byte) [][]byte, logger logr.Logger) ([]ansiblejob.AnsibleJob, error) {
	jobs := []ansiblejob.AnsibleJob{}
	// sync kube resource deployables
	for _, rscFile := range rscFiles {
		file, err := ioutil.ReadFile(rscFile) // #nosec G304 rscFile is not user input

		if err != nil {
			return []ansiblejob.AnsibleJob{}, err
		}

		resources := parser(file)

		if len(resources) > 0 {
			for _, resource := range resources {
				job := &ansiblejob.AnsibleJob{}
				err := yaml.Unmarshal(resource, job)

				if err != nil {
					logger.Error(err, "failed to parse a resource")
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
		if os.IsNotExist(err) {
			return []ansiblejob.AnsibleJob{}, nil
		}

		h.logger.Error(err, "fail to access the hook path")

		return []ansiblejob.AnsibleJob{}, err
	}

	sortedRes, err := sortClonedGitRepoGievnDestPath(h.localDir, hookPath, h.logger)
	if err != nil {
		return []ansiblejob.AnsibleJob{}, err
	}

	if len(sortedRes.kustomized) != 0 {
		return parseFromKutomizedAsAnsibleJobs(sortedRes.kustomized, parseAnsibleJobResoures, h.logger)
	}

	return parseAsAnsibleJobs(sortedRes.kubRes, parseAnsibleJobResoures, h.logger)
}
