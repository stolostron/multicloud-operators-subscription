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
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	clusterapi "open-cluster-management.io/api/cluster/v1beta1"
	ansiblejob "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/ansible/v1alpha1"
	placementrulev1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	subv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	placementutils "open-cluster-management.io/multicloud-operators-subscription/pkg/placementrule/utils"

	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"

	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	PrehookDirSuffix  = "/prehook/"
	PosthookDirSuffix = "/posthook/"
	JobCompleted      = "successful"
	AnsibleJobKind    = "AnsibleJob"
	AnsibleJobVersion = "tower.ansible.com/v1alpha1"
	Status            = "status"
	AnsibleJobResult  = "ansiblejobresult"
	SubLabel          = "ownersub"
	PreHookType       = "pre"
	PostHookType      = "post"

	DebugLog = 1
	InfoLog  = 0
)

// HookProcessor tracks the pre and post hook information of subscriptions.
type HookProcessor interface {
	// register subsription to the HookProcessor
	RegisterSubscription(sub *subv1.Subscription, placementDecisionUpdated bool, placementDecisionRv string) error
	DeregisterSubscription(subKey types.NamespacedName) error

	//SetSuffixFunc let user reset the suffixFunc rule of generating the suffix
	//of hook instance name
	SetSuffixFunc(suffixFunc SuffixFunc)
	ResetGitOps(gitOps GitOps)
	//ApplyPreHook returns a type.NamespacedName of the preHook
	ApplyPreHooks(subKey types.NamespacedName) error
	IsPreHooksCompleted(subKey types.NamespacedName) (bool, error)
	ApplyPostHooks(subKey types.NamespacedName) error
	IsPostHooksCompleted(subKey types.NamespacedName) (bool, error)

	HasHooks(hookType string, subKey types.NamespacedName) bool
	//WriteStatusToSubscription gets the status at the entry of the reconcile,
	//also the procssed subscription(which should carry all the update status on
	//the given reconciel), then  WriteStatusToSubscription will append the hook
	//status info and make a update to the cluster
	AppendStatusToSubscription(sub *subv1.Subscription) subv1.SubscriptionStatus

	AppendPreHookStatusToSubscription(sub *subv1.Subscription) subv1.SubscriptionStatus

	GetLastAppliedInstance(subKey types.NamespacedName) AppliedInstance
}

type Hooks struct {
	//store all the applied prehook instance
	preHooks  *JobInstances
	postHooks *JobInstances

	//store last subscription instance used for the hook operation
	lastSub *subv1.Subscription
}

type AnsibleHooks struct {
	gitClt GitOps
	clt    client.Client
	// subscription namespacedName will points to hooks
	mtx        sync.Mutex
	registry   map[types.NamespacedName]*Hooks
	suffixFunc SuffixFunc
	//logger
	logger       logr.Logger
	hookInterval time.Duration
}

func (h *Hooks) ConstructStatus() subv1.AnsibleJobsStatus {
	st := subv1.AnsibleJobsStatus{}

	preSt := h.constructPrehookStatus()
	st.LastPrehookJob = preSt.LastPrehookJob

	postSt := h.constructPosthookStatus()
	st.LastPosthookJob = postSt.LastPosthookJob

	return st
}

func (h *Hooks) constructPrehookStatus() subv1.AnsibleJobsStatus {
	st := subv1.AnsibleJobsStatus{}

	if h.preHooks != nil {
		jobRecords := h.preHooks.outputAppliedJobs(ansiblestatusFormat)
		st.LastPrehookJob = jobRecords.lastApplied
	}

	return st
}

func (h *Hooks) constructPosthookStatus() subv1.AnsibleJobsStatus {
	st := subv1.AnsibleJobsStatus{}

	if h.postHooks != nil {
		jobRecords := h.postHooks.outputAppliedJobs(ansiblestatusFormat)
		st.LastPosthookJob = jobRecords.lastApplied
	}

	return st
}

