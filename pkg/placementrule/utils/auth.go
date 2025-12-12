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

package utils

import (
	"context"
	"encoding/base64"
	"strings"

	rbacv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	spokeClusterV1 "open-cluster-management.io/api/cluster/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
)

var (
	AdminUsers = map[string]bool{
		"admin":                               true,
		"multicluster-observability-operator": true,
		"openshift-gitops-argocd-application-controller": true,
	}
	AdminGroups = map[string]bool{
		"masters":        true,
		"cluster-admins": true,
	}
)

func FilteClustersByIdentity(authClient kubernetes.Interface, object runtime.Object, clmap map[string]*spokeClusterV1.ManagedCluster) error {
	objmeta, err := meta.Accessor(object)
	if err != nil {
		return nil
	}

	objanno := objmeta.GetAnnotations()
	if objanno == nil {
		return nil
	}

	if _, ok := objanno[appv1.UserIdentityAnnotation]; !ok {
		return nil
	}

	var clusters []*spokeClusterV1.ManagedCluster

	for _, cl := range clmap {
		clusters = append(clusters, cl.DeepCopy())
	}

	clusters = filterClusterByUserIdentity(object, clusters, authClient, "managedclusters", "get")
	validclMap := make(map[string]bool)

	for _, cl := range clusters {
		validclMap[cl.GetName()] = true
	}

	for k := range clmap {
		if valid, ok := validclMap[k]; !ok || !valid {
			delete(clmap, k)
		}
	}

	return nil
}

// filterClusterByUserIdentity filters cluster by checking if user can act on resources
func filterClusterByUserIdentity(
	obj runtime.Object,
	clusters []*spokeClusterV1.ManagedCluster,
	kubeclient kubernetes.Interface,
	resource, verb string,
) []*spokeClusterV1.ManagedCluster {
	if kubeclient == nil {
		return clusters
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return clusters
	}

	annotations := accessor.GetAnnotations()
	if annotations == nil {
		return clusters
	}

	filteredClusters := []*spokeClusterV1.ManagedCluster{}

	for _, cluster := range clusters {
		user, groups := ExtractUserAndGroup(annotations)
		sar := &rbacv1.SubjectAccessReview{
			Spec: rbacv1.SubjectAccessReviewSpec{
				ResourceAttributes: &rbacv1.ResourceAttributes{
					Name:     cluster.Name,
					Group:    "cluster.open-cluster-management.io",
					Verb:     verb,
					Resource: resource,
				},
				User:   user,
				Groups: groups,
			},
		}
		result, err := kubeclient.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, v1.CreateOptions{})
		klog.V(1).Infof("user: %v, groups: %v, namespace:%v, result:%v, err:%v", user, groups, cluster.Namespace, result, err)

		if err != nil {
			continue
		}

		if !result.Status.Allowed {
			continue
		}

		filteredClusters = append(filteredClusters, cluster)
	}

	return filteredClusters
}

func ExtractUserAndGroup(annotations map[string]string) (string, []string) {
	var user string

	var groups []string

	encodedUser, ok := annotations[appv1.UserIdentityAnnotation]
	if ok {
		decodedUser, err := base64.StdEncoding.DecodeString(encodedUser)
		if err == nil {
			user = string(decodedUser)
		}
	}

	encodedGroups, ok := annotations[appv1.UserGroupAnnotation]
	if ok {
		decodedGroup, err := base64.StdEncoding.DecodeString(encodedGroups)
		if err == nil {
			groups = strings.Split(string(decodedGroup), ",")
		}
	}

	return user, groups
}

func IfClusterAdmin(user string, groups []string) bool {
	newUser := user

	ss := strings.Split(user, ":")
	if len(ss) > 0 {
		newUser = ss[len(ss)-1]
	}

	if _, ok := AdminUsers[newUser]; ok {
		return true
	}

	for _, group := range groups {
		//nolint:copyloopvar
		newGroup := group

		gg := strings.Split(group, ":")
		if len(gg) > 0 {
			newGroup = gg[len(gg)-1]
		}

		if _, ok := AdminGroups[newGroup]; ok {
			return true
		}
	}

	return false
}
