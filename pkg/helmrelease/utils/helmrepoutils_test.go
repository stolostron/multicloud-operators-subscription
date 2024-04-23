/*
Copyright 2021 The Kubernetes Authors.

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
	"crypto/tls"
	"encoding/pem"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	testutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
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
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	assert.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
		mgrStopped.Wait()
	}()

	c := mgr.GetClient()

	configMapRef := &corev1.ObjectReference{
		Name:      configMapName,
		Namespace: configMapNS,
	}

	configMapResp, err := GetConfigMap(c, configMapNS, configMapRef)
	assert.Error(t, err)

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
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})

	assert.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	mgrStopped := StartTestManager(ctx, mgr, g)

	defer func() {
		cancel()
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
					Urls:      []string{"https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git"},
					ChartPath: "testhr/github/subscription-release-test-1",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
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
					Urls:      []string{"https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git"},
					ChartPath: "testhr/github/subscription-release-test-1",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
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
						"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/main/testhr/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
			Digest:    "long-fake-digest-that-is-very-long",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destDir, err := DownloadChart(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destDir, "Chart.yaml"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destDir, "../", "subscription-release-test-1-0.1.0.tgz.long-f"))
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
						"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/main/testhr/helmrepo/subscription-release-test-1-0.1.0.tgz",
						"https://badURL1"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
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
						"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/main/testhr/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
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
	dir, err := os.MkdirTemp("/tmp", "charts")
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
					Urls:      []string{"https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git"},
					ChartPath: "testhr/github/subscription-release-test-1",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
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
					Urls:      []string{"https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git"},
					ChartPath: "testhr/github/subscription-release-test-1",
					Branch:    "main",
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
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
						"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/main/testhr/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
			Digest:    "short",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	chartDir, err := DownloadChartFromHelmRepo(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "Chart.yaml"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "../", "subscription-release-test-1-0.1.0.tgz.short"))
	assert.NoError(t, err)
}

func TestDownloadChartFromHelmRepoHTTPConfigMap(t *testing.T) {
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
						"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/main/testhr/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
			Digest:    "short",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	chartDir, err := DownloadChartFromHelmRepo(&corev1.ConfigMap{
		Data: map[string]string{
			"insecureSkipVerify": "true",
		},
	}, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "Chart.yaml"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "../", "subscription-release-test-1-0.1.0.tgz.short"))
	assert.NoError(t, err)
}

func TestDownloadChartFromHelmRepoHTTPNoDigest(t *testing.T) {
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
						"https://raw." + testutils.GetTestGitRepoURLFromEnvVar() + "/main/testhr/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	chartDir, err := DownloadChartFromHelmRepo(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "Chart.yaml"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "../", "subscription-release-test-1-0.1.0.tgz"))
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
					Urls: []string{"file:../../../testhr/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
			Digest:    "digest",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	chartDir, err := DownloadChartFromHelmRepo(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "Chart.yaml"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "../", "subscription-release-test-1-0.1.0.tgz.digest"))
	assert.NoError(t, err)
}

func TestDownloadChartFromHelmRepoLocalNoDigest(t *testing.T) {
	hr := &appv1.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscription-release-test-1-cr",
			Namespace: "default",
		},
		Repo: appv1.HelmReleaseRepo{
			Source: &appv1.Source{
				SourceType: appv1.HelmRepoSourceType,
				HelmRepo: &appv1.HelmRepo{
					Urls: []string{"file:../../../testhr/helmrepo/subscription-release-test-1-0.1.0.tgz"},
				},
			},
			ChartName: "subscription-release-test-1",
		},
	}
	dir, err := os.MkdirTemp("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	chartDir, err := DownloadChartFromHelmRepo(nil, nil, dir, hr)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "Chart.yaml"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(chartDir, "../", "subscription-release-test-1-0.1.0.tgz"))
	assert.NoError(t, err)
}

func TestDownloadGitRepo(t *testing.T) {
	httpURLs := []string{"https://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git"}
	sshURLs := []string{"ssh://" + testutils.GetTestGitRepoURLFromEnvVar() + ".git"}
	dir, err := os.MkdirTemp("/tmp", "charts")
	assert.NoError(t, err)

	defer os.RemoveAll(dir)

	destRepo := filepath.Join(dir, "test")
	commitID, err := DownloadGitRepo(nil, nil, destRepo, httpURLs, "main", true)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(destRepo, "OWNERS"))
	assert.NoError(t, err)

	assert.NotEqual(t, commitID, "")

	// Expect HTTP to pass with false insecureSkipVerify & invalid caCerts
	commitID, err = DownloadGitRepo(&corev1.ConfigMap{
		Data: map[string]string{
			"caCerts": `-----BEGIN CERTIFICATE-----`,
		},
	}, nil, destRepo, httpURLs, "main", false)
	assert.NoError(t, err)

	assert.NotEqual(t, commitID, "")

	// Expect HTTP to pass with false insecureSkipverify & valid caCerts
	validCert := `-----BEGIN CERTIFICATE-----
MIIDfzCCAmegAwIBAgIUFXNRBgbcI8RrO0m5Zm/dXOsAQGAwDQYJKoZIhvcNAQEL
BQAwTzELMAkGA1UEBhMCQ0ExCzAJBgNVBAgMAk9OMRUwEwYDVQQHDAxEZWZhdWx0
IENpdHkxHDAaBgNVBAoME0RlZmF1bHQgQ29tcGFueSBMdGQwHhcNMjIwODI0MjAx
MDI3WhcNMjMwODI0MjAxMDI3WjBPMQswCQYDVQQGEwJDQTELMAkGA1UECAwCT04x
FTATBgNVBAcMDERlZmF1bHQgQ2l0eTEcMBoGA1UECgwTRGVmYXVsdCBDb21wYW55
IEx0ZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAMGd5GerpOJD3tzy
Wm9yDOz7IFarG1Ap+dZEzGOu+pLmnsAd2kQgWi1myNIThnzMX6O46wf6876PaS/Z
6TTvceTZhKZ2AglQWxtntKZinozzOeoO3whqsjIs7Lh6Hcgm8NH+Yd6dyfvaayqP
C0Ko0uNVsF0T9aFXdMBRtRuAiv86xshRvJxNA0Wl1Znt0AZ1adXgaER8kZDUpqLV
EetVNeFfSBrd9ye6VPQaCRuAqEy4uvS9EC1tW6L9ktKMYvBW8pIqRis9GrK0W4Kc
dSpE1jsot+Zff2shIqNcdxoUVtTBqixX3EX2P/2aw2sSzTSPVn+plXEm15TE1S1J
NxhAUY0CAwEAAaNTMFEwHQYDVR0OBBYEFNaCruT1JTJmgwhg3NOFDsmhP5TbMB8G
A1UdIwQYMBaAFNaCruT1JTJmgwhg3NOFDsmhP5TbMA8GA1UdEwEB/wQFMAMBAf8w
DQYJKoZIhvcNAQELBQADggEBABjPd1qjSH/cFBNo7QJL8xUAIadCJpFMUb+HWf+6
Cq8A6wfE0+O8oRGv39w5VNj7pbn9aBQPxjlG0w55Lg/xBfeyG/8FtBWaTHfnHE3s
9Xms7+cVtw4aOC9JqfFV+ACUnWEwtT7pM+fnS+CFODu5WbeXx85M6XaTAj14YQz9
I6lYQaWOpUX6KYWDkwnmzAUMi5DhCDeQxA+8/7xQugO1G5f6QNIVqqtRr/j8JyJ3
4acGcICm8DX6fvN36mOKdDvhuFYVtNhDGp3H2htgTh/QeEJO2jTjW39ySv/ke5nn
nKxJ40dMYqyYtw4xgHrZMx35nF0jnZZn8kgFDzJAuflWRNc=
-----END CERTIFICATE-----`
	commitID, err = DownloadGitRepo(&corev1.ConfigMap{
		Data: map[string]string{
			"caCerts": validCert,
		},
	}, nil, destRepo, httpURLs, "main", false)
	assert.NoError(t, err)

	assert.NotEqual(t, commitID, "")

	// Expect SSH to fail due to invalid secret preparing SSH clone options
	secret1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "application-manager-token-1",
			Namespace:   "open-cluster-management-agent-addon",
			Annotations: map[string]string{"kubernetes.io/service-account.name": "application-manager"},
		},
		Data: map[string][]byte{
			"token": []byte("ZHVtbXkxCg=="),
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	commitID, err = DownloadGitRepo(nil, secret1, destRepo, sshURLs, "", false)
	assert.Error(t, err)

	assert.Equal(t, commitID, "")

	// Expect SSH to fail due to invalid handshake authentication
	sshPassphare := []byte("redhat")

	sshPrivKey := []byte(`-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAACmFlczI1Ni1jdHIAAAAGYmNyeXB0AAAAGAAAABA3X5/l67
/tLNBt/T5kWywKAAAAEAAAAAEAAAAzAAAAC3NzaC1lZDI1NTE5AAAAIDi6n9EYceR6eKJk
zLF/qDKpeGVS9WWjY4s5UfyJIUDSAAAAoIS7NOslr9L3xBh+PmugdLu788i31uH/8XcwNP
MgskdYrllhLbnVxI6vEw4FdK1kJx8GOyPa673+YuYr0V2ZKpzrUtbMsuEnokyBA0gGzM77
tYny6pJJNYEhf7HPmb2O3zBuuqsCC0O2SHrgFYH350zA4To9Ez5nifkZ0CBx0pn9jWn02V
1yxIFkpty18DN1/IudjVnOAT3oaPo/L8ybWuE=
-----END OPENSSH PRIVATE KEY-----`)
	secret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "application-manager-token-1",
			Namespace:   "open-cluster-management-agent-addon",
			Annotations: map[string]string{"kubernetes.io/service-account.name": "application-manager"},
		},
		Data: map[string][]byte{
			"sshKey":     sshPrivKey,
			"passphrase": sshPassphare,
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	commitID, err = DownloadGitRepo(nil, secret2, destRepo, sshURLs, "", true)
	assert.Error(t, err)

	assert.Equal(t, commitID, "")
}

func TestGetKnownHostFromURL(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "temptest")
	if err != nil {
		t.Error("error creating temp file")
	}

	defer os.Remove(tmpfile.Name()) // clean up

	testCases := []struct {
		desc        string
		sshURL      string
		filepath    string
		expectError bool
	}{
		{
			desc:        "invalid ssh url",
			sshURL:      "ssh:\r\n",
			filepath:    "",
			expectError: true,
		},
		{
			desc:        "invalid filepath",
			sshURL:      "",
			filepath:    "",
			expectError: true,
		},
		{
			desc:        "valid ssh url with port",
			sshURL:      "ssh://git@github.com:22/open-cluster-management-io/multicloud-operators-subscription.git",
			filepath:    tmpfile.Name(),
			expectError: false,
		},
		{
			desc:        "valid git url",
			sshURL:      "git@github.com:open-cluster-management-io/multicloud-operators-subscription.git",
			filepath:    tmpfile.Name(),
			expectError: false,
		},
		{
			desc:        "invalid ssh host",
			sshURL:      "ssh://git@fakegithub.com:22/",
			filepath:    tmpfile.Name(),
			expectError: true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := getKnownHostFromURL(tC.sshURL, tC.filepath)
			if got != nil && !tC.expectError { // If error and we don't expect an error
				t.Errorf("wanted error %v, got %v", tC.expectError, got)
			}
		})
	}
}

func TestGetCertChain(t *testing.T) {
	validCert := `
-----BEGIN CERTIFICATE-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAlRuRnThUjU8/prwYxbty
WPT9pURI3lbsKMiB6Fn/VHOKE13p4D8xgOCADpdRagdT6n4etr9atzDKUSvpMtR3
CP5noNc97WiNCggBjVWhs7szEe8ugyqF23XwpHQ6uV1LKH50m92MbOWfCtjU9p/x
qhNpQQ1AZhqNy5Gevap5k8XzRmjSldNAFZMY7Yv3Gi+nyCwGwpVtBUwhuLzgNFK/
yDtw2WcWmUU7NuC8Q6MWvPebxVtCfVp/iQU6q60yyt6aGOBkhAX0LpKAEhKidixY
nP9PNVBvxgu3XZ4P36gZV6+ummKdBVnc3NqwBLu5+CcdRdusmHPHd5pHf4/38Z3/
6qU2a/fPvWzceVTEgZ47QjFMTCTmCwNt29cvi7zZeQzjtwQgn4ipN9NibRH/Ax/q
TbIzHfrJ1xa2RteWSdFjwtxi9C20HUkjXSeI4YlzQMH0fPX6KCE7aVePTOnB69I/
a9/q96DiXZajwlpq3wFctrs1oXqBp5DVrCIj8hU2wNgB7LtQ1mCtsYz//heai0K9
PhE4X6hiE0YmeAZjR0uHl8M/5aW9xCoJ72+12kKpWAa0SFRWLy6FejNYCYpkupVJ
yecLk/4L1W0l6jQQZnWErXZYe0PNFcmwGXy1Rep83kfBRNKRy5tvocalLlwXLdUk
AIU+2GKjyT3iMuzZxxFxPFMCAwEAAQ==
-----END CERTIFICATE-----
and some more`

	byteArr, _ := pem.Decode([]byte(validCert))

	testCases := []struct {
		desc   string
		certs  string
		wanted tls.Certificate
	}{
		{
			desc:   "invalid cert",
			certs:  "",
			wanted: tls.Certificate{},
		},
		{
			desc: "empty cert",
			certs: `
-----BEGIN CERTIFICATE-----
-----END CERTIFICATE-----
			`,
			wanted: tls.Certificate{Certificate: [][]byte{{}}},
		},
		{
			desc:   "valid cert",
			certs:  validCert,
			wanted: tls.Certificate{Certificate: [][]byte{byteArr.Bytes}},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := getCertChain(tC.certs)
			if !reflect.DeepEqual(got, tC.wanted) {
				t.Errorf("wanted %v, got %v", tC.wanted, got)
			}
		})
	}
}
