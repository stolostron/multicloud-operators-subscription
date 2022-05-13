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

package placementrule

import (
	"context"
	"sort"

	"k8s.io/klog"

	"strings"

	rbacv1 "k8s.io/api/authorization/v1"
	cbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/placementrule/utils"
)

func (r *ReconcilePlacementRule) hubReconcile(instance *appv1alpha1.PlacementRule) error {
	// Return zero cluster decision if ClusterReplicas is set to 0, thus don't need to go through other filters for better performance
	if instance.Spec.ClusterReplicas != nil {
		total := int(*instance.Spec.ClusterReplicas)
		if total == 0 {
			instance.Status.Decisions = []appv1alpha1.PlacementDecision{}

			return nil
		}
	}

	clmap, err := utils.PlaceByGenericPlacmentFields(r.Client, instance.Spec.GenericPlacementFields, instance)
	if err != nil {
		klog.Error("Error in preparing clusters by status:", err)

		return err
	}

	err = r.filteClustersByStatus(instance, clmap /* , clstatusmap */)
	if err != nil {
		klog.Error("Error in filtering clusters by status:", err)

		return err
	}

	err = r.filteClustersByUser(instance, clmap)
	if err != nil {
		klog.Error("Error in filtering clusters by user Identity:", err)

		return err
	}

	err = r.filteClustersByPolicies(instance, clmap /* , clstatusmap */)
	if err != nil {
		klog.Error("Error in filtering clusters by policy:", err)

		return err
	}

	// go without mcm repositories, removed identity check

	clidx := r.sortClustersByResourceHint(instance, clmap /* , clstatusmap */)

	newpd := r.pickClustersByReplicas(instance, clmap, clidx)

	instance.Status.Decisions = newpd

	return nil
}

func (r *ReconcilePlacementRule) filteClustersByStatus(instance *appv1alpha1.PlacementRule, clmap map[string]*spokeClusterV1.ManagedCluster) error {
	if instance == nil || instance.Spec.ClusterConditions == nil || clmap == nil {
		return nil
	}

	// store all cluster condition defined in the placementrule instance to map[type]status
	placementClusterCondMap := make(map[string]string)
	for _, cond := range instance.Spec.ClusterConditions {
		placementClusterCondMap[cond.Type] = string(cond.Status)
	}

	for k, cl := range clmap {
		condMatched := true

		for _, clCond := range cl.Status.Conditions {
			if placementClusterStatus, ok := placementClusterCondMap[clCond.Type]; ok {
				if placementClusterStatus != string(clCond.Status) {
					condMatched = false
					break
				}
			}
		}

		if !condMatched {
			delete(clmap, k)
		}
	}

	klog.Infof("Cluster Conditions Check done, placementrule: %v/%v ", instance.Namespace, instance.Name)

	return nil
}

type clusterInfo struct {
	Name      string
	Namespace string
	Metrics   resource.Quantity
}

func (cinfo clusterInfo) DeepCopyInto(newinfo *clusterInfo) {
	newinfo.Name = cinfo.Name
	newinfo.Namespace = cinfo.Namespace
	cinfo.Metrics.DeepCopyInto(&(newinfo.Metrics))
}

type clusterIndex struct {
	Ascedent bool
	Clusters []clusterInfo
}

func (ci clusterIndex) Len() int {
	return len(ci.Clusters)
}

func (ci clusterIndex) Less(x, y int) bool {
	less := (ci.Clusters[x].Metrics.Cmp(ci.Clusters[y].Metrics) == -1)

	if !ci.Ascedent {
		return !less
	}

	return less
}

func (ci clusterIndex) Swap(x, y int) {
	tmp := clusterInfo{}
	ci.Clusters[x].DeepCopyInto(&tmp)
	ci.Clusters[y].DeepCopyInto(&(ci.Clusters[x]))
	tmp.DeepCopyInto(&(ci.Clusters[y]))
}

func (r *ReconcilePlacementRule) sortClustersByResourceHint(instance *appv1alpha1.PlacementRule,
	clmap map[string]*spokeClusterV1.ManagedCluster) *clusterIndex {
	sortedcls := &clusterIndex{}

	if instance == nil || clmap == nil || instance.Spec.ResourceHint == nil {
		return nil
	}

	sortedcls.Ascedent = false
	if instance.Spec.ResourceHint.Order == appv1alpha1.SelectionOrderAsce {
		sortedcls.Ascedent = true
	}

	for _, cl := range clmap {
		newcli := clusterInfo{
			Name:      cl.Name,
			Namespace: cl.Name,
		}

		if instance.Spec.ResourceHint.Type != "" && cl.Status.Allocatable != nil {
			switch instance.Spec.ResourceHint.Type {
			case appv1alpha1.ResourceTypeCPU:
				newcli.Metrics = cl.Status.Allocatable[spokeClusterV1.ResourceCPU]
			case appv1alpha1.ResourceTypeMemory:
				newcli.Metrics = cl.Status.Allocatable[spokeClusterV1.ResourceMemory]
			}
		}

		sortedcls.Clusters = append(sortedcls.Clusters, newcli)
	}

	sort.Sort(sortedcls)

	return sortedcls
}

