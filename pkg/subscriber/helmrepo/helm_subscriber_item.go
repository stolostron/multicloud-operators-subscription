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

package helmrepo

import (
	"context"
	"crypto/sha1" // #nosec G505 Used only to generate random value to be used to generate hash string
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	gerr "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ghodss/yaml"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	releasev1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/helmrelease/v1"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "open-cluster-management.io/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
)

// SubscriberItem - defines the unit of namespace subscription.
type SubscriberItem struct {
	appv1.SubscriberItem
	hash          string
	reconcileRate string
	syncTime      string
	stopch        chan struct{}
	count         int
	syncinterval  int
	success       bool
	synchronizer  SyncSource
	clusterAdmin  bool
}

var (
	helmGvk = schema.GroupVersionKind{
		Group:   appv1.SchemeGroupVersion.Group,
		Version: appv1.SchemeGroupVersion.Version,
		Kind:    "HelmRelease",
	}
)

// SubscribeItem subscribes a subscriber item with namespace channel.
func (hrsi *SubscriberItem) Start(restart bool) {
	// do nothing if already started
	if hrsi.stopch != nil {
		if restart {
			// restart this goroutine
			klog.Info("Stopping SubscriberItem: ", hrsi.Subscription.Name)
			hrsi.Stop()
		} else {
			klog.Info("SubscriberItem already started: ", hrsi.Subscription.Name)

			return
		}
	}

	hrsi.count = 0 // reset the counter

	hrsi.stopch = make(chan struct{})

	loopPeriod, retryInterval, retries := utils.GetReconcileInterval(hrsi.reconcileRate, chnv1.ChannelTypeHelmRepo)

	if strings.EqualFold(hrsi.reconcileRate, "off") {
		klog.Infof("auto-reconcile is OFF")

		hrsi.doSubscriptionWithRetries(retryInterval, retries)

		return
	}

	go wait.Until(func() {
		tw := hrsi.SubscriberItem.Subscription.Spec.TimeWindow
		if tw != nil {
			nextRun := utils.NextStartPoint(tw, time.Now())
			if nextRun > time.Duration(0) {
				klog.Infof("Subscription is currently blocked by the time window. It %v/%v will be deployed after %v",
					hrsi.SubscriberItem.Subscription.GetNamespace(),
					hrsi.SubscriberItem.Subscription.GetName(), nextRun)

				return
			}
		}

		// if the subscription pause lable is true, stop subscription here.
		if utils.GetPauseLabel(hrsi.SubscriberItem.Subscription) {
			klog.Infof("Helm Subscription %v/%v is paused.", hrsi.SubscriberItem.Subscription.GetNamespace(), hrsi.SubscriberItem.Subscription.GetName())

			return
		}

		hrsi.doSubscriptionWithRetries(retryInterval, retries)
	}, loopPeriod, hrsi.stopch)
}

func (hrsi *SubscriberItem) Stop() {
	if hrsi.stopch != nil {
		close(hrsi.stopch)
		hrsi.stopch = nil
	}
}

func (hrsi *SubscriberItem) doSubscriptionWithRetries(retryInterval time.Duration, retries int) {
	hrsi.doSubscription()

	// If the initial subscription fails, retry.
	n := 0

	for n < retries {
		if !hrsi.success {
			time.Sleep(retryInterval)
			klog.Infof("Re-try #%d: subcribing to the Helm repo", n+1)
			hrsi.doSubscription()
			n++
		} else {
			break
		}
	}
}

func (hrsi *SubscriberItem) getRepoInfo(usePrimary bool) (*repo.IndexFile, string, error) {
	channel := hrsi.Channel

	if !usePrimary && hrsi.SecondaryChannel != nil {
		channel = hrsi.SecondaryChannel
	}

	//Retrieve the helm repo
	repoURL := channel.Spec.Pathname

	httpClient, err := getHelmRepoClient(hrsi.ChannelConfigMap, channel.Spec.InsecureSkipVerify)

	if err != nil {
		klog.Error(err, "Unable to create client for helm repo", repoURL)
		return nil, "", err
	}

	indexFile, hash, err := getHelmRepoIndex(httpClient, hrsi.Subscription, hrsi.ChannelSecret, repoURL)

	if err != nil {
		klog.Error(err, "Unable to retrieve the helm repo index", repoURL)
		return nil, "", err
	}

	return indexFile, hash, nil
}

