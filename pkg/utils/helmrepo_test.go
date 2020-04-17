// Copyright 2020 The Kubernetes Authors.
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
	"testing"
	"time"

	"github.com/onsi/gomega"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestGetPackageAlias(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	pkgAlias := GetPackageAlias(githubsub, "")
	g.Expect(pkgAlias).To(gomega.Equal(""))

	pkgOverrides1 := &appv1.Overrides{}
	pkgOverrides1.PackageName = "pkgName1"

	pkgOverrides2 := &appv1.Overrides{}
	pkgOverrides2.PackageName = "pkgName2"
	pkgOverrides2.PackageAlias = "pkgName2Alias"

	packageOverrides := make([]*appv1.Overrides, 0)
	packageOverrides = append(packageOverrides, pkgOverrides1, pkgOverrides2)

	githubsub.Spec.PackageOverrides = packageOverrides

	pkgAlias = GetPackageAlias(githubsub, "pkgName1")
	g.Expect(pkgAlias).To(gomega.Equal(""))

	pkgAlias = GetPackageAlias(githubsub, "pkgName2")
	g.Expect(pkgAlias).To(gomega.Equal("pkgName2Alias"))
}

func TestGenerateHelmIndexFile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	chartDirs := make(map[string]string)
	chartDirs["../../test/github/helmcharts/chart1/"] = "../../test/github/helmcharts/chart1/"
	chartDirs["../../test/github/helmcharts/chart2/"] = "../../test/github/helmcharts/chart2/"

	indexFile, err := GenerateHelmIndexFile(githubsub, "../..", chartDirs)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(indexFile.Entries)).To(gomega.Equal(2))
}

func TestCreateOrUpdateHelmChart(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	chartDirs := make(map[string]string)
	chartDirs["../../test/github/helmcharts/chart1/"] = "../../test/github/helmcharts/chart1/"
	chartDirs["../../test/github/helmcharts/chart2/"] = "../../test/github/helmcharts/chart2/"

	indexFile, err := GenerateHelmIndexFile(githubsub, "../..", chartDirs)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(indexFile.Entries)).To(gomega.Equal(2))

	time.Sleep(3 * time.Second)

	githubsub.UID = "dummyuid"
	helmrelease, create, err := CreateOrUpdateHelmChart("chart1", "chart1-1.0.0", indexFile.Entries["chart1"], c, githubchn, githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(create).To(gomega.BeTrue())
	g.Expect(helmrelease).NotTo(gomega.BeNil())

	err = c.Create(context.TODO(), helmrelease)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Sleep to make sure the helm release is created in the test kube
	time.Sleep(5 * time.Second)

	helmrelease, create, err = CreateOrUpdateHelmChart("chart1", "chart1-1.0.0", indexFile.Entries["chart1"], c, githubchn, githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(create).To(gomega.BeFalse())
	g.Expect(helmrelease).NotTo(gomega.BeNil())
}