func (r *ReconcilePlacementRule) pickClustersByReplicas(instance *appv1alpha1.PlacementRule,
	clmap map[string]*spokeClusterV1.ManagedCluster, clidx *clusterIndex) []appv1alpha1.PlacementDecision {
	newpd := []appv1alpha1.PlacementDecision{}
	total := len(clmap)

	if instance.Spec.ClusterReplicas != nil && total > int(*(instance.Spec.ClusterReplicas)) {
		total = int(*instance.Spec.ClusterReplicas)
	}

	picked := 0

	// no sort, pick existing decisions first, then clmap
	if clidx == nil {
		for _, cli := range instance.Status.Decisions {
			// check if still eligible
			if _, ok := clmap[cli.ClusterName]; !ok {
				continue
			}

			if picked < total {
				pd := appv1alpha1.PlacementDecision{
					ClusterName:      cli.ClusterName,
					ClusterNamespace: cli.ClusterName,
				}
				newpd = append(newpd, pd)

				delete(clmap, cli.ClusterName)
				picked++
			} else {
				break
			}
		}

		for _, cl := range clmap {
			if picked < total {
				pd := appv1alpha1.PlacementDecision{
					ClusterName:      cl.Name,
					ClusterNamespace: cl.Name,
				}
				newpd = append(newpd, pd)
				picked++
			} else {
				break
			}
		}
	} else {
		// sort by something
		for _, cli := range clidx.Clusters {
			if _, ok := clmap[cli.Name]; !ok {
				continue
			}
			if picked < total {
				pd := appv1alpha1.PlacementDecision{
					ClusterName:      cli.Name,
					ClusterNamespace: cli.Name,
				}
				newpd = append(newpd, pd)
				picked++
			} else {
				break
			}
		}
	}

	klog.V(1).Info("New decisions for ", instance.Name, ": ", newpd)

	return newpd
}

func (r *ReconcilePlacementRule) filteClustersByPolicies(instance *appv1alpha1.PlacementRule,
	clmap map[string]*spokeClusterV1.ManagedCluster /* , clstatusmap map[string]*mcmv1alpha1.ClusterStatus */) error {
	if instance == nil || instance.Spec.Policies == nil || clmap == nil {
		return nil
	}

	return nil
}

func (r *ReconcilePlacementRule) filteClustersByUser(instance *appv1alpha1.PlacementRule,
	clmap map[string]*spokeClusterV1.ManagedCluster) error {
	if instance == nil || clmap == nil {
		return nil
	}

	annotations := instance.GetAnnotations()
	if annotations == nil {
		return nil
	}

	if _, ok := annotations[appv1alpha1.UserIdentityAnnotation]; !ok {
		return nil
	}

	// if user or groups are known admin cluster, return all selected clusters
	user, groups := utils.ExtractUserAndGroup(annotations)
	if utils.IfClusterAdmin(user, groups) {
		klog.Infof("All fetched clusters are returned as user/group is cluster admin. user: %v, groups: %v ", user, groups)

		return nil
	}

	// Check if the user has the GET cluster role for managedclusters resource
	// if no resource name, return all selected clusters.
	// if there is resource name list, return all selected clusters in the resource name list
	// Thus normal RBAC users won't need to create SelfSubjectAccessReview to check if the user can get each managed cluster
	if r.IfClusterAdminByClusterRole(user, groups, clmap) {
		klog.Infof("The user/groups has the GET cluster role for managedclusters resource. user: %v, groups: %v", user, groups)

		return nil
	}

	klog.Infof("clean up cluster map as the user/groups can't find any clusterRoles for managedclusters resource. user: %v, groups: %v", user, groups)

	for cl := range clmap {
		delete(clmap, cl)
	}

	return nil
}

func (r *ReconcilePlacementRule) IfClusterAdminByClusterRole(user string, groups []string, clmap map[string]*spokeClusterV1.ManagedCluster) bool {
	bindingList := &cbacv1.ClusterRoleBindingList{}

	err := r.List(context.TODO(), bindingList)

	if err != nil {
		klog.Errorf("Failed to fetch clusterRoleBinding list. err: %v", err)

		return false
	}

	for _, binding := range bindingList.Items {
		subjectFound := false

		for _, subject := range binding.Subjects {
			if strings.Trim(subject.Name, "") == strings.Trim(user, "") && strings.Trim(subject.Kind, "") == "User" {
				subjectFound = true

				break
			} else if subject.Kind == "ServiceAccount" && subject.Namespace != "" && subject.Name != "" {
				if strings.Trim(user, "") == "system:serviceaccount:"+subject.Namespace+":"+subject.Name {
					subjectFound = true

					break
				}
			} else if subject.Kind == "Group" {
				for _, groupName := range groups {
					if strings.Trim(subject.Name, "") == strings.Trim(groupName, "") {
						subjectFound = true

						break
					}
				}
			}
		}

		if subjectFound {
			klog.Infof("clusterRoleBinding %v found for user:%v, groups:%v ", binding.Name, user, groups)

			if binding.RoleRef.APIGroup == "rbac.authorization.k8s.io" && binding.RoleRef.Kind == "ClusterRole" &&
				r.IfGetManagedCluster(binding.RoleRef.Name, clmap) {
				klog.Infof("clusterRole %v found for user:%v, groups: %v", binding.RoleRef.Name, user, groups)

				return true
			}
		}
	}

	return false
}