func (hrsi *SubscriberItem) doSubscription() {
	var indexFile *repo.IndexFile

	var hash string

	var err error

	//Update the secret and config map
	if hrsi.Channel != nil {
		sec, cm := utils.FetchChannelReferences(hrsi.synchronizer.GetRemoteNonCachedClient(), *hrsi.Channel)
		if sec != nil {
			if err := utils.ListAndDeployReferredObject(hrsi.synchronizer.GetLocalNonCachedClient(), hrsi.Subscription,
				schema.GroupVersionKind{Group: "", Kind: "Secret", Version: "v1"}, sec); err != nil {
				klog.Warningf("can't deploy reference secret %v for subscription %v", hrsi.ChannelSecret.GetName(), hrsi.Subscription.GetName())
			}
		}

		if cm != nil {
			if err := utils.ListAndDeployReferredObject(hrsi.synchronizer.GetLocalNonCachedClient(), hrsi.Subscription,
				schema.GroupVersionKind{Group: "", Kind: "ConfigMap", Version: "v1"}, cm); err != nil {
				klog.Warningf("can't deploy reference configmap %v for subscription %v", hrsi.ChannelConfigMap.GetName(), hrsi.Subscription.GetName())
			}
		}

		sec, cm = utils.FetchChannelReferences(hrsi.synchronizer.GetLocalNonCachedClient(), *hrsi.Channel)
		if sec != nil {
			klog.V(1).Info("updated in memory channel secret for ", hrsi.Subscription.Name)
			hrsi.ChannelSecret = sec
		}

		if cm != nil {
			klog.V(1).Info("updated in memory channel configmap for ", hrsi.Subscription.Name)
			hrsi.ChannelConfigMap = cm
		}
	}

	if hrsi.SecondaryChannel != nil {
		sec, cm := utils.FetchChannelReferences(hrsi.synchronizer.GetRemoteNonCachedClient(), *hrsi.SecondaryChannel)
		if sec != nil {
			if err := utils.ListAndDeployReferredObject(hrsi.synchronizer.GetLocalNonCachedClient(), hrsi.Subscription,
				schema.GroupVersionKind{Group: "", Kind: "Secret", Version: "v1"}, sec); err != nil {
				klog.Warningf("can't deploy reference secondary secret %v for subscription %v", hrsi.SecondaryChannelSecret.GetName(), hrsi.Subscription.GetName())
			}
		}

		if cm != nil {
			if err := utils.ListAndDeployReferredObject(hrsi.synchronizer.GetLocalNonCachedClient(), hrsi.Subscription,
				schema.GroupVersionKind{Group: "", Kind: "ConfigMap", Version: "v1"}, cm); err != nil {
				klog.Warningf("can't deploy reference secondary configmap %v for subscription %v", hrsi.SecondaryChannelConfigMap.GetName(), hrsi.Subscription.GetName())
			}
		}

		sec, cm = utils.FetchChannelReferences(hrsi.synchronizer.GetLocalNonCachedClient(), *hrsi.SecondaryChannel)
		if sec != nil {
			klog.Info("updated in memory secondary channel secret for ", hrsi.Subscription.Name)
			hrsi.SecondaryChannelSecret = sec
		}

		if cm != nil {
			klog.V(1).Info("updated in memory secondary channel configmap for ", hrsi.Subscription.Name)
			hrsi.SecondaryChannelConfigMap = cm
		}
	}

	indexFile, hash, err = hrsi.getRepoInfo(true) // true for using primary channel

	if err != nil {
		klog.Error(err, "Unable to retrieve the helm repo index from the primary channel.")

		if hrsi.SecondaryChannel != nil {
			klog.Info("Trying the secondary channel")

			indexFile, hash, err = hrsi.getRepoInfo(false) // true for using primary channel

			if err != nil {
				klog.Error(err, "Unable to retrieve the helm repo index from the secondary channel.")

				return
			}
		} else {
			return
		}
	}

	klog.V(4).Infof("Check if helmRepo changed with hash %s", hash)

	hrNames := getHelmReleaseNames(indexFile, hrsi.Subscription)

	for _, hrName := range hrNames {
		isHashDiff := hash != hrsi.hash
		isUnsuccessful := !hrsi.success
		existsHelmRelease := false
		populatedHelmReleaseStatus := false

		existsHelmRelease, err = isHelmReleaseExists(hrsi.synchronizer.GetLocalClient(), hrsi.Subscription.Namespace, hrName)
		if err != nil {
			klog.Error("Failed to determine if HelmRelease exists: ", err)

			hrsi.success = false

			return
		}

		if existsHelmRelease {
			populatedHelmReleaseStatus, err = isHelmReleaseStatusPopulated(hrsi.synchronizer.GetLocalClient(),
				types.NamespacedName{Name: hrsi.Subscription.Name,
					Namespace: hrsi.Subscription.Namespace}, hrsi.Subscription.Namespace, hrName)
			if err != nil {
				klog.Error("Failed to determine if HelmRelease status is populated: ", err)

				hrsi.success = false

				return
			}
		}

		if isHashDiff || isUnsuccessful || !existsHelmRelease || !populatedHelmReleaseStatus {
			klog.Infof("Processing Helm Subscription... isHashDiff=%v isUnsuccessful=%v existsHelmRelease=%v populatedHelmReleaseStatus=%v",
				isHashDiff, isUnsuccessful, existsHelmRelease, populatedHelmReleaseStatus)

			if err := hrsi.processSubscription(indexFile, hash); err != nil {
				klog.Error("Failed to process helm repo subscription with error:", err)

				hrsi.success = false

				return
			}

			hrsi.success = true
		}
	}
}