// make sure the AnsibleHooks implementate the HookProcessor
var _ HookProcessor = &AnsibleHooks{}

type HookOps func(*AnsibleHooks)

func setLogger(logger logr.Logger) HookOps {
	return func(a *AnsibleHooks) {
		a.logger = logger
	}
}

func setGitOps(g GitOps) HookOps {
	return func(a *AnsibleHooks) {
		a.gitClt = g
	}
}

func NewAnsibleHooks(clt client.Client, hookInterval time.Duration, ops ...HookOps) *AnsibleHooks {
	a := &AnsibleHooks{
		clt:          clt,
		mtx:          sync.Mutex{},
		hookInterval: hookInterval,
		registry:     map[types.NamespacedName]*Hooks{},
		suffixFunc:   suffixBasedOnSpecAndCommitID,
	}

	for _, op := range ops {
		op(a)
	}

	return a
}

type AppliedInstance struct {
	pre  string
	post string
}

func (a *AnsibleHooks) GetLastAppliedInstance(subKey types.NamespacedName) AppliedInstance {
	hooks, ok := a.registry[subKey]
	if !ok {
		return AppliedInstance{}
	}

	preJobRecords := hooks.preHooks.outputAppliedJobs(formatAnsibleFromTopo)
	postJobRecords := hooks.postHooks.outputAppliedJobs(formatAnsibleFromTopo)

	return AppliedInstance{
		pre:  preJobRecords.lastApplied,
		post: postJobRecords.lastApplied,
	}
}

func (a *AnsibleHooks) ResetGitOps(g GitOps) {
	a.gitClt = g
}

func (a *AnsibleHooks) AppendStatusToSubscription(subIns *subv1.Subscription) subv1.SubscriptionStatus {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}
	hooks := a.registry[subKey]
	out := subIns.DeepCopy().Status

	//return if the sub doesn't have hook
	if hooks == nil {
		return out
	}

	out.AnsibleJobsStatus = hooks.ConstructStatus()

	return out
}

func (a *AnsibleHooks) AppendPreHookStatusToSubscription(subIns *subv1.Subscription) subv1.SubscriptionStatus {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}
	hooks := a.registry[subKey]
	out := subIns.DeepCopy().Status

	//return if the sub doesn't have hook
	if hooks == nil {
		return out
	}

	out.AnsibleJobsStatus = hooks.constructPrehookStatus()

	return out
}

func (a *AnsibleHooks) SetSuffixFunc(f SuffixFunc) {
	if f == nil {
		return
	}

	a.suffixFunc = f
}

func (a *AnsibleHooks) DeregisterSubscription(subKey types.NamespacedName) error {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	delete(a.registry, subKey)

	return nil
}

