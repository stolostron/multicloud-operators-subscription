// Copyright 2020 The Kubernetes Authors.
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

package utils

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	"strings"

	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/ghodss/yaml"
	githttp "gopkg.in/src-d/go-git.v4/plumbing/transport/http"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

const (
	// UserID is key of GitHub user ID in secret
	UserID = "user"
	// AccessToken is key of GitHub user password or personal token in secret
	AccessToken = "accessToken"
)

type kubeResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// ParseKubeResoures parses a YAML content and returns kube resources in byte array from the file
func ParseKubeResoures(file []byte) [][]byte {
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

		if t.APIVersion == "" || t.Kind == "" {
			// Ignore item that does not have apiVersion or kind.
			klog.Warning("Not a Kubernetes resource")
		} else {
			ret = append(ret, item)
		}
	}

	return ret
}

// CloneGitRepo clones a GitHub repository
func CloneGitRepo(repoURL string, branch plumbing.ReferenceName, user, password, destDir string) (commitID string, err error) {
	options := &git.CloneOptions{
		URL:               repoURL,
		Depth:             1,
		SingleBranch:      true,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		ReferenceName:     branch,
	}

	if user != "" && password != "" {
		options.Auth = &githttp.BasicAuth{
			Username: user,
			Password: password,
		}
	}

	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		err = os.MkdirAll(destDir, os.ModePerm)
		if err != nil {
			klog.Error(err, "Failed to make directory ", destDir)
			return "", err
		}
	} else {
		err = os.RemoveAll(destDir)
		if err != nil {
			klog.Error(err, "Failed to remove directory ", destDir)
			return "", err
		}
	}

	klog.Info("Cloning ", repoURL, " into ", destDir)
	r, err := git.PlainClone(destDir, false, options)

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

// GetSubscriptionBranch returns GitHub repo branch for a given subscription
func GetSubscriptionBranch(sub *appv1.Subscription) plumbing.ReferenceName {
	branch := plumbing.Master

	annotations := sub.GetAnnotations()

	branchStr := annotations[appv1.AnnotationGithubBranch]
	if branchStr != "" {
		if !strings.HasPrefix(branchStr, "refs/heads/") {
			branchStr = "refs/heads/" + annotations[appv1.AnnotationGithubBranch]
			branch = plumbing.ReferenceName(branchStr)
		}
	}

	return branch
}

// GetChannelSecret returns username and password for channel
func GetChannelSecret(client client.Client, chn *chnv1.Channel) (string, string, error) {
	username := ""
	accessToken := ""

	if chn.Spec.SecretRef != nil {
		secret := &corev1.Secret{}
		secns := chn.Spec.SecretRef.Namespace

		if secns == "" {
			secns = chn.Namespace
		}

		err := client.Get(context.TODO(), types.NamespacedName{Name: chn.Spec.SecretRef.Name, Namespace: secns}, secret)
		if err != nil {
			klog.Error(err, "Unable to get secret from local cluster.")
			return "", "", err
		}

		err = yaml.Unmarshal(secret.Data[UserID], &username)
		if err != nil {
			klog.Error(err, "Failed to unmarshal username from the secret.")
			return "", "", err
		} else if username == "" {
			klog.Error(err, "Failed to get user from the secret.")
			return "", "", errors.New("failed to get user from the secret")
		}

		err = yaml.Unmarshal(secret.Data[AccessToken], &accessToken)
		if err != nil {
			klog.Error(err, "Failed to unmarshal accessToken from the secret.")
			return "", "", err
		} else if accessToken == "" {
			klog.Error(err, "Failed to get accressToken from the secret.")
			return "", "", errors.New("failed to get accressToken from the secret")
		}
	}

	return username, accessToken, nil
}

func OverrideKustomize(pov appv1.PackageOverride, kustomizeDir string) error {
	kustomizeOverride := dplv1alpha1.ClusterOverride(pov)
	ovuobj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&kustomizeOverride)

	if err != nil {
		klog.Error("Kustomize parse error: ", ovuobj, "with err:", err, " path: ", ovuobj["path"], " value:", ovuobj["value"])
		return err
	}

	str := fmt.Sprintf("%v", ovuobj["value"])

	var override map[string]interface{}

	if err := yaml.Unmarshal([]byte(str), &override); err != nil {
		klog.Error("Failed to override kustomize with error: ", err)
		return err
	}

	kustomizeYamlFilePath := filepath.Join(kustomizeDir, "kustomization.yaml")

	if _, err := os.Stat(kustomizeYamlFilePath); os.IsNotExist(err) {
		kustomizeYamlFilePath = filepath.Join(kustomizeDir, "kustomization.yml")
		if _, err := os.Stat(kustomizeYamlFilePath); os.IsNotExist(err) {
			klog.Error("Kustomization file not found in ", kustomizeDir)
			return err
		}
	}

	err = mergeKustomization(kustomizeYamlFilePath, override)
	if err != nil {
		return err
	}

	return nil
}

