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

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/ghodss/yaml"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	githttp "gopkg.in/src-d/go-git.v4/plumbing/transport/http"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"

	corev1 "k8s.io/api/core/v1"

	gitignore "github.com/sabhiram/go-gitignore"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	dplutils "github.com/IBM/multicloud-operators-deployable/pkg/utils"
	releasev1alpha1 "github.com/IBM/multicloud-operators-subscription-release/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	kubesynchronizer "github.com/IBM/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

const (
	// UserID is key of GitHub user ID in secret
	UserID = "user"
	// AccessToken is key of GitHub user password or personal token in secret
	AccessToken = "accessToken"
	// Path is the key of GitHub package filter config map
	Path = "path"
)

// SubscriberItem - defines the unit of namespace subscription
type SubscriberItem struct {
	appv1alpha1.SubscriberItem

	stopch                chan struct{}
	syncinterval          int
	synchronizer          *kubesynchronizer.KubeSynchronizer
	repoRoot              string
	commitID              string
	chartDirs             map[string]string
	crdsAndNamespaceFiles []string
	rbacFiles             []string
	otherFiles            []string
	indexFile             *repo.IndexFile
}

type kubeResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// Start subscribes a subscriber item with namespace channel
func (ghsi *SubscriberItem) Start() {
	// do nothing if already started
	if ghsi.stopch != nil {
		klog.V(4).Info("SubscriberItem already started: ", ghsi.Subscription.Name)
		return
	}

	ghsi.stopch = make(chan struct{})

	go wait.Until(func() {
		tw := ghsi.SubscriberItem.Subscription.Spec.TimeWindow
		if tw != nil {
			nextRun := utils.NextStartPoint(tw, time.Now())
			if nextRun > time.Duration(0) {
				klog.V(1).Infof("Subcription %v/%v will de deploy after %v",
					ghsi.SubscriberItem.Subscription.GetNamespace(),
					ghsi.SubscriberItem.Subscription.GetName(), nextRun)
				return
			}
		}

		err := ghsi.doSubscription()
		if err != nil {
			klog.Error(err, "Subscription error.")
		}
	}, time.Duration(ghsi.syncinterval)*time.Second, ghsi.stopch)
}

// Stop unsubscribes a subscriber item with namespace channel
func (ghsi *SubscriberItem) Stop() {
	klog.V(4).Info("Stopping SubscriberItem ", ghsi.Subscription.Name)
	close(ghsi.stopch)
}

func (ghsi *SubscriberItem) doSubscription() error {
	//Clone the git repo
	commitID, err := ghsi.cloneGitRepo()
	if err != nil {
		klog.Error(err, "Unable to clone the git repo ", ghsi.Channel.Spec.PathName)
		return err
	}

	if commitID != ghsi.commitID {
		klog.V(4).Info("The commit ID is different. Process the cloned repo")

		err := ghsi.sortClonedGitRepo()
		if err != nil {
			klog.Error(err, "Unable to sort helm charts and kubernetes resources from the cloned git repo.")
			return err
		}

		hostkey := types.NamespacedName{Name: ghsi.Subscription.Name, Namespace: ghsi.Subscription.Namespace}
		syncsource := githubk8ssyncsource + hostkey.String()
		kvalid := ghsi.synchronizer.CreateValiadtor(syncsource)
		rscPkgMap := make(map[string]bool)

		klog.V(4).Info("Applying resources: ", ghsi.crdsAndNamespaceFiles)

		ghsi.subscribeResources(hostkey, syncsource, kvalid, rscPkgMap, ghsi.crdsAndNamespaceFiles)

		klog.V(4).Info("Applying resources: ", ghsi.rbacFiles)

		ghsi.subscribeResources(hostkey, syncsource, kvalid, rscPkgMap, ghsi.rbacFiles)

		klog.V(4).Info("Applying resources: ", ghsi.otherFiles)

		ghsi.subscribeResources(hostkey, syncsource, kvalid, rscPkgMap, ghsi.otherFiles)

		ghsi.synchronizer.ApplyValiadtor(kvalid)

		klog.V(4).Info("Applying helm charts..")

		err = ghsi.subscribeHelmCharts(ghsi.indexFile)

		if err != nil {
			klog.Error(err, "Unable to subscribe helm charts")
			return err
		}

		ghsi.commitID = commitID

		ghsi.chartDirs = nil
		ghsi.crdsAndNamespaceFiles = nil
		ghsi.rbacFiles = nil
		ghsi.otherFiles = nil
		ghsi.indexFile = nil
	} else {
		klog.V(4).Info("The commit ID is same as before. Skip processing the cloned repo")
	}

	return nil
}

