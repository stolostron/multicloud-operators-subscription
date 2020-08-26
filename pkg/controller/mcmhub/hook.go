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
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/klogr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	JobCompleted      = "successful"
	AnsibleJobKind    = "AnsibleJob"
	AnsibleJobVersion = "tower.ansible.com/v1alpha1"
	Status            = "status"
	AnsibleJobResult  = "ansiblejobresult"
)

var (
	preAnnotation  = subv1.SchemeGroupVersion.Group + "/pre-ansible"
	postAnnotation = subv1.SchemeGroupVersion.Group + "/post-ansible"
)

//HOHookProcessor tracks the pre and post hook informantion of subscriptions.

type HookProcessor interface {
	// register subsription to the HookProcessor
	RegisterSubscription(types.NamespacedName) error
	DegisterSubscription(types.NamespacedName) error
	//ApplyPreHook returns a type.NamespacedName of the preHook
	ApplyPreHook(types.NamespacedName) (types.NamespacedName, error)
	IsPreHookCompleted(types.NamespacedName) (bool, error)
	ApplyPostHook(types.NamespacedName) (types.NamespacedName, error)
	IsPostHookCompleted(types.NamespacedName) (bool, error)
	IsSubscriptionCompleted(types.NamespacedName) (bool, error)
}

type Hooks struct {
	pre  unstructured.Unstructured
	post unstructured.Unstructured
	//store all the applied prehook instance
	preHooks  []unstructured.Unstructured
	postHooks []unstructured.Unstructured

	//store last subscription instance used for the hook operation
	lastSub *subv1.Subscription
}

type AnsibleHooks struct {
	gitClt GitOps
	clt    client.Client
	// subscription namespacedName will points to hooks
	registry map[types.NamespacedName]*Hooks
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
		clt:      clt,
		gitClt:   NewHookGit(clt, logger),
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

	preHook := subIns.Spec.Prehook
	postHook := subIns.Spec.Posthook

	if len(preHook) == 0 && len(postHook) == 0 {
		return nil
	}

	if !a.isUpdateSubscription(subKey) {
		return nil
	}

	jobs, err := a.gitClt.DownloadHookResource(subIns)

	if err != nil {
		return err
	}

	printJobs(jobs, a.logger)
	//update the base Ansible job and append a generated job to the preHooks
	return a.addNewHook(subKey, subIns, preHook, postHook, jobs)
}

func printJobs(jobs []unstructured.Unstructured, logger logr.Logger) {
	for _, job := range jobs {
		logger.V(3).Info(fmt.Sprintf("download jobs %#v", job))
	}
}

func (a *AnsibleHooks) addNewHook(subKey types.NamespacedName, subIns *subv1.Subscription, preHook, postHook string, jobs []unstructured.Unstructured) error {
	a.logger.V(2).Info("entry addNewHook subscription")
	defer a.logger.V(2).Info("exit addNewHook subscription")

	a.registry[subKey] = &Hooks{
		pre:       unstructured.Unstructured{},
		post:      unstructured.Unstructured{},
		lastSub:   subIns,
		preHooks:  []unstructured.Unstructured{},
		postHooks: []unstructured.Unstructured{},
	}

	suffix := subIns.GetUID()[:5]
	for _, job := range jobs {
		jobKey := types.NamespacedName{Name: job.GetName(), Namespace: job.GetNamespace()}
		//need to skip the status, otherwise the creation will fail
		if strings.EqualFold(jobKey.String(), preHook) {
			a.registry[subKey].pre = job
			ins := a.registry[subKey].pre.DeepCopy()
			ins.SetName(fmt.Sprintf("%s-%s", ins.GetName(), suffix))
			a.registry[subKey].preHooks = append(a.registry[subKey].preHooks, *ins)
		}

		if strings.EqualFold(jobKey.String(), postHook) {
			a.registry[subKey].post = job
			ins := a.registry[subKey].post.DeepCopy()
			ins.SetName(fmt.Sprintf("%s-%s", ins.GetName(), suffix))
			a.registry[subKey].postHooks = append(a.registry[subKey].postHooks, *ins)
		}
	}

	return nil
}

func (a *AnsibleHooks) ApplyPreHook(subKey types.NamespacedName) (types.NamespacedName, error) {
	a.logger.WithName(subKey.String()).V(2).Info("entry ApplyPreHook")
	defer a.logger.WithName(subKey.String()).V(2).Info("exit ApplyPreHook")
	hks, ok := a.registry[subKey]
	if ok && len(hks.preHooks) != 0 {
		t := &hks.preHooks[len(hks.preHooks)-1]

		if err := a.clt.Create(context.TODO(), t); err != nil {
			if !k8serrors.IsAlreadyExists(err) {
				return types.NamespacedName{}, err
			}
		}

		return types.NamespacedName{Name: t.GetName(), Namespace: t.GetNamespace()}, nil
	}

	return types.NamespacedName{}, nil
}

func (a *AnsibleHooks) isUpdateSubscription(subKey types.NamespacedName) bool {
	return true
}

func (a *AnsibleHooks) IsPreHookCompleted(preKey types.NamespacedName) (bool, error) {
	return a.isJobDone(preKey)
}

func (a *AnsibleHooks) ApplyPostHook(subKey types.NamespacedName) (types.NamespacedName, error) {
	hks, ok := a.registry[subKey]
	if ok && len(hks.postHooks) != 0 {
		t := &hks.postHooks[len(hks.postHooks)-1]
		if err := a.clt.Create(context.TODO(), t); err != nil {
			return types.NamespacedName{}, err
		}

		return types.NamespacedName{Name: t.GetName(), Namespace: t.GetNamespace()}, nil
	}

	return types.NamespacedName{}, nil
}

func (a *AnsibleHooks) IsPostHookCompleted(postKey types.NamespacedName) (bool, error) {
	return a.isJobDone(postKey)
}

func (a *AnsibleHooks) isJobDone(key types.NamespacedName) (bool, error) {
	a.logger.WithName(key.String()).V(2).Info("entry isJobDone")
	defer a.logger.WithName(key.String()).V(2).Info("exit isJobDone")

	job := &ansiblejob.AnsibleJob{}

	if err := a.clt.Get(context.TODO(), key, job); err != nil {
		return false, err
	}

	if isJobRunSuccessful(job, a.logger) {
		return true, nil
	}

	return false, nil
}

func isJobRunSuccessful(job *ansiblejob.AnsibleJob, logger logr.Logger) bool {
	curStatus := job.Status.AnsibleJobResult.Status
	logger.V(3).Info(fmt.Sprintf("job status: %v", curStatus))

	return strings.EqualFold(curStatus, JobCompleted)
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
