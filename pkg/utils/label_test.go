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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Lf struct {
	name  string
	ls    *metav1.LabelSelector
	dplls map[string]string
	want  bool
}

// test cases,
//0. sub label selector is nil
////-> true to dpl
// 1. sub has label selector
//// 1.a dpl has labels
/////// -> all the labels within sub will has to be in dpl and they have exact match
//// 1.b dpl doen't have labels
////// -> false

// 2. sub doesn't have match label seletor
//-> true to all dpl

func TestLabelfilter(t *testing.T) {
	tcs := []Lf{
		{
			name: "0- sub.labelseletor == nil",
			ls:   nil,
			dplls: map[string]string{
				"package": "data",
				"version": "2"},
			want: true,
		},
		{
			name: "1.a-len(sub) == len(dpl)",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{"package": "data"},
			},
			dplls: map[string]string{
				"package": "data",
			},
			want: true,
		},
		{
			name: "1.a-len(sub) > len(dpl)",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"package": "data",
				},
			},
			dplls: map[string]string{
				"package": "data",
				"version": "2",
			},
			want: true,
		},
		{
			name: "1.a-len(sub)> len(dpl)",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"package": "data",
					"version": "2",
				},
			},
			dplls: map[string]string{"package": "data"},
			want:  false,
		},
		{
			name: "1.b-len(dpl)==0",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"package": "data",
					"version": "2",
				},
			},
			dplls: map[string]string{},
			want:  false,
		},
		{
			name: "2-len(sub)==0",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{},
			},
			dplls: map[string]string{},
			want:  false,
		},
	}

	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			got := MatchLabelForSubAndDpl(c.ls, c.dplls)
			assertMatch(t, got, c.want)
		})
	}
}

// type Lf struct {
// 	name  string
// 	ls    *metav1.LabelSelector
// 	dplls map[string]string
// 	want  bool
// }

// type LabelSelectorRequirement struct {
// 	// key is the label key that the selector applies to.
// 	// +patchMergeKey=key
// 	// +patchStrategy=merge
// 	Key string `json:"key" patchStrategy:"merge" patchMergeKey:"key" protobuf:"bytes,1,opt,name=key"`
// 	// operator represents a key's relationship to a set of values.
// 	// Valid operators are In, NotIn, Exists and DoesNotExist.
// 	Operator LabelSelectorOperator `json:"operator" protobuf:"bytes,2,opt,name=operator,casttype=LabelSelectorOperator"`
// 	// values is an array of string values. If the operator is In or NotIn,
// 	// the values array must be non-empty. If the operator is Exists or DoesNotExist,
// 	// the values array must be empty. This array is replaced during a strategic
// 	// merge patch.
// 	// +optional
// 	Values []string `json:"values,omitempty" protobuf:"bytes,3,rep,name=values"`
// }

// what the test should cover,
// 1, MatchLabels is not nil and MatchExpressions is nil
////a, dpl label is nil -- false
////b, dpl labe is not nil -- dpl label should match the MatchLabels

// 2, MatchLabels is  nil and MatchExpressions is not nil
////a, dpl label is nil --false
////b, dpl labe is not nil -- match the Expr

// 3, MatchLabels is nil and MatchExpressions is nil
////a, dpl label is nil -- true
////b, dpl labe is not nil -- true
// 4, MatchLabels is not nil and MatchExpressions is not nil
////a, dpl label is nil -- false
////b, dpl labe is not nil -- at least one of the dpl label should be qualified for MatchLabels or Expr

func TestSelector(t *testing.T) {
	testCase1(t)
	testCase2(t)
	testCase3(t)
	testCase4(t)
}

var labelKey = "package"
var ld = "data"
var labelKeyNone = "p"
var values = []string{ld, "d", "dp"}
var valuesNone = []string{"", "d", "dp"}