func (ghsi *SubscriberItem) getKubeIgnore() *gitignore.GitIgnore {
	resourcePath := ghsi.repoRoot

	if ghsi.SubscriberItem.SubscriptionConfigMap != nil {
		resourcePath = filepath.Join(ghsi.repoRoot, ghsi.SubscriberItem.SubscriptionConfigMap.Data["path"])
	}

	klog.V(4).Info("Git repo resource root directory: ", resourcePath)

	// Initialize .kubernetesIngore with no content and re-initialize it if the file is found in the root
	// of the resource root.
	lines := []string{""}
	kubeIgnore, _ := gitignore.CompileIgnoreLines(lines...)

	if _, err := os.Stat(filepath.Join(resourcePath, ".kubernetesignore")); err == nil {
		klog.V(4).Info("Found .kubernetesignore in ", resourcePath)
		kubeIgnore, _ = gitignore.CompileIgnoreFile(filepath.Join(resourcePath, ".kubernetesignore"))
	}

	return kubeIgnore
}

func (ghsi *SubscriberItem) subscribeResources(hostkey types.NamespacedName,
	syncsource string,
	kvalid *kubesynchronizer.Validator,
	pkgMap map[string]bool,
	rscFiles []string) {
	// sync kube resource deployables
	for _, rscFile := range rscFiles {
		file, _ := ioutil.ReadFile(rscFile)
		t := kubeResource{}
		err := yaml.Unmarshal(file, &t)

		if err != nil {
			klog.Error(err, "Failed to unmarshal YAML file")
		}

		if t.APIVersion == "" || t.Kind == "" {
			klog.V(4).Info("Not a Kubernetes resource")
		} else {
			klog.V(4).Info("Applying Kubernetes resource of kind ", t.Kind)

			dpltosync, validgvk, err := ghsi.subscribeResource(file, pkgMap)
			if err != nil {
				klog.Info("Skipping resource")
				continue
			}
			pkgMap[dpltosync.GetName()] = true

			klog.V(4).Info("Ready to register template:", hostkey, dpltosync, syncsource)

			err = ghsi.synchronizer.RegisterTemplate(hostkey, dpltosync, syncsource)
			if err != nil {
				err = utils.SetInClusterPackageStatus(&(ghsi.Subscription.Status), dpltosync.GetName(), err, nil)

				if err != nil {
					klog.V(4).Info("error in setting in cluster package status :", err)
				}

				pkgMap[dpltosync.GetName()] = true

				return
			}

			dplkey := types.NamespacedName{
				Name:      dpltosync.Name,
				Namespace: dpltosync.Namespace,
			}
			kvalid.AddValidResource(*validgvk, hostkey, dplkey)

			pkgMap[dplkey.Name] = true

			klog.V(4).Info("Finished Register ", *validgvk, hostkey, dplkey, " with err:", err)
		}
	}
}