func (a *AnsibleHooks) IsReadyPlacementDecisionList(appsub *subv1.Subscription) (bool, error) {
	// get all clusters from all the placementDecisions resources
	placementDecisionclusters, err := GetClustersByPlacement(appsub, a.clt, a.logger)

	if err != nil {
		klog.Infof("faile to get clusters from placementDecisions, err:%v", err)
		return false, err
	}

	if len(placementDecisionclusters) == 0 {
		klog.Infof("No clusters found from placementDecisions")
		return false, fmt.Errorf("no clusters found. sub: %v/%v", appsub.Namespace, appsub.Name)
	}

	clusters1 := []string{}
	for _, cl := range placementDecisionclusters {
		clusters1 = append(clusters1, cl.Name)
	}

	sort.Slice(clusters1, func(i, j int) bool {
		return clusters1[i] < clusters1[j]
	})

	pref := appsub.Spec.Placement.PlacementRef

	if pref != nil && pref.Kind == "PlacementRule" {
		placementRule := &placementrulev1.PlacementRule{}
		prKey := types.NamespacedName{Name: pref.Name, Namespace: appsub.GetNamespace()}

		if err := a.clt.Get(context.TODO(), prKey, placementRule); err != nil {
			klog.Infof("failed to get placementRule, err:%v", err)
			return false, err
		}

		placementRuleStatusClusters := placementRule.Status.Decisions

		clusters2 := []string{}
		for _, cl := range placementRuleStatusClusters {
			clusters2 = append(clusters2, cl.ClusterName)
		}

		sort.Slice(clusters2, func(i, j int) bool {
			return clusters2[i] < clusters2[j]
		})

		if reflect.DeepEqual(clusters1, clusters2) {
			klog.Infof("placementRule cluster decision list is ready, appsub: %v/%v, placementref: %v", appsub.Namespace, appsub.Name, pref.Name)
			return true, nil
		}
	}

	if pref != nil && pref.Kind == "Placement" {
		placement := &clusterapi.Placement{}
		pKey := types.NamespacedName{Name: pref.Name, Namespace: appsub.GetNamespace()}

		if err := a.clt.Get(context.TODO(), pKey, placement); err != nil {
			klog.Infof("failed to get placement, err:%v", err)
			return false, err
		}

		if int(placement.Status.NumberOfSelectedClusters) == len(placementDecisionclusters) {
			klog.Infof("placement cluster decision list is ready, appsub: %v/%v, placementref: %v", appsub.Namespace, appsub.Name, pref.Name)
			return true, nil
		}
	}

	klog.Infof("placement cluster decision list is NOT ready, appsub: %v/%v", appsub.Namespace, appsub.Name)

	return false, fmt.Errorf("placement cluster decision list is NOT ready, appsub: %v/%v", appsub.Namespace, appsub.Name)
}

func (a *AnsibleHooks) RegisterSubscription(subIns *subv1.Subscription, placementDecisionUpdated bool, placementRuleRv string) error {
	a.logger.Info(fmt.Sprintf("entry register subscription, appsub: %v/%v", subIns.Namespace, subIns.Name))
	defer a.logger.Info(fmt.Sprintf("exit register subscription, appsub: %v/%v", subIns.Namespace, subIns.Name))

	chn := &chnv1.Channel{}
	chnkey := utils.NamespacedNameFormat(subIns.Spec.Channel)

	if err := a.clt.Get(context.TODO(), chnkey, chn); err != nil {
		return err
	}

	chType := string(chn.Spec.Type)

	//if the given subscription is not pointing to a git channel, then skip
	if !strings.EqualFold(chType, chnv1.ChannelTypeGit) && !strings.EqualFold(chType, chnv1.ChannelTypeGitHub) {
		return nil
	}

	if !a.gitClt.HasHookFolders(subIns) {
		a.logger.Info(fmt.Sprintf("%s doesn't have hook folder(s) yet, skip", PrintHelper(subIns)))

		return nil
	}

	// Skip the ansibleJob hook registration if the placement decision list is not ready
	isReadyPlacementDecision, err := a.IsReadyPlacementDecisionList(subIns)
	if !isReadyPlacementDecision {
		return err
	}

	//if not forcing a register and the subIns has not being changed compare to the hook registry
	//then skip hook processing
	commitIDChanged := a.isSubscriptionUpdate(subIns, a.isSubscriptionSpecChange, a.isDesiredStateChanged)
	if getCommitID(subIns) != "" && !placementDecisionUpdated && !commitIDChanged {
		a.logger.Info(fmt.Sprintf("skip hook registry, commitIDChanged: %v, placementDecisionUpdated: %v ", commitIDChanged, placementDecisionUpdated))
		return nil
	}

	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

	if _, ok := a.registry[subKey]; !ok {
		a.registry[subKey] = &Hooks{
			lastSub:   subIns,
			preHooks:  &JobInstances{},
			postHooks: &JobInstances{},
		}
	}

	if err := a.gitClt.DownloadAnsibleHookResource(subIns); err != nil {
		return fmt.Errorf("failed to download from git source of subscription %s, err: %w", subKey, err)
	}

	//update the base Ansible job and append a generated job to the preHooks
	return a.addHookToRegisitry(subIns, placementDecisionUpdated, placementRuleRv, commitIDChanged)
}

