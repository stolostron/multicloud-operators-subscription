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

package git

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"

	corev1 "k8s.io/api/core/v1"

	dplv1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

const (
	// UserID is key of GitHub user ID in secret
	UserID = "user"
	// AccessToken is key of GitHub user password or personal token in secret
	AccessToken = "accessToken"
	// Path is the key of GitHub package filter config map
	Path = "path"
)

var (
	helmGvk = schema.GroupVersionKind{
		Group:   appv1.SchemeGroupVersion.Group,
		Version: appv1.SchemeGroupVersion.Version,
		Kind:    "HelmRelease",
	}
)

// SubscriberItem - defines the unit of namespace subscription
type SubscriberItem struct {
	appv1.SubscriberItem
	crdsAndNamespaceFiles []string
	rbacFiles             []string
	otherFiles            []string
	repoRoot              string
	commitID              string
	stopch                chan struct{}
	syncinterval          int
	synchronizer          SyncSource
	chartDirs             map[string]string
	kustomizeDirs         map[string]string
	resources             []kubesynchronizer.DplUnit
	indexFile             *repo.IndexFile
	webhookEnabled        bool
	successful            bool
	clusterAdmin          bool
}

type kubeResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

// Start subscribes a subscriber item with github channel
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
				klog.Infof("Subscription is currently blocked by the time window. It %v/%v will be deployed after %v",
					ghsi.SubscriberItem.Subscription.GetNamespace(),
					ghsi.SubscriberItem.Subscription.GetName(), nextRun)
				return
			}
		}

		// if the subscription pause lable is true, stop subscription here.
		if utils.GetPauseLabel(ghsi.SubscriberItem.Subscription) {
			klog.Infof("Git Subscription %v/%v is paused.", ghsi.SubscriberItem.Subscription.GetNamespace(), ghsi.SubscriberItem.Subscription.GetName())
			return
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
	// If webhook is enabled, don't do anything until next reconcilitation.
	if ghsi.webhookEnabled {
		klog.Infof("Git Webhook is enabled on subscription %s.", ghsi.Subscription.Name)

		if ghsi.successful {
			klog.Infof("All resources are reconciled successfully. Waiting for the next Git Webhook event.")
			return nil
		}

		klog.Infof("Resources are not reconciled successfully yet. Continue reconciling.")
	}

	klog.V(2).Info("Subscribing ...", ghsi.Subscription.Name)
	//Clone the git repo
	commitID, err := ghsi.cloneGitRepo()
	if err != nil {
		klog.Error(err, "Unable to clone the git repo ", ghsi.Channel.Spec.Pathname)
		return err
	}

	klog.Info("Git commit: ", commitID)

	ghsi.resources = []kubesynchronizer.DplUnit{}

	err = ghsi.sortClonedGitRepo()
	if err != nil {
		klog.Error(err, "Unable to sort helm charts and kubernetes resources from the cloned git repo.")
		return err
	}

	hostkey := types.NamespacedName{Name: ghsi.Subscription.Name, Namespace: ghsi.Subscription.Namespace}
	syncsource := githubk8ssyncsource + hostkey.String()

	klog.V(4).Info("Applying resources: ", ghsi.crdsAndNamespaceFiles)

	err = ghsi.subscribeResources(ghsi.crdsAndNamespaceFiles)

	if err != nil {
		klog.Error(err, "Unable to subscribe crd and ns resources")
	}

	klog.V(4).Info("Applying resources: ", ghsi.rbacFiles)

	err = ghsi.subscribeResources(ghsi.rbacFiles)

	if err != nil {
		klog.Error(err, "Unable to subscribe rbac resources")
	}

	klog.V(4).Info("Applying resources: ", ghsi.otherFiles)

	err = ghsi.subscribeResources(ghsi.otherFiles)

	if err != nil {
		klog.Error(err, "Unable to subscribe other resources")
	}

	klog.V(4).Info("Applying kustomizations: ", ghsi.kustomizeDirs)

	err = ghsi.subscribeKustomizations()

	if err != nil {
		klog.Error(err, "Unable to subscribe kustomize resources")
	}

	klog.V(4).Info("Applying helm charts..")

	err = ghsi.subscribeHelmCharts(ghsi.indexFile)

	if err != nil {
		klog.Error(err, "Unable to subscribe helm charts")
	}

	if err := ghsi.synchronizer.AddTemplates(syncsource, hostkey, ghsi.resources); err != nil {
		klog.Error(err)
	}

	ghsi.commitID = commitID

	ghsi.resources = nil
	ghsi.chartDirs = nil
	ghsi.kustomizeDirs = nil
	ghsi.crdsAndNamespaceFiles = nil
	ghsi.rbacFiles = nil
	ghsi.otherFiles = nil
	ghsi.indexFile = nil
	ghsi.successful = true

	return nil
}

