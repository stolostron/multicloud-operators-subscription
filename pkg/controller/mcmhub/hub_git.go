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
	hookInterval   = time.Second * 90
	commitIDSuffix = "-new"
)

type GitOps interface {
	//DownloadAnsibleHookResource downloads the ansible job from the git and marshal
	//the resource to
	DownloadAnsibleHookResource(*subv1.Subscription) error

	HasHookFolders(*subv1.Subscription) bool

	// GetHooks returns the ansiblejob from a given folder, if the folder is
	// inaccessible, then os.Error is returned
	GetHooks(*subv1.Subscription, string) ([]ansiblejob.AnsibleJob, error)

	// RegisterBranch to git watcher and do a initial download for other
	// components to consume
	RegisterBranch(*subv1.Subscription)

	// DeregisterBranch
	DeregisterBranch(types.NamespacedName)

	//helper for test
	GetRepoRecords() map[string]*RepoRegistery
	GetSubRecords() map[types.NamespacedName]string

	//GetLatestCommitID will output the latest commit id from local git record
	GetLatestCommitID(*subv1.Subscription) (string, error)
	//ResolveLocalGitFolder is used to open a local folder for downloading the
	//repo branch
	ResolveLocalGitFolder(*chnv1.Channel, *subv1.Subscription) string

	GetRepoRootDirctory(*subv1.Subscription) string

	//Runnable
	Start(<-chan struct{}) error
}

type branchInfo struct {
	localDir           string
	lastCommitID       string
	username           string
	secret             string
	insecureSkipVerify bool
	registeredSub      map[types.NamespacedName]struct{}
}

type RepoRegistery struct {
	url     string
	branchs map[string]*branchInfo
}

type GetCommitFunc func(url string, branchName string, user string, secret string) (string, error)

type cloneFunc func(url string, branchName string, user string, secret string,
	localDir string, insecureSkipVerify bool) (string, error)

type dirResolver func(*chnv1.Channel, *subv1.Subscription) string

type HubGitOps struct {
	clt                 client.Client
	logger              logr.Logger
	mtx                 sync.Mutex
	watcherInterval     time.Duration
	subRecords          map[types.NamespacedName]string
	repoRecords         map[string]*RepoRegistery
	downloadDirResolver dirResolver
	getCommitFunc       GetCommitFunc
	cloneFunc           cloneFunc
}

var _ GitOps = (*HubGitOps)(nil)

type HubGitOption func(*HubGitOps)

func setHubGitOpsInterval(interval time.Duration) HubGitOption {
	return func(a *HubGitOps) {
		a.watcherInterval = interval
	}
}

func setHubGitOpsLogger(logger logr.Logger) HubGitOption {
	return func(a *HubGitOps) {
		a.logger = logger
	}
}

func setLocalDirResovler(resolveFunc func(*chnv1.Channel, *subv1.Subscription) string) HubGitOption {
	return func(a *HubGitOps) {
		a.downloadDirResolver = resolveFunc
	}
}

func setGetCommitFunc(gFunc GetCommitFunc) HubGitOption {
	return func(a *HubGitOps) {
		a.getCommitFunc = gFunc
	}
}

func setGetCloneFunc(cFunc cloneFunc) HubGitOption {
	return func(a *HubGitOps) {
		a.cloneFunc = cFunc
	}
}

func NewHookGit(clt client.Client, ops ...HubGitOption) *HubGitOps {
	hGit := &HubGitOps{
		clt:                 clt,
		mtx:                 sync.Mutex{},
		watcherInterval:     hookInterval,
		subRecords:          map[types.NamespacedName]string{},
		repoRecords:         map[string]*RepoRegistery{},
		downloadDirResolver: utils.GetLocalGitFolder,
		getCommitFunc:       GetLatestRemoteGitCommitID,
		cloneFunc:           cloneGitRepoBranch,
	}

	for _, op := range ops {
		op(hGit)
	}

	return hGit
}

// the git watch will go to each subscription download the repo and compare the
// commit id, it's the commit id is different, then update the commit id to
// subscription
func (h *HubGitOps) Start(stop <-chan struct{}) error {
	h.logger.Info("entry StartGitWatch")
	defer h.logger.Info("exit StartGitWatch")

	go wait.Until(h.GitWatch, h.watcherInterval, stop)

	return nil
}

