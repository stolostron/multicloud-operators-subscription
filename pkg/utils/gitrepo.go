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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	"strings"

	"github.com/google/go-github/v32/github"
	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/ghodss/yaml"
	gitclient "gopkg.in/src-d/go-git.v4/plumbing/transport/client"
	githttp "gopkg.in/src-d/go-git.v4/plumbing/transport/http"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chnv1 "github.com/mikeshng/multicloud-operators-channel/pkg/apis/apps/v1"
	appv1 "github.com/stolostron/multicloud-operators-subscription/pkg/apis/apps/v1"
)

const (
	// UserID is key of GitHub user ID in secret
	UserID = "user"
	// AccessToken is key of GitHub user password or personal token in secret
	AccessToken = "accessToken"
	// SSHKey is use to connect to the channel via SSH
	SSHKey = "sshKey"
	// Passphrase is used to open the SSH key
	Passphrase = "passphrase"
	// ClientKey is a client private key for connecting to a Git server
	ClientKey = "clientKey"
	// ClientCert is a client certificate for connecting to a Git server
	ClientCert = "clientCert"
)

type kubeResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

//KKubeResource export the kuKubeResource for other package
type KubeResource struct {
	kubeResource
}

type GitCloneOption struct {
	CommitHash                string
	RevisionTag               string
	Branch                    plumbing.ReferenceName
	DestDir                   string
	CloneDepth                int
	PrimaryConnectionOption   *ChannelConnectionCfg
	SecondaryConnectionOption *ChannelConnectionCfg
}

type ChannelConnectionCfg struct {
	RepoURL            string
	User               string
	Password           string
	SSHKey             []byte
	Passphrase         []byte
	InsecureSkipVerify bool
	CaCerts            string
	ClientKey          []byte
	ClientCert         []byte
}

// ParseKubeResoures parses a YAML content and returns kube resources in byte array from the file
func ParseKubeResoures(file []byte) [][]byte {
	cond := func(t KubeResource) bool {
		return t.APIVersion == "" || t.Kind == ""
	}

	return KubeResourceParser(file, cond)
}

type Kube func(KubeResource) bool

