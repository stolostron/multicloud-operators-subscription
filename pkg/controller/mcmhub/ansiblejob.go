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
	"crypto/sha1" // #nosec G505 Used only to convert sync time string to a hash
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ansiblejob "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/ansible/v1alpha1"
	subv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Job struct {
	mux sync.Mutex

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

func findLastAnsibleJob(clt client.Client, subIns *subv1.Subscription, hookType string, jobKey types.NamespacedName) (*ansiblejob.AnsibleJob, error) {
	// List all AnsibleJob resources under the appsub NS
	ansibleJobList := &ansiblejob.AnsibleJobList{}

	err := clt.List(context.TODO(), ansibleJobList, &client.ListOptions{
		Namespace: subIns.Namespace,
	})

	if err != nil {
		klog.Infof("failed to list ansible jobs. Namespace: %v, err: %v", subIns.Namespace, err)
		return nil, err
	}

	// the list is sorted by CreationTimestamp desc, ansibleJobList.Items[0] is the ansible job applied lastly
	sort.Slice(ansibleJobList.Items, func(i, j int) bool {
		return ansibleJobList.Items[i].ObjectMeta.CreationTimestamp.Time.After(ansibleJobList.Items[j].ObjectMeta.CreationTimestamp.Time)
	})

	klog.Infof("total prehook/posthook ansible jobs num: %v", len(ansibleJobList.Items))

	for i := 0; i < len(ansibleJobList.Items); i++ {
		hostingAppsub, ok := ansibleJobList.Items[i].Annotations[subv1.AnnotationHosting]

		if !ok {
			continue
		}

		if hostingAppsub != subIns.Namespace+"/"+subIns.Name {
			continue
		}

		curHookType, ok := ansibleJobList.Items[i].Annotations[subv1.AnnotationHookType]

		if !ok {
			continue
		}

		if curHookType != hookType {
			continue
		}

		hookTpl, ok := ansibleJobList.Items[i].Annotations[subv1.AnnotationHookTemplate]

		if !ok {
			continue
		}

		if hookTpl != jobKey.String() {
			continue
		}

		lastAnsibleJob := ansibleJobList.Items[i].DeepCopy()

		klog.Infof("last ansible job: %v/%v, hookType: %v, hookTemplate: %v", lastAnsibleJob.Namespace, lastAnsibleJob.Name, hookType, jobKey.String())

		return lastAnsibleJob, nil
	}

	return nil, nil
}

func isEqualClusterList(logger logr.Logger, lastAnsibleJob, newAnsibleJob *ansiblejob.AnsibleJob) (bool, error) {
	if lastAnsibleJob == nil || lastAnsibleJob.Spec.ExtraVars == nil {
		return false, nil
	}

	newJobMap := make(map[string]interface{})
	lastJobMap := make(map[string]interface{})

	err := json.Unmarshal(newAnsibleJob.Spec.ExtraVars, &newJobMap)
	if err != nil {
		return false, err
	}

	err = json.Unmarshal(lastAnsibleJob.Spec.ExtraVars, &lastJobMap)
	if err != nil {
		return false, err
	}

	targetClusters := newJobMap["target_clusters"]
	lastJobTargetClusters := lastJobMap["target_clusters"]

	if reflect.DeepEqual(targetClusters, lastJobTargetClusters) {
		logger.Info("Both last and new ansible job target cluster list are equal")
		return true, nil
	}

	logger.Info("Both last and new ansible job target cluster list are NOT equal")

	return false, nil
}

// register single prehook/posthook ansible job
func (jIns *JobInstances) registryAnsibleJob(clt client.Client, logger logr.Logger, subIns *subv1.Subscription,
	jobKey types.NamespacedName, newAnsibleJob *ansiblejob.AnsibleJob, hookType string) {
	jobRecords := (*jIns)[jobKey]

	if jobRecords == nil {
		klog.Infof("invalid ansible job key: %v", jobKey)
		return
	}

	// if there is appsub manual sync, rename the new ansible job
	syncTimeSuffix := getSyncTimeHash(subIns.GetAnnotations()[subv1.AnnotationManualReconcileTime])

	// reset the ansible job instance list
	jobRecords.Instance = []ansiblejob.AnsibleJob{}
	jobRecords.Instance = append(jobRecords.Instance, ansiblejob.AnsibleJob{})

	// 1. reload the last existing ansibleJob as the last pre/post hook ansible job
	lastAnsibleJob, err := findLastAnsibleJob(clt, subIns, hookType, jobKey)
	if err != nil {
		return
	}

	// 2. if there is no last ansible job found, register a new one
	if lastAnsibleJob == nil {
		klog.Infof("register a new ansible job as there is no existing ansible job found. ansilbe job: %v/%v, hookType: %v, hookTemplate: %v",
			newAnsibleJob.Namespace, newAnsibleJob.Name, hookType, jobKey.String())

		jobRecords.Instance[0] = *newAnsibleJob

		return
	}

	// 3. if last ansible job is found and it is not complete yet, register the same last ansible job
	if !isJobRunSuccessful(lastAnsibleJob, logger) {
		klog.Infof("skip the job registration as the last ansible job is still running. ansilbe job: %v/%v, status: %v, hookType: %v, hookTemplate: %v",
			lastAnsibleJob.Namespace, lastAnsibleJob.Name, lastAnsibleJob.Status.AnsibleJobResult.Status, hookType, jobKey.String())

		jobRecords.Instance[0] = *lastAnsibleJob

		return
	}

	// 4. if the new ansible job name remains the same as the last done one, register the same last ansible job
	if lastAnsibleJob.Name == newAnsibleJob.Name {
		klog.Infof("skip the job registration as the ansible job name remains the same. ansilbe job: %v/%v, status: %v, hookType: %v, hookTemplate: %v",
			lastAnsibleJob.Namespace, lastAnsibleJob.Name, lastAnsibleJob.Status.AnsibleJobResult.Status, hookType, jobKey.String())

		jobRecords.Instance[0] = *lastAnsibleJob

		return
	}

	// 5. if there is appsub manual sync, register a new ansible job since the last ansible job is done
	if syncTimeSuffix != "" && lastAnsibleJob.Name != newAnsibleJob.Name {
		klog.Infof("register a new ansible job as the last ansible job is done and there is a new manual sync."+
			"ansilbe job: %v/%v, status: %v, hookType: %v, hookTemplate: %v",
			newAnsibleJob.Namespace, newAnsibleJob.Name, newAnsibleJob.Status.AnsibleJobResult.Status, hookType, jobKey.String())

		jobRecords.Instance[0] = *newAnsibleJob

		return
	}

	equalClusterList, err := isEqualClusterList(logger, lastAnsibleJob, newAnsibleJob)
	if err != nil {
		klog.Infof("failed to compare cluster list. err: %v", err)

		jobRecords.Instance[0] = *lastAnsibleJob

		return
	}

	// 6. if there is change in the cluster decision list, register a new ansible job since the last ansible job is done
	if !equalClusterList {
		klog.Infof("register a new ansible job as the last ansible job is done and the cluster decision list changed."+
			"ansilbe job: %v/%v, status: %v, hookType: %v, hookTemplate: %v",
			newAnsibleJob.Namespace, newAnsibleJob.Name, newAnsibleJob.Status.AnsibleJobResult.Status, hookType, jobKey.String())

		jobRecords.Instance[0] = *newAnsibleJob

		return
	}

	// 7. if there is no change in the cluster decision list, still register the last DONE ansible job
	klog.Infof("register the last Done ansible job as there is no change in the cluster list. ansilbe job: %v/%v, status: %v, hookType: %v, hookTemplate: %v",
		lastAnsibleJob.Namespace, lastAnsibleJob.Name, lastAnsibleJob.Status.AnsibleJobResult.Status, hookType, jobKey.String())

	jobRecords.Instance[0] = *lastAnsibleJob

	return
}

// jIns - the original ansible job templates fetched from the git repo, where
// key : appsub NS + ansilbeJob Name
// jIns[key].Instance: The actual ansible Jobs populated from original ansilbe job template
// jIns[key].InstanceSet: the actual ansible job namespaced name
// jobs - the original prehook/posthook ansible job templates from git repo
func (jIns *JobInstances) registryJobs(gClt GitOps, subIns *subv1.Subscription,
	suffixFunc SuffixFunc, jobs []ansiblejob.AnsibleJob, kubeclient client.Client,
	logger logr.Logger, placementDecisionUpdated bool, placementRuleRv string, hookType string,
	commitIDChanged bool) error {
	logger.Info(fmt.Sprintf("In registryJobs, placementDecisionUpdated = %v, commitIDChanged = %v", placementDecisionUpdated, commitIDChanged))

	for _, job := range jobs {
		ins, err := overrideAnsibleInstance(subIns, job, kubeclient, logger, hookType)

		if err != nil {
			return err
		}

		logger.Info("registering " + job.GetNamespace() + "/" + job.GetName())

		jobKey := types.NamespacedName{Name: job.GetName(), Namespace: job.GetNamespace()}

		if _, ok := (*jIns)[jobKey]; !ok {
			(*jIns)[jobKey] = &Job{
				mux:         sync.Mutex{},
				Instance:    []ansiblejob.AnsibleJob{},
				InstanceSet: make(map[types.NamespacedName]struct{}),
			}
		}

		nx := ins.DeepCopy()
		suffix := suffixFunc(gClt, subIns)

		if suffix == "" {
			continue
		}

		if nx.Spec.ExtraVars == nil {
			// No ExtraVars, skip
			continue
		}

		jobRecords := (*jIns)[jobKey]
		jobRecords.mux.Lock()
		jobRecords.Original = ins

		if placementDecisionUpdated {
			plrSuffixFunc := func() string {
				return fmt.Sprintf("-%v-%v", subIns.GetGeneration(), placementRuleRv)
			}

			suffix = plrSuffixFunc()

			logger.Info("placementDecisionUpdated suffix is: " + suffix)
		}

		syncTimeSuffix := getSyncTimeHash(subIns.GetAnnotations()[subv1.AnnotationManualReconcileTime])
		if syncTimeSuffix != "" {
			suffix = fmt.Sprintf("-%v-%v", subIns.GetGeneration(), syncTimeSuffix)
			logger.Info("manual sync suffix is: " + suffix)
		}

		nx.SetName(fmt.Sprintf("%s%s", nx.GetName(), suffix))

		// The suffix can be commit id or placement rule resource version or manu sync timestamp.
		// So the actual ansible job name could be the original anisble job template name with different suffix

		jIns.registryAnsibleJob(kubeclient, logger, subIns, jobKey, nx, hookType)

		jobRecords.mux.Unlock()
	}

	return nil
}

// Convert manual sync time string to a hash and use the first 6 chars
func getSyncTimeHash(syncTimeAnnotation string) string {
	if syncTimeAnnotation == "" {
		return ""
	}

	h := sha1.New() // #nosec G401 Used only to convert sync time string to a hash
	_, err := h.Write([]byte(syncTimeAnnotation))

	if err != nil {
		return ""
	}

	sha1Hash := hex.EncodeToString(h.Sum(nil))

	return sha1Hash[:6]
}

// applyjobs will get the original job and create a instance, the applied
// instance will is put into the job.Instance array upon the success of the
// creation
func (jIns *JobInstances) applyJobs(clt client.Client, subIns *subv1.Subscription, logger logr.Logger) error {
	if utils.IsSubscriptionBeDeleted(clt, types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}) {
		return nil
	}

	for _, j := range *jIns {
		if len(j.Instance) == 0 {
			continue
		}

		logger.Info("waiting for lock")
		j.mux.Lock()
		logger.Info("locked")

		n := len(j.Instance)
		if n < 1 {
			continue
		}

		nx := j.Instance[0]

		j.mux.Unlock()
		logger.Info("released lock")

		//add the created job to the ansiblejob Set if not exist

		job := &ansiblejob.AnsibleJob{}
		jKey := types.NamespacedName{Name: nx.GetName(), Namespace: nx.GetNamespace()}

		if err := clt.Get(context.TODO(), jKey, job); err != nil {
			if !kerr.IsNotFound(err) {
				return fmt.Errorf("failed to get job %v, err: %v", jKey, err)
			}

			if err := clt.Create(context.TODO(), &nx); err != nil {
				if !kerr.IsAlreadyExists(err) {
					return fmt.Errorf("failed to apply job %v, err: %v", jKey, err)
				}
			}

			logger.Info(fmt.Sprintf("applied ansiblejob %s/%s", nx.GetNamespace(), nx.GetName()))
		} else {
			logger.Info(fmt.Sprintf("no need to apply existing ansiblejob: %s/%s", nx.GetNamespace(), nx.GetName()))
		}
	}

	return nil
}

