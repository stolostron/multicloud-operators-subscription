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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	ansiblejob "github.com/stolostron/multicloud-operators-subscription/pkg/apis/apps/ansible/v1alpha1"
	subv1 "github.com/stolostron/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/stolostron/multicloud-operators-subscription/pkg/utils"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	gitWatchInterval = time.Hour
	commitIDSuffix   = "-new"
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
	RegisterBranch(*subv1.Subscription) error

	// DeregisterBranch
	DeregisterBranch(types.NamespacedName)

	//helper for test
	GetRepoRecords() map[string]*RepoRegistery
	GetSubRecords() map[types.NamespacedName]string

	//GetLatestCommitID will output the latest commit id from local git record
	GetLatestCommitID(*subv1.Subscription) (string, error)
	//ResolveLocalGitFolder is used to open a local folder for downloading the
	//repo branch
	ResolveLocalGitFolder(*subv1.Subscription) string

	GetRepoRootDirctory(*subv1.Subscription) string

	//Runnable
	Start(context.Context) error
}

type branchInfo struct {
	gitCloneOptions utils.GitCloneOption
	lastCommitID    string
	registeredSub   map[types.NamespacedName]struct{}
}

type RepoRegistery struct {
	url     string
	branchs map[string]*branchInfo
}

type cloneFunc func(cloneOptions *utils.GitCloneOption) (string, error)

type dirResolver func(*subv1.Subscription) string