func (a *AnsibleHooks) isSubscriptionSpecChange(o, n *subv1.Subscription) bool {
	a.logger.Info(fmt.Sprintf("isSubscriptionSpecChange old: %d, new: %d", o.GetGeneration(), n.GetGeneration()))

	return o.GetGeneration() != n.GetGeneration()
}

type SuffixFunc func(GitOps, *subv1.Subscription) string

func suffixBasedOnSpecAndCommitID(gClt GitOps, subIns *subv1.Subscription) string {
	prefixLen := 6

	//get actual commitID
	commitID, err := gClt.GetLatestCommitID(subIns)
	if err != nil {
		return ""
	}

	commitID = commitID[:prefixLen]

	return fmt.Sprintf("-%v-%v", subIns.GetGeneration(), commitID)
}

func (a *AnsibleHooks) registerHook(subIns *subv1.Subscription, hookFlag string,
	jobs []ansiblejob.AnsibleJob, placementDecisionUpdated bool, placementRuleRv string,
	commitIDChanged bool) error {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

	if hookFlag == PreHookType {
		if a.registry[subKey].preHooks == nil {
			a.registry[subKey].preHooks = &JobInstances{}
		}

		err := a.registry[subKey].preHooks.registryJobs(a.gitClt, subIns, a.suffixFunc, jobs, a.clt, a.logger,
			placementDecisionUpdated, placementRuleRv, "prehook", commitIDChanged)

		return err
	}

	if a.registry[subKey].postHooks == nil {
		a.registry[subKey].postHooks = &JobInstances{}
	}

	err := a.registry[subKey].postHooks.registryJobs(a.gitClt, subIns, a.suffixFunc, jobs, a.clt, a.logger,
		placementDecisionUpdated, placementRuleRv, "posthook", commitIDChanged)

	return err
}

func (a *AnsibleHooks) printAllHooks() {
	for subkey, hook := range a.registry {
		klog.Infof("================")

		klog.Infof("subkey: %v", subkey.String())

		for prehook, prehookJobs := range *hook.preHooks {
			klog.Infof("========")
			klog.Infof("prehook Ansible job template: %v", prehook.String())

			for _, prehookJob := range prehookJobs.Instance {
				klog.Infof("prehook Ansible Job instance: %v/%v", prehookJob.Namespace, prehookJob.Name)
			}
		}

		for posthook, posthookJobs := range *hook.postHooks {
			klog.Infof("========")
			klog.Infof("posthook Ansible job template: %v", posthook.String())

			for _, posthookJob := range posthookJobs.Instance {
				klog.Infof("posthook Ansible Job instance: %v/%v", posthookJob.Namespace, posthookJob.Name)
			}
		}
	}
}

func getHookPath(subIns *subv1.Subscription) (string, string) {
	annotations := subIns.GetAnnotations()

	preHookPath, postHookPath := "", ""
	if annotations[subv1.AnnotationGithubPath] != "" {
		preHookPath = fmt.Sprintf("%v/prehook", annotations[subv1.AnnotationGithubPath])
		postHookPath = fmt.Sprintf("%v/posthook", annotations[subv1.AnnotationGithubPath])
	} else if annotations[subv1.AnnotationGitPath] != "" {
		preHookPath = fmt.Sprintf("%v/prehook", annotations[subv1.AnnotationGitPath])
		postHookPath = fmt.Sprintf("%v/posthook", annotations[subv1.AnnotationGitPath])
	}

	return preHookPath, postHookPath
}

