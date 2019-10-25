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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// Password is key of GitHub user password or personal token in secret
	Password = "password"
	// Path is the key of GitHub package filter config map
	Path = "path"
)

// SubscriberItem - defines the unit of namespace subscription
type SubscriberItem struct {
	appv1alpha1.SubscriberItem

	commitID     string
	stopch       chan struct{}
	syncinterval int
	synchronizer *kubesynchronizer.KubeSynchronizer
}

type kubeResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// Start subscribes a subscriber item with namespace channel
func (ghsi *SubscriberItem) Start() {
	// do nothing if already started
	if ghsi.stopch != nil {
		klog.V(10).Info("GITHUB SubscriberItem already started: ", ghsi.Subscription.Name)
		return
	}

	ghsi.stopch = make(chan struct{})

	go wait.Until(func() {
		ghsi.doSubscription()
	}, time.Duration(ghsi.syncinterval)*time.Second, ghsi.stopch)
}

// Stop unsubscribes a subscriber item with namespace channel
func (ghsi *SubscriberItem) Stop() {
	klog.V(10).Info("GITHUB Stopping SubscriberItem ", ghsi.Subscription.Name)
	close(ghsi.stopch)
}

func (ghsi *SubscriberItem) doSubscription() {
	//Clone the git repo
	commitID, err := ghsi.cloneGitRepo()
	if err != nil {
		klog.Error(err, "Unable to clone the git repo ", ghsi.Channel.Spec.PathName)
		return
	}

	if commitID != ghsi.commitID {
		klog.V(10).Info("The commit ID is different. Process the cloned repo")

		index, rscDirs, err := ghsi.sortClonedGitRepo()
		if err != nil {
			klog.Error(err, "Unable to sort helm charts and kubernetes resources from the cloned git repo.")
			return
		}

		ghsi.subscribeResources(rscDirs)
		err = ghsi.subscribeHelmCharts(index)

		if err != nil {
			klog.Error(err, "Unable to subscribe helm charts")
			return
		}

		ghsi.commitID = commitID
	} else {
		klog.V(10).Info("The commit ID is same as before. Skip processing the cloned repo")
	}
}

