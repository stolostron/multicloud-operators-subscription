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
	"fmt"
	"unicode"

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

// ValidateK8sLabel returns a valid k8s label string by enforcing k8s label values rules as below
// 1. Must consist of alphanumeric characters, '-', '_' or '.'
//    No need to check this as the input string is the host name of the k8s api url
// 2. Must be no more than 63 characters
// 3. Must start and end with an alphanumeric character
func ValidateK8sLabel(s string) string {
	// must be no more than 63 characters
	s = fmt.Sprintf("%.63s", s)

	// look for the first alphanumeric byte from the start
	start := 0
	for ; start < len(s); start++ {
		c := s[start]
		if unicode.IsLetter(rune(c)) || unicode.IsNumber(rune(c)) {
			break
		}
	}

	// Now look for the first alphanumeric byte from the end
	stop := len(s)
	for ; stop > start; stop-- {
		c := s[stop-1]
		if unicode.IsLetter(rune(c)) || unicode.IsNumber(rune(c)) {
			break
		}
	}

	return s[start:stop]
}

// TrimLabelLast63Chars returns the last 63 characters of the input string
func TrimLabelLast63Chars(s string) string {
	if len(s) > 63 {
		s = s[len(s)-63:]
	}

	return s
}