func (ghsi *SubscriberItem) subscribeResource(file []byte, pkgMap map[string]bool) (*dplv1alpha1.Deployable, *schema.GroupVersionKind, error) {
	rsc := &unstructured.Unstructured{}
	err := yaml.Unmarshal(file, &rsc)

	if err != nil {
		klog.Error(err, "Failed to unmarshal Kubernetes resource")
	}

	dpl := &dplv1alpha1.Deployable{}
	if ghsi.Channel == nil {
		dpl.Name = ghsi.Subscription.Name + "-" + rsc.GetKind() + "-" + rsc.GetName()
		dpl.Namespace = ghsi.Subscription.Namespace
	} else {
		dpl.Name = ghsi.Channel.Name + "-" + rsc.GetKind() + "-" + rsc.GetName()
		dpl.Namespace = ghsi.Channel.Namespace
	}

	orggvk := rsc.GetObjectKind().GroupVersionKind()
	validgvk := ghsi.synchronizer.GetValidatedGVK(orggvk)

	if validgvk == nil {
		gvkerr := errors.New("Resource " + orggvk.String() + " is not supported")
		err = utils.SetInClusterPackageStatus(&(ghsi.Subscription.Status), dpl.GetName(), gvkerr, nil)

		if err != nil {
			klog.Info("error in setting in cluster package status :", err)
		}

		pkgMap[dpl.GetName()] = true

		return nil, nil, gvkerr
	}

	if ghsi.synchronizer.KubeResources[*validgvk].Namespaced {
		rsc.SetNamespace(ghsi.Subscription.Namespace)
	}

	if ghsi.Subscription.Spec.PackageFilter != nil {
		errMsg := ghsi.checkFilters(rsc)
		if errMsg != "" {
			klog.V(3).Info(errMsg)

			return nil, nil, errors.New(errMsg)
		}

		rsc, err = utils.OverrideResourceBySubscription(rsc, rsc.GetName(), ghsi.Subscription)
		if err != nil {
			pkgMap[dpl.GetName()] = true
			errmsg := "Failed override package " + dpl.Name + " with error: " + err.Error()
			err = utils.SetInClusterPackageStatus(&(ghsi.Subscription.Status), dpl.GetName(), err, nil)

			if err != nil {
				errmsg += " and failed to set in cluster package status with error: " + err.Error()
			}

			klog.V(2).Info(errmsg)

			return nil, nil, errors.New(errmsg)
		}
	}

	dpl.Spec.Template = &runtime.RawExtension{}
	dpl.Spec.Template.Raw, err = json.Marshal(rsc)

	if err != nil {
		klog.Error(err, "Failed to mashall the resource", rsc)
		return nil, nil, err
	}

	annotations := dpl.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[dplv1alpha1.AnnotationLocal] = "true"
	dpl.SetAnnotations(annotations)

	return dpl, validgvk, nil
}

func (ghsi *SubscriberItem) checkFilters(rsc *unstructured.Unstructured) (errMsg string) {
	if ghsi.Subscription.Spec.Package != "" && ghsi.Subscription.Spec.Package != rsc.GetName() {
		errMsg = "Name does not match, skiping:" + ghsi.Subscription.Spec.Package + "|" + rsc.GetName()

		return errMsg
	}

	if ghsi.Subscription.Spec.Package == rsc.GetName() {
		klog.V(4).Info("Name does matches: " + ghsi.Subscription.Spec.Package + "|" + rsc.GetName())
	}

	if ghsi.Subscription.Spec.PackageFilter != nil {
		if utils.LabelChecker(ghsi.Subscription.Spec.PackageFilter.LabelSelector, rsc.GetLabels()) {
			klog.V(4).Info("Passed label check on resource " + rsc.GetName())
		} else {
			errMsg = "Failed to pass label check on resource " + rsc.GetName()

			return errMsg
		}

		annotations := ghsi.Subscription.Spec.PackageFilter.Annotations
		if annotations != nil {
			klog.V(4).Info("checking annotations filter:", annotations)

			rscanno := rsc.GetAnnotations()
			if rscanno == nil {
				rscanno = make(map[string]string)
			}

			matched := true

			for k, v := range annotations {
				if rscanno[k] != v {
					klog.Info("Annotation filter does not match:", k, "|", v, "|", rscanno[k])

					matched = false

					break
				}
			}

			if !matched {
				errMsg = "Failed to pass annotation check to deployable " + rsc.GetName()

				return errMsg
			}
		}
	}

	return ""
}

