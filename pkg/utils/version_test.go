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

	"github.com/google/go-cmp/cmp"

	dplv1alpha1 "github.com/open-cluster-management/multicloud-operators-deployable/pkg/apis/apps/v1"
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

func TestGenerateVersionSetWithVersionInfo(t *testing.T) {
	groupA := "A"
	groupB := ""

	dpl1 := generateFakeDpl(groupA, "dpl1", "1.0.0")
	dpl2 := generateFakeDpl(groupA, "dpl2", "2.0.0")
	dpl3 := generateFakeDpl(groupA, "dpl3", "1.8.0")

	dpl1b := generateFakeDpl(groupB, "dpl1b", "1.0.0")
	dpl2b := generateFakeDpl(groupB, "dpl2b", "2.0.0")
	dpl3b := generateFakeDpl(groupB, "dpl3b", "1.6.0")

	test1 := []dplv1alpha1.Deployable{dpl1, dpl2, dpl3}
	test2 := []dplv1alpha1.Deployable{dpl1, dpl2, dpl3}

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

//While version info is empty, we will treat them as separate resouces, in which
// we will use the name instead of the generate name for the key of the version set
func TestGenerateVersionSetWithEmptyVersionInfo(t *testing.T) {
	groupA := "A"
	groupB := ""

	dpl2 := generateFakeDpl(groupA, "dpl2", "2.0.0")
	dpl215 := generateFakeDpl(groupA, "dpl215", "1.5.0")
	dpl4 := generateFakeDpl(groupA, "dpl4", "")
	dpl5 := generateFakeDpl(groupA, "dpl5", "")

	dpl2b := generateFakeDpl(groupB, "dpl2b", "2.0.0")
	dpl4b := generateFakeDpl(groupB, "dpl4b", "")
	dpl5b := generateFakeDpl(groupB, "dpl5b", "")

	testCases := []struct {
		desc       string
		dpls       []*dplv1alpha1.Deployable
		vstring    string
		versionSet map[string]VersionRep
	}{
		{
			desc:    "empty version fields",
			dpls:    []*dplv1alpha1.Deployable{&dpl4, &dpl5, &dpl4b, &dpl5b},
			vstring: "",
			versionSet: map[string]VersionRep{
				"dpl4":  {DplKey: "/dpl4", Vrange: ">0.0.0"},
				"dpl5":  {DplKey: "/dpl5", Vrange: ">0.0.0"},
				"dpl4b": {DplKey: "/dpl4b", Vrange: ">0.0.0"},
				"dpl5b": {DplKey: "/dpl5b", Vrange: ">0.0.0"},
			},
		},
		{
			desc:    "empty and valid version fields",
			dpls:    []*dplv1alpha1.Deployable{&dpl4, &dpl4b, &dpl2, &dpl2b},
			vstring: "",
			versionSet: map[string]VersionRep{
				"dpl4":  {DplKey: "/dpl4", Vrange: ">0.0.0"},
				"dpl4b": {DplKey: "/dpl4b", Vrange: ">0.0.0"},
				groupA:  {DplKey: "/dpl2", Vrange: ">2.0.0"},
				"dpl2b": {DplKey: "/dpl2b", Vrange: ">2.0.0"},
			},
		},
		{
			desc:    "empty and valid version fields",
			dpls:    []*dplv1alpha1.Deployable{&dpl4, &dpl4b, &dpl2, &dpl215, &dpl2b},
			vstring: "<2.0.0",
			versionSet: map[string]VersionRep{
				"dpl4":  {DplKey: "/dpl4", Vrange: ">0.0.0"},
				"dpl4b": {DplKey: "/dpl4b", Vrange: ">0.0.0"},
				groupA:  {DplKey: "/dpl215", Vrange: ">1.5.0"},
			},
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := GenerateVersionSet(tC.dpls, tC.vstring)
			assertVersionSet(t, got, tC.versionSet)
		})
	}
}

func assertVersionSet(t *testing.T, got, want map[string]VersionRep) {
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("GenerateVersionSet got error: got %v, want %v", got, want)
		t.Errorf("MakeGatewayInfo() mismatch (-want +got):\n%s", diff)
	}
}

func TestIsDeployableInVersionSet(t *testing.T) {
	groupA := "A"
	groupB := ""

	dpl2 := generateFakeDpl(groupA, "dpl2", "2.0.0")
	dpl215 := generateFakeDpl(groupA, "dpl215", "1.5.0")
	dpl4 := generateFakeDpl(groupA, "dpl4", "")
	dpl5 := generateFakeDpl(groupA, "dpl5", "")

	dpl2b := generateFakeDpl(groupB, "dpl2b", "2.0.0")
	dpl4b := generateFakeDpl(groupB, "dpl4b", "")
	dpl5b := generateFakeDpl(groupB, "dpl5b", "")

	testCases := []struct {
		desc        string
		dpls        []*dplv1alpha1.Deployable
		vstring     string
		checkingDpl *dplv1alpha1.Deployable
		want        bool
	}{
		{
			desc:        "empty version fields",
			dpls:        []*dplv1alpha1.Deployable{&dpl4, &dpl5, &dpl4b, &dpl5b},
			vstring:     "",
			checkingDpl: &dpl4,
			want:        true,
		},
		{
			desc:        "version <2.0.0 empty and valid version fields",
			dpls:        []*dplv1alpha1.Deployable{&dpl4, &dpl4b, &dpl2, &dpl215, &dpl2b},
			vstring:     "<2.0.0",
			checkingDpl: &dpl2,
			want:        false,
		},
		{
			desc:        "version <2.0.0 on dpl215 empty and valid version fields",
			dpls:        []*dplv1alpha1.Deployable{&dpl4, &dpl4b, &dpl2, &dpl215, &dpl2b},
			vstring:     "<2.0.0",
			checkingDpl: &dpl215,
			want:        true,
		},
		{
			desc:        "version >2.0.0 empty and valid version fields",
			dpls:        []*dplv1alpha1.Deployable{&dpl4, &dpl4b, &dpl2, &dpl215, &dpl2b},
			vstring:     ">1.0.0",
			checkingDpl: &dpl2,
			want:        true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			vset := GenerateVersionSet(tC.dpls, tC.vstring)
			got := IsDeployableInVersionSet(vset, tC.checkingDpl)

			if got != tC.want {
				t.Errorf("case %v failed, wanted %v, got %v for deployable %v with version %v",
					tC.desc, tC.want, got,
					tC.checkingDpl.GetName(), tC.checkingDpl.GetAnnotations())
			}
		})
	}
}