func (ghsi *SubscriberItem) subscribeKustomizations() error {
	for _, kustomizeDir := range ghsi.kustomizeDirs {
		klog.Info("Applying kustomization ", kustomizeDir)

		relativePath := kustomizeDir

		if len(strings.SplitAfter(kustomizeDir, ghsi.repoRoot+"/")) > 1 {
			relativePath = strings.SplitAfter(kustomizeDir, ghsi.repoRoot+"/")[1]
		}

		for _, ov := range ghsi.Subscription.Spec.PackageOverrides {
			ovKustomizeDir := strings.Split(ov.PackageName, "kustomization")[0]

			if ovKustomizeDir == "" {
				ovKustomizeDir = relativePath
			}

			if !strings.EqualFold(ovKustomizeDir, relativePath) {
				continue
			} else {
				klog.Info("Overriding kustomization ", kustomizeDir)
				pov := ov.PackageOverrides[0] // there is only one override for kustomization.yaml
				err := utils.OverrideKustomize(pov, kustomizeDir)
				if err != nil {
					klog.Error("Failed to override kustomization.")
					break
				}
			}
		}

		out, err := utils.RunKustomizeBuild(kustomizeDir)

		if err != nil {
			klog.Error("Failed to applying kustomization, error: ", err.Error())
			return err
		}

		// Split the output of kustomize build output into individual kube resource YAML files
		resources := utils.ParseYAML(out)
		for _, resource := range resources {
			resourceFile := []byte(strings.Trim(resource, "\t \n"))

			t := kubeResource{}
			err := yaml.Unmarshal(resourceFile, &t)

			if err != nil {
				klog.Error(err, "Failed to unmarshal YAML file")
				continue
			}

			if t.APIVersion == "" || t.Kind == "" {
				klog.Info("Not a Kubernetes resource")
			} else {
				err := checkSubscriptionAnnotation(t)
				if err != nil {
					klog.Errorf("Failed to apply %s/%s resource. err: %s", t.APIVersion, t.Kind, err)
				}

				ghsi.subscribeResourceFile(resourceFile)
			}
		}
	}

	return nil
}

func checkSubscriptionAnnotation(resource kubeResource) error {
	if strings.EqualFold(resource.APIVersion, appv1.SchemeGroupVersion.String()) && strings.EqualFold(resource.Kind, "Subscription") {
		annotations := resource.GetAnnotations()
		if strings.EqualFold(annotations[appv1.AnnotationClusterAdmin], "true") {
			klog.Errorf("%s %s contains annotation %s set to true.", resource.APIVersion, resource.Name, appv1.AnnotationClusterAdmin)
			return errors.New("contains " + appv1.AnnotationClusterAdmin + " = true annotation.")
		}
	}

	return nil
}

func (ghsi *SubscriberItem) subscribeResources(rscFiles []string) error {
	// sync kube resource deployables
	for _, rscFile := range rscFiles {
		file, err := ioutil.ReadFile(rscFile) // #nosec G304 rscFile is not user input

		if err != nil {
			klog.Error(err, "Failed to read YAML file "+rscFile)
			return err
		}

		resources := utils.ParseKubeResoures(file)

		if len(resources) > 0 {
			for _, resource := range resources {
				t := kubeResource{}
				err := yaml.Unmarshal(resource, &t)

				if err != nil {
					// Ignore if it does not have apiVersion or kind fields in the YAML
					continue
				}

				klog.V(4).Info("Applying Kubernetes resource of kind ", t.Kind)

				ghsi.subscribeResourceFile(resource)
			}
		}
	}

	return nil
}

func (ghsi *SubscriberItem) subscribeResourceFile(file []byte) {
	dpltosync, validgvk, err := ghsi.subscribeResource(file)
	if err != nil {
		klog.Error(err)
	}

	if dpltosync == nil || validgvk == nil {
		klog.Info("Skipping resource")
		return
	}

	ghsi.resources = append(ghsi.resources, kubesynchronizer.DplUnit{Dpl: dpltosync, Gvk: *validgvk})
}

