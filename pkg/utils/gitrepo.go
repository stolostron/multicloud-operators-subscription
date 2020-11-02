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
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	"strings"

	"github.com/google/go-github/v32/github"
	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/ghodss/yaml"
	gitclient "gopkg.in/src-d/go-git.v4/plumbing/transport/client"
	githttp "gopkg.in/src-d/go-git.v4/plumbing/transport/http"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
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

//KKubeResource export the kuKubeResource for other package
type KubeResource struct {
	kubeResource
}

// ParseKubeResoures parses a YAML content and returns kube resources in byte array from the file
func ParseKubeResoures(file []byte) [][]byte {
	cond := func(t KubeResource) bool {
		return t.APIVersion == "" || t.Kind == ""
	}

	return KubeResourceParser(file, cond)
}

type Kube func(KubeResource) bool

func KubeResourceParser(file []byte, cond Kube) [][]byte {
	var ret [][]byte

	items := ParseYAML(file)

	for _, i := range items {
		item := []byte(strings.Trim(i, "\t \n"))

		t := KubeResource{}
		err := yaml.Unmarshal(item, &t)

		if err != nil {
			// Ignore item that cannot be unmarshalled..
			klog.Warning(err, "Failed to unmarshal YAML content")
			continue
		}

		if cond(t) {
			// Ignore item that does not have apiVersion or kind.
			klog.Warning("Not a Kubernetes resource")
		} else {
			ret = append(ret, item)
		}
	}

	return ret
}