func (a *AnsibleHooks) addHookToRegisitry(subIns *subv1.Subscription, placementDecisionUpdated bool, placementRuleRv string,
	commitIDChanged bool) error {
	a.logger.Info("entry addNewHook subscription")
	defer a.logger.Info("exit addNewHook subscription")

	preHookPath, postHookPath := getHookPath(subIns)

	preJobs, err := a.gitClt.GetHooks(subIns, preHookPath)
	if err != nil {
		a.logger.Error(fmt.Errorf("prehook"), "failed to find hook:")
	}

	postJobs, err := a.gitClt.GetHooks(subIns, postHookPath)
	if err != nil {
		a.logger.Error(fmt.Errorf("posthook"), "failed to find hook:")
	}

	if len(preJobs) != 0 || len(postJobs) != 0 {
		subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}
		a.registry[subKey].lastSub = subIns
	}

	if len(preJobs) != 0 {
		if err := a.registerHook(subIns, PreHookType, preJobs, placementDecisionUpdated, placementRuleRv, commitIDChanged); err != nil {
			return err
		}
	}

	if len(postJobs) != 0 {
		if err := a.registerHook(subIns, PostHookType, postJobs, placementDecisionUpdated, placementRuleRv, commitIDChanged); err != nil {
			return err
		}
	}

	a.printAllHooks()

	return nil
}

func GetReferenceString(ref *corev1.ObjectReference) string {
	return ref.Name
}

func addingHostingSubscriptionAnno(job ansiblejob.AnsibleJob, subKey types.NamespacedName, hookType string) ansiblejob.AnsibleJob {
	a := job.GetAnnotations()
	if len(a) == 0 {
		a = map[string]string{}
	}

	a[subv1.AnnotationHosting] = subKey.String()
	a[subv1.AnnotationHookType] = hookType
	a[subv1.AnnotationHookTemplate] = job.Namespace + "/" + job.Name

	job.SetAnnotations(a)

	return job
}

// overrideAnsibleInstance adds the owner reference to job, and also reset the
// secret file of ansibleJob
func overrideAnsibleInstance(subIns *subv1.Subscription, job ansiblejob.AnsibleJob,
	kubeclient client.Client, logger logr.Logger, hookType string) (ansiblejob.AnsibleJob, error) {
	job.SetResourceVersion("")
	// avoid the error:
	// status.conditions.lastTransitionTime in body must be of type string: \"null\""
	job.Status = ansiblejob.AnsibleJobStatus{}

	if subIns.Spec.HookSecretRef != nil {
		job.Spec.TowerAuthSecretName = GetReferenceString(subIns.Spec.HookSecretRef)
	}

	if subIns.Spec.Placement != nil &&
		(subIns.Spec.Placement.Local == nil || !*subIns.Spec.Placement.Local) {
		clusters, err := GetClustersByPlacement(subIns, kubeclient, logger)
		if err != nil {
			return job, err
		}

		if len(clusters) > 0 {
			var targetClusters []string

			for _, cluster := range clusters {
				targetClusters = append(targetClusters, cluster.Name)
			}

			extraVarsMap := make(map[string]interface{})

			if job.Spec.ExtraVars != nil {
				err := json.Unmarshal(job.Spec.ExtraVars, &extraVarsMap)
				if err != nil {
					return job, err
				}
			}

			extraVarsMap["target_clusters"] = targetClusters

			extraVars, err := json.Marshal(extraVarsMap)
			if err != nil {
				return job, err
			}

			job.Spec.ExtraVars = extraVars
		} else {
			job.Spec.ExtraVars = nil
		}
	}

	//make sure all the ansiblejob is deployed at the subscription namespace
	job.SetNamespace(subIns.GetNamespace())

	job = addingHostingSubscriptionAnno(job,
		types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}, hookType)

	//set owerreferce
	if err := ctrlutil.SetOwnerReference(subIns.DeepCopy(), &job, scheme.Scheme); err != nil {
		return job, err
	}

	return job, nil
}

func (a *AnsibleHooks) isRegistered(subKey types.NamespacedName) bool {
	return a.registry[subKey] != nil
}

func (a *AnsibleHooks) ApplyPreHooks(subKey types.NamespacedName) error {
	if a.HasHooks(PreHookType, subKey) {
		hks := a.registry[subKey].preHooks

		return hks.applyJobs(a.clt, a.registry[subKey].lastSub, a.logger)
	}

	return nil
}

