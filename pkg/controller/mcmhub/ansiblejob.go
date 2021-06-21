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
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	ansiblejob "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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

func (jIns *JobInstances) registryJobs(gClt GitOps, subIns *subv1.Subscription,
	suffixFunc SuffixFunc, jobs []ansiblejob.AnsibleJob, kubeclient client.Client,
	logger logr.Logger, placementDecisionUpdated bool, placementRuleRv string, hookType string,
	commitIDChanged bool) error {
	logger.Info(fmt.Sprintf("IN REGISTRYJOBS, placementDecisionUpdated = %v, commitIDChanged = %v", placementDecisionUpdated, commitIDChanged))

	for _, job := range jobs {
		logger.Info("REGISTERING " + job.GetNamespace() + "/" + job.GetName())
		jobKey := types.NamespacedName{Name: job.GetName(), Namespace: job.GetNamespace()}
		ins, err := overrideAnsibleInstance(subIns, job, kubeclient, logger, hookType)

		if err != nil {
			logger.Info("ERROR: " + err.Error())
			return err
		}

		if _, ok := (*jIns)[jobKey]; !ok {
			(*jIns)[jobKey] = &Job{
				mux:         sync.Mutex{},
				InstanceSet: make(map[types.NamespacedName]struct{}),
			}
		}

		nx := ins.DeepCopy()
		suffix := suffixFunc(gClt, subIns)

		logger.Info("SUFFIX = " + suffix)

		if suffix == "" {
			logger.Info("EMPTY SUFFIX")
			continue
		}

		if nx.Spec.ExtraVars == nil {
			logger.Info("EMPTY ExtraVars")
			// No ExtraVars, skip
			continue
		}

		jobRecords := (*jIns)[jobKey]
		jobRecords.mux.Lock()
		jobRecords.Original = ins

		if placementDecisionUpdated && len(jobRecords.Instance) != 0 {
			plrSuffixFunc := func() string {
				return fmt.Sprintf("-%v-%v", subIns.GetGeneration(), placementRuleRv)
			}

			suffix = plrSuffixFunc()

			logger.Info("placementDecisionUpdated suffix is: " + suffix)
		}

		// OK, I might get a new suffix or the same suffix. How the hell do I figure out
		// if I should use this suffix or timestamp?
		//   - suffix
		//   - timestamp

		nx.SetName(fmt.Sprintf("%s%s", nx.GetName(), suffix))

		// The key name is the job name + suffix.
		// The suffix can be commit id or placement rule resource version.
		// So the same job can have multiple key names -> multiple jobRecords.InstanceSet[nxKey].
		// Why multiple jobRecords.InstanceSet?
		nxKey := types.NamespacedName{Name: nx.GetName(), Namespace: nx.GetNamespace()}

		logger.Info("nxKeyWithCommitHash = " + nxKey.String())

		_, jobWithCommitHashAlreadyExists := jobRecords.InstanceSet[nxKey]

		jobWithSyncTimeHashAlreadyExists := false

		syncTimeSuffix := getSyncTimeHash(subIns.GetAnnotations()[subv1.AnnotationManualReconcileTime])

		if syncTimeSuffix != "" && jobWithCommitHashAlreadyExists {

			nxKeyWithSyncTime := types.NamespacedName{Name: fmt.Sprintf("%s%s", ins.GetName(), fmt.Sprintf("-%v-%v", subIns.GetGeneration(), syncTimeSuffix)), Namespace: nx.GetNamespace()}

			logger.Info("nxKeyWithSyncTime = " + nxKeyWithSyncTime.String())

			_, jobWithSyncTimeHashAlreadyExists = jobRecords.InstanceSet[nxKeyWithSyncTime]

			nxKey = nxKeyWithSyncTime

			nx.SetName(fmt.Sprintf("%s%s", ins.GetName(), fmt.Sprintf("-%v-%v", subIns.GetGeneration(), syncTimeSuffix)))
		}

		// jobRecords.InstanceSet[nxKey] is to prevent creating the same ansibleJob CR with the same name.
		// jobRecords.Instance is an array of ansibleJob CRs that have been created so far.
		if !jobWithCommitHashAlreadyExists || !jobWithSyncTimeHashAlreadyExists {
			// If there is no instance set,
			logger.Info("there is no jobRecords.InstanceSet for " + nxKey.String())

			jobRecordsInstancePopulated := len(jobRecords.Instance) > 0

			if jobRecordsInstancePopulated {
				if placementDecisionUpdated {
					// If placement decision is updated, then see if the previously run ansible job
					// has the same target cluster. If so, skip creating a new ansible job. Otherwise,
					// re-create the ansible job since the target clusters are different now.
					lastJob := jobRecords.Instance[len(jobRecords.Instance)-1]

					// No need to check this because an ansiblejob will not be created if Spec.ExtraVars == nil.
					//if nx.Spec.ExtraVars != nil && lastJob.Spec.ExtraVars != nil {
					jobDoneOrRunning := isJobDoneOrRunning(lastJob, logger)

					if jobDoneOrRunning {
						jobMap := make(map[string]interface{})
						lastJobMap := make(map[string]interface{})

						err := json.Unmarshal(nx.Spec.ExtraVars, &jobMap)
						if err != nil {
							jobRecords.mux.Unlock()

							return err
						}

						err = json.Unmarshal(lastJob.Spec.ExtraVars, &lastJobMap)
						if err != nil {
							jobRecords.mux.Unlock()

							return err
						}

						targetClusters := jobMap["target_clusters"]
						lastJobTargetClusters := lastJobMap["target_clusters"]

						if reflect.DeepEqual(targetClusters, lastJobTargetClusters) {
							logger.Info("Both last and new ansible job target cluster list are equal")
							jobRecords.mux.Unlock()

							continue
						}
					}
				} else if commitIDChanged {
					// Commit ID has changed. Re-run all pre/post ansible jobs
					logger.Info("Skipping duplicated AnsibleJob creation check because commit ID has changed")
				} else {
					// Commit ID hasn't changed and placement decision hasn't been updated. Don't create ansible job.
					logger.Info("Commit ID and placement decision are the same.")
					continue
				}
			} else {
				// If there is no jobRecordsInstance, this is the first time to create this ansiblejob CR. Just create it.
				logger.Info("Skipping duplicated AnsibleJob creation check because no job has been created yet")
			}

			jobRecords.InstanceSet[nxKey] = struct{}{}

			logger.Info(fmt.Sprintf("registered ansiblejob %s", nxKey))

			jobRecords.Instance = append(jobRecords.Instance, *nx)
		} else {
			logger.Info("THERE IS jobRecords.InstanceSet for " + nxKey.String())

			// Here check the timestamp

		}

		jobRecords.mux.Unlock()
	}

	return nil
}