func testCase1(t *testing.T) {
	tcs := []Lf{
		{
			name: "1.a",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelKey: "data",
				},
				MatchExpressions: nil,
			},
			dplls: nil,
			want:  false,
		},
		{
			name: "1.b.1",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelKey: "data",
				},
				MatchExpressions: nil,
			},
			dplls: map[string]string{labelKeyNone: "data"},
			want:  false,
		},
		{
			name: "1.b.2",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelKey: "data",
				},
				MatchExpressions: nil,
			},
			dplls: map[string]string{labelKeyNone: "data"},
			want:  false,
		},
		{
			name: "1.b.3",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelKey:     "data",
					labelKeyNone: "data", // extra label will fail this case
				},
				MatchExpressions: nil,
			},
			dplls: map[string]string{labelKeyNone: "data"},
			want:  false,
		},
	}
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			got := LabelChecker(c.ls, c.dplls)
			assertMatch(t, got, c.want)
		})
	}
}

func testCase2(t *testing.T) {
	tcs := []Lf{
		{
			name: "2.a",
			ls: &metav1.LabelSelector{
				MatchLabels: nil,
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: values},
				},
			},
			dplls: nil,
			want:  false,
		},
		{
			name: "2.b.1",
			ls: &metav1.LabelSelector{
				MatchLabels: nil,
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: values},
				},
			},
			dplls: map[string]string{"pa": "data"},
			want:  false,
		},
		{
			name: "2.b.2",
			ls: &metav1.LabelSelector{
				MatchLabels: nil,
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: values},
				},
			},
			dplls: map[string]string{"package": "data"},
			want:  true,
		},
		{
			name: "2.b.3",
			ls: &metav1.LabelSelector{
				MatchLabels: nil,
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: values},
					{Key: labelKeyNone, Operator: metav1.LabelSelectorOpIn, Values: valuesNone},
				},
			},
			dplls: map[string]string{"package": "data"},
			want:  false,
		},
	}
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			got := LabelChecker(c.ls, c.dplls)
			assertMatch(t, got, c.want)
		})
	}
}

func testCase3(t *testing.T) {
	tcs := []Lf{
		{
			name: "3.a",
			ls: &metav1.LabelSelector{
				MatchLabels:      nil,
				MatchExpressions: nil,
			},
			dplls: nil,
			want:  true,
		},
		{
			name: "3.b.1",
			ls: &metav1.LabelSelector{
				MatchLabels:      nil,
				MatchExpressions: nil,
			},
			dplls: map[string]string{"pa": "data"},
			want:  true,
		},
		{
			name: "3.b.2",
			ls: &metav1.LabelSelector{
				MatchLabels:      nil,
				MatchExpressions: nil,
			},
			dplls: map[string]string{"package": "data"},
			want:  true,
		},
		{
			name: "3.b.3",
			ls: &metav1.LabelSelector{
				MatchLabels:      nil,
				MatchExpressions: nil,
			},
			dplls: map[string]string{"package": "data"},
			want:  true,
		},
	}
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			got := LabelChecker(c.ls, c.dplls)
			assertMatch(t, got, c.want)
		})
	}
}

func testCase4(t *testing.T) {
	tcs := []Lf{
		{
			name: "4.a",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{labelKey: ld},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: values},
				},
			},
			dplls: nil,
			want:  false,
		},
		{
			name: "4.b.1",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{labelKey: ld},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: values},
				},
			},
			dplls: map[string]string{labelKey: ld, labelKeyNone: ld},
			want:  true,
		},
		{
			name: "4.b.2",
			ls: &metav1.LabelSelector{
				MatchLabels: map[string]string{labelKey: ld},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: labelKey, Operator: metav1.LabelSelectorOpIn, Values: values},
				},
			},
			dplls: map[string]string{labelKey: ld},
			want:  true,
		},
	}
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			got := LabelChecker(c.ls, c.dplls)
			assertMatch(t, got, c.want)
		})
	}
}

func assertMatch(t *testing.T, got, want bool) {
	t.Helper()

	if got != want {
		t.Errorf("want %v, got %v", want, got)
	}
}