func (r *ReconcilePlacementRule) IfGetManagedCluster(clusterRoleName string, clmap map[string]*spokeClusterV1.ManagedCluster) bool {
	clusterRoleKey := types.NamespacedName{Name: clusterRoleName}
	clusterRole := &cbacv1.ClusterRole{}

	err := r.Get(context.TODO(), clusterRoleKey, clusterRole)

	if err != nil {
		klog.Errorf("Failed to fetch clusterRole. clusterRoleKey: %v, err: %v", clusterRoleKey, err)

		return false
	}

	for _, rule := range clusterRole.Rules {
		ifGetManagedCluster := false

		for _, apiGroup := range rule.APIGroups {
			if apiGroup == "cluster.open-cluster-management.io" || apiGroup == "*" {
				ifGetManagedCluster = true

				break
			}
		}

		if !ifGetManagedCluster {
			continue
		}

		for _, resource := range rule.Resources {
			if resource == "managedclusters" || resource == "*" {
				ifGetManagedCluster = true

				break
			}
		}

		if !ifGetManagedCluster {
			continue
		}

		for _, verb := range rule.Verbs {
			if verb == "get" || verb == "*" {
				ifGetManagedCluster = true

				break
			}
		}

		if !ifGetManagedCluster {
			continue
		}

		if len(rule.ResourceNames) == 0 {
			klog.Infof("return all selected clusters. clusterRole: %v, rule: %#v", clusterRole.Name, rule)

			return true
		}

		clusterListinClusterRole := map[string]bool{}

		for _, cluster := range rule.ResourceNames {
			clusterListinClusterRole[cluster] = true
		}

		for clusterName, _ := range clmap {
			if _, ok := clusterListinClusterRole[clusterName]; !ok {
				delete(clmap, clusterName)
				klog.Infof("cluster %v not found in the cluster role resource name list", clusterName)
			}
		}

		return true
	}

	return false
}

func (r *ReconcilePlacementRule) filteClustersByIdentityAnno(instance *appv1alpha1.PlacementRule, clmap map[string]*spokeClusterV1.ManagedCluster) {
	objanno := instance.GetAnnotations()
	if objanno == nil {
		return
	}

	if _, ok := objanno[appv1alpha1.UserIdentityAnnotation]; !ok {
		return
	}

	user, groups := utils.ExtractUserAndGroup(objanno)

	userKubeClient := r.Impersonate(user, groups)

	for clusterName, cl := range clmap {
		manageCluster := cl.DeepCopy()
		if !r.checkUserPermission(userKubeClient, manageCluster, user, groups) {
			delete(clmap, clusterName)
		}
	}

	r.UnsetImpersonate(user, groups)

	return
}

// checkUserPermission checks if user can get managedCluster KIND resource.
func (r *ReconcilePlacementRule) checkUserPermission(userKubeClient *kubernetes.Clientset, managedCluster *spokeClusterV1.ManagedCluster,
	user string, groups []string) bool {
	clusterName := managedCluster.GetName()

	selfSAR := &rbacv1.SelfSubjectAccessReview{
		Spec: rbacv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &rbacv1.ResourceAttributes{
				Name:     clusterName,
				Group:    "cluster.open-cluster-management.io",
				Verb:     "get",
				Resource: "managedclusters",
			},
		},
	}

	result, err := userKubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(context.TODO(), selfSAR, v1.CreateOptions{})
	klog.Infof("user: %v, groups: %v, cluster:%v, result:%v, err:%v", user, groups, clusterName, result, err)

	if err != nil {
		return false
	}

	if !result.Status.Allowed {
		return false
	}

	return true
}

func (r *ReconcilePlacementRule) Impersonate(user string, groups []string) *kubernetes.Clientset {
	klog.Info("Impersonate user config...")

	userKubeConfig := r.authConfig
	userKubeConfig.Impersonate = rest.ImpersonationConfig{
		UserName: user,
		Groups:   groups,
	}

	userKubeClient, err := kubernetes.NewForConfig(userKubeConfig)
	if err != nil {
		klog.Errorf("Impersonate user failed. user: %v, groups: %v, err: %v", user, groups, err)

		return r.UnsetImpersonate(user, groups)
	}

	return userKubeClient
}

func (r *ReconcilePlacementRule) UnsetImpersonate(user string, groups []string) *kubernetes.Clientset {
	klog.Info("Unset Impersonate user config...")

	userKubeConfig := r.authConfig
	userKubeConfig.Impersonate = rest.ImpersonationConfig{
		UserName: "",
		Groups:   nil,
	}

	userKubeClient, err := kubernetes.NewForConfig(userKubeConfig)
	if err != nil {
		klog.Errorf("Unset Impersonate user failed. user: %v, groups: %v, err: %v", user, groups, err)

		return kubernetes.NewForConfigOrDie(r.authConfig)
	}

	return userKubeClient
}
