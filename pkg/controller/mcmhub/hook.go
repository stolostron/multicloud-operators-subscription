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
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	job "github.com/open-cluster-management/ansiblejob-go-lib/api/v1alpha1"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	JobCompleted   = "successful"
	AnsibleJobKind = "AnsibleJob"
)

var (
	preAnnotation  = subv1.SchemeGroupVersion.Group + "pre-ansible"
	postAnnotation = subv1.SchemeGroupVersion.Group + "post-ansible"
)

type HookProcessor interface {
	// register subsription to the HookProcessor
	RegisterSubscription(types.NamespacedName) error
	DegisterSubscription(types.NamespacedName) error
	//GetPreHook returns a type.NamespacedName of the preHook
	GetPreHook(types.NamespacedName) (types.NamespacedName, error)
	IsPreHookCompleted(types.NamespacedName) (bool, error)
	GetPostHook(types.NamespacedName) (types.NamespacedName, error)
	IsPostHookCompleted(types.NamespacedName) (bool, error)
	IsSubscriptionCompleted(types.NamespacedName) (bool, error)
}

type Hooks struct {
	pre       *job.AnsibleJob
	post      *job.AnsibleJob
	preHooks  []*job.AnsibleJob
	postHooks []*job.AnsibleJob
	lastSub   *subv1.Subscription
}

type AnsibleHooks struct {
	clt client.Client
	// subscription namespacedName will points to hooks
	registry map[types.NamespacedName]*Hooks
	//logger
	logger logr.Logger
}

var _ HookProcessor = (*AnsibleHooks)(nil)

func NewAnsibleHooks(clt client.Client, logger logr.Logger) *AnsibleHooks {
	if logger == nil {
		logger = ctrlLog.Log.WithValues("ansiblehook")
	}
	return &AnsibleHooks{
		clt:      clt,
		registry: map[types.NamespacedName]*Hooks{},
		logger:   logger,
	}
}

func (a *AnsibleHooks) DegisterSubscription(subKey types.NamespacedName) error {
	delete(a.registry, subKey)
	return nil
}

func (a *AnsibleHooks) RegisterSubscription(subKey types.NamespacedName) error {
	a.logger.Info("entry register subscription")
	defer a.logger.Info("exit register subscription")

	subIns := &subv1.Subscription{}
	if err := a.clt.Get(context.TODO(), subKey, subIns); err != nil {
		// subscription is deleted
		if kerr.IsNotFound(err) {
			return nil
		}

		return err
	}

	subAn := subIns.GetAnnotations()
	//subsription doesn't have preHook defined
	if len(subAn) == 0 {
		return nil
	}

	preHook := subAn[preAnnotation]
	postHook := subAn[postAnnotation]

	if len(preHook) == 0 && len(postHook) == 0 {
		return nil
	}

	if !a.isUpdateSubscription(subKey) {
		return nil
	}

	chn := &chnv1.Channel{}
	chnkey := utils.NamespacedNameFormat(subIns.Spec.Channel)
	if err := a.clt.Get(context.TODO(), chnkey, chn); err != nil {
		return err
	}

	jobs, err := DonwloadAnsibleJobFromGit(a.clt, chn, subIns, a.logger)
	if err != nil {
		return err
	}

	//update the base Ansible job and append a generated job to the preHooks
	return a.addNewHook(subKey, subIns, preHook, postHook, jobs)
}

func (a *AnsibleHooks) addNewHook(subKey types.NamespacedName, subIns *subv1.Subscription, preHook, postHook string, jobs []*job.AnsibleJob) error {
	a.logger.V(2).Info("entry addNewHook subscription")
	defer a.logger.V(2).Info("exit addNewHook subscription")

	pre := &job.AnsibleJob{}
	post := &job.AnsibleJob{}
	a.registry[subKey] = &Hooks{
		lastSub:   subIns,
		pre:       pre,
		post:      post,
		preHooks:  []*job.AnsibleJob{},
		postHooks: []*job.AnsibleJob{},
	}

	suffix := subIns.GetUID()[:5]
	for _, job := range jobs {
		jobKey := types.NamespacedName{Name: job.GetName(), Namespace: job.GetNamespace()}
		if strings.EqualFold(jobKey.String(), preHook) {
			a.registry[subKey].pre = job

			ins := job.DeepCopy()
			ins.SetName(fmt.Sprintf("%s-%s", ins.GetName(), suffix))
			a.registry[subKey].preHooks = append(a.registry[subKey].preHooks, ins)
		}

		if strings.EqualFold(jobKey.String(), postHook) {
			a.registry[subKey].post = job

			ins := job.DeepCopy()
			ins.SetName(fmt.Sprintf("%s-%s", ins.GetName(), suffix))
			a.registry[subKey].postHooks = append(a.registry[subKey].postHooks, ins)
		}
	}

	return nil
}

