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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"

	"strings"

	"github.com/blang/semver"
	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/ghodss/yaml"
	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	githttp "gopkg.in/src-d/go-git.v4/plumbing/transport/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// GetLocalGitFolder returns the local Git repo clone directory
func GetLocalGitFolder(chn *chnv1.Channel, sub *appv1.Subscription) string {
	return filepath.Join(os.TempDir(), chn.Namespace, chn.Name, GetSubscriptionBranch(sub).Short())
}

func createTmpDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			klog.Error(err, "Failed to make directory "+dir)
			return err
		}
	} else {
		if err := os.RemoveAll(dir); err != nil {
			klog.Error(err, "Failed to remove directory "+dir)
			return err
		}
	}

	return nil
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
					klog.Info("otherfile[0]=" + otherFiles[0])
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

// GenerateHelmIndexFile generate helm repo index file
func GenerateHelmIndexFile(sub *appv1.Subscription, repoRoot string, chartDirs map[string]string) (*repo.IndexFile, error) {
	// Build a helm repo index file
	indexFile := repo.NewIndexFile()

	for chartDir := range chartDirs {
		chartFolderName := filepath.Base(chartDir)
		chartParentDir := strings.Split(chartDir, chartFolderName)[0]

		// Get the relative parent directory from the git repo root
		chartBaseDir := strings.SplitAfter(chartParentDir, repoRoot+"/")[1]

		chartMetadata, err := chartutil.LoadChartfile(chartDir + "Chart.yaml")

		if err != nil {
			klog.Error("There was a problem in generating helm charts index file: ", err.Error())
			return indexFile, err
		}

		indexFile.Add(chartMetadata, chartFolderName, chartBaseDir, "generated-by-multicloud-operators-subscription")
	}

	indexFile.SortEntries()

	filterCharts(sub, indexFile)

	return indexFile, nil
}

//filterCharts filters the indexFile by name, tillerVersion, version, digest
func filterCharts(sub *appv1.Subscription, indexFile *repo.IndexFile) {
	//Removes all entries from the indexFile with non matching name
	_ = removeNoMatchingName(sub, indexFile)

	//Removes non matching version, tillerVersion, digest
	filterOnVersion(sub, indexFile)
}

//removeNoMatchingName Deletes entries that the name doesn't match the name provided in the subscription
func removeNoMatchingName(sub *appv1.Subscription, indexFile *repo.IndexFile) error {
	if sub.Spec.Package != "" {
		keys := make([]string, 0)
		for k := range indexFile.Entries {
			keys = append(keys, k)
		}

		for _, k := range keys {
			if k != sub.Spec.Package {
				delete(indexFile.Entries, k)
			}
		}
	} else {
		return fmt.Errorf("subsciption.spec.name is missing for subscription: %s/%s", sub.Namespace, sub.Name)
	}

	klog.V(4).Info("After name matching:", indexFile)

	return nil
}

//filterOnVersion filters the indexFile with the version, tillerVersion and Digest provided in the subscription
//The version provided in the subscription can be an expression like ">=1.2.3" (see https://github.com/blang/semver)
//The tillerVersion and the digest provided in the subscription must be literals.
func filterOnVersion(sub *appv1.Subscription, indexFile *repo.IndexFile) {
	keys := make([]string, 0)
	for k := range indexFile.Entries {
		keys = append(keys, k)
	}

	for _, k := range keys {
		chartVersions := indexFile.Entries[k]
		newChartVersions := make([]*repo.ChartVersion, 0)

		for index, chartVersion := range chartVersions {
			if checkKeywords(sub, chartVersion) && checkTillerVersion(sub, chartVersion) && checkVersion(sub, chartVersion) {
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

//checkKeywords Checks if the charts has at least 1 keyword from the packageFilter.Keywords array
func checkKeywords(sub *appv1.Subscription, chartVersion *repo.ChartVersion) bool {
	var labelSelector *metav1.LabelSelector
	if sub.Spec.PackageFilter != nil {
		labelSelector = sub.Spec.PackageFilter.LabelSelector
	}

	return KeywordsChecker(labelSelector, chartVersion.Keywords)
}

//checkTillerVersion Checks if the TillerVersion matches
func checkTillerVersion(sub *appv1.Subscription, chartVersion *repo.ChartVersion) bool {
	if sub.Spec.PackageFilter != nil {
		if sub.Spec.PackageFilter.Annotations != nil {
			if filterTillerVersion, ok := sub.Spec.PackageFilter.Annotations["tillerVersion"]; ok {
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

	klog.V(4).Info("Tiller check passed for:", chartVersion)

	return true
}

//checkVersion checks if the version matches
func checkVersion(sub *appv1.Subscription, chartVersion *repo.ChartVersion) bool {
	if sub.Spec.PackageFilter != nil {
		if sub.Spec.PackageFilter.Version != "" {
			version := chartVersion.GetVersion()
			versionVersion, err := semver.Parse(version)

			if err != nil {
				klog.Error(err)
				return false
			}

			filterVersion, err := semver.ParseRange(sub.Spec.PackageFilter.Version)

			if err != nil {
				klog.Error(err)
				return false
			}

			return filterVersion(versionVersion)
		}
	}

	klog.V(4).Info("Version check passed for:", chartVersion)

	return true
}