// check the last instance of the ansiblejobs to see if it's applied and
// completed or not
func (jIns *JobInstances) isJobsCompleted(clt client.Client, logger logr.Logger) (bool, error) {
	for _, job := range *jIns {
		n := len(job.Instance)
		if n == 0 {
			return true, nil
		}

		j := job.Instance[n-1]
		jKey := types.NamespacedName{Name: j.GetName(), Namespace: j.GetNamespace()}

		logger.Info(fmt.Sprintf("checking if %v job for completed or not", jKey.String()))

		if ok, err := isJobDone(clt, jKey, logger); err != nil || !ok {
			return ok, err
		}
	}

	return true, nil
}

func isJobDone(clt client.Client, key types.NamespacedName, logger logr.Logger) (bool, error) {
	job := &ansiblejob.AnsibleJob{}

	if err := clt.Get(context.TODO(), key, job); err != nil {
		// it might not be created by the k8s side yet
		if kerr.IsNotFound(err) {
			logger.Info(fmt.Sprintf("ansible job not found, job: %v, err: %v", key.String(), err))

			return false, nil
		}

		logger.Info(fmt.Sprintf("faild to get ansible job, job: %v, err: %v", key.String(), err))

		return false, err
	}

	if isJobRunSuccessful(job, logger) {
		logger.Info(fmt.Sprintf("ansible job done, job: %v", key.String()))
		return true, nil
	}

	logger.Info(fmt.Sprintf("ansible job NOT done, job: %v", key.String()))

	return false, nil
}

