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
	"sync"
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
	hookInterval = time.Minute * 3
)

type GitOps interface {
	//DownloadAnsibleHookResource downloads the ansible job from the git and marshal
	//the resource to
	DownloadAnsibleHookResource(*subv1.Subscription) error

	// GetHooks returns the ansiblejob from a given folder, if the folder is
	// inaccessible, then os.Error is returned
	GetHooks(*subv1.Subscription, string) ([]ansiblejob.AnsibleJob, error)

	// RegisterBranch
	RegisterBranch(*subv1.Subscription)

	// DeregisterBranch
	DeregisterBranch(*subv1.Subscription)

	//Runnable
	Start(<-chan struct{}) error
}

type branchInfo struct {
	lastCommitID  string
	username      string
	secret        string
	registeredSub map[types.NamespacedName]struct{}
}

type repoRegistery struct {
	url     string
	branchs map[string]branchInfo
}

type HubGitOps struct {
	clt         client.Client
	logger      logr.Logger
	localDir    string
	mtx         sync.Mutex
	subRecords  map[types.NamespacedName]string
	repoRecords map[string]repoRegistery
}

var _ GitOps = (*HubGitOps)(nil)

func NewHookGit(clt client.Client, logger logr.Logger) *HubGitOps {
	return &HubGitOps{
		clt:         clt,
		logger:      logger,
		mtx:         sync.Mutex{},
		subRecords:  map[types.NamespacedName]string{},
		repoRecords: map[string]repoRegistery{},
	}
}

// the git watch will go to each subscription download the repo and compare the
// commit id, it's the commit id is different, then update the commit id to
// subscription
func (h *HubGitOps) Start(stop <-chan struct{}) error {
	h.logger.Info("entry StartGitWatch")
	defer h.logger.Info("exit StartGitWatch")

	go wait.Until(h.GitWatch, hookInterval, stop)

	return nil
}

func (h *HubGitOps) GitWatch() {
	h.logger.V(DebugLog).Info("entry GitWatch")
	defer h.logger.V(DebugLog).Info("exit GitWatch")

	h.mtx.Lock()
	defer h.mtx.Unlock()

	for _, repoRegistery := range h.repoRecords {
		url := repoRegistery.url
		// need to figure out a way to separate the private repo
		for bName, branchInfo := range repoRegistery.branchs {
			nCommit, err := GetLatestRemoteGitCommitID(url, bName, branchInfo.username, branchInfo.secret)
			if err != nil {
				h.logger.Error(err, "failed to get the latest commit id")
			}

			if nCommit == branchInfo.lastCommitID {
				continue
			}

			for subKey := range branchInfo.registeredSub {
				if err := updateCommitAnnotation(h.clt, subKey, nCommit); err != nil {
					h.logger.Error(err, fmt.Sprintf("failed to update newcommit %s to subscrption %s", nCommit, subKey.String()))
					continue
				}

				h.logger.Info(fmt.Sprintf("updated the commit annotation of subscrption %s", subKey))
			}
		}
	}
}

func updateCommitAnnotation(clt client.Client, subKey types.NamespacedName, newCommit string) error {
	subIns := &subv1.Subscription{}
	ctx := context.TODO()

	if err := clt.Get(ctx, subKey, subIns); err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	anno := subIns.GetAnnotations()
	anno[subv1.AnnotationGitCommit] = newCommit
	subIns.SetAnnotations(anno)

	return clt.Update(ctx, subIns)
}

func isGitChannel(ch *chnv1.Channel) bool {
	cType := string(ch.Spec.Type)

	if strings.EqualFold(cType, chnv1.ChannelTypeGit) || strings.EqualFold(cType, chnv1.ChannelTypeGitHub) {
		return true
	}

	return false
}

func genBranchString(subIns *subv1.Subscription) string {
	branch := ""
	an := subIns.GetAnnotations()

	if an[subv1.AnnotationGithubBranch] != "" {
		branch = an[subv1.AnnotationGithubBranch]
	}

	if an[subv1.AnnotationGitBranch] != "" {
		branch = an[subv1.AnnotationGitBranch]
	}

	if branch == "" {
		branch = "master"
	}

	return branch
}