func getHelmReleaseNames(indexFile *repo.IndexFile, sub *appv1.Subscription) []string {
	klog.Infof("Calculating the HelmRelease names")

	var hrNames []string

	for packageName := range indexFile.Entries {
		releaseCRName, err := utils.PkgToReleaseCRName(sub, packageName)
		if err != nil {
			klog.Error(err, "Unable to get HelmRelease name for package: ", packageName)

			continue
		}

		hrNames = append(hrNames, releaseCRName)
	}

	return hrNames
}

func isHelmReleaseExists(client client.Client, namespace string, releaseCRName string) (bool, error) {
	klog.Infof("Checking to see if the HelmRelease %s/%s exists", namespace, releaseCRName)

	helmRelease := &releasev1.HelmRelease{}

	if err := client.Get(context.TODO(),
		types.NamespacedName{Name: releaseCRName, Namespace: namespace}, helmRelease); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}

		klog.Error(err, "Unable to get HelmRelease %s/%s", namespace, releaseCRName)

		return false, err
	}

	return true, nil
}

func isHelmReleaseStatusPopulated(client client.Client, hostSub types.NamespacedName, namespace string, releaseCRName string) (bool, error) {
	klog.Infof("Checking to see if the HelmRelease %s/%s status is populated", namespace, releaseCRName)

	helmRelease := &releasev1.HelmRelease{}

	if err := client.Get(context.TODO(),
		types.NamespacedName{Name: releaseCRName, Namespace: namespace}, helmRelease); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}

		klog.Error(err, "Unable to get HelmRelease %s/%s", namespace, releaseCRName)

		return false, err
	}

	sub := &appv1.Subscription{}
	if err := client.Get(context.TODO(), hostSub, sub); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}

		klog.Error(err, "Unable to get Subscription %v", hostSub)

		return false, err
	}

	return true, nil
}

func (hrsi *SubscriberItem) processSubscription(indexFile *repo.IndexFile, hash string) error {
	if err := hrsi.manageHelmCR(indexFile); err != nil {
		return err
	}

	hrsi.hash = hash

	return nil
}

func getHelmRepoClient(chnCfg *corev1.ConfigMap, insecureSkipVerify bool) (*http.Client, error) {
	client := http.DefaultClient

	if insecureSkipVerify {
		klog.Info("Channel spec has insecureSkipVerify: true. Skipping Helm repo server certificate verification.")
	}

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
		/* #nosec G402 */
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipVerify, // #nosec G402 InsecureSkipVerify optionally
			MinVersion:         tls.VersionTLS12,
		},
	}

	if chnCfg != nil && !insecureSkipVerify {
		helmRepoConfigData := chnCfg.Data
		klog.V(1).Infof("s.HelmRepoConfig.Data %v", helmRepoConfigData)

		if helmRepoConfigData["insecureSkipVerify"] != "" {
			b, err := strconv.ParseBool(helmRepoConfigData["insecureSkipVerify"])
			if err != nil {
				klog.Error(err, "Unable to parse insecureSkipVerify: ", helmRepoConfigData["insecureSkipVerify"])

				return nil, err
			}

			transport.TLSClientConfig.InsecureSkipVerify = b

			if b {
				klog.Info("Channel has config map with insecureSkipVerify: true. Skipping Helm repo server certificate verification.")
			}
		} else {
			klog.Info("helmRepoConfigData[\"insecureSkipVerify\"] is empty")
		}
	} else {
		klog.Info("s.HelmRepoConfig is nil")
	}

	client.Transport = transport

	return client, nil
}