func getSyncTimeHash(syncTimeAnnotation string) string {
	if syncTimeAnnotation == "" {
		return ""
	} else {
		h := sha1.New()
		h.Write([]byte(syncTimeAnnotation))
		sha1_hash := hex.EncodeToString(h.Sum(nil))

		return sha1_hash[:6]
	}
}

// applyjobs will get the original job and create a instance, the applied
// instance will is put into the job.Instance array upon the success of the
// creation
func (jIns *JobInstances) applyJobs(clt client.Client, subIns *subv1.Subscription, logger logr.Logger) error {
	if utils.IsSubscriptionBeDeleted(clt, types.NamespacedName{Name: subIns.GetName(), Namespace: subIns.GetNamespace()}) {
		return nil
	}

	logger.Info("I AM IN APPLY JOBS")

	for k, j := range *jIns {
		logger.Info("I AM IN APPLY JOBS LOOP LOOP")

		if len(j.Instance) == 0 {
			logger.Info("NO INSTANCE")
			continue
		}

		j.mux.Lock()

		n := len(j.Instance)

		nx := j.Instance[n-1]

		j.mux.Unlock()

		//add the created job to the ansiblejob Set if not exist

		job := &ansiblejob.AnsibleJob{}
		jKey := types.NamespacedName{Name: nx.GetName(), Namespace: nx.GetNamespace()}

		if err := clt.Get(context.TODO(), jKey, job); err != nil {
			if !kerr.IsNotFound(err) {
				return fmt.Errorf("failed to get job %v, err: %v", jKey, err)
			}

			if err := clt.Create(context.TODO(), &nx); err != nil {
				if !kerr.IsAlreadyExists(err) {
					return fmt.Errorf("failed to apply job %v, err: %v", k.String(), err)
				}
			}

			logger.Info(fmt.Sprintf("applied ansiblejob %s/%s", nx.GetNamespace(), nx.GetName()))
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
		// it might not be created by the k8s side yet
		if kerr.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	if isJobRunSuccessful(job, logger) {
		return true, nil
	}

	return false, nil
}

// Check if last job is running or already done
// The last job could have not been created in k8s. e.g. posthook job will be created only after prehook jobs
// and main subscription are done. But the posthook jobs have been created in memory ansible job list.
func isJobDoneOrRunning(lastJob ansiblejob.AnsibleJob, logger logr.Logger) bool {
	job := &lastJob

	if job == nil {
		return false
	}

	if isJobRunning(job, logger) {
		return true
	}

	if isJobRunSuccessful(job, logger) {
		return true
	}

	return false
}

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

//merge multiple hook string
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