func (ghsi *SubscriberItem) subscribeHelmCharts(indexFile *repo.IndexFile) (err error) {
	hostkey := types.NamespacedName{Name: ghsi.Subscription.Name, Namespace: ghsi.Subscription.Namespace}
	syncsource := githubhelmsyncsource + hostkey.String()
	pkgMap := make(map[string]bool)

	for packageName, chartVersions := range indexFile.Entries {
		klog.V(4).Infof("chart: %s\n%v", packageName, chartVersions)

		//Compose release name
		helmReleaseNewName := packageName + "-" + ghsi.Subscription.Name + "-" + ghsi.Subscription.Namespace

		helmRelease := &releasev1alpha1.HelmRelease{}
		err = ghsi.synchronizer.LocalClient.Get(context.TODO(),
			types.NamespacedName{Name: helmReleaseNewName, Namespace: ghsi.Subscription.Namespace}, helmRelease)

		//Check if Update or Create
		if err != nil {
			if kerrors.IsNotFound(err) {
				klog.V(4).Infof("Create helmRelease %s", helmReleaseNewName)

				helmRelease = &releasev1alpha1.HelmRelease{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "app.ibm.com/v1alpha1",
						Kind:       "HelmRelease",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      helmReleaseNewName,
						Namespace: ghsi.Subscription.Namespace,
						OwnerReferences: []metav1.OwnerReference{{
							APIVersion: ghsi.Subscription.APIVersion,
							Kind:       ghsi.Subscription.Kind,
							Name:       ghsi.Subscription.Name,
							UID:        ghsi.Subscription.UID,
						}},
					},
					Spec: releasev1alpha1.HelmReleaseSpec{
						Source: &releasev1alpha1.Source{
							SourceType: releasev1alpha1.GitHubSourceType,
							GitHub: &releasev1alpha1.GitHub{
								Urls:      []string{ghsi.Channel.Spec.PathName},
								ChartPath: chartVersions[0].URLs[0],
								Branch:    ghsi.getGitBranch().Short(),
							},
						},
						ConfigMapRef: ghsi.Channel.Spec.ConfigMapRef,
						SecretRef:    ghsi.Channel.Spec.SecretRef,
						ChartName:    packageName,
						ReleaseName:  packageName,
						Version:      chartVersions[0].GetVersion(),
					},
				}
			} else {
				klog.Error(err)
				break
			}
		} else {
			// set kind and apiversion, coz it is not in the resource get from k8s
			helmRelease.APIVersion = "app.ibm.com/v1alpha1"
			helmRelease.Kind = "HelmRelease"
			klog.V(4).Infof("Update helmRelease spec %s", helmRelease.Name)
			releaseName := helmRelease.Spec.ReleaseName
			helmRelease.Spec = releasev1alpha1.HelmReleaseSpec{
				Source: &releasev1alpha1.Source{
					SourceType: releasev1alpha1.GitHubSourceType,
					GitHub: &releasev1alpha1.GitHub{
						Urls:      []string{ghsi.Channel.Spec.PathName},
						ChartPath: chartVersions[0].URLs[0],
						Branch:    ghsi.getGitBranch().Short(),
					},
				},
				ConfigMapRef: ghsi.Channel.Spec.ConfigMapRef,
				SecretRef:    ghsi.Channel.Spec.SecretRef,
				ChartName:    packageName,
				ReleaseName:  releaseName,
				Version:      chartVersions[0].GetVersion(),
			}
		}

		err := ghsi.override(helmRelease)

		if err != nil {
			klog.Error("Failed to override ", helmRelease.Name, " err:", err)
			return err
		}

		dpl := &dplv1alpha1.Deployable{}
		if ghsi.Channel == nil {
			dpl.Name = ghsi.Subscription.Name + "-" + packageName + "-" + chartVersions[0].GetVersion()
			dpl.Namespace = ghsi.Subscription.Namespace
		} else {
			dpl.Name = ghsi.Channel.Name + "-" + packageName + "-" + chartVersions[0].GetVersion()
			dpl.Namespace = ghsi.Channel.Namespace
		}

		dpl.Spec.Template = &runtime.RawExtension{}
		dpl.Spec.Template.Raw, err = json.Marshal(helmRelease)

		if err != nil {
			klog.Error("Failed to mashall helm release", helmRelease)
			continue
		}

		dplanno := make(map[string]string)
		dplanno[dplv1alpha1.AnnotationLocal] = "true"
		dpl.SetAnnotations(dplanno)

		err = ghsi.synchronizer.RegisterTemplate(hostkey, dpl, syncsource)
		if err != nil {
			klog.Info("eror in registering :", err)
			err = utils.SetInClusterPackageStatus(&(ghsi.Subscription.Status), dpl.GetName(), err, nil)

			if err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}

			pkgMap[dpl.GetName()] = true

			continue
		}

		dplkey := types.NamespacedName{
			Name:      dpl.Name,
			Namespace: dpl.Namespace,
		}
		pkgMap[dplkey.Name] = true
	}

	if utils.ValidatePackagesInSubscriptionStatus(ghsi.synchronizer.LocalClient, ghsi.Subscription, pkgMap) != nil {
		err = ghsi.synchronizer.LocalClient.Get(context.TODO(), hostkey, ghsi.Subscription)
		if err != nil {
			klog.Error("Failed to get subscription resource with error:", err)
		}

		err = utils.ValidatePackagesInSubscriptionStatus(ghsi.synchronizer.LocalClient, ghsi.Subscription, pkgMap)
	}

	return err
}