type EqualSub func(*subv1.Subscription, *subv1.Subscription) bool

func (a *AnsibleHooks) isSubscriptionUpdate(subIns *subv1.Subscription, isNotEqual ...EqualSub) bool {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}
	record, ok := a.registry[subKey]

	if !ok {
		return true
	}

	for _, eFn := range isNotEqual {
		if eFn(record.lastSub, subIns) {
			return true
		}
	}

	return false
}

func (a *AnsibleHooks) isDesiredStateChanged(oldSub, newSub *subv1.Subscription) bool {
	a.logger.Info("Comparing if subscription's desired state has changed")

	oldAnnotations := oldSub.GetAnnotations()

	newAnnotations := newSub.GetAnnotations()

	// If the desired commit or tag is set, compare and re-register hooks if necessary
	if newAnnotations[subv1.AnnotationGitTargetCommit] != "" &&
		(oldAnnotations[subv1.AnnotationGitTargetCommit] != newAnnotations[subv1.AnnotationGitTargetCommit]) {
		a.logger.Info(fmt.Sprintf("Desired commit has changed from %s to %s",
			oldAnnotations[subv1.AnnotationGitTargetCommit],
			newAnnotations[subv1.AnnotationGitTargetCommit]))

		return true
	} else if newAnnotations[subv1.AnnotationGitTag] != "" &&
		(oldAnnotations[subv1.AnnotationGitTag] != newAnnotations[subv1.AnnotationGitTag]) {
		a.logger.Info(fmt.Sprintf("Desired tag has changed from %s to %s",
			oldAnnotations[subv1.AnnotationGitTag],
			newAnnotations[subv1.AnnotationGitTag]))

		return true
	}

	// If manual eync is triggered, re-register hooks if necessary
	if oldAnnotations[subv1.AnnotationManualReconcileTime] != newAnnotations[subv1.AnnotationManualReconcileTime] {
		a.logger.Info(fmt.Sprintf("Manual sync time has changed from %s to %s",
			oldAnnotations[subv1.AnnotationManualReconcileTime],
			newAnnotations[subv1.AnnotationManualReconcileTime]))

		return true
	}

	aCommit := unmaskFakeCommitID(getCommitID(oldSub))
	bCommit := unmaskFakeCommitID(getCommitID(newSub))

	if aCommit != bCommit {
		a.logger.Info(fmt.Sprintf("The latest commit has changed from %s to %s", aCommit, bCommit))
	}

	return aCommit != bCommit
}

func getCommitID(a *subv1.Subscription) string {
	aAno := a.GetAnnotations()
	if len(aAno) == 0 {
		return ""
	}

	if aAno[subv1.AnnotationGitCommit] != "" {
		return aAno[subv1.AnnotationGitCommit]
	}

	if aAno[subv1.AnnotationGithubCommit] != "" {
		return aAno[subv1.AnnotationGithubCommit]
	}

	return ""
}

func (a *AnsibleHooks) IsPreHooksCompleted(subKey types.NamespacedName) (bool, error) {
	if !a.isRegistered(subKey) {
		return true, nil
	}

	hks := a.registry[subKey].preHooks

	if hks == nil || len(*hks) == 0 {
		return true, nil
	}

	return hks.isJobsCompleted(a.clt, a.logger)
}

func (a *AnsibleHooks) HasHooks(hookType string, subKey types.NamespacedName) bool {
	if !a.isRegistered(subKey) {
		a.logger.V(DebugLog).Info(fmt.Sprintf("there's not %v-hook registered for %v", hookType, subKey.String()))
		return false
	}

	if hookType == PreHookType {
		hks := a.registry[subKey].preHooks

		if hks == nil || len(*hks) == 0 {
			return false
		}

		return true
	}

	hks := a.registry[subKey].postHooks

	if hks == nil || len(*hks) == 0 {
		return false
	}

	return true
}

