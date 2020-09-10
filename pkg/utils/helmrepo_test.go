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

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	clientsetx "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/helm/pkg/repo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	releasev1 "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

var (
	helmkey = types.NamespacedName{
		Name:      "testhelmkey",
		Namespace: "default",
	}

	helmchn = &chnv1.Channel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmkey.Name,
			Namespace: helmkey.Namespace,
		},
		Spec: chnv1.ChannelSpec{
			Type:     "HelmRepo",
			Pathname: "https://github.com/open-cluster-management/multicloud-operators-subscription/test/helm",
		},
	}

	helmsub = &appv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmkey.Name,
			Namespace: helmkey.Namespace,
		},
		Spec: appv1.SubscriptionSpec{
			Channel: helmkey.String(),
		},
	}
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
	helmrelease, err := CreateOrUpdateHelmChart("chart1", "chart1-1.0.0", indexFile.Entries["chart1"], c, githubchn, githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(helmrelease).NotTo(gomega.BeNil())

	err = c.Create(context.TODO(), helmrelease)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Sleep to make sure the helm release is created in the test kube
	time.Sleep(5 * time.Second)

	helmrelease, err = CreateOrUpdateHelmChart("chart1", "chart1-1.0.0", indexFile.Entries["chart1"], c, githubchn, githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(helmrelease).NotTo(gomega.BeNil())

	var relativeUrls []string
	relativeUrls = append(relativeUrls, "my-app-0.1.0.tgz")

	var relativeChartVersions []*repo.ChartVersion
	relativeChartVersions = append(relativeChartVersions, &repo.ChartVersion{URLs: relativeUrls})

	helmrelease, err = CreateOrUpdateHelmChart("my-app", "my-app-0.1.0", relativeChartVersions, c, helmchn, helmsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(helmrelease).NotTo(gomega.BeNil())
	g.Expect(helmrelease.Repo.Source.HelmRepo.Urls[0]).
		Should(gomega.Equal(
			"https://github.com/open-cluster-management/multicloud-operators-subscription/test/helm/my-app-0.1.0.tgz"))

	var fullUrls []string
	fullUrls = append(fullUrls, "https://kubernetes-charts.storage.googleapis.com/nginx-ingress-1.36.3.tgz")

	var fullChartVersions []*repo.ChartVersion
	fullChartVersions = append(fullChartVersions, &repo.ChartVersion{URLs: fullUrls})

	helmrelease, err = CreateOrUpdateHelmChart("nginx-ingress", "nginx-ingress-1.36.3", fullChartVersions, c, helmchn, helmsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(helmrelease).NotTo(gomega.BeNil())
	g.Expect(helmrelease.Repo.Source.HelmRepo.Urls[0]).
		Should(gomega.Equal(
			"https://kubernetes-charts.storage.googleapis.com/nginx-ingress-1.36.3.tgz"))
}

func TestCheckTillerVersion(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	subanno := make(map[string]string)
	subanno[appv1alpha1.AnnotationGitPath] = "test/github/helmcharts"
	githubsub.SetAnnotations(subanno)

	packageFilter := &appv1alpha1.PackageFilter{}
	annotations := make(map[string]string)
	annotations["tillerVersion"] = "2.10.0"

	packageFilter.Annotations = annotations

	githubsub.Spec.PackageFilter = packageFilter

	githubsub.Spec.Package = "chart1"

	chartDirs := make(map[string]string)
	chartDirs["../../test/github/helmcharts/chart1/"] = "../../test/github/helmcharts/chart1/"

	indexFile, err := GenerateHelmIndexFile(githubsub, "../..", chartDirs)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	chartVersion, err := indexFile.Get("chart1", "1.1.1")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(chartVersion).NotTo(gomega.BeNil())

	ret := checkTillerVersion(githubsub, chartVersion)
	g.Expect(ret).To(gomega.BeTrue())

	packageFilter = &appv1alpha1.PackageFilter{}
	annotations = make(map[string]string)
	annotations["tillerVersion"] = "2.8.0"

	packageFilter.Annotations = annotations

	githubsub.Spec.PackageFilter = packageFilter

	ret = checkTillerVersion(githubsub, chartVersion)
	g.Expect(ret).To(gomega.BeFalse())
}

func TestCheckVersion(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
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

	packageFilter := &appv1alpha1.PackageFilter{}
	packageFilter.Version = "1.1.1"

	githubsub.Spec.PackageFilter = packageFilter

	githubsub.Spec.Package = "chart1"

	indexFile, err := GenerateHelmIndexFile(githubsub, "../..", chartDirs)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	chartVersion, err := indexFile.Get("chart1", "1.1.1")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(chartVersion).NotTo(gomega.BeNil())

	ret := checkVersion(githubsub, chartVersion)
	g.Expect(ret).To(gomega.BeTrue())

	subanno := make(map[string]string)
	subanno[appv1alpha1.AnnotationGitPath] = "test/github/helmcharts"
	githubsub.SetAnnotations(subanno)

	packageFilter = &appv1alpha1.PackageFilter{}
	packageFilter.Version = "2.0.0"

	githubsub.Spec.PackageFilter = packageFilter

	ret = checkVersion(githubsub, chartVersion)
	g.Expect(ret).To(gomega.BeFalse())

	packageFilter = &appv1alpha1.PackageFilter{}
	githubsub.Spec.PackageFilter = packageFilter

	ret = checkVersion(githubsub, chartVersion)
	g.Expect(ret).To(gomega.BeTrue())
}

func TestOverride(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	substr2 := `apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: git-sub
  namespace: default
spec:
  channel: default/testkey
  package: chart1
  packageFilter:
    version: 1.1.1
  packageOverrides:
  - packageName: chart1
    packageOverrides:
    - path: spec
      value: |
persistence:
  enabled: false`

	sub2 := &appv1alpha1.Subscription{}
	err = yaml.Unmarshal([]byte(substr2), &sub2)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	chartDirs := make(map[string]string)
	chartDirs["../../test/github/helmcharts/chart1/"] = "../../test/github/helmcharts/chart1/"
	chartDirs["../../test/github/helmcharts/chart2/"] = "../../test/github/helmcharts/chart2/"

	indexFile, err := GenerateHelmIndexFile(sub2, "../..", chartDirs)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(indexFile.Entries)).To(gomega.Equal(1))

	time.Sleep(3 * time.Second)

	sub2.UID = "dummyuid"
	helmrelease, err := CreateOrUpdateHelmChart("chart1", "chart1-1.1.1", indexFile.Entries["chart1"], c, githubchn, sub2)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(helmrelease).NotTo(gomega.BeNil())

	err = Override(helmrelease, sub2)
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestCreateHelmCRDeployable(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test Git clone with a secret
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
	chartDirs["../../test/github/helmcharts/chart1Upgrade/"] = "../../test/github/helmcharts/chart1Upgrade/"
	chartDirs["../../test/github/helmcharts/chart2/"] = "../../test/github/helmcharts/chart2/"

	packageFilter := &appv1alpha1.PackageFilter{}
	packageFilter.Version = "1.1.1"

	githubsub.Spec.PackageFilter = packageFilter

	githubsub.Spec.Package = "chart1"

	indexFile, err := GenerateHelmIndexFile(githubsub, "../..", chartDirs)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(indexFile.Entries)).To(gomega.Equal(1))

	time.Sleep(3 * time.Second)

	githubsub.UID = "dummyuid"

	dpl, err := CreateHelmCRDeployable("../..", "chart1", indexFile.Entries["chart1"], c, githubchn, githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(dpl).NotTo(gomega.BeNil())

	dplName1 := dpl.Name

	githubchn.Spec.Type = chnv1.ChannelTypeHelmRepo
	dpl, err = CreateHelmCRDeployable("../..", "chart1", indexFile.Entries["chart1"], c, githubchn, githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(dpl).NotTo(gomega.BeNil())

	packageFilter.Version = "1.2.2"

	githubsub.Spec.PackageFilter = packageFilter

	githubsub.Spec.Package = "chart1"

	indexFile, err = GenerateHelmIndexFile(githubsub, "../..", chartDirs)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(indexFile.Entries)).To(gomega.Equal(1))

	time.Sleep(3 * time.Second)

	dpl, err = CreateHelmCRDeployable("../..", "chart1", indexFile.Entries["chart1"], c, githubchn, githubsub)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(dpl).NotTo(gomega.BeNil())

	dplName2 := dpl.Name

	// Test that the deployable names are the same for the same charts with different versions
	g.Expect(dplName1).To(gomega.Equal(dplName2))
}

func TestDeleteHelmReleaseCRD(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	crdx, err := clientsetx.NewForConfig(cfg)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	runtimeClient, err := client.New(cfg, client.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	hrlist := &releasev1.HelmReleaseList{}
	err = runtimeClient.List(context.TODO(), hrlist, &client.ListOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	DeleteHelmReleaseCRD(runtimeClient, crdx)

	hrlist = &releasev1.HelmReleaseList{}
	err = runtimeClient.List(context.TODO(), hrlist, &client.ListOptions{})
	g.Expect(!errors.IsNotFound(err)).To(gomega.BeTrue())

	hrlist = &releasev1.HelmReleaseList{}
	err = runtimeClient.List(context.TODO(), hrlist, &client.ListOptions{})
	g.Expect(!errors.IsNotFound(err)).To(gomega.BeTrue())
}

func TestIsURL(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	g.Expect(IsURL("https://kubernetes-charts.storage.googleapis.com/nginx-ingress-1.40.1.tgz")).To(gomega.BeTrue())
	g.Expect(IsURL("nginx-ingress-1.40.1.tgz")).To(gomega.BeFalse())
}