// Check if last job is running or already done
// The last job could have not been created in k8s. e.g. posthook job will be created only after prehook jobs
// and main subscription are done. But the posthook jobs have been created in memory ansible job list.

type FormatFunc func(ansiblejob.AnsibleJob) string

func ansiblestatusFormat(j ansiblejob.AnsibleJob) string {
	ns := j.GetNamespace()
	if len(ns) == 0 {
		ns = "default"
	}

	return fmt.Sprintf("%s/%s", ns, j.GetName())
}

func formatAnsibleFromTopo(j ansiblejob.AnsibleJob) string {
	ns := j.GetNamespace()
	if len(ns) == 0 {
		ns = "default"
	}

	return fmt.Sprintf("%v/%v/%v/%v/%v/%v", hookParent, "", AnsibleJobKind, ns, j.GetName(), 0)
}

func getJobsString(jobs []ansiblejob.AnsibleJob, format FormatFunc) []string {
	if len(jobs) == 0 {
		return []string{}
	}

	res := []string{}

	for _, j := range jobs {
		res = append(res, format(j))
	}

	return res
}

// merge multiple hook string
func (jIns *JobInstances) outputAppliedJobs(format FormatFunc) appliedJobs {
	res := appliedJobs{}

	lastApplied := []string{}
	lastAppliedJobs := []string{}

	for _, jobRecords := range *jIns {
		if len(jobRecords.Instance) == 0 {
			continue
		}

		jobRecords.mux.Lock()
		applied := getJobsString(jobRecords.Instance, format)

		n := len(applied)
		lastApplied = append(lastApplied, applied[n-1])
		lastAppliedJobs = append(lastAppliedJobs, applied...)
		jobRecords.mux.Unlock()
	}

	sep := ","
	res.lastApplied = strings.Join(lastApplied, sep)
	res.lastAppliedJobs = lastAppliedJobs

	return res
}
