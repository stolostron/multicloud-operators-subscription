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

package v1

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

var (
	key = types.NamespacedName{
		Name:      "foo",
		Namespace: "default",
	}

	helm = &HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
	}

	s1 = Source{
		SourceType: HelmRepoSourceType,
		HelmRepo:   &HelmRepo{Urls: []string{""}},
	}
	s2 = Source{
		SourceType: GitHubSourceType,
		GitHub:     &GitHub{Urls: []string{""}, Branch: "main", ChartPath: "default"},
	}
	s3 = Source{
		SourceType: GitSourceType,
		Git:        &Git{Urls: []string{""}, Branch: "main", ChartPath: "default"},
	}
	sDefault = Source{
		SourceType: "unknown",
	}

	alts1 = AltSource{
		SourceType: HelmRepoSourceType,
		HelmRepo:   &HelmRepo{Urls: []string{""}},
	}
	alts2 = AltSource{
		SourceType: GitHubSourceType,
		GitHub:     &GitHub{Urls: []string{""}, Branch: "main", ChartPath: "default"},
	}
	alts3 = AltSource{
		SourceType: GitSourceType,
		Git:        &Git{Urls: []string{""}, Branch: "main", ChartPath: "default"},
	}
	altsDefault = AltSource{
		SourceType: "unknown",
	}

	helmClone = HelmReleaseRepo{
		ChartName: "foo",
		Version:   "v1",
		Digest:    "0c2a26276afec20dfddb4cbafa72057aa6625e1eb32843bcaa87ebe176d9f058",
		AltSource: &AltSource{
			SourceType: GitHubSourceType,
			GitHub:     &GitHub{},
			Git:        &Git{},
			HelmRepo:   &HelmRepo{},
		},
		SecretRef:                     nil,
		ConfigMapRef:                  nil,
		InsecureSkipVerify:            false,
		Source:                        &Source{},
		WatchNamespaceScopedResources: true,
	}

	helmAltSource = HelmReleaseRepo{
		ChartName:                     "foo",
		Version:                       "v1",
		Digest:                        "0c2a26276afec20dfddb4cbafa72057aa6625e1eb32843bcaa87ebe176d9f058",
		WatchNamespaceScopedResources: true,
		AltSource: &AltSource{
			SourceType: GitHubSourceType,
			GitHub:     &GitHub{},
			Git:        &Git{},
			HelmRepo:   &HelmRepo{},
		},
		SecretRef:          nil,
		ConfigMapRef:       nil,
		InsecureSkipVerify: false,
		Source: &Source{
			SourceType: GitHubSourceType,
			GitHub:     &GitHub{},
			Git:        &Git{},
			HelmRepo:   &HelmRepo{},
		},
	}

	unstructuredObj = &unstructured.Unstructured{
		Object: map[string]interface{}{"status": "Running"},
	}
	unstructuredObj2 = &unstructured.Unstructured{
		Object: map[string]interface{}{"status": map[string]interface{}{}},
	}
	unstructuredObj3 = &unstructured.Unstructured{
		Object: map[string]interface{}{"status": &HelmAppStatus{}},
	}
)

func TestHelmRelease(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Create
	fetched := &HelmRelease{}

	created := helm.DeepCopy()
	g.Expect(c.Create(context.TODO(), created)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Get(context.TODO(), key, fetched)).NotTo(gomega.HaveOccurred())

	// Source.String()
	g.Expect(s1.String()).To(gomega.Equal("[]"))
	g.Expect(s2.String()).To(gomega.Equal("[]|main|default"))
	g.Expect(s3.String()).To(gomega.Equal("[]|main|default"))
	g.Expect(sDefault.String()).To(gomega.Equal("SourceType unknown not supported"))

	// AltSource.String()
	g.Expect(alts1.String()).To(gomega.Equal("[]"))
	g.Expect(alts2.String()).To(gomega.Equal("[]|main|default"))
	g.Expect(alts3.String()).To(gomega.Equal("[]|main|default"))
	g.Expect(altsDefault.String()).To(gomega.Equal("SourceType unknown not supported"))

	// HelmReleaseRepo.Clone()
	g.Expect(helmClone.Clone()).To(gomega.Equal(helmClone))

	// HelmReleaseRepo.AltSourceToSource()
	g.Expect(helmClone.AltSourceToSource()).To(gomega.Equal(helmAltSource))

	// HelmAppStatus.ToMap()
	helmAppStat := &HelmAppStatus{}
	out, err := helmAppStat.ToMap()
	g.Expect(out).To(gomega.Equal(map[string]interface{}{"conditions": nil}))
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// HelmAppStatus.SetCondition()
	helmAppStat = helmAppStat.SetCondition(HelmAppCondition{})
	g.Expect(helmAppStat).To(gomega.Equal(helmAppStat))

	helmAppWithConds := &HelmAppStatus{Conditions: []HelmAppCondition{{Type: "default", Status: "Running"}}}
	helmAppWithConds = helmAppWithConds.SetCondition(HelmAppCondition{Type: "default", Status: "Failed"})
	g.Expect(helmAppWithConds).To(gomega.Equal(helmAppWithConds))

	helmAppWithConds = helmAppWithConds.SetCondition(HelmAppCondition{Type: "default", Status: "Failed"})
	g.Expect(helmAppWithConds).To(gomega.Equal(helmAppWithConds))

	// HelmAppStatus.RemoveCondition()
	helmAppStat = &HelmAppStatus{}
	helmAppStat = helmAppStat.RemoveCondition("default")
	g.Expect(helmAppStat).To(gomega.Equal(helmAppStat))

	helmAppWithConds = &HelmAppStatus{Conditions: []HelmAppCondition{{Type: "default", Status: "Running"}}}
	helmAppWithConds = helmAppWithConds.RemoveCondition("default")
	g.Expect(helmAppWithConds).To(gomega.Equal(helmAppWithConds))

	// StatusFor()
	g.Expect(StatusFor(unstructuredObj)).To(gomega.Equal(&HelmAppStatus{}))
	g.Expect(StatusFor(unstructuredObj2)).To(gomega.Equal(&HelmAppStatus{}))
	g.Expect(StatusFor(unstructuredObj3)).To(gomega.Equal(&HelmAppStatus{}))
}
