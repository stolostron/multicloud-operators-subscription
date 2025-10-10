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
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"helm.sh/helm/v3/pkg/chartutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/klog"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	appsubv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
)

// GetHelmRepoClient returns an *http.client to access the helm repo
func GetHelmRepoClient(parentNamespace string, configMap *corev1.ConfigMap, skipCertVerify bool) (rest.HTTPClient, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: skipCertVerify,            // #nosec G402 InsecureSkipVerify conditionally
			MinVersion:         appsubv1.TLSMinVersionInt, // #nosec G402 -- TLS 1.2 is required for FIPS
		},
	}

	if skipCertVerify {
		klog.Info("repo.insecureSkipVerify=true. Skipping repo server's certificate verification.")
	}

	if configMap != nil {
		configData := configMap.Data
		klog.V(5).Info("ConfigRef retrieved :", configData)
		insecureSkipVerify := configData["insecureSkipVerify"]

		if insecureSkipVerify != "" {
			b, err := strconv.ParseBool(insecureSkipVerify)
			if err != nil {
				if errors.IsNotFound(err) {
					return nil, nil
				}

				klog.Error(err, " - Unable to parse insecureSkipVerify", insecureSkipVerify)

				return nil, err
			}

			klog.Info("From config map, seting InsecureSkipVerify: ", b)
			transport.TLSClientConfig.InsecureSkipVerify = b
		} else {
			klog.V(5).Info("insecureSkipVerify is not specified")
		}
	} else {
		klog.V(5).Info("configMap is nil")
	}

	httpClient := http.DefaultClient
	httpClient.Transport = transport
	klog.V(5).Info("InsecureSkipVerify equal ", transport.TLSClientConfig.InsecureSkipVerify)

	return httpClient, nil
}

// DownloadChart downloads the charts
func DownloadChart(configMap *corev1.ConfigMap,
	secret *corev1.Secret,
	chartsDir string,
	s *appv1.HelmRelease) (chartDir string, err error) {
	destRepo := filepath.Join(chartsDir, s.Name, s.Namespace, s.Repo.ChartName)
	if _, err := os.Stat(destRepo); os.IsNotExist(err) {
		err := os.MkdirAll(destRepo, 0750)
		if err != nil {
			klog.Error(err, " - Unable to create chartDir: ", destRepo)
			return "", err
		}
	}

	switch strings.ToLower(string(s.Repo.Source.SourceType)) {
	case string(appv1.HelmRepoSourceType):
		return DownloadChartFromHelmRepo(configMap, secret, destRepo, s)
	case string(appv1.GitHubSourceType):
		return DownloadChartFromGit(configMap, secret, destRepo, s)
	case string(appv1.GitSourceType):
		return DownloadChartFromGit(configMap, secret, destRepo, s)
	default:
		return "", fmt.Errorf("sourceType '%s' unsupported", s.Repo.Source.SourceType)
	}
}

// DownloadChartFromGit downloads a chart into the charsDir
func DownloadChartFromGit(configMap *corev1.ConfigMap, secret *corev1.Secret, destRepo string, s *appv1.HelmRelease) (chartDir string, err error) {
	if s.Repo.Source.GitHub == nil && s.Repo.Source.Git == nil {
		err := fmt.Errorf("git type, need Repo.Source.Git or Repo.Source.GitHub to be populated")
		return "", err
	}

	if s.Repo.Source.GitHub != nil {
		_, err = DownloadGitRepo(configMap, secret, destRepo, s.Repo.Source.GitHub.Urls, s.Repo.Source.GitHub.Branch, s.Repo.InsecureSkipVerify)
	} else if s.Repo.Source.Git != nil {
		_, err = DownloadGitRepo(configMap, secret, destRepo, s.Repo.Source.Git.Urls, s.Repo.Source.Git.Branch, s.Repo.InsecureSkipVerify)
	}

	if err != nil {
		return "", err
	}

	if s.Repo.Source.GitHub != nil {
		chartDir = filepath.Join(destRepo, s.Repo.Source.GitHub.ChartPath)
	} else if s.Repo.Source.Git != nil {
		chartDir = filepath.Join(destRepo, s.Repo.Source.Git.ChartPath)
	}

	return chartDir, nil
}

