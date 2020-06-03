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
	"fmt"
	"strings"

	gerr "github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/kube"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/controller/helmrelease"
	subv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	helmops "github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber/helmrepo"
)

const (
	sep = ","
)

func UpdateHelmTopoAnnotation(hubClt client.Client, hubCfg *rest.Config, sub *subv1.Subscription) bool {
	subanno := sub.GetAnnotations()
	if len(subanno) == 0 {
		subanno = make(map[string]string, 0)
	}

	expectTopo, err := generateResrouceList(hubClt, hubCfg, sub)
	if err != nil {
		klog.Errorf("failed to get the resource info for helm subscription %v, err: %v", sub, err)
		return false
	}

	if subanno[subv1.AnnotationTopo] != expectTopo {
		subanno[subv1.AnnotationTopo] = expectTopo
		sub.SetAnnotations(subanno)
		return true
	}

	return false
}

func generateResrouceList(hubClt client.Client, hubCfg *rest.Config, sub *subv1.Subscription) (string, error) {
	helmRls, err := helmops.GetSubscriptionChartsOnHub(hubClt, sub)
	if err != nil {
		return "", err
	}

	res := make([]string, 0)
	for _, helmRl := range helmRls {
		resList, err := helmrelease.GenerateResourceListByConfig(hubClt, hubCfg, helmRl)
		if err != nil {
			return "", gerr.Wrap(err, "failed to get resource string")
		}

		res = append(res, parseResourceList(resList))
	}

	return strings.Join(res, sep), nil
}

type resourceUnit struct {
	name      string
	namespace string
	kind      string
	addition  string
}

func (r resourceUnit) String() string {
	return fmt.Sprintf("%v/%v/%v/%v", r.kind, r.namespace, r.name, r.addition)
}

func parseResourceList(rs kube.ResourceList) string {
	res := make([]string, 0)
	for _, resInfo := range rs {
		res = append(res, infoToUnit(resInfo).String())
	}

	return strings.Join(res, sep)
}

func infoToUnit(ri *resource.Info) resourceUnit {
	addition := ""
	k := ri.Object.GetObjectKind().GroupVersionKind().Kind
	if k == "deployment" {
		addition = "2"
	}

	return resourceUnit{
		name:      ri.Name,
		namespace: ri.Namespace,
		kind:      ri.Object.GetObjectKind().GroupVersionKind().Kind,
		addition:  addition,
	}
}