func (ghsi *SubscriberItem) cloneGitRepo() (commitID string, err error) {
	options := &git.CloneOptions{
		URL:               ghsi.Channel.Spec.PathName,
		Depth:             1,
		SingleBranch:      true,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		ReferenceName:     ghsi.getGitBranch(),
	}

	if ghsi.Channel.Spec.SecretRef != nil {
		secret := &corev1.Secret{}
		secns := ghsi.Channel.Spec.SecretRef.Namespace

		if secns == "" {
			secns = ghsi.Channel.Namespace
		}

		err := ghsi.synchronizer.LocalClient.Get(context.TODO(), types.NamespacedName{Name: ghsi.Channel.Spec.SecretRef.Name, Namespace: secns}, secret)
		if err != nil {
			klog.Error(err, "Unable to get secret.")
			return "", err
		}

		username := ""
		accessToken := ""

		err = yaml.Unmarshal(secret.Data[UserID], &username)
		if err != nil {
			klog.Error(err, "Failed to unmarshal username from the secret.")
			return "", err
		} else if username == "" {
			klog.Error(err, "Failed to get user from the secret.")
			return "", errors.New("failed to get user from the secret")
		}

		err = yaml.Unmarshal(secret.Data[AccessToken], &accessToken)
		if err != nil {
			klog.Error(err, "Failed to unmarshal accessToken from the secret.")
			return "", err
		} else if accessToken == "" {
			klog.Error(err, "Failed to get accressToken from the secret.")
			return "", errors.New("failed to get accressToken from the secret")
		}

		options.Auth = &githttp.BasicAuth{
			Username: username,
			Password: accessToken,
		}
	}

	ghsi.repoRoot = filepath.Join(os.TempDir(), ghsi.Channel.Namespace, ghsi.Channel.Name)
	if _, err := os.Stat(ghsi.repoRoot); os.IsNotExist(err) {
		err = os.MkdirAll(ghsi.repoRoot, os.ModePerm)
		if err != nil {
			klog.Error(err, "Failed to make directory ", ghsi.repoRoot)
			return "", err
		}
	} else {
		err = os.RemoveAll(ghsi.repoRoot)
		if err != nil {
			klog.Error(err, "Failed to remove directory ", ghsi.repoRoot)
			return "", err
		}
	}

	klog.V(4).Info("Cloning ", ghsi.Channel.Spec.PathName, " into ", ghsi.repoRoot)
	r, err := git.PlainClone(ghsi.repoRoot, false, options)

	if err != nil {
		klog.Error(err, "Failed to git clone: ", err.Error())
		return "", err
	}

	ref, err := r.Head()
	if err != nil {
		klog.Error(err, "Failed to get git repo head")
		return "", err
	}

	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		klog.Error(err, "Failed to get git repo commit")
		return "", err
	}

	return commit.ID().String(), nil
}

func (ghsi *SubscriberItem) getGitBranch() plumbing.ReferenceName {
	branch := plumbing.Master

	if ghsi.SubscriberItem.SubscriptionConfigMap != nil {
		if ghsi.SubscriberItem.SubscriptionConfigMap.Data["branch"] != "" {
			branchStr := ghsi.SubscriberItem.SubscriptionConfigMap.Data["branch"]
			if !strings.HasPrefix(branchStr, "refs/heads/") {
				branchStr = "refs/heads/" + ghsi.SubscriberItem.SubscriptionConfigMap.Data["branch"]
			}

			branch = plumbing.ReferenceName(branchStr)
		}
	}

	klog.V(4).Info("Subscribing to branch: ", branch)

	return branch
}

