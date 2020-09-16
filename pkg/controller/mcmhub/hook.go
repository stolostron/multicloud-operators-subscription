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
	"strings"

	"github.com/go-logr/logr"
	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/klog/klogr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	isPreHook         = "pre"
	isPostHook        = "post"

	DebugLog = 3
)

//HOHookProcessor tracks the pre and post hook informantion of subscriptions.

type HookProcessor interface {
	// register subsription to the HookProcessor
	RegisterSubscription(types.NamespacedName) error
	DeregisterSubscription(types.NamespacedName) error

	//SetSuffixFunc let user reset the suffixFunc rule of generating the suffix
	//of hook instance name
	SetSuffixFunc(SuffixFunc)
	//ApplyPreHook returns a type.NamespacedName of the preHook
	ApplyPreHooks(types.NamespacedName) error
	IsPreHooksCompleted(types.NamespacedName) (bool, error)
	ApplyPostHooks(types.NamespacedName) error
	IsPostHooksCompleted(types.NamespacedName) (bool, error)
	IsSubscriptionCompleted(types.NamespacedName) (bool, error)

	//WriteStatusToSubscription copies all the entry from hook registry to
	//subscription status
	WriteStatusToSubscription(types.NamespacedName) error
}

type Job struct {
	Original ansiblejob.AnsibleJob
	Instance []ansiblejob.AnsibleJob
	// track the create instance
	InstanceSet map[types.NamespacedName]struct{}
}

// JobInstances can be applied and can be quired to see if the most applied
// instance is succeeded or not
type JobInstances map[types.NamespacedName]*Job

type appliedJobs struct {
	lastApplied     string
	lastAppliedJobs []string
}

func getJobsString(jobs []ansiblejob.AnsibleJob) []string {
	if len(jobs) == 0 {
		return []string{}
	}

	formString := func(j ansiblejob.AnsibleJob) string {
		ns := j.GetNamespace()
		if len(ns) == 0 {
			ns = "default"
		}

		return fmt.Sprintf("%s/%s", ns, j.GetName())
	}

	res := []string{}

	for _, j := range jobs {
		res = append(res, formString(j))
	}

	return res
}

//merge multiple hook string
func (jIns *JobInstances) outputAppliedJobs() appliedJobs {
	res := appliedJobs{}

	lastApplied := []string{}
	lastAppliedJobs := []string{}

	for _, job := range *jIns {
		if len(job.Instance) == 0 {
			continue
		}

		applied := getJobsString(job.Instance)

		n := len(applied)
		lastApplied = append(lastApplied, applied[n-1])
		lastAppliedJobs = append(lastAppliedJobs, applied...)
	}

	sep := ","
	res.lastApplied = strings.Join(lastApplied, sep)
	res.lastAppliedJobs = lastAppliedJobs

	return res
}

func (jIns *JobInstances) registryJobs(subIns *subv1.Subscription, jobs []ansiblejob.AnsibleJob) {
	for _, job := range jobs {
		jobKey := types.NamespacedName{Name: job.GetName(), Namespace: job.GetNamespace()}
		ins := overrideAnsibleInstance(subIns, job)

		if _, ok := (*jIns)[jobKey]; !ok {
			(*jIns)[jobKey] = &Job{
				InstanceSet: make(map[types.NamespacedName]struct{}),
			}
		}

		(*jIns)[jobKey].Original = ins
	}
}

// applyjobs will get the original job and create a instance, the applied
// instance will is put into the job.Instance array upon the success of the
// creation
func (jIns *JobInstances) applyJobs(clt client.Client, suffixFunc SuffixFunc, subIns *subv1.Subscription) error {
	for k, j := range *jIns {
		nx := j.Original.DeepCopy()

		suffix := suffixFunc(subIns)
		nx.SetName(fmt.Sprintf("%s%s", nx.GetName(), suffix))

		if err := clt.Create(context.TODO(), nx); err != nil {
			if !kerr.IsAlreadyExists(err) {
				return fmt.Errorf("failed to apply job %v, err: %v", k.String(), err)
			}
		}

		//add the created job to the ansiblejob Set
		nxKey := types.NamespacedName{Name: nx.GetName(), Namespace: nx.GetNamespace()}
		if _, ok := j.InstanceSet[nxKey]; !ok {
			j.Instance = append(j.Instance, *nx)
			j.InstanceSet[nxKey] = struct{}{}
		}
	}

	return nil
}