//getHelmRepoIndex retreives the index.yaml, loads it into a repo.IndexFile and filters it
func getHelmRepoIndex(client rest.HTTPClient, sub *appv1.Subscription,
	chnSrt *corev1.Secret, repoURL string) (indexFile *repo.IndexFile, hash string, err error) {
	cleanRepoURL := strings.TrimSuffix(repoURL, "/") + "/index.yaml"
	req, err := http.NewRequest(http.MethodGet, cleanRepoURL, nil)

	if err != nil {
		klog.Error(err, "Can not build request: ", cleanRepoURL)

		return nil, "", err
	}

	if chnSrt != nil && chnSrt.Data != nil {
		if authHeader, ok := chnSrt.Data["authHeader"]; ok {
			req.Header.Set("Authorization", string(authHeader))
		} else if user, ok := chnSrt.Data["user"]; ok {
			if password, ok := chnSrt.Data["password"]; ok {
				req.SetBasicAuth(string(user), string(password))
			} else {
				return nil, "", fmt.Errorf("password not found in secret for basic authentication")
			}
		}
	}

	klog.V(1).Info(req)
	resp, err := client.Do(req)

	if err != nil {
		klog.Error(err, "Http request failed: ", cleanRepoURL)

		return nil, "", err
	}

	if resp.StatusCode != http.StatusOK {
		klog.Errorf("http request %s failed: status %s", cleanRepoURL, resp.Status)

		return nil, "", fmt.Errorf("http request %s failed: status %s", cleanRepoURL, resp.Status)
	}

	klog.V(5).Info("Get succeeded: ", cleanRepoURL)

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		klog.Error(err, "Unable to read body: ", cleanRepoURL)

		return nil, "", err
	}

	defer resp.Body.Close()

	hash = hashKey(body)
	indexfile, err := loadIndex(body)

	if err != nil {
		klog.Error(err, "Unable to parse the indexfile: ", cleanRepoURL)

		return nil, "", err
	}

	err = utils.FilterCharts(sub, indexfile)

	return indexfile, hash, err
}

func GetSubscriptionChartsOnHub(hubClt client.Client, channel, secondChannel *chnv1.Channel, sub *appv1.Subscription) ([]*releasev1.HelmRelease, error) {
	// Try with the primary channel first
	indexFile, err := getChartIndexWithChannel(hubClt, channel, sub)

	if err != nil {
		klog.Errorf("unable to retrieve the helm repo index from %v", channel.Spec.Pathname)

		if secondChannel != nil {
			// try with the secondary channel
			indexFile, err = getChartIndexWithChannel(hubClt, secondChannel, sub)

			if err != nil {
				klog.Errorf("unable to retrieve the helm repo index from %v", secondChannel.Spec.Pathname)
				return nil, gerr.Wrapf(err, "unable to retrieve the helm repo index from %v", secondChannel.Spec.Pathname)
			}
		} else {
			return nil, gerr.Wrapf(err, "unable to retrieve the helm repo index from %v", channel.Spec.Pathname)
		}
	}

	return ChartIndexToHelmReleases(hubClt, channel, secondChannel, sub, indexFile)
}