func (a *AnsibleHooks) GetPreHook(subKey types.NamespacedName) (types.NamespacedName, error) {
	hks, ok := a.registry[subKey]
	if ok {

		t := hks.preHooks[len(hks.preHooks)-1]
		return types.NamespacedName{Name: t.GetName(), Namespace: t.GetNamespace()}, nil
	}

	return types.NamespacedName{}, nil
}

func (a *AnsibleHooks) isUpdateSubscription(subKey types.NamespacedName) bool {
	return false
}

func (a *AnsibleHooks) IsPreHookCompleted(preKey types.NamespacedName) (bool, error) {
	return a.isJobDone(preKey)
}

func (a *AnsibleHooks) GetPostHook(subKey types.NamespacedName) (types.NamespacedName, error) {
	hks, ok := a.registry[subKey]
	if ok {

		t := hks.postHooks[len(hks.postHooks)-1]
		return types.NamespacedName{Name: t.GetName(), Namespace: t.GetNamespace()}, nil
	}

	return types.NamespacedName{}, nil
}

func (a *AnsibleHooks) IsPostHookCompleted(postKey types.NamespacedName) (bool, error) {
	return a.isJobDone(postKey)
}

func (a *AnsibleHooks) isJobDone(key types.NamespacedName) (bool, error) {
	job := &job.AnsibleJob{}

	if err := a.clt.Get(context.TODO(), key, job); err != nil {
		return false, err
	}

	if strings.EqualFold(job.Status.AnsibleJobResult.Status, JobCompleted) {
		return true, nil
	}

	return false, nil
}

func (a *AnsibleHooks) IsSubscriptionCompleted(subKey types.NamespacedName) (bool, error) {
	subIns := &subv1.Subscription{}
	if err := a.clt.Get(context.TODO(), subKey, subIns); err != nil {
		return false, err
	}

	if subIns.Status.Phase != subv1.SubscriptionPropagated {
		return false, nil
	}

	return true, nil
}

// get channel
func DonwloadAnsibleJobFromGit(clt client.Client, chn *chnv1.Channel, sub *subv1.Subscription, logger logr.Logger) ([]*job.AnsibleJob, error) {
	repoRoot := utils.GetLocalGitFolder(chn, sub)
	_, err := cloneGitRepo(clt, repoRoot, chn, sub)
	if err != nil {
		return []*job.AnsibleJob{}, err
	}

	kubeRes, err := sortClonedGitRepo(repoRoot, sub, logger)
	if err != nil {
		return []*job.AnsibleJob{}, err
	}

	return ParseAnsibleJobs(kubeRes, ParseAnsibleJobResoures)
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
		logger.Error(err, "Failed to sort kubernetes resources and helm charts.")
		return []string{}, err
	}

	return kubeRes, nil
}

func ParseAnsibleJobResoures(file []byte) [][]byte {
	var ret [][]byte

	items := strings.Split(string(file), "---")

	for _, i := range items {
		item := []byte(strings.Trim(i, "\t \n"))

		t := kubeResource{}
		err := yaml.Unmarshal(item, &t)

		if err != nil {
			// Ignore item that cannot be unmarshalled..
			klog.Warning(err, "Failed to unmarshal YAML content")
			continue
		}

		if t.APIVersion == "" || t.Kind == "" || !strings.EqualFold(t.Kind, AnsibleJobKind) {
			continue
		}

		ret = append(ret, item)
	}

	return ret
}

func ParseAnsibleJobs(rscFiles []string, paser func([]byte) [][]byte) ([]*job.AnsibleJob, error) {
	jobs := []*job.AnsibleJob{}
	// sync kube resource deployables
	for _, rscFile := range rscFiles {
		file, err := ioutil.ReadFile(rscFile) // #nosec G304 rscFile is not user input

		if err != nil {
			return []*job.AnsibleJob{}, err
		}

		resources := paser(file)

		if len(resources) > 0 {
			for _, resource := range resources {
				job := &job.AnsibleJob{}
				err := yaml.Unmarshal(resource, job)

				if err != nil {
					continue
				}

				jobs = append(jobs, job)
			}
		}
	}

	return jobs, nil
}