func mergeKustomization(kustomizeYamlFilePath string, override map[string]interface{}) error {
	var master map[string]interface{}

	bs, err := ioutil.ReadFile(kustomizeYamlFilePath) // #nosec G304 constructed filepath.Join(kustomizeDir, "kustomization.yaml")

	if err != nil {
		klog.Error("Failed to read file ", kustomizeYamlFilePath, " err: ", err)
		return err
	}

	if err := yaml.Unmarshal(bs, &master); err != nil {
		klog.Error("Failed to unmarshal kustomize file ", " err: ", err)
		return err
	}

	for k, v := range override {
		master[k] = v
	}

	bs, err = yaml.Marshal(master)

	if err != nil {
		klog.Error("Failed to marshal kustomize file ", " err: ", err)
		return err
	}

	if err := ioutil.WriteFile(kustomizeYamlFilePath, bs, 0600); err != nil {
		klog.Error("Failed to overwrite kustomize file ", " err: ", err)
		return err
	}

	return nil
}

// GetLocalGitFolder returns the local Git repo clone directory
func GetLocalGitFolder(chn *chnv1.Channel, sub *appv1.Subscription) string {
	return filepath.Join(os.TempDir(), chn.Namespace, chn.Name, GetSubscriptionBranch(sub).Short())
}

// SortResources sorts kube resources into different arrays for processing them later.
func SortResources(repoRoot, resourcePath string) (map[string]string, map[string]string, []string, []string, []string, error) {
	klog.V(4).Info("Git repo subscription directory: ", resourcePath)

	// In the cloned git repo root, find all helm chart directories
	chartDirs := make(map[string]string)

	// In the cloned git repo root, find all kustomization directories
	kustomizeDirs := make(map[string]string)

	// Apply CustomResourceDefinition and Namespace Kubernetes resources first
	crdsAndNamespaceFiles := []string{}
	// Then apply ServiceAccount, ClusterRole and Role Kubernetes resources next
	rbacFiles := []string{}
	// Then apply the rest of resource
	otherFiles := []string{}

	currentChartDir := "NONE"
	currentKustomizeDir := "NONE"

	kubeIgnore := GetKubeIgnore(resourcePath)

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
							chartDirs[path+"/"] = path + "/"
							currentChartDir = path + "/"
						}
					} else if _, err := os.Stat(path + "/kustomization.yaml"); err == nil {
						klog.V(4).Info("Found kustomization.yaml in ", path)
						currentKustomizeDir = path
						kustomizeDirs[path+"/"] = path + "/"
					} else if _, err := os.Stat(path + "/kustomization.yml"); err == nil {
						klog.V(4).Info("Found kustomization.yml in ", path)
						currentKustomizeDir = path
						kustomizeDirs[path+"/"] = path + "/"
					}
				} else if !strings.HasPrefix(path, currentChartDir) &&
					!strings.HasPrefix(path, repoRoot+"/.git") &&
					!strings.EqualFold(filepath.Dir(path), currentKustomizeDir) {
					// Do not process kubernetes YAML files under helm chart or kustomization directory
					crdsAndNamespaceFiles, rbacFiles, otherFiles, err = sortKubeResource(crdsAndNamespaceFiles, rbacFiles, otherFiles, path)
					if err != nil {
						klog.Error(err.Error())
						return err
					}
				}
			}

			return nil
		})

	klog.Info("otherFiles size " + string(len(otherFiles)))

	return chartDirs, kustomizeDirs, crdsAndNamespaceFiles, rbacFiles, otherFiles, err
}

func sortKubeResource(crdsAndNamespaceFiles, rbacFiles, otherFiles []string, path string) ([]string, []string, []string, error) {
	if strings.EqualFold(filepath.Ext(path), ".yml") || strings.EqualFold(filepath.Ext(path), ".yaml") {
		klog.V(4).Info("Reading file: ", path)

		file, err := ioutil.ReadFile(path) // #nosec G304 path is not user input

		if err != nil {
			klog.Error(err, "Failed to read YAML file "+path)
			return crdsAndNamespaceFiles, rbacFiles, otherFiles, err
		}

		resources := ParseKubeResoures(file)

		if len(resources) == 1 {
			t := kubeResource{}
			err := yaml.Unmarshal(resources[0], &t)

			if err != nil {
				fmt.Println("Failed to unmarshal YAML file")
				// Just ignore the YAML
				return crdsAndNamespaceFiles, rbacFiles, otherFiles, nil
			}

			if t.APIVersion != "" && t.Kind != "" {
				klog.Info(t.APIVersion + "/" + t.Kind)

				if strings.EqualFold(t.Kind, "customresourcedefinition") {
					crdsAndNamespaceFiles = append(crdsAndNamespaceFiles, path)
				} else if strings.EqualFold(t.Kind, "namespace") {
					crdsAndNamespaceFiles = append(crdsAndNamespaceFiles, path)
				} else if strings.EqualFold(t.Kind, "serviceaccount") {
					rbacFiles = append(rbacFiles, path)
				} else if strings.EqualFold(t.Kind, "clusterrole") {
					rbacFiles = append(rbacFiles, path)
				} else if strings.EqualFold(t.Kind, "role") {
					rbacFiles = append(rbacFiles, path)
				} else {
					otherFiles = append(otherFiles, path)
				}
			}
		} else if len(resources) > 1 {
			klog.Info("Multi resource")
			otherFiles = append(otherFiles, path)
		}
	}

	return crdsAndNamespaceFiles, rbacFiles, otherFiles, nil
}

// GetKubeIgnore get .kubernetesignore list
func GetKubeIgnore(resourcePath string) *gitignore.GitIgnore {
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