// check the last instance of the ansiblejobs to see if it's applied and
// completed or not
func (jIns *JobInstances) isJobsCompleted(clt client.Client, logger logr.Logger) (bool, error) {
	for k, job := range *jIns {
		logger.V(DebugLog).Info(fmt.Sprintf("checking if%v job for completed or not", k.String()))

		n := len(job.Instance)
		if n == 0 {
			return true, nil
		}

		j := job.Instance[n-1]
		jKey := types.NamespacedName{Name: j.GetName(), Namespace: j.GetNamespace()}

		if ok, err := isJobDone(clt, jKey, logger); err != nil || !ok {
			return ok, err
		}
	}

	return true, nil
}

func isJobDone(clt client.Client, key types.NamespacedName, logger logr.Logger) (bool, error) {
	job := &ansiblejob.AnsibleJob{}

	if err := clt.Get(context.TODO(), key, job); err != nil {
		return false, err
	}

	if isJobRunSuccessful(job, logger) {
		return true, nil
	}

	return false, nil
}

type Hooks struct {
	//store all the applied prehook instance
	preHooks  *JobInstances
	postHooks *JobInstances

	//store last subscription instance used for the hook operation
	lastSub *subv1.Subscription
}

func (h *Hooks) ConstructStatus() subv1.AnsibleJobsStatus {
	st := subv1.AnsibleJobsStatus{}

	if h.preHooks != nil {
		jobRecords := h.preHooks.outputAppliedJobs()
		st.LastPrehookJob = jobRecords.lastApplied
		st.PrehookJobsHistory = jobRecords.lastAppliedJobs
	}

	if h.postHooks != nil {
		jobRecords := h.postHooks.outputAppliedJobs()
		st.LastPosthookJob = jobRecords.lastApplied
		st.PosthookJobsHistory = jobRecords.lastAppliedJobs
	}

	return st
}

type AnsibleHooks struct {
	gitClt GitOps
	clt    client.Client
	// subscription namespacedName will points to hooks
	registry   map[types.NamespacedName]*Hooks
	suffixFunc SuffixFunc
	//logger
	logger logr.Logger
}

// make sure the AnsibleHooks implementate the HookProcessor
var _ HookProcessor = &AnsibleHooks{}

func NewAnsibleHooks(clt client.Client, logger logr.Logger) *AnsibleHooks {
	if logger == nil {
		logger = klogr.New()
		logger.WithName("ansiblehook")
	}

	return &AnsibleHooks{
		clt:        clt,
		gitClt:     NewHookGit(clt, logger),
		registry:   map[types.NamespacedName]*Hooks{},
		logger:     logger,
		suffixFunc: suffixFromUUID,
	}
}

func (a *AnsibleHooks) WriteStatusToSubscription(subKey types.NamespacedName) error {
	subIns := &subv1.Subscription{}
	if err := a.clt.Get(context.TODO(), subKey, subIns); err != nil {
		if kerr.IsNotFound(err) {
			a.logger.V(DebugLog).Info("subscription is deleted, no need to write status")
			return nil
		}

		return err
	}

	if !subIns.DeletionTimestamp.IsZero() {
		return nil
	}

	hooks := a.registry[subKey]
	if hooks == nil {
		a.logger.Error(fmt.Errorf("can't find %v from hook registry", subKey.String()), "ansiblejob status update error")
		return nil
	}

	newSub := subIns.DeepCopy()
	newSub.Status.AnsibleJobsStatus = hooks.ConstructStatus()
	newSub.Status.LastUpdateTime = metav1.Now()

	if err := a.clt.Status().Update(context.TODO(), newSub); err != nil {
		return fmt.Errorf("failed to %s update hook status, err: %v", subKey.String(), err)
	}

	return nil
}