func (ghsi *SubscriberItem) sortClonedGitRepo() error {
	if ghsi.Subscription.Spec.PackageFilter != nil && ghsi.Subscription.Spec.PackageFilter.FilterRef != nil {
		ghsi.SubscriberItem.SubscriptionConfigMap = &corev1.ConfigMap{}
		subcfgkey := types.NamespacedName{
			Name:      ghsi.Subscription.Spec.PackageFilter.FilterRef.Name,
			Namespace: ghsi.Subscription.Namespace,
		}

		err := ghsi.synchronizer.LocalClient.Get(context.TODO(), subcfgkey, ghsi.SubscriberItem.SubscriptionConfigMap)
		if err != nil {
			klog.Error("Failed to get filterRef configmap, error: ", err)
		}
	}

	resourcePath := ghsi.repoRoot

	if ghsi.SubscriberItem.SubscriptionConfigMap != nil {
		resourcePath = filepath.Join(ghsi.repoRoot, ghsi.SubscriberItem.SubscriptionConfigMap.Data["path"])
	}

	// chartDirs contains helm chart directories
	// crdsAndNamespaceFiles contains CustomResourceDefinition and Namespace Kubernetes resources file paths
	// rbacFiles contains ServiceAccount, ClusterRole and Role Kubernetes resource file paths
	// otherFiles contains all other Kubernetes resource file paths
	err := ghsi.sortResources(ghsi.repoRoot, resourcePath)
	if err != nil {
		klog.Error(err, "Failed to sort kubernetes resources and helm charts.")
		return err
	}

	// Build a helm repo index file
	err = ghsi.generateHelmIndexFile(ghsi.repoRoot, ghsi.chartDirs)

	if err != nil {
		// If package name is not specified in the subscription, filterCharts throws an error. In this case, just return the original index file.
		klog.Error(err, "Failed to generate helm index file.")
		return err
	}

	b, _ := yaml.Marshal(ghsi.indexFile)
	klog.V(4).Info("New index file ", string(b))

	return nil
}

func (ghsi *SubscriberItem) sortResources(repoRoot string, resourcePath string) error {
	klog.V(4).Info("Git repo resource root directory: ", resourcePath)

	// In the cloned git repo root, find all helm chart directories
	ghsi.chartDirs = make(map[string]string)

	// Apply CustomResourceDefinition and Namespace Kubernetes resources first
	ghsi.crdsAndNamespaceFiles = []string{}
	// Then apply ServiceAccount, ClusterRole and Role Kubernetes resources next
	ghsi.rbacFiles = []string{}
	// Then apply the rest of resource
	ghsi.otherFiles = []string{}

	currentChartDir := "NONE"

	kubeIgnore := ghsi.getKubeIgnore()

	err := filepath.Walk(resourcePath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			relativePath := path

			if len(strings.SplitAfter(path, repoRoot+"/")) > 1 {
				relativePath = strings.SplitAfter(path, repoRoot+"/")[1]
			}

			if !kubeIgnore.MatchesPath(relativePath) {
				if info.IsDir() {
					klog.V(4).Info("Ignoring subfolders of ", currentChartDir)
					if _, err := os.Stat(path + "/Chart.yaml"); err == nil {
						klog.V(4).Info("Found Chart.yaml in ", path)
						if !strings.HasPrefix(path, currentChartDir) {
							klog.V(4).Info("This is a helm chart folder.")
							ghsi.chartDirs[path+"/"] = path + "/"
							currentChartDir = path + "/"
						}
					}
				} else if !strings.HasPrefix(path, currentChartDir) && !strings.HasPrefix(path, repoRoot+"/.git") {
					err = ghsi.sortKubeResources(path)
					if err != nil {
						return err
					}
				}
			}

			return nil
		})

	if err != nil {
		return err
	}

	return nil
}