func (h *HubGitOps) GitWatch() {
	h.logger.V(DebugLog).Info("entry GitWatch")
	defer h.logger.V(DebugLog).Info("exit GitWatch")

	h.mtx.Lock()
	defer h.mtx.Unlock()

	for repoName, repoRegistery := range h.repoRecords {
		url := repoRegistery.url
		// need to figure out a way to separate the private repo
		for bName, branchInfo := range repoRegistery.branchs {
			h.logger.Info(fmt.Sprintf("Checking commit for Git: %s Branch: %s", url, bName))
			nCommit, err := h.getCommitFunc(url, bName, branchInfo.username, branchInfo.secret)

			if err != nil {
				h.logger.Error(err, "failed to get the latest commit id via API, will try to get the commit ID by clone")

				nCommit, err = h.cloneFunc(url, bName, branchInfo.username, branchInfo.secret, branchInfo.localDir, branchInfo.insecureSkipVerify)
				if err != nil {
					h.logger.Error(err, "failed to get the latest commit id by clone the repo")
				}
			}

			// safe guard condition to filter out the edge case
			if nCommit == "" {
				h.logger.Info(fmt.Sprintf("repo %s, branch %s get empty commit ID from remote", url, bName))
				continue
			}

			h.logger.V(InfoLog).Info(fmt.Sprintf("repo %s, branch %s commit update from (%s) to (%s)", url, bName, branchInfo.lastCommitID, nCommit))

			if nCommit == branchInfo.lastCommitID {
				h.logger.Info("The repo commit hasn't changed.")
				continue
			}

			h.repoRecords[repoName].branchs[bName].lastCommitID = nCommit
			h.logger.Info("The repo has new commit: " + nCommit)

			if _, err := h.cloneFunc(url, bName, branchInfo.username, branchInfo.secret, branchInfo.localDir, branchInfo.insecureSkipVerify); err != nil {
				h.logger.Error(err, "failed to download repo for %s, at brnach @%s", repoName, bName)
			}

			for subKey := range branchInfo.registeredSub {
				// Update the commit annotation with a wrong commit ID to trigger hub subscription reconcile.
				// The hub subscription reconcile will compare this to the commit ID in the map h.repoRecords[repoName].branchs[bName].lastCommitID
				// to determine it needs to regenerate deployables.
				if err := updateCommitAnnotation(h.clt, subKey, fakeCommitID(nCommit)); err != nil {
					h.logger.Error(err, fmt.Sprintf("failed to update new commit %s to subscription %s", nCommit, subKey.String()))
					continue
				}

				h.logger.Info(fmt.Sprintf("updated the commit annotation of subscription %s", subKey))
			}
		}
	}
}

//helper for test
func (h *HubGitOps) GetSubRecords() map[types.NamespacedName]string {
	return h.subRecords
}

//helper for test
func (h *HubGitOps) GetRepoRecords() map[string]*RepoRegistery {
	return h.repoRecords
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
	an := subIns.GetAnnotations()
	if len(an) == 0 {
		return ""
	}

	if an[subv1.AnnotationGitBranch] != "" {
		return an[subv1.AnnotationGitBranch]
	}

	if an[subv1.AnnotationGithubBranch] != "" {
		return an[subv1.AnnotationGithubBranch]
	}

	return "master"
}

func setBranch(subIns *subv1.Subscription, bName string) {
	an := subIns.GetAnnotations()
	if len(an) == 0 {
		an = map[string]string{}
	}

	an[subv1.AnnotationGitBranch] = bName

	subIns.SetAnnotations(an)
}

func genRepoName(subName, subNamespace string) string {
	repoName := subName + subNamespace

	return repoName
}

func (h *HubGitOps) ResolveLocalGitFolder(chn *chnv1.Channel, subIns *subv1.Subscription) string {
	return h.downloadDirResolver(chn, subIns)
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

	skipCertVerify := false

	if channel.Spec.InsecureSkipVerify {
		skipCertVerify = true

		h.logger.Info("Channel spec has insecureSkipVerify: true.")
	}

	repoURL := channel.Spec.Pathname
	// repoName is the key of a map that stores repository information for subscription.
	// It needs to be unique for each subscription because each subscription need to
	// have its own copy of cloned repo to work on subscription specific overrides.
	repoName := genRepoName(subIns.Name, subIns.Namespace)
	branchName := genBranchString(subIns)
	repoBranchDir := h.downloadDirResolver(channel, subIns)

	h.mtx.Lock()
	defer h.mtx.Unlock()

	h.subRecords[subKey] = repoName
	bInfo, ok := h.repoRecords[repoName]

	commitID, err := h.cloneFunc(repoURL, branchName, user, pwd, repoBranchDir, skipCertVerify)
	if err != nil {
		h.logger.Error(err, "failed to get commitID from initialDownload")
	}

	//make sure the initial prehook is passed
	if !ok {
		h.repoRecords[repoName] = &RepoRegistery{
			url: repoURL,
			branchs: map[string]*branchInfo{
				branchName: {
					localDir:           repoBranchDir,
					username:           user,
					secret:             pwd,
					insecureSkipVerify: skipCertVerify,
					lastCommitID:       commitID,
					registeredSub: map[types.NamespacedName]struct{}{
						subKey: {},
					},
				},
			},
		}

		return
	}

	if bInfo.branchs[branchName] == nil {
		bInfo.branchs[branchName] = &branchInfo{
			username:           user,
			secret:             pwd,
			insecureSkipVerify: skipCertVerify,
			localDir:           repoBranchDir,
			lastCommitID:       commitID,
			registeredSub: map[types.NamespacedName]struct{}{
				subKey: {},
			},
		}

		return
	}

	bInfo.branchs[branchName].registeredSub[subKey] = struct{}{}
}

func fakeCommitID(c string) string {
	return fmt.Sprintf("%s%s", c, commitIDSuffix)
}