func getChartIndexWithChannel(hubClt client.Client, channel *chnv1.Channel, sub *appv1.Subscription) (*repo.IndexFile, error) {
	klog.Infof("Preparing HTTP client with channel %s/%s", channel.Namespace, channel.Name)

	chSecret := &corev1.Secret{}

	if channel.Spec.SecretRef != nil {
		srtNs := channel.GetNamespace()

		chnSecretKey := types.NamespacedName{
			Name:      channel.Spec.SecretRef.Name,
			Namespace: srtNs,
		}

		if err := hubClt.Get(context.TODO(), chnSecretKey, chSecret); err != nil {
			return nil, gerr.Wrapf(err, "failed to get reference secret %v from channel", chnSecretKey.String())
		}

		klog.Infof("got secret %v from channel %v", chnSecretKey.String(), channel)
	}

	chnCfg := &corev1.ConfigMap{}

	if channel.Spec.ConfigMapRef != nil {
		cfgNs := channel.Spec.ConfigMapRef.Namespace

		if cfgNs == "" {
			cfgNs = channel.GetNamespace()
		}

		chnCfgKey := types.NamespacedName{
			Name:      channel.Spec.ConfigMapRef.Name,
			Namespace: cfgNs,
		}

		if err := hubClt.Get(context.TODO(), chnCfgKey, chnCfg); err != nil {
			return nil, gerr.Wrapf(err, "failed to get reference configmap %v from channel", chnCfgKey.String())
		}

		klog.Infof("got configmap %v from channel %v", chnCfgKey.String(), channel)
	}

	httpClient, err := getHelmRepoClient(chnCfg, channel.Spec.InsecureSkipVerify)

	if err != nil {
		return nil, gerr.Wrapf(err, "Unable to create client for helm repo %v", channel.Spec.Pathname)
	}

	indexFile, _, err := getHelmRepoIndex(httpClient, sub, chSecret, channel.Spec.Pathname)
	if err != nil {
		return nil, gerr.Wrapf(err, "unable to retrieve the helm repo index %v", channel.Spec.Pathname)
	}

	return indexFile, nil
}

func ChartIndexToHelmReleases(hclt client.Client,
	chn *chnv1.Channel,
	secondChn *chnv1.Channel,
	sub *appv1.Subscription,
	indexFile *repo.IndexFile) ([]*releasev1.HelmRelease, error) {
	helms := make([]*releasev1.HelmRelease, 0)

	for pkgName, chartVer := range indexFile.Entries {
		releaseCRName, err := utils.PkgToReleaseCRName(sub, pkgName)
		if err != nil {
			return nil, gerr.Wrapf(err, "failed to generate releaseCRName of helm chart %v for subscription %v", pkgName, sub)
		}

		helm, err := utils.CreateOrUpdateHelmChart(pkgName, releaseCRName, chartVer, hclt, chn, secondChn, sub)
		if err != nil {
			return nil, gerr.Wrapf(err, "failed to get helm chart of %v for subscription %v", pkgName, sub)
		}

		if err := utils.Override(helm, sub); err != nil {
			return nil, err
		}

		helms = append(helms, helm)
	}

	return helms, nil
}

// loadIndex loads data into a repo.IndexFile.
func loadIndex(data []byte) (*repo.IndexFile, error) {
	i := &repo.IndexFile{}
	if err := yaml.Unmarshal(data, i); err != nil {
		klog.Error(err, "Unmarshal failed. Data: ", data)

		return i, err
	}

	i.SortEntries()

	if i.APIVersion == "" {
		return i, repo.ErrNoAPIVersion
	}

	return i, nil
}

//hashKey Calculate a hash key
func hashKey(b []byte) string {
	h := sha1.New() // #nosec G401 Used only to generate random value to be used to generate hash string
	_, err := h.Write(b)

	if err != nil {
		klog.Error("Failed to hash key with error:", err)
	}

	return string(h.Sum(nil))
}

func (hrsi *SubscriberItem) manageHelmCR(indexFile *repo.IndexFile) error {
	var doErr error

	resources := make([]kubesynchronizer.ResourceUnit, 0)

	//Loop on all packages selected by the subscription
	for packageName, chartVersions := range indexFile.Entries {
		klog.Infof("chart: %s\n%v", packageName, chartVersions)

		dpl, err := utils.CreateHelmCRManifest(
			hrsi.Channel.Spec.Pathname, packageName, chartVersions, hrsi.synchronizer.GetLocalClient(),
			hrsi.Channel, hrsi.SecondaryChannel, hrsi.Subscription, hrsi.clusterAdmin)

		if err != nil {
			klog.Error("failed to create a helmrelease CR manifest, err: ", err)

			doErr = err

			continue
		}

		unit := kubesynchronizer.ResourceUnit{Resource: dpl, Gvk: helmGvk}
		resources = append(resources, unit)
	}

	if len(resources) > 0 || (len(resources) == 0 && doErr == nil) {
		if len(resources) == 0 {
			klog.Warningf("The resources length is 0, this might lead to deregistration for subscription %s/%s",
				hrsi.Subscription.Namespace, hrsi.Subscription.Name)
		}

		if err := hrsi.synchronizer.ProcessSubResources(hrsi.Subscription, resources, nil, nil, false); err != nil {
			klog.Warningf("failed to put helm manifest to cache (will retry), err: %v", err)
			doErr = err
		}
	}

	return doErr
}