// CloneGitRepo clones a GitHub repository
func CloneGitRepo(repoURL string, branch plumbing.ReferenceName, user, password, destDir string, insecureSkipVerify bool) (commitID string, err error) {
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

	// skip TLS certificate verification for Git servers with custom or self-signed certs
	if insecureSkipVerify {
		klog.Info("insecureSkipVerify = true, skipping Git server's certificate verification.")

		customClient := &http.Client{
			/* #nosec G402 */
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, // #nosec G402 InsecureSkipVerify conditionally
			},

			// 15 second timeout
			Timeout: 15 * time.Second,

			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		gitclient.InstallProtocol("https", githttp.NewClient(customClient))
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

	klog.V(2).Info("Cloning ", repoURL, " into ", destDir)
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
	annotations := sub.GetAnnotations()
	branchStr := annotations[appv1.AnnotationGitBranch]

	if branchStr == "" {
		branchStr = annotations[appv1.AnnotationGithubBranch] // AnnotationGithubBranch will be depricated
	}

	return GetSubscriptionBranchRef(branchStr)
}

func GetSubscriptionBranchRef(b string) plumbing.ReferenceName {
	branch := plumbing.Master

	if b != "" && b != "master" {
		if !strings.HasPrefix(b, "refs/heads/") {
			b = "refs/heads/" + b
		}

		branch = plumbing.ReferenceName(b)
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

		username, accessToken, err = ParseChannelSecret(secret)

		if err != nil {
			return "", "", err
		}
	}

	return username, accessToken, nil
}

// GetDataFromChannelConfigMap returns username and password for channel
func GetChannelConfigMap(client client.Client, chn *chnv1.Channel) *corev1.ConfigMap {
	if chn.Spec.ConfigMapRef != nil {
		configMapRet := &corev1.ConfigMap{}
		cmns := chn.Namespace

		err := client.Get(context.TODO(), types.NamespacedName{Name: chn.Spec.ConfigMapRef.Name, Namespace: cmns}, configMapRet)
		if err != nil {
			klog.Error(err, "Unable to get config map from local cluster.")
			return nil
		}

		return configMapRet
	}

	return nil
}

func ParseChannelSecret(secret *corev1.Secret) (string, string, error) {
	username := ""
	accessToken := ""
	err := yaml.Unmarshal(secret.Data[UserID], &username)

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

	return username, accessToken, nil
}

// GetLocalGitFolder returns the local Git repo clone directory
func GetLocalGitFolder(chn *chnv1.Channel, sub *appv1.Subscription) string {
	return filepath.Join(os.TempDir(), sub.Name, GetSubscriptionBranch(sub).Short())
}

type SkipFunc func(string, string) bool

// SortResources sorts kube resources into different arrays for processing them later.
func SortResources(repoRoot, resourcePath string, skips ...SkipFunc) (map[string]string, map[string]string, []string, []string, []string, error) {
	klog.V(4).Info("Git repo subscription directory: ", resourcePath)

	var skip SkipFunc

	if len(skips) == 0 {
		skip = func(string, string) bool { return false }
	} else {
		skip = skips[0]
	}

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

			if !kubeIgnore.MatchesPath(relativePath) && !skip(resourcePath, path) {
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
						// If there are nested kustomizations or any other folder structures containing kube
						// resources under a kustomization, subscription should not process them and let kustomize
						// build handle them based on the top-level kustomization.yaml.
						if !strings.HasPrefix(path, currentKustomizeDir) {
							klog.V(4).Info("Found kustomization.yaml in ", path)
							currentKustomizeDir = path + "/"
							kustomizeDirs[path+"/"] = path + "/"
						}
					} else if _, err := os.Stat(path + "/kustomization.yml"); err == nil {
						// If there are nested kustomizations or any other folder structures containing kube
						// resources under a kustomization, subscription should not process them and let kustomize
						// build handle them based on the top-level kustomization.yaml
						if !strings.HasPrefix(path, currentKustomizeDir) {
							klog.V(4).Info("Found kustomization.yml in ", path)
							currentKustomizeDir = path + "/"
							kustomizeDirs[path+"/"] = path + "/"
						}
					}
				} else if !strings.HasPrefix(path, currentChartDir) &&
					!strings.HasPrefix(path, repoRoot+"/.git") &&
					!strings.HasPrefix(path, currentKustomizeDir) {
					// Do not process kubernetes YAML files under helm chart or kustomization directory
					// If there are nested kustomizations or any other folder structures containing kube
					// resources under a kustomization, subscription should not process them and let kustomize
					// build handle them based on the top-level kustomization.yaml
					crdsAndNamespaceFiles, rbacFiles, otherFiles, err = sortKubeResource(crdsAndNamespaceFiles, rbacFiles, otherFiles, path)
					if err != nil {
						klog.Error(err.Error())
						return err
					}
				}
			}

			return nil
		})

	klog.Infof("otherFiles size %v", len(otherFiles))

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
				} else if strings.EqualFold(t.Kind, "serviceaccount") ||
					strings.EqualFold(t.Kind, "clusterrole") ||
					strings.EqualFold(t.Kind, "role") ||
					strings.EqualFold(t.Kind, "clusterrolebinding") ||
					strings.EqualFold(t.Kind, "rolebinding") {
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

func SkipHooksOnManaged(resourcePath, curPath string) bool {
	PREHOOK := "prehook"
	POSTHOOK := "posthook"

	// of the resource root.
	pre := fmt.Sprintf("%s/%s", resourcePath, PREHOOK)
	post := fmt.Sprintf("%s/%s", resourcePath, POSTHOOK)

	return strings.HasPrefix(curPath, pre) || strings.HasPrefix(curPath, post)
}

// GetKubeIgnore get .kubernetesignore list
func GetKubeIgnore(resourcePath string) *gitignore.GitIgnore {
	klog.V(4).Info("Git repo resource root directory: ", resourcePath)

	lines := []string{""}
	kubeIgnore, _ := gitignore.CompileIgnoreLines(lines...)

	if _, err := os.Stat(filepath.Join(resourcePath, ".kubernetesignore")); err == nil {
		klog.V(4).Info("Found .kubernetesignore in ", resourcePath)
		kubeIgnore, _ = gitignore.CompileIgnoreFile(filepath.Join(resourcePath, ".kubernetesignore"))
	}

	return kubeIgnore
}

// IsGitChannel returns true if channel type is github or git
func IsGitChannel(chType string) bool {
	return strings.EqualFold(chType, chnv1.ChannelTypeGitHub) ||
		strings.EqualFold(chType, chnv1.ChannelTypeGit)
}

func IsClusterAdmin(client client.Client, sub *appv1.Subscription, eventRecorder *EventRecorder) bool {
	isClusterAdmin := false
	isUserSubAdmin := false
	isSubPropagatedFromHub := false
	isClusterAdminAnnotationTrue := false

	userIdentity := ""
	userGroups := ""
	annotations := sub.GetAnnotations()

	if annotations != nil {
		encodedUserGroup := strings.Trim(annotations[appv1.AnnotationUserGroup], "")
		encodedUserIdentity := strings.Trim(annotations[appv1.AnnotationUserIdentity], "")

		if encodedUserGroup != "" {
			userGroups = base64StringDecode(encodedUserGroup)
		}

		if encodedUserIdentity != "" {
			userIdentity = base64StringDecode(encodedUserIdentity)
		}

		if annotations[appv1.AnnotationHosting] != "" {
			isSubPropagatedFromHub = true
		}

		if strings.EqualFold(annotations[appv1.AnnotationClusterAdmin], "true") {
			isClusterAdminAnnotationTrue = true
		}
	}

	doesWebhookExist := false
	theWebhook := &admissionv1.MutatingWebhookConfiguration{}

	if err := client.Get(context.TODO(), types.NamespacedName{Name: appv1.AcmWebhook}, theWebhook); err == nil {
		doesWebhookExist = true
	}

	if userIdentity != "" && doesWebhookExist {
		isUserSubAdmin = matchUserSubAdmin(client, userIdentity, userGroups)
	}

	// If subscription has cluster-admin:true and propagated from hub and cannot find the webhook, we know we are
	// on the managed cluster so trust the annotations to decide that the subscription is from subscription-admin.
	// But the subscription can also be propagated to the self-managed hub cluster.
	if isClusterAdminAnnotationTrue && isSubPropagatedFromHub {
		if !doesWebhookExist || // not on the hub cluster
			(doesWebhookExist && strings.HasSuffix(sub.GetName(), "-local")) { // on the hub cluster and the subscription has -local suffix
			if eventRecorder != nil {
				eventRecorder.RecordEvent(sub, "RoleElevation",
					"Role was elevated to cluster admin for subscription "+sub.Name, nil)
			}

			isClusterAdmin = true
		}
	} else if isUserSubAdmin {
		if eventRecorder != nil {
			eventRecorder.RecordEvent(sub, "RoleElevation",
				"Role was elevated to cluster admin for subscription "+sub.Name+" by user "+userIdentity, nil)
		}

		isClusterAdmin = true
	}

	klog.Infof("isClusterAdmin = %v", isClusterAdmin)

	return isClusterAdmin
}

func matchUserSubAdmin(client client.Client, userIdentity, userGroups string) bool {
	isUserSubAdmin := false
	foundClusterRoleBinding := &rbacv1.ClusterRoleBinding{}

	err := client.Get(context.TODO(), types.NamespacedName{Name: appv1.SubscriptionAdmin}, foundClusterRoleBinding)

	if err == nil {
		klog.Infof("ClusterRoleBinding %s found.", appv1.SubscriptionAdmin)

		for _, subject := range foundClusterRoleBinding.Subjects {
			if strings.Trim(subject.Name, "") == strings.Trim(userIdentity, "") && strings.Trim(subject.Kind, "") == "User" {
				klog.Info("User match. cluster-admin: true")

				isUserSubAdmin = true
			} else if subject.Kind == "Group" {
				groupNames := strings.Split(userGroups, ",")
				for _, groupName := range groupNames {
					if strings.Trim(subject.Name, "") == strings.Trim(groupName, "") {
						klog.Info("Group match. cluster-admin: true")

						isUserSubAdmin = true
					}
				}
			}
		}
	} else {
		klog.Error(err)
	}

	foundClusterRoleBinding = nil

	return isUserSubAdmin
}

func base64StringDecode(encodedStr string) string {
	decodedBytes, err := base64.StdEncoding.DecodeString(encodedStr)
	if err != nil {
		klog.Error("Failed to base64 decode")
		klog.Error(err)
	}

	return string(decodedBytes)
}

func GetLatestCommitID(url, branch string, clt ...*github.Client) (string, error) {
	gitClt := github.NewClient(nil)
	if len(clt) != 0 {
		gitClt = clt[0]
	}

	u, err := getOwnerAndRepo(url)
	if err != nil {
		return "", err
	}

	owner, repo := u[0], u[1]
	ctx := context.TODO()

	b, _, err := gitClt.Repositories.GetBranch(ctx, owner, repo, branch)
	if err != nil {
		return "", err
	}

	return *b.Commit.SHA, nil
}

func ParseYAML(fileContent []byte) []string {
	fileContentString := string(fileContent)
	lines := strings.Split(fileContentString, "\n")
	newFileContent := []byte("")

	// Multi-document YAML delimeter --- might have trailing spaces. Trim those first.
	for _, line := range lines {
		if strings.HasPrefix(line, "---") {
			line = strings.Trim(line, " ")
		}

		line += "\n"

		newFileContent = append(newFileContent, line...)
	}

	// Then now split the YAML content using --- delimeter
	items := strings.Split(string(newFileContent), "\n---\n")

	return items
}

/*
Valid URL format
	url := "https://github.com/rokej/rokejtest.git"
	url := "https://github.com/rokej/rokejtest"
	url := "github.com/rokej/rokejtest.git"
	url := "github.com/rokej/rokejtest"
*/
func getOwnerAndRepo(url string) ([]string, error) {
	if len(url) == 0 {
		return []string{}, nil
	}

	gitSuffix := ".git"

	l1 := strings.Split(url, "//")
	if len(l1) < 1 {
		return []string{}, fmt.Errorf("invalid git url l1")
	}

	var l2 string

	if len(l1) == 1 {
		l2 = strings.TrimSuffix(l1[0], gitSuffix)
	} else {
		l2 = strings.TrimSuffix(l1[1], gitSuffix)
	}

	l3 := strings.Split(l2, "/")

	n := len(l3)
	if n < 2 {
		return []string{}, fmt.Errorf("invalid git url l2")
	}

	return []string{l3[n-2], l3[n-1]}, nil
}