func unmaskFakeCommitID(c string) string {
	return strings.TrimSuffix(c, commitIDSuffix)
}

func unmaskFakeCommiIDOnSubIns(subIns *subv1.Subscription) *subv1.Subscription {
	out := subIns.DeepCopy()

	umaskCommitID := unmaskFakeCommitID(getCommitID(out))

	setCommitID(out, umaskCommitID)

	return out
}

func setCommitID(subIns *subv1.Subscription, commitID string) {
	aAno := subIns.GetAnnotations()
	if len(aAno) == 0 {
		aAno = map[string]string{}
	}

	aAno[subv1.AnnotationGitCommit] = commitID

	subIns.SetAnnotations(aAno)
}

func (h *HubGitOps) DeregisterBranch(subKey types.NamespacedName) {
	_, ok := h.subRecords[subKey]

	if !ok {
		return
	}

	h.mtx.Lock()
	defer h.mtx.Unlock()

	repoName := h.subRecords[subKey]
	delete(h.subRecords, subKey)

	for bName := range h.repoRecords[repoName].branchs {
		delete(h.repoRecords[repoName].branchs[bName].registeredSub, subKey)

		if len(h.repoRecords[repoName].branchs[bName].registeredSub) == 0 {
			delete(h.repoRecords[repoName].branchs, bName)
		}

		if len(h.repoRecords[repoName].branchs) == 0 {
			delete(h.repoRecords, repoName)
		}
	}
}

func GetLatestRemoteGitCommitID(repo, branch, user, pwd string) (string, error) {
	tp := github.BasicAuthTransport{
		Username: strings.TrimSpace(user),
		Password: strings.TrimSpace(pwd),
	}

	return utils.GetLatestCommitID(repo, branch, github.NewClient(tp.Client()))
}

func (h *HubGitOps) GetLatestCommitID(subIns *subv1.Subscription) (string, error) {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

	_, ok := h.subRecords[subKey]
	if !ok { // when git watcher doesn't have the record, go ahead clone the repo and return the commitID
		h.RegisterBranch(subIns)
	}

	if len(h.repoRecords) == 0 {
		return "", fmt.Errorf("failed to register the branch")
	}

	repoName := h.subRecords[subKey]

	return h.repoRecords[repoName].branchs[genBranchString(subIns)].lastCommitID, nil
}

func (h *HubGitOps) GetRepoRootDirctory(subIns *subv1.Subscription) string {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

	repoKey, ok := h.subRecords[subKey]
	if !ok {
		return ""
	}

	return h.repoRecords[repoKey].branchs[genBranchString(subIns)].localDir
}

func (h *HubGitOps) DownloadAnsibleHookResource(subIns *subv1.Subscription) error {
	h.logger.V(DebugLog).Info("entry DownloadAnsibleHookResource")
	defer h.logger.V(DebugLog).Info("exit DownloadAnsibleHookResource")

	// meaning the branch is already downloaded
	if h.GetRepoRootDirctory(subIns) != "" {
		return nil
	}

	h.RegisterBranch(subIns)

	return nil
}

func cloneGitRepoBranch(repoURL string, branchName string, user, pwd, repoBranchDir string, skipCertVerify bool) (string, error) {
	return utils.CloneGitRepo(repoURL, utils.GetSubscriptionBranchRef(branchName), user, pwd, repoBranchDir, skipCertVerify)
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

func (h *HubGitOps) HasHookFolders(subIns *subv1.Subscription) bool {
	pre, post := getHookPath(subIns)

	if pre == "" && post == "" {
		return false
	}

	bLocalPath := h.GetRepoRootDirctory(subIns)

	preFullPath := fmt.Sprintf("%v/%v", bLocalPath, pre)
	_, preErr := os.Stat(preFullPath)

	postFullPath := fmt.Sprintf("%v/%v", bLocalPath, post)
	_, postErr := os.Stat(postFullPath)

	return preErr == nil || postErr == nil
}

//GetHooks will provided the ansibleJobs at the given hookPath(if given a
//posthook path, then posthook ansiblejob is returned)
func (h *HubGitOps) GetHooks(subIns *subv1.Subscription, hookPath string) ([]ansiblejob.AnsibleJob, error) {
	fullPath := fmt.Sprintf("%v/%v", h.GetRepoRootDirctory(subIns), hookPath)
	if _, err := os.Stat(fullPath); err != nil {
		if os.IsNotExist(err) {
			return []ansiblejob.AnsibleJob{}, nil
		}

		h.logger.Error(err, "fail to access the hook path")

		return []ansiblejob.AnsibleJob{}, err
	}

	sortedRes, err := sortClonedGitRepoGievnDestPath(h.GetRepoRootDirctory(subIns), hookPath, h.logger)
	if err != nil {
		return []ansiblejob.AnsibleJob{}, err
	}

	if len(sortedRes.kustomized) != 0 {
		return parseFromKutomizedAsAnsibleJobs(sortedRes.kustomized, parseAnsibleJobResoures, h.logger)
	}

	return parseAsAnsibleJobs(sortedRes.kubRes, parseAnsibleJobResoures, h.logger)
}