func (ghsi *SubscriberItem) sortKubeResources(path string) error {
	if strings.EqualFold(filepath.Ext(path), ".yml") || strings.EqualFold(filepath.Ext(path), ".yaml") {
		klog.V(4).Info("Reading file: ", path)

		file, _ := ioutil.ReadFile(path)
		t := kubeResource{}
		err := yaml.Unmarshal(file, &t)

		if err != nil {
			fmt.Println("Failed to unmarshal YAML file")
			return err
		}

		if t.APIVersion != "" && t.Kind != "" {
			if strings.EqualFold(t.Kind, "customresourcedefinition") {
				ghsi.crdsAndNamespaceFiles = append(ghsi.crdsAndNamespaceFiles, path)
			} else if strings.EqualFold(t.Kind, "namespace") {
				ghsi.crdsAndNamespaceFiles = append(ghsi.crdsAndNamespaceFiles, path)
			} else if strings.EqualFold(t.Kind, "serviceaccount") {
				ghsi.rbacFiles = append(ghsi.rbacFiles, path)
			} else if strings.EqualFold(t.Kind, "clusterrole") {
				ghsi.rbacFiles = append(ghsi.rbacFiles, path)
			} else if strings.EqualFold(t.Kind, "role") {
				ghsi.rbacFiles = append(ghsi.rbacFiles, path)
			} else {
				ghsi.otherFiles = append(ghsi.otherFiles, path)
			}
		}
	}

	return nil
}

func (ghsi *SubscriberItem) generateHelmIndexFile(repoRoot string, chartDirs map[string]string) error {
	// Build a helm repo index file
	ghsi.indexFile = repo.NewIndexFile()

	for chartDir := range chartDirs {
		chartFolderName := filepath.Base(chartDir)
		chartParentDir := strings.Split(chartDir, chartFolderName)[0]

		// Get the relative parent directory from the git repo root
		chartBaseDir := strings.SplitAfter(chartParentDir, repoRoot+"/")[1]

		chartMetadata, err := chartutil.LoadChartfile(chartDir + "Chart.yaml")

		if err != nil {
			klog.Error("There was a problem in generating helm charts index file: ", err.Error())
			return err
		}

		ghsi.indexFile.Add(chartMetadata, chartFolderName, chartBaseDir, "generated-by-multicloud-operators-subscription")
	}

	ghsi.indexFile.SortEntries()

	ghsi.filterCharts(ghsi.indexFile)

	return nil
}

func (ghsi *SubscriberItem) getOverrides(packageName string) dplv1alpha1.Overrides {
	dploverrides := dplv1alpha1.Overrides{}

	for _, overrides := range ghsi.Subscription.Spec.PackageOverrides {
		if overrides.PackageName == packageName {
			klog.Infof("Overrides for package %s found", packageName)
			dploverrides.ClusterName = packageName
			dploverrides.ClusterOverrides = make([]dplv1alpha1.ClusterOverride, 0)

			for _, override := range overrides.PackageOverrides {
				clusterOverride := dplv1alpha1.ClusterOverride{
					RawExtension: runtime.RawExtension{
						Raw: override.RawExtension.Raw,
					},
				}
				dploverrides.ClusterOverrides = append(dploverrides.ClusterOverrides, clusterOverride)
			}

			return dploverrides
		}
	}

	return dploverrides
}

func (ghsi *SubscriberItem) override(helmRelease *releasev1alpha1.HelmRelease) error {
	//Overrides with the values provided in the subscription for that package
	overrides := ghsi.getOverrides(helmRelease.Spec.ChartName)
	data, err := yaml.Marshal(helmRelease)

	if err != nil {
		klog.Error("Failed to mashall ", helmRelease.Name, " err:", err)
		return err
	}

	template := &unstructured.Unstructured{}
	err = yaml.Unmarshal(data, template)

	if err != nil {
		klog.Warning("Processing local deployable with error template:", helmRelease, err)
	}

	template, err = dplutils.OverrideTemplate(template, overrides.ClusterOverrides)

	if err != nil {
		klog.Error("Failed to apply override for instance: ")
		return err
	}

	data, err = yaml.Marshal(template)

	if err != nil {
		klog.Error("Failed to mashall ", helmRelease.Name, " err:", err)
		return err
	}

	err = yaml.Unmarshal(data, helmRelease)

	if err != nil {
		klog.Error("Failed to unmashall ", helmRelease.Name, " err:", err)
		return err
	}

	return nil
}

//filterCharts filters the indexFile by name, tillerVersion, version, digest
func (ghsi *SubscriberItem) filterCharts(indexFile *repo.IndexFile) {
	//Removes all entries from the indexFile with non matching name
	_ = ghsi.removeNoMatchingName(indexFile)

	//Removes non matching version, tillerVersion, digest
	ghsi.filterOnVersion(indexFile)
}

