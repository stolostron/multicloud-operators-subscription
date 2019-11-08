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
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
)

type versionTest struct {
	vsub   string
	vdpl   string
	result bool
}

//when not using wildrange, such as 1.x, then the range has to follow: Major.Minor.Patch

//full test case could be found at:
// github.com/blang/semver@1a9109f8c4a1d669a78442f567dfe8eedf982793/-/blob/range_test.go
var versionTests = []versionTest{
	{">=1.2.1", "v1.2.3", true},
	{">=1.0.0", "1.2.3 ", true},
	{">=1.x", "1.0.0", true},
	{">=1.x", "0.3.4", false},
	{">=1.x", "2.0.0", true},
	{"3.4.x", "3.4", true},
	{"<=1.3.1", "1.3", true},
	{">=v2.1.1", "1.1", false}, // this one is due to the range string is not following the correct format
	{">=2.1.1", "v1.1", false},
	{">=2.1.1", "v1.1", false},
	{">=1.1.0-a", "1.1.0", true},
	{">=1.1.10", "1.1.10", true},
	{">1.2.2 <1.2.5 !=1.2.4", "1.2.2", false},
	{">1.2.2 <1.2.5 !=1.2.4", "1.2.3", true},
}

func TestSemverCheck(t *testing.T) {
	for _, test := range versionTests {
		if SemverCheck(test.vsub, test.vdpl) != test.result {
			t.Errorf("vSub %v, vDpl %v, expecting %v", test.vsub, test.vdpl, test.result)
		}
	}
}

// dpllist *dplv1alpha1.DeployableList, vsub string

type groupVersionTest struct {
	caseName string
	vsub     string
	dpllist  []*dplv1alpha1.Deployable
	result   string
}

func generateFakeDpl(generateName, name, version string) dplv1alpha1.Deployable {
	return dplv1alpha1.Deployable{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateName,
			Name:         name,
			Annotations: map[string]string{
				dplv1alpha1.AnnotationDeployableVersion: version,
			},
		},
	}
}

func TestGenerateVersionSet(t *testing.T) {
	groupA := "A"
	dpl1 := generateFakeDpl(groupA, "dpl1", "1.0.0")
	dpl2 := generateFakeDpl(groupA, "dpl2", "2.0.0")
	dpl3 := generateFakeDpl(groupA, "dpl3", "1.8.0")
	dpl4 := generateFakeDpl(groupA, "dpl4", "")
	dpl5 := generateFakeDpl(groupA, "dpl5", "")

	groupB := ""
	dpl1b := generateFakeDpl(groupB, "dpl1b", "1.0.0")
	dpl2b := generateFakeDpl(groupB, "dpl2b", "2.0.0")
	dpl3b := generateFakeDpl(groupB, "dpl3b", "1.6.0")

	test1 := []dplv1alpha1.Deployable{dpl1, dpl2, dpl3}
	test2 := []dplv1alpha1.Deployable{dpl1, dpl2, dpl3}
	test3 := []dplv1alpha1.Deployable{dpl4, dpl5}

	test4 := []dplv1alpha1.Deployable{dpl4, dpl5}
	test5 := []*dplv1alpha1.Deployable{&dpl4, &dpl5}

	test6 := []dplv1alpha1.Deployable{dpl1b, dpl2b, dpl3b}

	cases := []groupVersionTest{
		{
			caseName: "1 both subscription and deployables have version info",
			vsub:     "<2.1.0",
			dpllist:  DplArrayToDplPointers(test1),
			result:   "/dpl2",
		},
		{
			caseName: "2 subscription version info is empty but deployable has a version info",
			vsub:     "",
			dpllist:  DplArrayToDplPointers(test2),
			result:   "/dpl2",
		},

		{
			caseName: "3 subscription has a verion but deployables version is empty",
			vsub:     "<2.1.0",
			dpllist:  DplArrayToDplPointers(test3),
			result:   "/dpl4",
		},

		{
			caseName: "4 both subscription and deployable has empty version info",
			vsub:     "",
			dpllist:  DplArrayToDplPointers(test4),
			result:   "/dpl4",
		},

		{
			caseName: "5 subscription,deployable has empty version info, passing in deployable pointer",
			vsub:     "",
			dpllist:  test5,
			result:   "/dpl4",
		},
	}

	for _, c := range cases {
		t.Run(c.caseName, func(t *testing.T) {
			got := GenerateVersionSet(c.dpllist, c.vsub)
			fmt.Printf("got: %#v", got)
			if !reflect.DeepEqual(got[groupA].DplKey, c.result) {
				t.Errorf("wanted %#v, got %#v", c.result, got)
			}
		})
	}

	cs2 := []groupVersionTest{
		{
			caseName: "generate name is empty",
			vsub:     "",
			dpllist:  DplArrayToDplPointers(test6),
			result:   "/dpl1b",
		},
	}

	for _, c := range cs2 {
		t.Run(c.caseName, func(t *testing.T) {
			got := GenerateVersionSet(c.dpllist, c.vsub)
			fmt.Printf("got: %#v", got)
			if !reflect.DeepEqual(got["dpl1b"].DplKey, c.result) {
				t.Errorf("wanted %#v, got %#v", c.result, got)
			}
		})
	}
}