type HubGitOps struct {
	clt                 client.Client
	logger              logr.Logger
	mtx                 sync.Mutex
	watcherInterval     time.Duration
	subRecords          map[types.NamespacedName]string
	repoRecords         map[string]*RepoRegistery
	downloadDirResolver dirResolver
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

func setLocalDirResovler(resolveFunc func(*subv1.Subscription) string) HubGitOption {
	return func(a *HubGitOps) {
		a.downloadDirResolver = resolveFunc
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
		watcherInterval:     gitWatchInterval,
		subRecords:          map[types.NamespacedName]string{},
		repoRecords:         map[string]*RepoRegistery{},
		downloadDirResolver: utils.GetLocalGitFolder,
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
func (h *HubGitOps) Start(ctx context.Context) error {
	h.logger.Info("entry StartGitWatch")
	defer h.logger.Info("exit StartGitWatch")

	err := CreateSubscriptionAdminRBAC(h.clt)
	if err != nil {
		klog.Error(err, "failed create subscriberitem admin RBAC")
	}

	go wait.UntilWithContext(ctx, h.GitWatch, h.watcherInterval)

	return nil
}

func (h *HubGitOps) GitWatch(ctx context.Context) {
	h.logger.V(DebugLog).Info("entry GitWatch")
	defer h.logger.V(DebugLog).Info("exit GitWatch")

	h.mtx.Lock()
	defer h.mtx.Unlock()

	for repoName, repoRegistery := range h.repoRecords {
		url := repoRegistery.url
		// need to figure out a way to separate the private repo
		for branchInfoName, branchInfo := range repoRegistery.branchs {
			// If commitHash is provided, compare this to the currently deployed commit
			// If tag is provided, resolve tag to commit SHA and compare it to the currently deployed commit
			// Otherwise, compare the latest commit of the repo branch to the currently deployed commit
			h.logger.Info(fmt.Sprintf("Checking commit for Git: %s Branch: %s", url, branchInfoName))
			newCommit, err := h.cloneFunc(&branchInfo.gitCloneOptions)

			if err != nil {
				h.logger.Error(err, " failed to get the commit SHA")
			}

			cloneDone := true

			// safe guard condition to filter out the edge case
			if newCommit == "" {
				h.logger.Info(fmt.Sprintf("repo %s, branch %s, commit%s, tag %s, get empty commit ID from remote",
					url,
					branchInfo.gitCloneOptions.Branch.Short(),
					branchInfo.gitCloneOptions.CommitHash,
					branchInfo.gitCloneOptions.RevisionTag))

				continue
			}

			h.logger.Info("Currently deployed commit: ", branchInfo.lastCommitID)
			h.logger.Info("Commit to be deployed: ", newCommit)

			if newCommit == branchInfo.lastCommitID {
				h.logger.Info("The repo commit hasn't changed.")
				continue
			}

			h.repoRecords[repoName].branchs[branchInfoName].lastCommitID = newCommit
			h.logger.Info("The repo has new commit: " + newCommit)

			if !cloneDone {
				if _, err := h.cloneFunc(&branchInfo.gitCloneOptions); err != nil {
					h.logger.Error(err, err.Error())
				}
			}

			for subKey := range branchInfo.registeredSub {
				// Update the commit annotation with a wrong commit ID to trigger hub subscription reconcile.
				// The hub subscription reconcile will compare this to the commit ID in the map h.repoRecords[repoName].branchs[bName].lastCommitID
				// to determine it needs to regenerate deployables.
				if err := updateCommitAnnotation(h.clt, subKey, fakeCommitID(newCommit)); err != nil {
					h.logger.Error(err, fmt.Sprintf("failed to update new commit %s to subscription %s", newCommit, subKey.String()))
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

func getBranchCommitDepthAndTag(subIns *subv1.Subscription) (string, string, string, string) {
	an := subIns.GetAnnotations()
	if len(an) == 0 {
		return "", "", "", ""
	}

	branch := ""

	if an[subv1.AnnotationGitBranch] != "" {
		branch = an[subv1.AnnotationGitBranch]
	}

	if an[subv1.AnnotationGithubBranch] != "" {
		branch = an[subv1.AnnotationGithubBranch]
	}

	return branch, an[subv1.AnnotationGitTargetCommit], an[subv1.AnnotationGitTag], an[subv1.AnnotationGitCloneDepth]
}

func genBranchString(subIns *subv1.Subscription) string {
	an := subIns.GetAnnotations()
	if len(an) == 0 {
		return ""
	}

	branch, commit, tag, _ := getBranchCommitDepthAndTag(subIns)

	// Honor commit first, then tag, then branch. These are mutually exclusive
	if commit != "" {
		return commit
	}

	if tag != "" {
		return tag
	}

	if branch != "" {
		return branch
	}

	return "nobranchdummy"
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

func (h *HubGitOps) ResolveLocalGitFolder(subIns *subv1.Subscription) string {
	return h.downloadDirResolver(subIns)
}

func (h *HubGitOps) RegisterBranch(subIns *subv1.Subscription) error {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

	// This does not pick up new changes to channel configuration
	//if _, ok := h.subRecords[subKey]; ok {
	//	return
	//}

	// repoName is the key of a map that stores repository information for subscription.
	// It needs to be unique for each subscription because each subscription need to
	// have its own copy of cloned repo to work on subscription specific overrides.
	repoName := genRepoName(subIns.Name, subIns.Namespace)
	branchInfoName := genBranchString(subIns)
	branchName, commit, tag, depth := getBranchCommitDepthAndTag(subIns)
	repoBranchDir := h.downloadDirResolver(subIns)

	h.mtx.Lock()
	defer h.mtx.Unlock()

	h.subRecords[subKey] = repoName
	subscriptionRepoInfo, ok := h.repoRecords[repoName]

	// Start with git clone depth 0.
	depthInt := 0

	if depth != "" {
		depthInt2, err2 := strconv.Atoi(depth)

		if err2 != nil {
			h.logger.Error(err2, " failed to convert git-clone-depth to integer")

			depthInt2 = 0
		}

		depthInt = depthInt2
	}

	cloneOptions := &utils.GitCloneOption{
		Branch:      utils.GetSubscriptionBranchRef(branchName),
		CommitHash:  commit,
		RevisionTag: tag,
		DestDir:     repoBranchDir,
		CloneDepth:  depthInt,
	}

	primaryChannel, secondaryChannel, err := GetSubscriptionRefChannel(h.clt, subIns)

	if err != nil {
		h.logger.Error(err, "failed to register subscription to GitOps")
		return err
	}

	if !isGitChannel(primaryChannel) {
		return nil
	}

	user, pwd, sshKey, passphrase, clientkey, clientcert, err := utils.GetChannelSecret(h.clt, primaryChannel)

	if err != nil {
		h.logger.Error(err, "failed to register subscription to git watcher register")
		return err
	}

	channelConfig := utils.GetChannelConfigMap(h.clt, primaryChannel)
	caCert := ""

	if channelConfig != nil {
		caCert = channelConfig.Data[subv1.ChannelCertificateData]
		if caCert != "" {
			h.logger.Info("Channel config map with CA certs found")
		}
	}

	skipCertVerify := false

	if primaryChannel.Spec.InsecureSkipVerify {
		skipCertVerify = true

		h.logger.Info("Channel spec has insecureSkipVerify: true.")
	}

	primaryChannelConnectionConfig := &utils.ChannelConnectionCfg{}
	primaryChannelConnectionConfig.RepoURL = primaryChannel.Spec.Pathname
	primaryChannelConnectionConfig.CaCerts = caCert
	primaryChannelConnectionConfig.InsecureSkipVerify = skipCertVerify
	primaryChannelConnectionConfig.Passphrase = passphrase
	primaryChannelConnectionConfig.Password = pwd
	primaryChannelConnectionConfig.SSHKey = sshKey
	primaryChannelConnectionConfig.User = user
	primaryChannelConnectionConfig.ClientCert = clientcert
	primaryChannelConnectionConfig.ClientKey = clientkey

	cloneOptions.PrimaryConnectionOption = primaryChannelConnectionConfig

	if secondaryChannel != nil {
		user, pwd, sshKey, passphrase, clientkey, clientcert, err := utils.GetChannelSecret(h.clt, secondaryChannel)

		if err != nil {
			h.logger.Error(err, "failed to register subscription to git watcher register")
			return err
		}

		channelConfig := utils.GetChannelConfigMap(h.clt, primaryChannel)
		caCert := ""

		if channelConfig != nil {
			caCert = channelConfig.Data[subv1.ChannelCertificateData]
			if caCert != "" {
				h.logger.Info("Channel config map with CA certs found")
			}
		}

		skipCertVerify := false

		if primaryChannel.Spec.InsecureSkipVerify {
			skipCertVerify = true

			h.logger.Info("Channel spec has insecureSkipVerify: true.")
		}

		secondaryChannelConnectionConfig := &utils.ChannelConnectionCfg{}
		secondaryChannelConnectionConfig.RepoURL = secondaryChannel.Spec.Pathname
		secondaryChannelConnectionConfig.CaCerts = caCert
		secondaryChannelConnectionConfig.InsecureSkipVerify = skipCertVerify
		secondaryChannelConnectionConfig.Passphrase = passphrase
		secondaryChannelConnectionConfig.Password = pwd
		secondaryChannelConnectionConfig.SSHKey = sshKey
		secondaryChannelConnectionConfig.User = user
		secondaryChannelConnectionConfig.ClientCert = clientcert
		secondaryChannelConnectionConfig.ClientKey = clientkey

		cloneOptions.SecondaryConnectionOption = secondaryChannelConnectionConfig
	}

	commitID, err := h.cloneFunc(cloneOptions)
	if err != nil {
		h.logger.Error(err, "failed to get commitID from initialDownload")
		return err
	}

	//make sure the initial prehook is passed
	if !ok {
		h.repoRecords[repoName] = &RepoRegistery{
			url: primaryChannelConnectionConfig.RepoURL,
			branchs: map[string]*branchInfo{
				branchInfoName: {
					gitCloneOptions: *cloneOptions,
					lastCommitID:    commitID,
					registeredSub: map[types.NamespacedName]struct{}{
						subKey: {},
					},
				},
			},
		}

		return nil
	}

	if subscriptionRepoInfo.branchs[branchInfoName] == nil {
		subscriptionRepoInfo.branchs[branchInfoName] = &branchInfo{
			gitCloneOptions: *cloneOptions,
			lastCommitID:    commitID,
			registeredSub: map[types.NamespacedName]struct{}{
				subKey: {},
			},
		}

		return nil
	}

	h.logger.Info("setting the latest commit ID to ", commitID)

	subscriptionRepoInfo.branchs[branchInfoName].lastCommitID = commitID

	// Pick up new channel configurations
	subscriptionRepoInfo.branchs[branchInfoName].gitCloneOptions = *cloneOptions

	subscriptionRepoInfo.branchs[branchInfoName].registeredSub[subKey] = struct{}{}

	return nil
}

func fakeCommitID(c string) string {
	return fmt.Sprintf("%s%s", c, commitIDSuffix)
}

func unmaskFakeCommitID(c string) string {
	return strings.TrimSuffix(c, commitIDSuffix)
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

	if h.repoRecords == nil || h.repoRecords[repoName] == nil || h.repoRecords[repoName].branchs == nil {
		return
	}

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

func (h *HubGitOps) GetLatestCommitID(subIns *subv1.Subscription) (string, error) {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

	_, ok := h.subRecords[subKey]
	if !ok { // when git watcher doesn't have the record, go ahead clone the repo and return the commitID
		err := h.RegisterBranch(subIns)
		return "", err
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

	return h.repoRecords[repoKey].branchs[genBranchString(subIns)].gitCloneOptions.DestDir
}

func (h *HubGitOps) DownloadAnsibleHookResource(subIns *subv1.Subscription) error {
	h.logger.V(DebugLog).Info("entry DownloadAnsibleHookResource")
	defer h.logger.V(DebugLog).Info("exit DownloadAnsibleHookResource")

	// meaning the branch is already downloaded
	if h.GetRepoRootDirctory(subIns) != "" {
		return nil
	}

	if err := h.RegisterBranch(subIns); err != nil {
		return err
	}

	return nil
}

func cloneGitRepoBranch(cloneOptions *utils.GitCloneOption) (string, error) {
	return utils.CloneGitRepo(cloneOptions)
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