//checkKeywords Checks if the charts has at least 1 keyword from the packageFilter.Keywords array
func (ghsi *SubscriberItem) checkKeywords(chartVersion *repo.ChartVersion) bool {
	var labelSelector *metav1.LabelSelector
	if ghsi.Subscription.Spec.PackageFilter != nil {
		labelSelector = ghsi.Subscription.Spec.PackageFilter.LabelSelector
	}

	return utils.KeywordsChecker(labelSelector, chartVersion.Keywords)
}

//filterOnVersion filters the indexFile with the version, tillerVersion and Digest provided in the subscription
//The version provided in the subscription can be an expression like ">=1.2.3" (see https://github.com/blang/semver)
//The tillerVersion and the digest provided in the subscription must be literals.
func (ghsi *SubscriberItem) filterOnVersion(indexFile *repo.IndexFile) {
	keys := make([]string, 0)
	for k := range indexFile.Entries {
		keys = append(keys, k)
	}

	for _, k := range keys {
		chartVersions := indexFile.Entries[k]
		newChartVersions := make([]*repo.ChartVersion, 0)

		for index, chartVersion := range chartVersions {
			if ghsi.checkKeywords(chartVersion) && ghsi.checkTillerVersion(chartVersion) && ghsi.checkVersion(chartVersion) {
				newChartVersions = append(newChartVersions, chartVersions[index])
			}
		}

		if len(newChartVersions) > 0 {
			indexFile.Entries[k] = newChartVersions
		} else {
			delete(indexFile.Entries, k)
		}
	}

	klog.V(4).Info("After version matching:", indexFile)
}

//removeNoMatchingName Deletes entries that the name doesn't match the name provided in the subscription
func (ghsi *SubscriberItem) removeNoMatchingName(indexFile *repo.IndexFile) error {
	if ghsi.Subscription != nil {
		if ghsi.Subscription.Spec.Package != "" {
			keys := make([]string, 0)
			for k := range indexFile.Entries {
				keys = append(keys, k)
			}

			for _, k := range keys {
				if k != ghsi.Subscription.Spec.Package {
					delete(indexFile.Entries, k)
				}
			}
		} else {
			return fmt.Errorf("subsciption.spec.name is missing for subscription: %s/%s", ghsi.Subscription.Namespace, ghsi.Subscription.Name)
		}
	}

	klog.V(4).Info("After name matching:", indexFile)

	return nil
}

//checkTillerVersion Checks if the TillerVersion matches
func (ghsi *SubscriberItem) checkTillerVersion(chartVersion *repo.ChartVersion) bool {
	if ghsi.Subscription != nil {
		if ghsi.Subscription.Spec.PackageFilter != nil {
			if ghsi.Subscription.Spec.PackageFilter.Annotations != nil {
				if filterTillerVersion, ok := ghsi.Subscription.Spec.PackageFilter.Annotations["tillerVersion"]; ok {
					tillerVersion := chartVersion.GetTillerVersion()
					if tillerVersion != "" {
						tillerVersionVersion, err := semver.ParseRange(tillerVersion)
						if err != nil {
							klog.Errorf("Error while parsing tillerVersion: %s of %s Error: %s", tillerVersion, chartVersion.GetName(), err.Error())
							return false
						}

						filterTillerVersion, err := semver.Parse(filterTillerVersion)

						if err != nil {
							klog.Error(err)
							return false
						}

						return tillerVersionVersion(filterTillerVersion)
					}
				}
			}
		}
	}

	klog.V(4).Info("Tiller check passed for:", chartVersion)

	return true
}

//checkVersion checks if the version matches
func (ghsi *SubscriberItem) checkVersion(chartVersion *repo.ChartVersion) bool {
	if ghsi.Subscription != nil {
		if ghsi.Subscription.Spec.PackageFilter != nil {
			if ghsi.Subscription.Spec.PackageFilter.Version != "" {
				version := chartVersion.GetVersion()
				versionVersion, err := semver.Parse(version)

				if err != nil {
					klog.Error(err)
					return false
				}

				filterVersion, err := semver.ParseRange(ghsi.Subscription.Spec.PackageFilter.Version)

				if err != nil {
					klog.Error(err)
					return false
				}

				return filterVersion(versionVersion)
			}
		}
	}

	klog.V(4).Info("Version check passed for:", chartVersion)

	return true
}