func (a *AnsibleHooks) SetSuffixFunc(f SuffixFunc) {
	if f == nil {
		return
	}

	a.suffixFunc = f
}

func (a *AnsibleHooks) DeregisterSubscription(subKey types.NamespacedName) error {
	delete(a.registry, subKey)
	return nil
}

func (a *AnsibleHooks) RegisterSubscription(subKey types.NamespacedName) error {
	a.logger.V(DebugLog).Info("entry register subscription")
	defer a.logger.V(DebugLog).Info("exit register subscription")

	subIns := &subv1.Subscription{}
	if err := a.clt.Get(context.TODO(), subKey, subIns); err != nil {
		// subscription is deleted
		if kerr.IsNotFound(err) {
			return nil
		}

		return err
	}

	if subIns.Spec.HookSecretRef == nil {
		a.logger.V(DebugLog).Info("subscription doesn't have hook to process")
	}

	//check if the subIns have being changed compare to the hook registry
	if !a.isUpdateSubscription(subIns) {
		return nil
	}

	if _, ok := a.registry[subKey]; !ok {
		a.registry[subKey] = &Hooks{
			lastSub:   subIns,
			preHooks:  &JobInstances{},
			postHooks: &JobInstances{},
		}
	}

	if err := a.gitClt.DownloadAnsibleHookResource(subIns); err != nil {
		return fmt.Errorf("failed to download from git source, err: %v", err)
	}

	//update the base Ansible job and append a generated job to the preHooks
	return a.addHookToRegisitry(subIns)
}

type SuffixFunc func(*subv1.Subscription) string

func suffixFromUUID(subIns *subv1.Subscription) string {
	return fmt.Sprintf("-%v", subIns.GetGeneration())
}

func (a *AnsibleHooks) registerHook(subIns *subv1.Subscription, hookFlag string, jobs []ansiblejob.AnsibleJob) error {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}

	if hookFlag == isPreHook {
		if a.registry[subKey].preHooks == nil {
			a.registry[subKey].preHooks = &JobInstances{}
		}

		a.registry[subKey].preHooks.registryJobs(subIns, jobs)

		return nil
	}

	if a.registry[subKey].postHooks == nil {
		a.registry[subKey].postHooks = &JobInstances{}
	}

	a.registry[subKey].postHooks.registryJobs(subIns, jobs)

	return nil
}

func (a *AnsibleHooks) addHookToRegisitry(subIns *subv1.Subscription) error {
	a.logger.V(2).Info("entry addNewHook subscription")
	defer a.logger.V(2).Info("exit addNewHook subscription")

	annotations := subIns.GetAnnotations()

	preHookPath, postHookPath := "", ""
	if annotations[appv1.AnnotationGithubPath] != "" {
		preHookPath = fmt.Sprintf("%v/prehook", annotations[appv1.AnnotationGithubPath])
		postHookPath = fmt.Sprintf("%v/posthook", annotations[appv1.AnnotationGithubPath])
	} else if annotations[appv1.AnnotationGitPath] != "" {
		preHookPath = fmt.Sprintf("%v/prehook", annotations[appv1.AnnotationGitPath])
		postHookPath = fmt.Sprintf("%v/posthook", annotations[appv1.AnnotationGitPath])
	}

	preJobs, err := a.gitClt.GetHooks(subIns, preHookPath)
	if err != nil {
		a.logger.Error(fmt.Errorf("prehook"), "failed to find hook:")
	}

	postJobs, err := a.gitClt.GetHooks(subIns, postHookPath)
	if err != nil {
		a.logger.Error(fmt.Errorf("posthook"), "failed to find hook:")
	}

	if len(preJobs) == 0 && len(postJobs) == 0 {
		return fmt.Errorf("failed to find any hook YAMLs of subscription %v", subIns.GetName())
	}

	if len(preJobs) != 0 {
		if err := a.registerHook(subIns, isPreHook, preJobs); err != nil {
			return err
		}
	}

	if len(postJobs) != 0 {
		if err := a.registerHook(subIns, isPostHook, postJobs); err != nil {
			return err
		}
	}

	return nil
}

func GetReferenceString(ref *corev1.ObjectReference) string {
	return ref.Name
}

