/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
)

var (
	configMapName = "cm-helmoutils"
	configMapNS   = "default"
	secretName    = "secret-helmoutils"
	secretNS      = "default"
)

func TestGetConfig(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
	})
	assert.NoError(t, err)

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	c := mgr.GetClient()

	configMapRef := &corev1.ObjectReference{
		Name:      configMapName,
		Namespace: configMapNS,
	}

	configMapResp, err := GetConfigMap(c, configMapNS, configMapRef)
	assert.NoError(t, err)

	assert.Nil(t, configMapResp)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNS,
		},
		Data: map[string]string{
			"att1": "att1value",
			"att2": "att2value",
		},
	}

	err = c.Create(context.TODO(), configMap)
	assert.NoError(t, err)

	time.Sleep(2 * time.Second)

	configMapResp, err = GetConfigMap(c, configMapNS, configMapRef)
	assert.NoError(t, err)

	assert.NotNil(t, configMapResp)
	assert.Equal(t, "att1value", configMapResp.Data["att1"])
}

func TestSecret(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
	})
	assert.NoError(t, err)

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	c := mgr.GetClient()

	secretRef := &corev1.ObjectReference{
		Name:      secretName,
		Namespace: secretNS,
	}

	secretResp, err := GetSecret(c, secretNS, secretRef)
	assert.Error(t, err)

	assert.Nil(t, secretResp)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNS,
		},
		Data: map[string][]byte{
			"att1": []byte("att1value"),
			"att2": []byte("att2value"),
		},
	}

	err = c.Create(context.TODO(), secret)
	assert.NoError(t, err)

	time.Sleep(2 * time.Second)

	secretResp, err = GetSecret(c, secretNS, secretRef)
	assert.NoError(t, err)

	assert.NotNil(t, secretResp)
	assert.Equal(t, []byte("att1value"), secretResp.Data["att1"])
}

func TestDownloadChartGitHub(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://github.com/open-cluster-management/multicloud-operators-subscription-release.git"},
					ChartPath: "test/github/subscription-release-test-1",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destDir, err := DownloadChart(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destDir, "Chart.yaml"))
	assert.NoError(t, err)
}

func TestDownloadChartGit(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitSourceType,
				Git: &appv1.Git{
					Urls:      []string{"https://github.com/open-cluster-management/multicloud-operators-subscription-release.git"},
					ChartPath: "test/github/subscription-release-test-1",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destDir, err := DownloadChart(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destDir, "Chart.yaml"))
	assert.NoError(t, err)
}

func TestDownloadChartHelmRepo(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{
						"https://raw.github.com/open-cluster-management/multicloud-operators-subscription-release/main/test/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destDir, err := DownloadChart(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destDir, "Chart.yaml"))
	assert.NoError(t, err)
}

func TestDownloadChartHelmRepoContainsInvalidURL(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{
						"https://raw.github.com/open-cluster-management/multicloud-operators-subscription-release/main/test/helmrepo/subscription-release-test-1-0.1.0.tgz",
						"https://badURL1"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destDir, err := DownloadChart(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destDir, "Chart.yaml"))
	assert.NoError(t, err)
}

func TestDownloadChartHelmRepoContainsInvalidURL2(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{"https://badURL1",
						"https://raw.github.com/open-cluster-management/multicloud-operators-subscription-release/main/test/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destDir, err := DownloadChart(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destDir, "Chart.yaml"))
	assert.NoError(t, err)
}

func TestDownloadChartHelmRepoAllInvalidURLs(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{"https://badURL1", "https://badURL2", "https://badURL3", "https://badURL4", "https://badURL5"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	_, err = DownloadChart(nil, nil, dir, hr)
	assert.Error(t, err)
}

func TestDownloadChartFromGitHub(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitHubSourceType,
				GitHub: &appv1.GitHub{
					Urls:      []string{"https://github.com/open-cluster-management/multicloud-operators-subscription-release.git"},
					ChartPath: "test/github/subscription-release-test-1",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destDir, err := DownloadChartFromGit(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destDir, "Chart.yaml"))
	assert.NoError(t, err)
}

func TestDownloadChartFromGit(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.GitSourceType,
				Git: &appv1.Git{
					Urls:      []string{"https://github.com/open-cluster-management/multicloud-operators-subscription-release.git"},
					ChartPath: "test/github/subscription-release-test-1",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destDir, err := DownloadChartFromGit(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destDir, "Chart.yaml"))
	assert.NoError(t, err)
}

func TestDownloadChartFromHelmRepoHTTP(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{
						"https://raw.github.com/open-cluster-management/multicloud-operators-subscription-release/main/test/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	chartDir, err := DownloadChartFromHelmRepo(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "Chart.yaml"))
	assert.NoError(t, err)
}

func TestDownloadChartFromHelmRepoLocal(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{"file:../../test/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	chartDir, err := DownloadChartFromHelmRepo(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "Chart.yaml"))
	assert.NoError(t, err)
}

func TestDownloadGitRepo(t *testing.T) {
	dir, err := ioutil.TempDir("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destRepo := filepath.Join(dir, "test")
	commitID, err := DownloadGitRepo(nil, nil, destRepo,
		[]string{"https://github.com/open-cluster-management/multicloud-operators-subscription-release.git"}, "main")
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destRepo, "OWNERS"))
	assert.NoError(t, err)

	assert.NotEqual(t, commitID, "")
}