// DownloadGitRepo downloads a git repo into the charsDir
func DownloadGitRepo(configMap *corev1.ConfigMap,
	secret *corev1.Secret,
	destRepo string,
	urls []string, branch string,
	insecureSkipVerify bool) (commitID string, err error) {
	for _, url := range urls {
		options := &git.CloneOptions{
			URL:               url,
			Depth:             1,
			SingleBranch:      true,
			RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		}

		if secret != nil && secret.Data != nil {
			klog.V(5).Info("Add credentials")

			options.Auth = &githttp.BasicAuth{
				Username: string(secret.Data["user"]),
				Password: GetAccessToken(secret),
			}
		}

		if branch == "" {
			options.ReferenceName = plumbing.Master
		} else {
			options.ReferenceName = plumbing.ReferenceName("refs/heads/" + branch)
		}

		rErr := os.RemoveAll(destRepo)
		if rErr != nil {
			klog.Error(err, "- Failed to remove all: ", destRepo)
		}

		err = os.MkdirAll(destRepo, os.ModePerm) // #nosec G301
		if err != nil {
			return "", err
		}

		if strings.HasPrefix(url, "http") {
			klog.Info("Connecting to Git server via HTTP")

			caCert := ""

			if configMap != nil {
				caCert = configMap.Data["caCerts"]
			}

			err := getHTTPOptions(caCert, insecureSkipVerify)

			if err != nil {
				klog.Error(err, "failed to prepare HTTP clone options")
				return "", err
			}
		} else {
			klog.Info("Connecting to Git server via SSH")

			knownhostsfile := filepath.Join(destRepo, "known_hosts")

			if !insecureSkipVerify {
				err := getKnownHostFromURL(url, knownhostsfile)

				if err != nil {
					return "", err
				}
			}

			sshKey := []byte("")
			passphrase := []byte("")

			if secret != nil {
				sshKey = bytes.TrimSpace(secret.Data["sshKey"])
				passphrase = bytes.TrimSpace(secret.Data["passphrase"])
			}

			err = getSSHOptions(options, sshKey, passphrase, knownhostsfile, insecureSkipVerify)
			if err != nil {
				klog.Error(err, " failed to prepare SSH clone options")
				return "", err
			}
		}

		r, errClone := git.PlainClone(destRepo, false, options)

		if errClone != nil {
			rErr = os.RemoveAll(destRepo)
			if rErr != nil {
				klog.Error(err, "- Failed to remove all: ", destRepo)
			}

			klog.Error(errClone, " - Clone failed: ", url)
			err = errClone

			continue
		}

		h, errHead := r.Head()

		if errHead != nil {
			rErr := os.RemoveAll(destRepo)
			if rErr != nil {
				klog.Error(err, "- Failed to remove all: ", destRepo)
			}

			klog.Error(errHead, " - Get Head failed: ", url)
			err = errHead

			continue
		}

		commitID = h.Hash().String()
		klog.V(5).Info("commitID: ", commitID)
	}

	if err != nil {
		klog.Error(err, " - All urls failed")
	}

	return commitID, err
}

func getCertChain(certs string) tls.Certificate {
	var certChain tls.Certificate

	certPEMBlock := []byte(certs)

	var certDERBlock *pem.Block

	for {
		certDERBlock, certPEMBlock = pem.Decode(certPEMBlock)

		if certDERBlock == nil {
			break
		}

		if certDERBlock.Type == "CERTIFICATE" {
			certChain.Certificate = append(certChain.Certificate, certDERBlock.Bytes)
		}
	}

	return certChain
}