func (a *AnsibleHooks) ApplyPostHooks(subKey types.NamespacedName) error {
	if a.HasHooks(PostHookType, subKey) {
		hks := a.registry[subKey].postHooks
		return hks.applyJobs(a.clt, a.registry[subKey].lastSub, a.logger)
	}

	return nil
}

func (a *AnsibleHooks) IsPostHooksCompleted(subKey types.NamespacedName) (bool, error) {
	hks := a.registry[subKey].postHooks

	if hks == nil || len(*hks) == 0 {
		return true, nil
	}

	return hks.isJobsCompleted(a.clt, a.logger)
}

func isJobRunSuccessful(job *ansiblejob.AnsibleJob, logger logr.Logger) bool {
	curStatus := job.Status.AnsibleJobResult.Status
	logger.Info(fmt.Sprintf("job: %v, job url: %v status: %#v", PrintHelper(job), job.Status.AnsibleJobResult.URL, job.Status.AnsibleJobResult))

	return strings.EqualFold(curStatus, JobCompleted)
}

// Top priority: placementRef, ignore others
// Next priority: clusterNames, ignore selector
// Bottomline: Use label selector
func GetClustersByPlacement(instance *subv1.Subscription, kubeclient client.Client, logger logr.Logger) ([]types.NamespacedName, error) {
	var clusters []types.NamespacedName

	if instance.Spec.Placement != nil {
		var err error

		// Top priority: placementRef, ignore others
		// Next priority: clusterNames, ignore selector
		// Bottomline: Use label selector
		if instance.Spec.Placement.PlacementRef != nil {
			clusters, err = getClustersFromPlacementRef(instance, kubeclient, logger)
		} else {
			clustermap, err := placementutils.PlaceByGenericPlacmentFields(kubeclient, instance.Spec.Placement.GenericPlacementFields, instance)
			if err != nil {
				logger.Error(err, " - Failed to get clusters from generic fields with error.")
				return nil, err
			}

			for _, cl := range clustermap {
				clusters = append(clusters, types.NamespacedName{Name: cl.Name, Namespace: cl.Name})
			}
		}

		if err != nil {
			logger.Error(err, " - Failed in finding cluster namespaces. error.")
			return nil, err
		}
	}

	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Name < clusters[j].Name
	})

	if len(clusters) > 20 {
		logger.V(1).Info(fmt.Sprintln("The first 20 clusters: ", clusters[:20]))
	} else {
		logger.V(1).Info(fmt.Sprintln("clusters: ", clusters))
	}

	return clusters, nil
}

func getClustersFromPlacementRef(instance *subv1.Subscription, kubeclient client.Client, logger logr.Logger) ([]types.NamespacedName, error) {
	var clusters []types.NamespacedName

	pref := instance.Spec.Placement.PlacementRef

	if (len(pref.Kind) > 0 && pref.Kind != "PlacementRule" && pref.Kind != "Placement") ||
		(len(pref.APIVersion) > 0 &&
			pref.APIVersion != "apps.open-cluster-management.io/v1" &&
			pref.APIVersion != "cluster.open-cluster-management.io/v1alpha1" &&
			pref.APIVersion != "cluster.open-cluster-management.io/v1beta1") {
		logger.Info(fmt.Sprintln("Unsupported placement reference:", instance.Spec.Placement.PlacementRef))

		return nil, nil
	}

	logger.Info(fmt.Sprintln("Referencing placement: ", pref, " in ", instance.GetNamespace()))

	ns := instance.GetNamespace()

	if pref.Namespace != "" {
		ns = pref.Namespace
	}

	clusterNames, err := getDecisionsFromPlacementRef(pref, ns, kubeclient)
	if err != nil {
		logger.Error(err, "Failed to get decisions from placement reference: "+pref.Name)

		return nil, err
	}

	for _, clusterName := range clusterNames {
		cluster := types.NamespacedName{Name: clusterName, Namespace: clusterName}
		clusters = append(clusters, cluster)
	}

	return clusters, nil
}
