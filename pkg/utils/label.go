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

package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

func MatchLabelForSubAndDpl(ls *metav1.LabelSelector, dplls map[string]string) bool {
	klog.V(5).Infof("sub label: %#v, dpl label: %#v", ls, dplls)

	if ls == nil {
		return true
	}

	if len(dplls) < 1 {
		return false
	}

	for k, mv := range ls.MatchLabels {
		v, ok := dplls[k]
		if !ok {
			return false
		}

		if v != mv {
			return false
		}
	}

	return true
}

// to leverage the expression and matcthlabels, we can do the following,
// 1, get a selector from LabelSelector
//		https://github.com/kubernetes/apimachinery/blob/461753078381c979582f217a28eb759ebee5295d/pkg/apis/meta/v1/helpers.go#L34
// this is done at:
// clSelector, err := dplutils.ConvertLabels(s.Subscription.Spec.PackageFilter.LabelSelector)
// func ConvertLabels(labelSelector *metav1.LabelSelector) (labels.Selector, error) {}

// 2, use selector to match up, https://github.com/kubernetes/apimachinery/blob/master/pkg/labels/selector_test.go

func LabelChecker(ls *metav1.LabelSelector, dplls map[string]string) bool {
	clSelector, err := ConvertLabels(ls)
	if err != nil { // which means the subscription's lable selector is not set up correctly
		klog.Infof("Can't process labels due to: %v. In this case, all labels will be rejected", err)
		return false
	}

	return clSelector.Matches(labels.Set(dplls))
}
