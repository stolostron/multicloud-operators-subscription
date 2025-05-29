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

package helmrelease

import (
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	testutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	helmReleaseNS = "kube-system"
)

func TestReconcile(t *testing.T) {
	defer klog.Flush()

	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.

	t.Log("Create manager")

	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	g.Expect(err).NotTo(gomega.HaveOccurred())

	syncID := types.NamespacedName{
		Namespace: "default",
		Name:      "default",
	}
	err = kubesynchronizer.Add(mgr, cfg, &syncID, 0, true, true)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c := mgr.GetClient()

	rec := &ReconcileHelmRelease{
		mgr,
		nil,
		nil,
	}

	// Create a new controller
	_, err = controller.New("helmrelease-controller", mgr, controller.Options{Reconciler: rec})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	t.Log("Setup test reconcile")
	g.Expect(Add(mgr)).NotTo(gomega.HaveOccurred())

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	//
	//Github succeed
	//
	t.Log("Github succeed test")

	helmReleaseName := "example-github-succeed"
	helmReleaseKey := types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}
	instance := &appv1.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HelmRelease",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://github.com/stolostron/application-lifecycle-samples.git"},
					ChartPath: "mortgage-helm",
					Branch:    "main",
				},
			},
			ChartName: "mortgage-helm",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instanceResp := &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(instanceResp.Status.DeployedRelease).NotTo(gomega.BeNil())

	err = c.Delete(context.TODO(), instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	time.Sleep(10 * time.Second)

	//
	//Github failed
	//
	t.Log("Github failed test")

	helmReleaseName = "example-github-failed"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}
	instance = &appv1.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HelmRelease",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git"},
					ChartPath: "wrong path",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instanceResp = &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(instanceResp.Status.DeployedRelease).To(gomega.BeNil())

	err = c.Delete(context.TODO(), instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	time.Sleep(10 * time.Second)

	//
	//Git failed
	//
	t.Log("Git failed test")

	helmReleaseName = "example-git-failed"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}
	instance = &appv1.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HelmRelease",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitSourceType,
				Git: &appv1.Git{
					Urls:      []string{"https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git"},
					ChartPath: "wrong path",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instanceResp = &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(instanceResp.Status.DeployedRelease).To(gomega.BeNil())

	err = c.Delete(context.TODO(), instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	time.Sleep(10 * time.Second)

	//
	//Git alt source succeeded
	//
	t.Log("Git alt source succeeded test")

	helmReleaseName = "example-git-alt-source-succeeded"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}
	instance = &appv1.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HelmRelease",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitSourceType,
				Git: &appv1.Git{
					Urls:      []string{"https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git/wrongurl"},
					ChartPath: "testhr/github/nginx-chart",
					Branch:    "main",
				},
			},
			AltSource: &appv1.AltSource{
				SourceType: appv1.GitSourceType,
				Git: &appv1.Git{
					Urls:      []string{"https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git"},
					ChartPath: "testhr/github/nginx-chart",
					Branch:    "main",
				},
			},
			ChartName: "nginx-chart",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instanceResp = &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(instanceResp.Status.DeployedRelease).NotTo(gomega.BeNil())

	err = c.Delete(context.TODO(), instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	time.Sleep(10 * time.Second)

	//
	//helmRepo succeeds
	//
	t.Log("helmrepo succeed test")

	helmReleaseName = "example-helmrepo-succeed"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}
	instance = &appv1.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HelmRelease",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{
						"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/main/testhr/helmrepo/subscription-release-test-3-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instanceResp = &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(instanceResp.Status.DeployedRelease).NotTo(gomega.BeNil())

	annotations := make(map[string]string)
	annotations["helm.sdk.operatorframework.io/upgrade-force"] = "true"

	instanceResp.SetAnnotations(annotations)
	instanceResp.Repo.Version = "3-0.1.0"

	time.Sleep(10 * time.Second)

	err = c.Update(context.TODO(), instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instanceResp = &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(instanceResp.Status.DeployedRelease).NotTo(gomega.BeNil())
	g.Expect(instanceResp.Repo.Version).Should(gomega.Equal("3-0.1.0"))

	instanceResp.Repo.Source.HelmRepo.Urls[0] = "https://" + testutils.GetTestGitRepoURLFromEnvVar() + "-release-wrongurl"

	err = c.Delete(context.TODO(), instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	//
	//helmRepo failure
	//
	t.Log("helmRepo failure test")

	helmReleaseName = "example-helmrepo-failure"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}
	instance = &appv1.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HelmRelease",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/wrongurl"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instanceResp = &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(instanceResp.Status.DeployedRelease).To(gomega.BeNil())

	err = c.Delete(context.TODO(), instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	time.Sleep(10 * time.Second)

	//
	//helmRepo alt source succeeded
	//
	t.Log("helmRepo alt source succeeded test")

	helmReleaseName = "example-helmrepo-alt-source-succeeded"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}
	instance = &appv1.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HelmRelease",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/wrongurl"},
				},
			},
			AltSource: &appv1.AltSource{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/main/testhr/helmrepo/subscription-release-test-3-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instanceResp = &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(instanceResp.Status.DeployedRelease).NotTo(gomega.BeNil())

	err = c.Delete(context.TODO(), instanceResp)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	time.Sleep(10 * time.Second)

	//
	//Github succeed create-delete
	//
	t.Log("Github succeed create-delete test")

	helmReleaseName = "example-github-delete"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}
	instance = &appv1.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HelmRelease",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://github.com/stolostron/application-lifecycle-samples.git"},
					ChartPath: "mortgage-helm",
					Branch:    "main",
				},
			},
			ChartName: "mortgage-helm",
		},
	}

	//Creation
	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instanceRespCD := &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceRespCD)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	g.Expect(instanceRespCD.Status.DeployedRelease).NotTo(gomega.BeNil())

	//Deletion
	err = c.Delete(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(60 * time.Second)

	instanceRespDel := &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceRespDel)
	g.Expect(err).To(gomega.HaveOccurred())

	//
	//Github succeed create-update
	//
	t.Log("Github succeed create-update")

	helmReleaseName = "example-github-update"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}
	instance = &appv1.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HelmRelease",
			APIVersion: "apps.open-cluster-management.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://github.com/stolostron/application-lifecycle-samples.git"},
					ChartPath: "mortgage-helm",
					Branch:    "main",
				},
			},
			ChartName: "mortgage-helm",
		},
	}

	//Creation
	t.Log("Github succeed create-update -> CR create")

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	t.Log("Github succeed create-update -> CR get response")

	instanceRespCU := &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceRespCU)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	g.Expect(instanceRespCU.Status.DeployedRelease).NotTo(gomega.BeNil())

	//Update
	t.Log("Github succeed create-update -> CR get")

	err = c.Get(context.TODO(), helmReleaseKey, instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	var spec interface{}

	yaml.Unmarshal([]byte("l1:v1"), &spec)
	instance.Spec = spec

	t.Log("Github succeed create-update -> CR update")

	err = c.Update(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	err = c.Get(context.TODO(), helmReleaseKey, instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Delete(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	time.Sleep(10 * time.Second)

	t.Log("Github succeed create-update -> CR get response")

	instanceRespUp := &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instanceRespUp)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// TestNewManager
	helmReleaseName = "test-new-manager"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}

	instance = &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://github.com/stolostron/application-lifecycle-samples.git"},
					ChartPath: "mortgage-helm",
					Branch:    "main",
				},
			},
			ChartName: "mortgage-helm",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instance = &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	factory, err := rec.newHelmOperatorManagerFactory(instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	nsn := types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}

	request := reconcile.Request{
		NamespacedName: nsn,
	}
	_, err = rec.newHelmOperatorManager(instance, request, factory)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// TestNewManagerShortReleaseName
	helmReleaseName = "test-new-manager-short-release-name"
	helmReleaseKey = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}

	instance = &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://github.com/stolostron/application-lifecycle-samples.git"},
					ChartPath: "mortgage-helm",
					Branch:    "main",
				},
			},
			ChartName: "mortgage-helm",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	instance = &appv1.HelmRelease{}
	err = c.Get(context.TODO(), helmReleaseKey, instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	factory, err = rec.newHelmOperatorManagerFactory(instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	nsn = types.NamespacedName{
		Name:      helmReleaseName,
		Namespace: helmReleaseNS,
	}

	request = reconcile.Request{
		NamespacedName: nsn,
	}
	_, err = rec.newHelmOperatorManager(instance, request, factory)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// TestNewManagerValues
	helmReleaseName = "test-new-manager-values"
	instance = &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://github.com/stolostron/application-lifecycle-samples.git"},
					ChartPath: "mortgage-helm",
					Branch:    "main",
				},
			},
			ChartName: "mortgage-helm",
		},
	}

	yaml.Unmarshal([]byte("l1:v1"), &spec)
	instance.Spec = spec

	//Values well formed
	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(10 * time.Second)

	// TestNewManagerErrors
	helmReleaseName = "test-new-manager-errors"

	instance = &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmReleaseName,
			Namespace: helmReleaseNS,
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://github.com/stolostron/application-lifecycle-samples.git"},
					ChartPath: "mortgage-helm",
					Branch:    "main",
				},
			},
			ChartName: "mortgage-helm",
		},
	}

	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	time.Sleep(60 * time.Second)

	err = c.Get(context.TODO(), helmReleaseKey, instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	err = c.Delete(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

// 	time.Sleep(2 * time.Second)

// 	t.Log("helmrepo keep test")

// 	helmReleaseName = "example-helmrepo-keep"
// 	helmReleaseKey = types.NamespacedName{
// 		Name:      helmReleaseName,
// 		Namespace: helmReleaseNS,
// 	}
// 	instance = &appv1.HelmRelease{
// 		TypeMeta: metav1.TypeMeta{
// 			Kind:       "HelmRelease",
// 			APIVersion: "apps.open-cluster-management.io/v1",
// 		},
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      helmReleaseName,
// 			Namespace: helmReleaseNS,
// 		},
// 		Repo: appv1.HelmReleaseRepo{
// 			Source: &appv1.Source{
// 				SourceType: appv1.HelmRepoSourceType,
// 				HelmRepo: &appv1.HelmRepo{
// 					Urls: []string{
// 						"https://raw.githubusercontent.com/open-cluster-management/multicloud-operators-subscription/main/testhr/helmrepo/nginx-ingress-1.40.0_keep.tgz"},
// 				},
// 			},
// 			ChartName: "nginx-ingress",
// 		},
// 	}

// 	err = c.Create(context.TODO(), instance)
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// 	time.Sleep(4 * time.Second)

// 	instanceResp = &appv1.HelmRelease{}
// 	err = c.Get(context.TODO(), helmReleaseKey, instanceResp)
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// 	time.Sleep(4 * time.Second)

// 	clusterRoleKey := types.NamespacedName{
// 		Name: helmReleaseName + "-" + "nginx-ingress",
// 	}
// 	roleKey := types.NamespacedName{
// 		Name:      helmReleaseName + "-" + "nginx-ingress",
// 		Namespace: helmReleaseNS,
// 	}
// 	clusterRole := &v1.ClusterRole{}
// 	err = c.Get(context.TODO(), clusterRoleKey, clusterRole)
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// 	role := &v1.Role{}
// 	err = c.Get(context.TODO(), roleKey, role)
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// 	err = c.Delete(context.TODO(), instanceResp)
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// 	time.Sleep(4 * time.Second)

// 	err = c.Get(context.TODO(), clusterRoleKey, clusterRole)
// 	g.Expect(err).NotTo(gomega.HaveOccurred())

// err = c.Get(context.TODO(), roleKey, role)
// g.Expect(err).NotTo(gomega.HaveOccurred())
// }