func getKnownHostFromURL(sshURL string, filepath string) error {
	sshhostname := ""
	sshhostport := ""

	if strings.HasPrefix(sshURL, "ssh:") {
		u, err := url.Parse(sshURL)

		if err != nil {
			klog.Error("failed toparse SSH URL: ", err)
			return err
		}

		sshhostname = strings.Split(u.Host, ":")[0]

		sshhostport = u.Host
	} else if strings.HasPrefix(sshURL, "git@") {
		sshhostname = strings.Split(strings.SplitAfter(sshURL, "@")[1], ":")[0]
	}

	klog.Info("Getting public SSH host key for " + sshhostname)

	cmd := exec.Command("ssh-keyscan", sshhostname) // #nosec G204 the variable is generated within this function.
	stdout, err := cmd.Output()

	if err != nil {
		klog.Error("failed to get public SSH host key: ", err)

		if sshhostport != "" && (sshhostport != sshhostname) {
			klog.Info("Getting public SSH host key for " + sshhostport)

			cmd2 := exec.Command("ssh-keyscan", sshhostport) // #nosec G204 the variable is generated within this function.
			stdout2, err2 := cmd2.Output()

			if err2 != nil {
				klog.Error("failed to get public SSH host key: ", err2)
				return err2
			}

			stdout = stdout2
		} else {
			return err
		}
	}

	klog.Info("SSH host key: " + string(stdout))

	if err := os.WriteFile(filepath, stdout, 0600); err != nil {
		klog.Error("failed to write known_hosts file: ", err)
		return err
	}

	return nil
}

func getSSHOptions(options *git.CloneOptions, sshKey, passphrase []byte, knownhostsfile string, insecureSkipVerify bool) error {
	publicKey := &gitssh.PublicKeys{}
	publicKey.User = "git"

	if len(passphrase) > 0 {
		klog.Info("Parsing SSH private key with passphrase")

		signer, err := ssh.ParsePrivateKeyWithPassphrase(sshKey, passphrase)

		if err != nil {
			klog.Error("failed to parse SSH key", err.Error())
			return err
		}

		publicKey.Signer = signer
	} else {
		signer, err := ssh.ParsePrivateKey(sshKey)
		if err != nil {
			klog.Error("failed to parse SSH key", err.Error())
			return err
		}
		publicKey.Signer = signer
	}

	if insecureSkipVerify {
		klog.Info("Insecure ignore SSH host key")

		publicKey.HostKeyCallback = ssh.InsecureIgnoreHostKey() // #nosec G106 this is optional and used only if users specify it in channel configuration
	} else {
		klog.Info("Using SSH known host keys")
		callback, err := knownhosts.New(knownhostsfile)

		if err != nil {
			klog.Error("failed to get knownhosts ", err)
			return err
		}

		publicKey.HostKeyCallback = callback
	}

	options.Auth = publicKey

	return nil
}