//overrideAnsibleInstance adds the owner reference to job, and also reset the
//secret file of ansibleJob
func overrideAnsibleInstance(subIns *subv1.Subscription, job ansiblejob.AnsibleJob) ansiblejob.AnsibleJob {
	job.SetResourceVersion("")
	// avoid the error:
	// status.conditions.lastTransitionTime in body must be of type string: \"null\""
	job.Status = ansiblejob.AnsibleJobStatus{}

	if subIns.Spec.HookSecretRef != nil {
		job.Spec.TowerAuthSecretName = GetReferenceString(subIns.Spec.HookSecretRef)
	}

	//make sure all the ansiblejob is deployed at the subscription namespace
	job.SetNamespace(subIns.GetNamespace())

	//set owerreferce
	setOwnerReferences(subIns, &job)

	return job
}

func setOwnerReferences(owner *subv1.Subscription, obj metav1.Object) {
	obj.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(owner, owner.GetObjectKind().GroupVersionKind())})
}

func (a *AnsibleHooks) isRegistered(subKey types.NamespacedName) bool {
	return a.registry[subKey] != nil
}

func (a *AnsibleHooks) ApplyPreHooks(subKey types.NamespacedName) error {
	a.logger.WithName(subKey.String()).V(DebugLog).Info("entry ApplyPreHook")
	defer a.logger.WithName(subKey.String()).V(DebugLog).Info("exit ApplyPreHook")

	if !a.isRegistered(subKey) {
		a.logger.V(DebugLog).Info(fmt.Sprintf("there's not prehook registered for %v", subKey.String()))
		return nil
	}

	hks := a.registry[subKey].preHooks
	if hks == nil || len(*hks) == 0 {
		return nil
	}

	return hks.applyJobs(a.clt, a.suffixFunc, a.registry[subKey].lastSub)
}

func (a *AnsibleHooks) isUpdateSubscription(subIns *subv1.Subscription) bool {
	subKey := types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}
	record, ok := a.registry[subKey]

	return !ok || utils.IsSubscriptionChanged(record.lastSub, subIns)
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

func (a *AnsibleHooks) ApplyPostHooks(subKey types.NamespacedName) error {
	if !a.isRegistered(subKey) {
		a.logger.V(DebugLog).Info(fmt.Sprintf("there's not posthook registered for %v", subKey.String()))
		return nil
	}

	//wait till the subscription is propagated
	f, err := a.IsSubscriptionCompleted(subKey)
	if !f || err != nil {
		return fmt.Errorf("failed to apply post hook, subscription isCompleted %v", f)
	}

	hks := a.registry[subKey].postHooks

	if hks == nil || len(*hks) == 0 {
		return nil
	}

	return hks.applyJobs(a.clt, a.suffixFunc, a.registry[subKey].lastSub)
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
	logger.V(3).Info(fmt.Sprintf("job status: %v", curStatus))

	return strings.EqualFold(curStatus, JobCompleted)
}

// IIsSubscriptionCompleted will check:
// a, if the subscription itself is processed
// b, for each of the subscription created on managed cluster, it will check if
// it is 1, propagated and 2, subscribed
func (a *AnsibleHooks) IsSubscriptionCompleted(subKey types.NamespacedName) (bool, error) {
	subIns := &subv1.Subscription{}
	if err := a.clt.Get(context.TODO(), subKey, subIns); err != nil {
		return false, err
	}

	//check up the hub cluster status
	if subIns.Status.Phase != subv1.SubscriptionPropagated {
		return false, nil
	}

	managedStatus := subIns.Status.Statuses
	if len(managedStatus) == 0 {
		return true, nil
	}

	for cluster, cSt := range managedStatus {
		if len(cSt.SubscriptionPackageStatus) == 0 {
			continue
		}

		for pkg, pSt := range cSt.SubscriptionPackageStatus {
			if pSt.Phase != subv1.SubscriptionSubscribed {
				a.logger.Error(fmt.Errorf("cluster %s package %s is at status %s", cluster, pkg, pSt.Phase),
					"subscription is not completed")
				return false, nil
			}
		}
	}

	return true, nil
}