func (ghsi *SubscriberItem) subscribeResource(file []byte) (*dplv1.Deployable, *schema.GroupVersionKind, error) {
	rsc := &unstructured.Unstructured{}
	err := yaml.Unmarshal(file, &rsc)

	if err != nil {
		klog.Error(err, "Failed to unmarshal Kubernetes resource")
	}

	dpl := &dplv1.Deployable{}

	if ghsi.Channel == nil {
		dpl.Name = ghsi.Subscription.Name + "-" + rsc.GetKind() + "-" + rsc.GetName()
		dpl.Namespace = ghsi.Subscription.Namespace

		if ghsi.clusterAdmin && (rsc.GetNamespace() != "") {
			// With the cluster admin, the same resource with the same name can be applied to multiple namespaces.
			// This avoids name collisions.
			dpl.Namespace = rsc.GetNamespace()
		}
	} else {
		dpl.Name = ghsi.Channel.Name + "-" + rsc.GetKind() + "-" + rsc.GetName()
		dpl.Namespace = ghsi.Channel.Namespace

		if ghsi.clusterAdmin && (rsc.GetNamespace() != "") {
			// With the cluster admin, the same resource with the same name can be applied to multiple namespaces.
			// This avoids name collisions.
			dpl.Namespace = rsc.GetNamespace()
		}
	}

	orggvk := rsc.GetObjectKind().GroupVersionKind()
	validgvk := ghsi.synchronizer.GetValidatedGVK(orggvk)

	if validgvk == nil {
		gvkerr := errors.New("Resource " + orggvk.String() + " is not supported")
		err = utils.SetInClusterPackageStatus(&(ghsi.Subscription.Status), dpl.GetName(), gvkerr, nil)

		if err != nil {
			klog.Info("error in setting in cluster package status :", err)
		}

		return nil, nil, gvkerr
	}

	if ghsi.synchronizer.IsResourceNamespaced(*validgvk) {
		if ghsi.clusterAdmin {
			klog.Info("cluster-admin is true.")

			if rsc.GetNamespace() != "" {
				klog.Info("Using resource's original namespace. Resource namespace is " + rsc.GetNamespace())
			} else {
				klog.Info("Setting it to subscription namespace " + ghsi.Subscription.Namespace)
				rsc.SetNamespace(ghsi.Subscription.Namespace)
			}

			rscAnnotations := rsc.GetAnnotations()

			if rscAnnotations == nil {
				rscAnnotations = make(map[string]string)
			}

			if strings.EqualFold(rsc.GroupVersionKind().Group, "apps.open-cluster-management.io") &&
				strings.EqualFold(rsc.GroupVersionKind().Kind, "Subscription") {
				// Adding cluster-admin=true annotation to child subscription
				rscAnnotations[appv1.AnnotationClusterAdmin] = "true"
				rsc.SetAnnotations(rscAnnotations)
			}
		} else {
			klog.Info("No cluster-admin. Setting it to subscription namespace " + ghsi.Subscription.Namespace)
			rsc.SetNamespace(ghsi.Subscription.Namespace)
		}
	}

	if ghsi.Subscription.Spec.PackageFilter != nil {
		errMsg := ghsi.checkFilters(rsc)
		if errMsg != "" {
			klog.V(3).Info(errMsg)

			return nil, nil, nil
		}
	}

	if ghsi.Subscription.Spec.PackageOverrides != nil {
		rsc, err = utils.OverrideResourceBySubscription(rsc, rsc.GetName(), ghsi.Subscription)
		if err != nil {
			errmsg := "Failed override package " + dpl.Name + " with error: " + err.Error()
			err = utils.SetInClusterPackageStatus(&(ghsi.Subscription.Status), dpl.GetName(), err, nil)

			if err != nil {
				errmsg += " and failed to set in cluster package status with error: " + err.Error()
			}

			klog.V(2).Info(errmsg)

			return nil, nil, errors.New(errmsg)
		}
	}

	subAnnotations := ghsi.Subscription.GetAnnotations()
	if subAnnotations != nil {
		rscAnnotations := rsc.GetAnnotations()
		if rscAnnotations == nil {
			rscAnnotations = make(map[string]string)
		}

		if strings.EqualFold(subAnnotations[appv1.AnnotationClusterAdmin], "true") {
			rscAnnotations[appv1.AnnotationClusterAdmin] = "true"
		}

		if subAnnotations[appv1.AnnotationResourceReconcileOption] != "" {
			rscAnnotations[appv1.AnnotationResourceReconcileOption] = subAnnotations[appv1.AnnotationResourceReconcileOption]
		}

		rsc.SetAnnotations(rscAnnotations)
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

	annotations[dplv1.AnnotationLocal] = "true"
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
	for packageName, chartVersions := range indexFile.Entries {
		klog.V(4).Infof("chart: %s\n%v", packageName, chartVersions)

		dpl, err := utils.CreateHelmCRDeployable(
			"", packageName, chartVersions, ghsi.synchronizer.GetLocalClient(), ghsi.Channel, ghsi.Subscription)

		if err != nil {
			klog.Error("Failed to create a helmrelease CR deployable, err: ", err)
			return err
		}

		ghsi.resources = append(ghsi.resources, kubesynchronizer.DplUnit{Dpl: dpl, Gvk: helmGvk})
	}

	return err
}

func (ghsi *SubscriberItem) cloneGitRepo() (commitID string, err error) {
	ghsi.repoRoot = utils.GetLocalGitFolder(ghsi.Channel, ghsi.Subscription)

	user := ""
	token := ""

	if ghsi.SubscriberItem.ChannelSecret != nil {
		user, token, err = utils.ParseChannelSecret(ghsi.SubscriberItem.ChannelSecret)

		if err != nil {
			return "", err
		}
	}

	return utils.CloneGitRepo(
		ghsi.Channel.Spec.Pathname,
		utils.GetSubscriptionBranch(ghsi.Subscription),
		user,
		token,
		ghsi.repoRoot,
		ghsi.Channel.Spec.InsecureSkipVerify)
}

func (ghsi *SubscriberItem) sortClonedGitRepo() error {
	if ghsi.Subscription.Spec.PackageFilter != nil && ghsi.Subscription.Spec.PackageFilter.FilterRef != nil {
		ghsi.SubscriberItem.SubscriptionConfigMap = &corev1.ConfigMap{}
		subcfgkey := types.NamespacedName{
			Name:      ghsi.Subscription.Spec.PackageFilter.FilterRef.Name,
			Namespace: ghsi.Subscription.Namespace,
		}

		err := ghsi.synchronizer.GetLocalClient().Get(context.TODO(), subcfgkey, ghsi.SubscriberItem.SubscriptionConfigMap)
		if err != nil {
			klog.Error("Failed to get filterRef configmap, error: ", err)
		}
	}

	resourcePath := ghsi.repoRoot

	annotations := ghsi.Subscription.GetAnnotations()

	if annotations[appv1.AnnotationGithubPath] != "" {
		resourcePath = filepath.Join(ghsi.repoRoot, annotations[appv1.AnnotationGithubPath])
	} else if annotations[appv1.AnnotationGitPath] != "" {
		resourcePath = filepath.Join(ghsi.repoRoot, annotations[appv1.AnnotationGitPath])
	} else if ghsi.SubscriberItem.SubscriptionConfigMap != nil {
		resourcePath = filepath.Join(ghsi.repoRoot, ghsi.SubscriberItem.SubscriptionConfigMap.Data["path"])
	}

	// chartDirs contains helm chart directories
	// crdsAndNamespaceFiles contains CustomResourceDefinition and Namespace Kubernetes resources file paths
	// rbacFiles contains ServiceAccount, ClusterRole and Role Kubernetes resource file paths
	// otherFiles contains all other Kubernetes resource file paths
	chartDirs, kustomizeDirs, crdsAndNamespaceFiles, rbacFiles, otherFiles, err := utils.SortResources(ghsi.repoRoot, resourcePath, utils.SkipHooksOnManaged)
	if err != nil {
		klog.Error(err, "Failed to sort kubernetes resources and helm charts.")
		return err
	}

	ghsi.chartDirs = chartDirs
	ghsi.kustomizeDirs = kustomizeDirs
	ghsi.crdsAndNamespaceFiles = crdsAndNamespaceFiles
	ghsi.rbacFiles = rbacFiles
	ghsi.otherFiles = otherFiles

	// Build a helm repo index file
	indexFile, err := utils.GenerateHelmIndexFile(ghsi.Subscription, ghsi.repoRoot, chartDirs)

	if err != nil {
		// If package name is not specified in the subscription, filterCharts throws an error. In this case, just return the original index file.
		klog.Error(err, "Failed to generate helm index file.")
		return err
	}

	ghsi.indexFile = indexFile

	b, _ := yaml.Marshal(ghsi.indexFile)
	klog.V(4).Info("New index file ", string(b))

	return nil
}