func getHTTPOptions(caCerts string, insecureSkipVerify bool) error {
	installProtocol := false

	// #nosec G402 -- TLS 1.2 is required for FIPS
	clientConfig := &tls.Config{MinVersion: appsubv1.TLSMinVersionInt}

	// skip TLS certificate verification for Git servers with custom or self-signed certs
	if insecureSkipVerify {
		klog.Info("insecureSkipVerify = true, skipping Git server's certificate verification.")

		clientConfig.InsecureSkipVerify = true

		installProtocol = true
	} else if !strings.EqualFold(caCerts, "") {
		klog.Info("Adding Git server's CA certificate to trust certificate pool")

		// Load the host's trusted certs into memory
		certPool, _ := x509.SystemCertPool()
		if certPool == nil {
			certPool = x509.NewCertPool()
		}

		certChain := getCertChain(caCerts)

		if len(certChain.Certificate) == 0 {
			klog.Warning("No certificate found")
		}

		// Add CA certs from the channel config map to the cert pool
		// It will not add duplicate certs
		for _, cert := range certChain.Certificate {
			x509Cert, err := x509.ParseCertificate(cert)
			if err != nil {
				return err
			}

			klog.Info("Adding certificate -->" + x509Cert.Subject.String())
			certPool.AddCert(x509Cert)
		}

		clientConfig.RootCAs = certPool

		installProtocol = true
	}

	if installProtocol {
		klog.Info("HTTP_PROXY = " + os.Getenv("HTTP_PROXY"))
		klog.Info("HTTPS_PROXY = " + os.Getenv("HTTPS_PROXY"))

		transportConfig := &http.Transport{
			TLSClientConfig: clientConfig,
		}

		proxyURLEnv := ""

		if os.Getenv("HTTPS_PROXY") != "" {
			proxyURLEnv = os.Getenv("HTTPS_PROXY")
		} else if os.Getenv("HTTP_PROXY") != "" {
			proxyURLEnv = os.Getenv("HTTP_PROXY")
		}

		if proxyURLEnv != "" {
			proxyURL, err := url.Parse(proxyURLEnv)

			if err != nil {
				klog.Error(err.Error())
				return err
			}

			transportConfig.Proxy = http.ProxyURL(proxyURL)

			klog.Info("setting HTTP transport proxy to " + proxyURLEnv)
		}

		customClient := &http.Client{
			Transport: transportConfig,

			// 15 second timeout
			Timeout: 15 * time.Second,

			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		gitclient.InstallProtocol("https", githttp.NewClient(customClient))
	}

	return nil
}

// DownloadChartFromHelmRepo downloads a chart into the chartDir
func DownloadChartFromHelmRepo(configMap *corev1.ConfigMap,
	secret *corev1.Secret,
	destRepo string,
	s *appv1.HelmRelease) (chartDir string, err error) {
	if s.Repo.Source.HelmRepo == nil {
		err := fmt.Errorf("helmrepo type but Spec.HelmRepo is not defined")
		return "", err
	}

	var urlsError string

	for _, url := range s.Repo.Source.HelmRepo.Urls {
		chartDir, err := downloadChartFromURL(configMap, secret, destRepo, s, url)
		if err == nil {
			return chartDir, nil
		}

		urlsError += " - url: " + url + " error: " + err.Error()
	}

	return "", fmt.Errorf("failed to download chart from helm repo. %v", urlsError)
}

func downloadChartFromURL(configMap *corev1.ConfigMap,
	secret *corev1.Secret,
	destRepo string,
	s *appv1.HelmRelease,
	url string) (chartDir string, err error) {
	digestTrim := s.Repo.Digest
	if digestTrim != "" {
		if len(digestTrim) >= 6 {
			digestTrim = digestTrim[0:6]
		}
	}

	chartZip, downloadErr := downloadFile(s.Namespace, configMap, url, secret, destRepo, s.Repo.InsecureSkipVerify, digestTrim)
	if downloadErr != nil {
		klog.Error(downloadErr, " - url: ", url)
		return "", downloadErr
	}

	r, downloadErr := os.Open(filepath.Clean(chartZip))
	if downloadErr != nil {
		klog.Error(downloadErr, " - Failed to open: ", chartZip, " using url: ", url)
		return "", downloadErr
	}

	chartDir = filepath.Join(destRepo, s.Repo.ChartName)
	chartDir = filepath.Clean(chartDir)
	//Clean before untar
	err = os.RemoveAll(chartDir)
	if err != nil {
		klog.Error(err, "- Failed to remove all: ", chartDir, " for ", chartZip, " using url: ", url)
	}

	//Untar
	err = chartutil.Expand(destRepo, r)
	if err != nil {
		//Remove zip because failed to untar and so probably corrupted
		rErr := os.RemoveAll(chartZip)
		if rErr != nil {
			klog.Error(rErr, "- Failed to remove all: ", chartZip)
		}

		klog.Error(err, "- Failed to unzip: ", chartZip, " using url: ", url)

		return "", err
	}

	return chartDir, nil
}

// downloadFile downloads a files and post it in the chartsDir.
func downloadFile(parentNamespace string, configMap *corev1.ConfigMap,
	fileURL string,
	secret *corev1.Secret,
	chartsDir string,
	insecureSkipVerify bool,
	digestTrim string) (string, error) {
	klog.V(4).Info("fileURL: ", fileURL)

	URLP, downloadErr := url.Parse(fileURL)
	if downloadErr != nil {
		klog.Error(downloadErr, " - url:", fileURL)
		return "", downloadErr
	}

	fileName := filepath.Base(URLP.RequestURI())
	if digestTrim != "" {
		fileName = fileName + "." + digestTrim
	}

	klog.V(4).Info("fileName: ", fileName)
	// Create the file
	chartZip := filepath.Join(chartsDir, fileName)
	klog.V(4).Info("chartZip: ", chartZip)

	if chartZip == chartsDir {
		downloadErr = fmt.Errorf("failed to parse fileName from fileURL %s", fileURL)
		return "", downloadErr
	}

	switch URLP.Scheme {
	case "file":
		downloadErr = downloadFileLocal(URLP, chartZip)
	case "http", "https":
		downloadErr = downloadFileHTTP(parentNamespace, configMap, fileURL, secret, chartZip, insecureSkipVerify)
	default:
		downloadErr = fmt.Errorf("unsupported scheme %s", URLP.Scheme)
	}

	return chartZip, downloadErr
}

func downloadFileLocal(urlP *url.URL,
	chartZip string) error {
	sourceFile, downloadErr := os.Open(urlP.RequestURI())
	if downloadErr != nil {
		klog.Error(downloadErr, " - urlP.RequestURI: ", urlP.RequestURI())
		return downloadErr
	}

	defer closeHelper(sourceFile)

	// Create new file
	newFile, downloadErr := os.Create(filepath.Clean(chartZip))
	if downloadErr != nil {
		klog.Error(downloadErr, " - chartZip: ", chartZip)
		return downloadErr
	}

	defer closeHelper(newFile)

	_, downloadErr = io.Copy(newFile, sourceFile)
	if downloadErr != nil {
		klog.Error(downloadErr)
		return downloadErr
	}

	return nil
}

func downloadFileHTTP(parentNamespace string, configMap *corev1.ConfigMap,
	fileURL string,
	secret *corev1.Secret,
	chartZip string,
	insecureSkipVerify bool) error {
	fileInfo, err := os.Stat(chartZip)
	if fileInfo != nil && fileInfo.IsDir() {
		downloadErr := fmt.Errorf("expecting chartZip to be a file but it's a directory: %s", chartZip)
		klog.Error(downloadErr)

		return downloadErr
	}

	if os.IsNotExist(err) {
		httpClient, downloadErr := GetHelmRepoClient(parentNamespace, configMap, insecureSkipVerify)
		if downloadErr != nil {
			klog.Error(downloadErr, " - Failed to create httpClient")
			return downloadErr
		}

		var req *http.Request

		req, downloadErr = http.NewRequest(http.MethodGet, fileURL, nil)
		if downloadErr != nil {
			klog.Error(downloadErr, "- Can not build request: ", "fileURL", fileURL)
			return downloadErr
		}

		if secret != nil && secret.Data != nil {
			req.SetBasicAuth(string(secret.Data["user"]), GetPassword(secret))
		}

		var resp *http.Response

		resp, downloadErr = httpClient.Do(req)
		if downloadErr != nil {
			klog.Error(downloadErr, "- Http request failed: ", "fileURL", fileURL)
			return downloadErr
		}

		if resp.StatusCode != 200 {
			downloadErr = fmt.Errorf("return code: %d unable to retrieve chart", resp.StatusCode)
			klog.Error(downloadErr, " - Unable to retrieve chart")

			return downloadErr
		}

		klog.V(5).Info("Download chart form helmrepo succeeded: ", fileURL)

		defer func() {
			if err := resp.Body.Close(); err != nil {
				klog.Error("Error closing response: ", err)
			}
		}()

		var out *os.File

		out, downloadErr = os.Create(filepath.Clean(chartZip))
		if downloadErr != nil {
			klog.Error(downloadErr, " - Failed to create: ", chartZip)
			return downloadErr
		}

		defer closeHelper(out)

		// Write the body to file
		_, downloadErr = io.Copy(out, resp.Body)
		if downloadErr != nil {
			klog.Error(downloadErr, " - Failed to copy body:", chartZip)
			return downloadErr
		}
	} else {
		klog.V(5).Info("Skip download chartZip already exists: ", chartZip)
	}

	return nil
}

func closeHelper(file io.Closer) {
	if err := file.Close(); err != nil {
		klog.Error(err, " - Failed to close file: ", file)
	}
}