func genRepoName(repoURL, user, pwd string) string {
	repoName := repoURL
	if pwd != "" {
		repoName += user
	}

	return repoName
}

func (h *HubGitOps) RegisterBranch(subIns *subv1.Subscription) {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}
	if _, ok := h.subRecords[subKey]; ok {
		return
	}

	channel, err := GetSubscriptionRefChannel(h.clt, subIns)

	if err != nil {
		h.logger.Error(err, "failed to register subscription to GitOps")
		return
	}

	if !isGitChannel(channel) {
		return
	}

	user, pwd, err := utils.GetChannelSecret(h.clt, channel)

	if err != nil {
		h.logger.Error(err, "failed to register subscription to git watcher register")
		return
	}

	repoURL := channel.Spec.Pathname
	repoName := genRepoName(repoURL, user, pwd)
	branch := genBranchString(subIns)

	h.mtx.Lock()
	defer h.mtx.Unlock()

	h.subRecords[subKey] = repoName
	bInfo, ok := h.repoRecords[repoName]

	if !ok {
		h.repoRecords[repoName] = repoRegistery{
			url: repoURL,
			branchs: map[string]branchInfo{
				branch: {
					username: user,
					secret:   pwd,
					registeredSub: map[types.NamespacedName]struct{}{
						subKey: {},
					},
				},
			},
		}

		return
	}

	bInfo.branchs[repoName].registeredSub[subKey] = struct{}{}
}

func (h *HubGitOps) DeregisterBranch(subIns *subv1.Subscription) {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}
	_, ok := h.subRecords[subKey]

	if !ok {
		return
	}

	channel, err := GetSubscriptionRefChannel(h.clt, subIns)

	if err != nil {
		h.logger.Error(err, "failed to register subscription to GitOps")
		return
	}

	if !isGitChannel(channel) {
		return
	}

	user, pwd, err := utils.GetChannelSecret(h.clt, channel)

	if err != nil {
		h.logger.Error(err, "failed to register subscription to GitOps")
		return
	}

	repoName := genRepoName(channel.Spec.Pathname, user, pwd)

	h.mtx.Lock()
	defer h.mtx.Unlock()

	delete(h.repoRecords[repoName].branchs[repoName].registeredSub, subKey)
}

func GetLatestRemoteGitCommitID(repo, branch, secret, pwd string) (string, error) {
	tp := github.BasicAuthTransport{
		Username: strings.TrimSpace(secret),
		Password: strings.TrimSpace(pwd),
	}

	return utils.GetLatestCommitID(repo, branch, github.NewClient(tp.Client()))
}

func (h *HubGitOps) DownloadAnsibleHookResource(subIns *subv1.Subscription) error {
	chn := &chnv1.Channel{}
	chnkey := utils.NamespacedNameFormat(subIns.Spec.Channel)

	if err := h.clt.Get(context.TODO(), chnkey, chn); err != nil {
		return err
	}

	return h.donwloadAnsibleJobFromGit(h.clt, chn, subIns, h.logger)
}

func (h *HubGitOps) donwloadAnsibleJobFromGit(clt client.Client, chn *chnv1.Channel, sub *subv1.Subscription, logger logr.Logger) error {
	logger.V(DebugLog).Info("entry donwloadAnsibleJobFromGit")
	defer logger.V(DebugLog).Info("exit donwloadAnsibleJobFromGit")

	repoRoot := utils.GetLocalGitFolder(chn, sub)
	h.localDir = repoRoot

	_, err := cloneGitRepo(clt, repoRoot, chn, sub)

	if err != nil {
		return err
	}

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
func (h *HubGitOps) GetHooks(subIns *subv1.Subscription, hookPath string) ([]ansiblejob.AnsibleJob, error) {
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