func (ghsi *SubscriberItem) subscribeResources(rscDirs map[string]string) {
	hostkey := types.NamespacedName{Name: ghsi.Subscription.Name, Namespace: ghsi.Subscription.Namespace}
	syncsource := "subscription-" + hostkey.String()
	kvalid := ghsi.synchronizer.CreateValiadtor(syncsource)
	pkgMap := make(map[string]bool)

	// sync kube resource deployables
	for _, dir := range rscDirs {
		files, err := ioutil.ReadDir(dir)
		if err != nil {
			klog.Error("Failed to list files in directory ", dir, err.Error())
		}

		for _, f := range files {
			klog.V(10).Info("scanning  ", dir, f.Name())
			// If YAML or YML,
			if f.Mode().IsRegular() {
				if strings.EqualFold(filepath.Ext(f.Name()), ".yml") || strings.EqualFold(filepath.Ext(f.Name()), ".yaml") {
					// check it it is Kubernetes resource
					klog.V(10).Info("scanning file ", f.Name())
					file, _ := ioutil.ReadFile(filepath.Join(dir, f.Name()))
					t := kubeResource{}
					err = yaml.Unmarshal(file, &t)

					if err != nil {
						klog.Error(err, "Failed to unmarshal YAML file")
					}

					if t.APIVersion == "" || t.Kind == "" {
						klog.V(10).Info("Not a Kubernetes resource")
					} else {
						klog.V(10).Info("Applying Kubernetes resource of kind ", t.Kind)

						dpltosync, validgvk, err := ghsi.subscribeResource(file, pkgMap)
						if err != nil {
							klog.Info("Skipping resource")
							continue
						}

						klog.V(10).Info("Ready to register template:", hostkey, dpltosync, syncsource)

						err = ghsi.synchronizer.RegisterTemplate(hostkey, dpltosync, syncsource)
						if err != nil {
							err = utils.SetInClusterPackageStatus(&(ghsi.Subscription.Status), dpltosync.GetName(), err, nil)

							if err != nil {
								klog.V(10).Info("error in setting in cluster package status :", err)
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

						klog.V(10).Info("Finished Register ", *validgvk, hostkey, dplkey, " with err:", err)
					}
				}
			}
		}
	}

	ghsi.synchronizer.ApplyValiadtor(kvalid)
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

func (ghsi *SubscriberItem) subscribeHelmCharts(indexFile *repo.IndexFile) (err error) {
	hostkey := types.NamespacedName{Name: ghsi.Subscription.Name, Namespace: ghsi.Subscription.Namespace}
	syncsource := "subscription-" + hostkey.String()
	pkgMap := make(map[string]bool)

	for packageName, chartVersions := range indexFile.Entries {
		klog.V(10).Infof("chart: %s\n%v", packageName, chartVersions)

		//Compose release name
		helmReleaseNewName := packageName + "-" + ghsi.Subscription.Name + "-" + ghsi.Subscription.Namespace

		helmRelease := &releasev1alpha1.HelmRelease{}
		err = ghsi.synchronizer.LocalClient.Get(context.TODO(),
			types.NamespacedName{Name: helmReleaseNewName, Namespace: ghsi.Subscription.Namespace}, helmRelease)

		//Check if Update or Create
		if err != nil {
			if kerrors.IsNotFound(err) {
				klog.V(10).Infof("Create helmRelease %s", helmReleaseNewName)

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
			klog.V(10).Infof("Update helmRelease spec %s", helmRelease.Name)
			releaseName := helmRelease.Spec.ReleaseName
			helmRelease.Spec = releasev1alpha1.HelmReleaseSpec{
				Source: &releasev1alpha1.Source{
					SourceType: releasev1alpha1.GitHubSourceType,
					GitHub: &releasev1alpha1.GitHub{
						Urls:      []string{ghsi.Channel.Spec.PathName},
						ChartPath: chartVersions[0].URLs[0],
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
			klog.Error("Failed to get and subscription resource with error:", err)
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
		ReferenceName:     plumbing.Master,
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
		password := ""

		err = yaml.Unmarshal(secret.Data[UserID], &username)
		if err != nil {
			klog.Error(err, "Failed to unmarshal username from the secret.")
			return "", err
		}

		err = yaml.Unmarshal(secret.Data[Password], &password)
		if err != nil {
			klog.Error(err, "Failed to unmarshal password from the secret.")
			return "", err
		}

		options.Auth = &githttp.BasicAuth{
			Username: username,
			Password: password,
		}
	}

	repoRoot := filepath.Join(os.TempDir(), ghsi.Channel.Namespace, ghsi.Channel.Name)
	if _, err := os.Stat(repoRoot); os.IsNotExist(err) {
		err = os.MkdirAll(repoRoot, os.ModePerm)
		if err != nil {
			klog.Error(err, "Failed to make directory ", repoRoot)
			return "", err
		}
	} else {
		err = os.RemoveAll(repoRoot)
		if err != nil {
			klog.Error(err, "Failed to remove directory ", repoRoot)
			return "", err
		}
	}

	klog.V(10).Info("Cloning ", ghsi.Channel.Spec.PathName, " into ", repoRoot)
	r, err := git.PlainClone(repoRoot, false, options)

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

func (ghsi *SubscriberItem) sortClonedGitRepo() (*repo.IndexFile, map[string]string, error) {
	if ghsi.Subscription.Spec.PackageFilter.FilterRef != nil {
		ghsi.SubscriberItem.SubscriptionConfigMap = &corev1.ConfigMap{}
		subcfgkey := types.NamespacedName{
			Name:      ghsi.Subscription.Spec.PackageFilter.FilterRef.Name,
			Namespace: ghsi.Subscription.Namespace,
		}

		err := ghsi.synchronizer.LocalClient.Get(context.TODO(), subcfgkey, ghsi.SubscriberItem.SubscriptionConfigMap)
		if err != nil {
			klog.Error("Failed to get filterRef configmap, error: ", err)
		}

		klog.V(10).Info("ghsi.SubscriberItem.SubscriptionConfigMap.Data[path]", ghsi.SubscriberItem.SubscriptionConfigMap.Data["path"])
		klog.V(10).Info("ghsi.SubscriberItem.SubscriptionConfigMap.Data[pathsss]", ghsi.SubscriberItem.SubscriptionConfigMap.Data["pathsss"])
	}

	// In the cloned git repo root, find all helm chart directories
	chartDirs := make(map[string]string)
	// In the cloned git repo root, also find all non-helm-chart directories
	resourceDirs := make(map[string]string)

	var currentChartDir string
	currentChartDir = "NONE"

	repoRoot := filepath.Join(os.TempDir(), ghsi.Channel.Namespace, ghsi.Channel.Name)
	resourcePath := repoRoot

	if ghsi.SubscriberItem.SubscriptionConfigMap != nil {
		resourcePath = filepath.Join(repoRoot, ghsi.SubscriberItem.SubscriptionConfigMap.Data["path"])
	}

	klog.V(10).Info("Git repo resource root directory: ", resourcePath)

	err := filepath.Walk(resourcePath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				klog.V(10).Info("Ignoring subfolders of ", currentChartDir)
				if _, err := os.Stat(path + "/Chart.yaml"); err == nil {
					klog.V(10).Info("Found Chart.yaml in ", path)
					if !strings.HasPrefix(path, currentChartDir) {
						klog.V(10).Info("This is a helm chart folder.")
						chartDirs[path+"/"] = path + "/"
						currentChartDir = path + "/"
					}
				} else if !strings.HasPrefix(path, currentChartDir) && !strings.HasPrefix(path, repoRoot+"/.git") {
					klog.V(10).Info("This is not a helm chart directory. ", path)
					resourceDirs[path+"/"] = path + "/"
				}
			}
			return nil
		})

	if err != nil {
		return nil, nil, err
	}

	for resourceDir := range resourceDirs {
		klog.V(10).Info("resourceDir = ", resourceDir)
	}

	for chartDir := range chartDirs {
		klog.V(10).Info("chartDir = ", chartDir)
	}

	// Build a helm repo index file
	indexFile := repo.NewIndexFile()

	for chartDir := range chartDirs {
		chartFolderName := filepath.Base(chartDir)
		chartParentDir := strings.Split(chartDir, chartFolderName)[0]

		klog.V(10).Info("chartFolderName = ", chartFolderName)
		klog.V(10).Info("chartParentDir = ", chartParentDir)
		klog.V(10).Info("chartRoot = ", resourcePath)

		// Get the relative parent directory from the git repo root
		chartBaseDir := strings.SplitAfter(chartParentDir, repoRoot+"/")[1]

		klog.V(10).Info("chartBaseDir = ", chartBaseDir)

		chartMetadata, err := chartutil.LoadChartfile(chartDir + "Chart.yaml")

		if err != nil {
			klog.Error("There was a problem in generating helm charts index file: ", err.Error())
			return nil, nil, err
		}

		indexFile.Add(chartMetadata, chartFolderName, chartBaseDir, "generated-by-multicloud-operators-subscription")
	}

	indexFile.SortEntries()
	b, _ := yaml.Marshal(indexFile)
	klog.V(10).Info("New index file ", string(b))

	return indexFile, resourceDirs, nil
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