func KubeResourceParser(file []byte, cond Kube) [][]byte {
	var ret [][]byte

	items := ParseYAML(file)

	for _, i := range items {
		item := []byte(strings.Trim(i, "\t \n"))

		t := KubeResource{}
		err := yaml.Unmarshal(item, &t)

		if err != nil {
			// Ignore item that cannot be unmarshalled..
			klog.Warning(err, "Failed to unmarshal YAML content")
			continue
		}

		if cond(t) {
			// Ignore item that does not have apiVersion or kind.
			klog.Warning("Not a Kubernetes resource")
		} else {
			ret = append(ret, item)
		}
	}

	return ret
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

func getConnectionOptions(cloneOptions *GitCloneOption, primary bool) (connectionOptions *git.CloneOptions, err error) {
	channelConnOptions := cloneOptions.PrimaryConnectionOption

	if !primary {
		if cloneOptions.SecondaryConnectionOption == nil {
			klog.Error("no secondary channel to try")
			return nil, errors.New("no secondary channel to try")
		}

		channelConnOptions = cloneOptions.SecondaryConnectionOption
	}

	options := &git.CloneOptions{
		URL:               channelConnOptions.RepoURL,
		SingleBranch:      true,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		ReferenceName:     cloneOptions.Branch,
	}

	// The destination directory needs to be created here
	err = os.RemoveAll(cloneOptions.DestDir)

	if err != nil {
		klog.Warning(err, "Failed to remove directory ", cloneOptions.DestDir)
	}

	err = os.MkdirAll(cloneOptions.DestDir, os.ModePerm)

	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(options.URL, "http") {
		klog.Info("Connecting to Git server via HTTP")

		err := getHTTPOptions(options,
			channelConnOptions.User,
			channelConnOptions.Password,
			channelConnOptions.CaCerts,
			channelConnOptions.InsecureSkipVerify,
			channelConnOptions.ClientKey,
			channelConnOptions.ClientCert)

		if err != nil {
			klog.Error(err, "failed to prepare HTTP clone options")
			return nil, err
		}
	} else {
		klog.Info("Connecting to Git server via SSH")

		knownhostsfile := filepath.Join(cloneOptions.DestDir, "known_hosts")

		if !channelConnOptions.InsecureSkipVerify {
			err := getKnownHostFromURL(channelConnOptions.RepoURL, knownhostsfile)

			if err != nil {
				return nil, err
			}
		}

		err = getSSHOptions(options, channelConnOptions.SSHKey, channelConnOptions.Passphrase, knownhostsfile, channelConnOptions.InsecureSkipVerify)
		if err != nil {
			klog.Error(err, " failed to prepare SSH clone options")
			return nil, err
		}
	}

	options.Depth = 1

	if cloneOptions.CommitHash != "" || cloneOptions.RevisionTag != "" {
		if cloneOptions.CloneDepth > 1 {
			klog.Infof("Setting clone depth to %d", cloneOptions.CloneDepth)
			options.Depth = cloneOptions.CloneDepth
		} else {
			klog.Info("Setting clone depth to 20")
			options.Depth = 20
		}
	}

	return options, nil
}

// CloneGitRepo clones a GitHub repository
func CloneGitRepo(cloneOptions *GitCloneOption) (commitID string, err error) {
	usingPrimary := true

	options, err := getConnectionOptions(cloneOptions, true)

	if err != nil {
		klog.Error("Failed to get Git clone options with the primary channel. Trying the secondary channel.")

		options, err = getConnectionOptions(cloneOptions, false)

		if err != nil {
			klog.Error("Failed to get Git clone options with the secondary channel.")

			usingPrimary = false

			return "", err
		}
	}

	klog.Info("Cloning ", options.URL, " into ", cloneOptions.DestDir)

	klog.Info("cloneOptions.DestDir = " + cloneOptions.DestDir)
	klog.Info("cloneOptions.Branch = " + cloneOptions.Branch)
	klog.Info("cloneOptions.CommitHash = " + cloneOptions.CommitHash)
	klog.Info("cloneOptions.RevisionTag = " + cloneOptions.RevisionTag)
	klog.Infof("cloneOptions.CloneDepth = %d", cloneOptions.CloneDepth)

	repo, err := git.PlainClone(cloneOptions.DestDir, false, options)

	if err != nil {
		if usingPrimary {
			klog.Error(err, " Failed to git clone with the primary channel: ", err.Error())

			// Get clone options with the secondary channel
			options, err = getConnectionOptions(cloneOptions, false)

			if err != nil {
				klog.Error("Failed to get Git clone options with the secondary channel.")

				return "", errors.New("Failed to get Git clone options with the secondary channel: " + " err: " + err.Error())
			}

			klog.Info("Trying to clone with the secondary channel")
			klog.Info("Cloning ", options.URL, " into ", cloneOptions.DestDir)

			repo, err = git.PlainClone(cloneOptions.DestDir, false, options)

			if err != nil {
				klog.Error("Failed to clone Git with the secondary channel. err:" + err.Error())

				return "", errors.New("Failed to clone git: " + options.URL + " branch: " + cloneOptions.Branch.String() + " err: " + err.Error())
			}
		} else {
			return "", errors.New("Failed to clone git: " + options.URL + " branch: " + cloneOptions.Branch.String() + " err: " + err.Error())
		}
	}

	ref, err := repo.Head()
	if err != nil {
		klog.Error(err, " Failed to get git repo head")
		return "", errors.New("failed to get git repo head, err: " + err.Error())
	}

	// If both commitHash and revisionTag are provided, take commitHash.
	targetCommit := cloneOptions.CommitHash

	if cloneOptions.RevisionTag != "" && targetCommit == "" {
		tag := "refs/tags/" + cloneOptions.RevisionTag
		releasetag := plumbing.Revision(tag)

		revisionHash, err := repo.ResolveRevision(releasetag)

		if err != nil {
			klog.Error(err, " failed to resolve revision")
			return "", errors.New("failed to resolve revision tag " + cloneOptions.RevisionTag + " err: " + err.Error())
		}

		klog.Infof("Revision tag %s is resolved to %s", cloneOptions.RevisionTag, revisionHash)
		targetCommit = revisionHash.String()
	}

	if targetCommit != "" {
		workTree, err := repo.Worktree()

		if err != nil {
			klog.Error(err, " Failed to get work tree")
			return "", err
		}

		klog.Infof("Checking out commit %s ", targetCommit)

		err = workTree.Checkout(&git.CheckoutOptions{
			Hash:   plumbing.NewHash(strings.TrimSpace(targetCommit)),
			Create: false,
		})

		if err != nil {
			klog.Error(err, " Failed to checkout commit")
			return "", errors.New("failed to checkout commit " + targetCommit + " err: " + err.Error())
		}

		klog.Infof("Successfully checked out commit %s ", targetCommit)

		return targetCommit, nil
	}

	// Otherwise return the latest commit ID
	commit, err := repo.CommitObject(ref.Hash())

	if err != nil {
		klog.Error(err, " Failed to get git repo commit")
		return "", errors.New("failed to get the repo's latest commit hash, err: " + err.Error())
	}

	return commit.ID().String(), nil
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

		sshhostname = u.Hostname()
		sshhostport = u.Port()
	} else if strings.HasPrefix(sshURL, "git@") {
		sshhostname = strings.Split(strings.SplitAfter(sshURL, "@")[1], ":")[0]
	}

	klog.Info("sshhostname =  " + sshhostname)
	klog.Info("sshhostport =  " + sshhostport)

	klog.Info("Getting public SSH host key for " + sshhostname)

	cmd := exec.Command("ssh-keyscan", sshhostname) // #nosec G204 the variable is generated within this function.

	if sshhostport != "" {
		cmd = exec.Command("ssh-keyscan", sshhostname, "-p", sshhostport) // #nosec G204 the variable is generated within this function.
		klog.Infof("Running command ssh-keyscan %s -p %s", sshhostname, sshhostport)
	}

	stdout, err := cmd.Output()

	if err != nil {
		klog.Error("failed to get public SSH host key: ", err)
	}

	klog.Info("SSH host key: " + string(stdout))

	if err := ioutil.WriteFile(filepath, stdout, 0600); err != nil {
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

func getHTTPOptions(options *git.CloneOptions, user, password, caCerts string, insecureSkipVerify bool, clientkey, clientcert []byte) error {
	if user != "" && password != "" {
		options.Auth = &githttp.BasicAuth{
			Username: user,
			Password: password,
		}
	}

	installProtocol := false

	clientConfig := &tls.Config{MinVersion: tls.VersionTLS12}

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

	// If client key pair is provided, make mTLS connection
	if len(clientkey) > 0 && len(clientcert) > 0 {
		klog.Info("Client certificate key pair is provieded. Making mTLS connection.")

		clientCertificate, err := tls.X509KeyPair(clientcert, clientkey)

		if err != nil {
			klog.Error(err.Error())
			return err
		}

		// Add the client certificate in the connection
		clientConfig.Certificates = []tls.Certificate{clientCertificate}

		klog.Info("Client certificate key pair added successfully")
	}

	if installProtocol {
		klog.Info("HTTP_PROXY = " + os.Getenv("HTTP_PROXY"))
		klog.Info("HTTPS_PROXY = " + os.Getenv("HTTPS_PROXY"))
		klog.Info("NO_PROXY = " + os.Getenv("NO_PROXY"))

		transportConfig := &http.Transport{
			/* #nosec G402 */
			TLSClientConfig: clientConfig,
		}

		proxyURLEnv := ""

		if os.Getenv("HTTPS_PROXY") != "" {
			proxyURLEnv = os.Getenv("HTTPS_PROXY")
		} else if os.Getenv("HTTP_PROXY") != "" {
			proxyURLEnv = os.Getenv("HTTP_PROXY")
		} else if os.Getenv("NO_PROXY") != "" {
			proxyURLEnv = os.Getenv("NO_PROXY")
		}

		if proxyURLEnv != "" {
			transportConfig.Proxy = http.ProxyFromEnvironment

			klog.Info("HTTP transport proxy set")
		}

		customClient := &http.Client{
			/* #nosec G402 */
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

// GetSubscriptionBranch returns GitHub repo branch for a given subscription
func GetSubscriptionBranch(sub *appv1.Subscription) plumbing.ReferenceName {
	annotations := sub.GetAnnotations()
	branchStr := annotations[appv1.AnnotationGitBranch]

	if branchStr == "" {
		branchStr = annotations[appv1.AnnotationGithubBranch] // AnnotationGithubBranch will be depricated
	}

	return GetSubscriptionBranchRef(branchStr)
}

func GetSubscriptionBranchRef(b string) plumbing.ReferenceName {
	branch := plumbing.Master

	if b != "" && b != "master" {
		if !strings.HasPrefix(b, "refs/heads/") {
			b = "refs/heads/" + b
		}

		branch = plumbing.ReferenceName(b)
	}

	return branch
}

func GetChannelConnectionConfig(secret *corev1.Secret, configmap *corev1.ConfigMap) (connCfg *ChannelConnectionCfg, err error) {
	connCfg = &ChannelConnectionCfg{}

	if secret != nil {
		user, token, sshKey, passphrase, clientkey, clientcert, err := ParseChannelSecret(secret)

		if err != nil {
			return nil, err
		}

		connCfg.User = user
		connCfg.Password = token
		connCfg.SSHKey = sshKey
		connCfg.Passphrase = passphrase
		connCfg.ClientCert = clientcert
		connCfg.ClientKey = clientkey
	}

	if configmap != nil {
		caCert := configmap.Data[appv1.ChannelCertificateData]

		connCfg.CaCerts = caCert
	}

	return connCfg, nil
}

// GetChannelSecret returns username and password for channel
func GetChannelSecret(client client.Client, chn *chnv1.Channel) (string, string, []byte, []byte, []byte, []byte, error) {
	username := ""
	accessToken := ""
	sshKey := []byte("")
	passphrase := []byte("")
	clientkey := []byte("")
	clientcert := []byte("")

	if chn.Spec.SecretRef != nil {
		secret := &corev1.Secret{}
		secns := chn.Spec.SecretRef.Namespace

		if secns == "" {
			secns = chn.Namespace
		}

		err := client.Get(context.TODO(), types.NamespacedName{Name: chn.Spec.SecretRef.Name, Namespace: secns}, secret)
		if err != nil {
			klog.Error(err, "Unable to get secret from local cluster.")
			return username, accessToken, sshKey, passphrase, clientkey, clientcert, err
		}

		username, accessToken, sshKey, passphrase, clientkey, clientcert, err = ParseChannelSecret(secret)

		if err != nil {
			return username, accessToken, sshKey, passphrase, clientkey, clientcert, err
		}
	}

	return username, accessToken, sshKey, passphrase, clientkey, clientcert, nil
}

// GetDataFromChannelConfigMap returns username and password for channel
func GetChannelConfigMap(client client.Client, chn *chnv1.Channel) *corev1.ConfigMap {
	if chn.Spec.ConfigMapRef != nil {
		configMapRet := &corev1.ConfigMap{}

		cmns := chn.Namespace

		err := client.Get(context.TODO(), types.NamespacedName{Name: chn.Spec.ConfigMapRef.Name, Namespace: cmns}, configMapRet)

		if err != nil {
			klog.Error(err, "Unable to get config map from local cluster.")
			return nil
		}

		return configMapRet
	}

	return nil
}

func ParseChannelSecret(secret *corev1.Secret) (string, string, []byte, []byte, []byte, []byte, error) {
	username := ""
	accessToken := ""
	sshKey := []byte("")
	passphrase := []byte("")
	clientKey := []byte("")
	clientCert := []byte("")
	err := yaml.Unmarshal(secret.Data[UserID], &username)

	if err != nil {
		klog.Error(err, "Failed to unmarshal username from the secret.")
		return username, accessToken, sshKey, passphrase, clientKey, clientCert, err
	}

	err = yaml.Unmarshal(secret.Data[AccessToken], &accessToken)

	if err != nil {
		klog.Error(err, "Failed to unmarshal accessToken from the secret.")
		return username, accessToken, sshKey, passphrase, clientKey, clientCert, err
	}

	sshKey = bytes.TrimSpace(secret.Data[SSHKey])
	passphrase = bytes.TrimSpace(secret.Data[Passphrase])
	clientKey = bytes.TrimSpace(secret.Data[ClientKey])
	clientCert = bytes.TrimSpace(secret.Data[ClientCert])

	if (len(clientKey) == 0 && len(clientCert) > 0) || (len(clientKey) > 0 && len(clientCert) == 0) {
		klog.Error(err, "for mTLS connection to Git, both clientKey (private key) and clientCert (certificate) are required in the channel secret")
		return username, accessToken, sshKey, passphrase, clientKey, clientCert,
			errors.New("for mTLS connection to Git, both clientKey (private key) and clientCert (certificate) are required in the channel secret")
	}

	if len(sshKey) == 0 && len(clientKey) == 0 {
		if username == "" || accessToken == "" {
			klog.Error(err, "sshKey (and optionally passphrase) or user and accressToken need to be specified in the channel secret")
			return username, accessToken, sshKey, passphrase, clientKey, clientCert,
				errors.New("ssh_key (and optionally passphrase) or user and accressToken need to be specified in the channel secret")
		}
	}

	return username, accessToken, sshKey, passphrase, clientKey, clientCert, nil
}

// GetLocalGitFolder returns the local Git repo clone directory
func GetLocalGitFolder(sub *appv1.Subscription) string {
	return filepath.Join(os.TempDir(), sub.Name, GetSubscriptionBranch(sub).Short())
}

type SkipFunc func(string, string) bool

// SortResources sorts kube resources into different arrays for processing them later.
func SortResources(repoRoot, resourcePath string, skips ...SkipFunc) (map[string]string, map[string]string, []string, []string, []string, error) {
	klog.V(4).Info("Git repo subscription directory: ", resourcePath)

	var skip SkipFunc

	if len(skips) == 0 {
		skip = func(string, string) bool { return false }
	} else {
		skip = skips[0]
	}

	// In the cloned git repo root, find all helm chart directories
	chartDirs := make(map[string]string)

	// In the cloned git repo root, find all kustomization directories
	kustomizeDirs := make(map[string]string)

	// Apply CustomResourceDefinition and Namespace Kubernetes resources first
	crdsAndNamespaceFiles := []string{}
	// Then apply ServiceAccount, ClusterRole and Role Kubernetes resources next
	rbacFiles := []string{}
	// Then apply the rest of resource
	otherFiles := []string{}

	currentChartDir := "NONE"
	currentKustomizeDir := "NONE"

	kubeIgnore := GetKubeIgnore(resourcePath)

	err := filepath.Walk(resourcePath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			relativePath := path

			if len(strings.SplitAfter(path, repoRoot+"/")) > 1 {
				relativePath = strings.SplitAfter(path, repoRoot+"/")[1]
			}

			if !kubeIgnore.MatchesPath(relativePath) && !skip(resourcePath, path) {
				if info.IsDir() {
					klog.V(4).Info("Ignoring subfolders of ", currentChartDir)
					if _, err := os.Stat(path + "/Chart.yaml"); err == nil {
						klog.V(4).Info("Found Chart.yaml in ", path)
						if !strings.HasPrefix(path, currentChartDir) {
							klog.V(4).Info("This is a helm chart folder.")
							chartDirs[path+"/"] = path + "/"
							currentChartDir = path + "/"
						}
					} else if _, err := os.Stat(path + "/kustomization.yaml"); err == nil {
						// If there are nested kustomizations or any other folder structures containing kube
						// resources under a kustomization, subscription should not process them and let kustomize
						// build handle them based on the top-level kustomization.yaml.
						if !strings.HasPrefix(path, currentKustomizeDir) {
							klog.V(4).Info("Found kustomization.yaml in ", path)
							currentKustomizeDir = path + "/"
							kustomizeDirs[path+"/"] = path + "/"
						}
					} else if _, err := os.Stat(path + "/kustomization.yml"); err == nil {
						// If there are nested kustomizations or any other folder structures containing kube
						// resources under a kustomization, subscription should not process them and let kustomize
						// build handle them based on the top-level kustomization.yaml
						if !strings.HasPrefix(path, currentKustomizeDir) {
							klog.V(4).Info("Found kustomization.yml in ", path)
							currentKustomizeDir = path + "/"
							kustomizeDirs[path+"/"] = path + "/"
						}
					}
				} else if !strings.HasPrefix(path, currentChartDir) &&
					!strings.HasPrefix(path, repoRoot+"/.git") &&
					!strings.HasPrefix(path, currentKustomizeDir) {
					// Do not process kubernetes YAML files under helm chart or kustomization directory
					// If there are nested kustomizations or any other folder structures containing kube
					// resources under a kustomization, subscription should not process them and let kustomize
					// build handle them based on the top-level kustomization.yaml
					crdsAndNamespaceFiles, rbacFiles, otherFiles, err = sortKubeResource(crdsAndNamespaceFiles, rbacFiles, otherFiles, path)
					if err != nil {
						klog.Error(err.Error())
						return err
					}
				}
			}

			return nil
		})

	return chartDirs, kustomizeDirs, crdsAndNamespaceFiles, rbacFiles, otherFiles, err
}

func sortKubeResource(crdsAndNamespaceFiles, rbacFiles, otherFiles []string, path string) ([]string, []string, []string, error) {
	if strings.EqualFold(filepath.Ext(path), ".yml") || strings.EqualFold(filepath.Ext(path), ".yaml") {
		klog.V(4).Info("Reading file: ", path)

		file, err := ioutil.ReadFile(path) // #nosec G304 path is not user input

		if err != nil {
			klog.Error(err, "Failed to read YAML file "+path)
			return crdsAndNamespaceFiles, rbacFiles, otherFiles, err
		}

		resources := ParseKubeResoures(file)

		if len(resources) == 1 {
			t := kubeResource{}
			err := yaml.Unmarshal(resources[0], &t)

			if err != nil {
				klog.Warning("Failed to unmarshal YAML file")
				// Just ignore the YAML
				return crdsAndNamespaceFiles, rbacFiles, otherFiles, nil
			}

			if t.APIVersion != "" && t.Kind != "" {
				if strings.EqualFold(t.Kind, "customresourcedefinition") {
					crdsAndNamespaceFiles = append(crdsAndNamespaceFiles, path)
				} else if strings.EqualFold(t.Kind, "namespace") {
					crdsAndNamespaceFiles = append(crdsAndNamespaceFiles, path)
				} else if strings.EqualFold(t.Kind, "serviceaccount") ||
					strings.EqualFold(t.Kind, "clusterrole") ||
					strings.EqualFold(t.Kind, "role") ||
					strings.EqualFold(t.Kind, "clusterrolebinding") ||
					strings.EqualFold(t.Kind, "rolebinding") {
					rbacFiles = append(rbacFiles, path)
				} else {
					otherFiles = append(otherFiles, path)
				}
			}
		} else if len(resources) > 1 {
			klog.Info("Multi resource")
			otherFiles = append(otherFiles, path)
		}
	}

	return crdsAndNamespaceFiles, rbacFiles, otherFiles, nil
}

func SkipHooksOnManaged(resourcePath, curPath string) bool {
	PREHOOK := "prehook"
	POSTHOOK := "posthook"

	// of the resource root.
	pre := fmt.Sprintf("%s/%s", resourcePath, PREHOOK)
	post := fmt.Sprintf("%s/%s", resourcePath, POSTHOOK)

	return strings.HasPrefix(curPath, pre) || strings.HasPrefix(curPath, post)
}

// GetKubeIgnore get .kubernetesignore list
func GetKubeIgnore(resourcePath string) *gitignore.GitIgnore {
	klog.V(4).Info("Git repo resource root directory: ", resourcePath)

	lines := []string{""}
	kubeIgnore, _ := gitignore.CompileIgnoreLines(lines...)

	if _, err := os.Stat(filepath.Join(resourcePath, ".kubernetesignore")); err == nil {
		klog.V(4).Info("Found .kubernetesignore in ", resourcePath)
		kubeIgnore, _ = gitignore.CompileIgnoreFile(filepath.Join(resourcePath, ".kubernetesignore"))
	}

	return kubeIgnore
}

// IsGitChannel returns true if channel type is github or git
func IsGitChannel(chType string) bool {
	return strings.EqualFold(chType, chnv1.ChannelTypeGitHub) ||
		strings.EqualFold(chType, chnv1.ChannelTypeGit)
}

func IsClusterAdmin(client client.Client, sub *appv1.Subscription, eventRecorder *EventRecorder) bool {
	isClusterAdmin := false
	isUserSubAdmin := false
	isSubPropagatedFromHub := false
	isClusterAdminAnnotationTrue := false

	userIdentity := ""
	userGroups := ""
	annotations := sub.GetAnnotations()

	if annotations != nil {
		encodedUserGroup := strings.Trim(annotations[appv1.AnnotationUserGroup], "")
		encodedUserIdentity := strings.Trim(annotations[appv1.AnnotationUserIdentity], "")

		if encodedUserGroup != "" {
			userGroups = base64StringDecode(encodedUserGroup)
		}

		if encodedUserIdentity != "" {
			userIdentity = base64StringDecode(encodedUserIdentity)
		}

		if annotations[appv1.AnnotationHosting] != "" {
			isSubPropagatedFromHub = true
		}

		if strings.EqualFold(annotations[appv1.AnnotationClusterAdmin], "true") {
			isClusterAdminAnnotationTrue = true
		}
	}

	doesWebhookExist := false
	theWebhook := &admissionv1.MutatingWebhookConfiguration{}

	if err := client.Get(context.TODO(), types.NamespacedName{Name: appv1.AcmWebhook}, theWebhook); err == nil {
		doesWebhookExist = true
	}

	if userIdentity != "" && doesWebhookExist {
		isUserSubAdmin = matchUserSubAdmin(client, userIdentity, userGroups)
	}

	// If subscription has cluster-admin:true and propagated from hub and cannot find the webhook, we know we are
	// on the managed cluster so trust the annotations to decide that the subscription is from subscription-admin.
	// But the subscription can also be propagated to the self-managed hub cluster.
	if isClusterAdminAnnotationTrue && isSubPropagatedFromHub {
		if !doesWebhookExist || // not on the hub cluster
			(doesWebhookExist && strings.HasSuffix(sub.GetName(), "-local")) { // on the hub cluster and the subscription has -local suffix
			if eventRecorder != nil {
				eventRecorder.RecordEvent(sub, "RoleElevation",
					"Role was elevated to cluster admin for subscription "+sub.Name, nil)
			}

			isClusterAdmin = true
		}
	} else if isUserSubAdmin {
		if eventRecorder != nil {
			eventRecorder.RecordEvent(sub, "RoleElevation",
				"Role was elevated to cluster admin for subscription "+sub.Name+" by user "+userIdentity, nil)
		}

		isClusterAdmin = true
	}

	klog.Infof("isClusterAdmin = %v", isClusterAdmin)

	return isClusterAdmin
}

func matchUserSubAdmin(client client.Client, userIdentity, userGroups string) bool {
	isUserSubAdmin := false
	foundClusterRoleBinding := &rbacv1.ClusterRoleBinding{}

	err := client.Get(context.TODO(), types.NamespacedName{Name: appv1.SubscriptionAdmin}, foundClusterRoleBinding)

	if err == nil {
		klog.Infof("ClusterRoleBinding %s found.", appv1.SubscriptionAdmin)

		for _, subject := range foundClusterRoleBinding.Subjects {
			if strings.Trim(subject.Name, "") == strings.Trim(userIdentity, "") && strings.Trim(subject.Kind, "") == "User" {
				klog.Info("User match. cluster-admin: true")

				isUserSubAdmin = true
			} else if subject.Kind == "Group" {
				groupNames := strings.Split(userGroups, ",")
				for _, groupName := range groupNames {
					if strings.Trim(subject.Name, "") == strings.Trim(groupName, "") {
						klog.Info("Group match. cluster-admin: true")

						isUserSubAdmin = true
					}
				}
			}
		}
	} else {
		klog.Error(err)
	}

	foundClusterRoleBinding = nil

	return isUserSubAdmin
}

func base64StringDecode(encodedStr string) string {
	decodedBytes, err := base64.StdEncoding.DecodeString(encodedStr)
	if err != nil {
		klog.Error("Failed to base64 decode")
		klog.Error(err)
	}

	return string(decodedBytes)
}

func GetLatestCommitID(url, branch string, clt ...*github.Client) (string, error) {
	gitClt := github.NewClient(nil)
	if len(clt) != 0 {
		gitClt = clt[0]
	}

	u, err := getOwnerAndRepo(url)
	if err != nil {
		return "", err
	}

	owner, repo := u[0], u[1]
	ctx := context.TODO()

	b, _, err := gitClt.Repositories.GetBranch(ctx, owner, repo, branch)
	if err != nil {
		return "", err
	}

	return *b.Commit.SHA, nil
}

func ParseYAML(fileContent []byte) []string {
	fileContentString := string(fileContent)
	lines := strings.Split(fileContentString, "\n")
	newFileContent := []byte("")

	// Multi-document YAML delimeter --- might have trailing spaces. Trim those first.
	for _, line := range lines {
		if strings.HasPrefix(line, "---") {
			line = strings.Trim(line, " ")
		}

		line += "\n"

		newFileContent = append(newFileContent, line...)
	}

	// Then now split the YAML content using --- delimeter
	items := strings.Split(string(newFileContent), "\n---\n")

	return items
}

func getOwnerAndRepo(url string) ([]string, error) {
	if len(url) == 0 {
		return []string{}, nil
	}

	gitSuffix := ".git"

	l1 := strings.Split(url, "//")
	if len(l1) < 1 {
		return []string{}, fmt.Errorf("invalid git url l1")
	}

	var l2 string

	if len(l1) == 1 {
		l2 = strings.TrimSuffix(l1[0], gitSuffix)
	} else {
		l2 = strings.TrimSuffix(l1[1], gitSuffix)
	}

	l3 := strings.Split(l2, "/")

	n := len(l3)
	if n < 2 {
		return []string{}, fmt.Errorf("invalid git url l2")
	}

	return []string{l3[n-2], l3[n-1]}, nil
}
