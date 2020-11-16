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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"

	chnv1 "github.com/open-cluster-management/multicloud-operators-channel/pkg/apis/apps/v1"
	releasev1 "github.com/open-cluster-management/multicloud-operators-subscription-release/pkg/apis/apps/v1"
	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	dplpro "github.com/open-cluster-management/multicloud-operators-subscription/pkg/subscriber/processdeployable"
	kubesynchronizer "github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

// SubscriberItem - defines the unit of namespace subscription
type SubscriberItem struct {
	appv1.SubscriberItem

	hash         string
	stopch       chan struct{}
	syncinterval int
	success      bool
	synchronizer SyncSource
}

var (
	helmGvk = schema.GroupVersionKind{
		Group:   appv1.SchemeGroupVersion.Group,
		Version: appv1.SchemeGroupVersion.Version,
		Kind:    "HelmRelease",
	}
)

// SubscribeItem subscribes a subscriber item with namespace channel
func (hrsi *SubscriberItem) Start() {
	// do nothing if already started
	if hrsi.stopch != nil {
		return
	}

	hrsi.stopch = make(chan struct{})

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

		hrsi.doSubscription()
	}, time.Duration(hrsi.syncinterval)*time.Second, hrsi.stopch)
}

func (hrsi *SubscriberItem) Stop() {
	if hrsi.stopch != nil {
		close(hrsi.stopch)
		hrsi.stopch = nil
	}
}

func (hrsi *SubscriberItem) doSubscription() {
	//Retrieve the helm repo
	repoURL := hrsi.Channel.Spec.Pathname

	httpClient, err := getHelmRepoClient(hrsi.ChannelConfigMap, hrsi.Channel.Spec.InsecureSkipVerify)

	if err != nil {
		klog.Error(err, "Unable to create client for helm repo", repoURL)
		return
	}

	indexFile, hash, err := getHelmRepoIndex(httpClient, hrsi.Subscription, hrsi.ChannelSecret, repoURL)

	if err != nil {
		klog.Error(err, "Unable to retrieve the helm repo index", repoURL)
		return
	}

	klog.V(4).Infof("Check if helmRepo %s changed with hash %s", repoURL, hash)

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
	klog.V(4).Infof("Calculating the HelmRelease names")

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
	klog.V(3).Infof("Checking to see if the HelmRelease %s/%s exists", namespace, releaseCRName)

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
	klog.V(3).Infof("Checking to see if the HelmRelease %s/%s status is populated", namespace, releaseCRName)

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

	statuses := sub.Status.Statuses
	if len(statuses) == 0 {
		return false, nil
	}

	localStatus := statuses["/"]
	if localStatus == nil {
		return false, nil
	}

	pkgStatus := localStatus.SubscriptionPackageStatus
	if len(pkgStatus) == 0 {
		return false, nil
	}

	hostDpl := utils.GetHostDeployableFromObject(helmRelease)
	if hostDpl == nil {
		return true, nil
	}

	if pkgStatus[hostDpl.Name] == nil || pkgStatus[hostDpl.Name].ResourceStatus == nil || len(pkgStatus[hostDpl.Name].ResourceStatus.Raw) == 0 {
		return false, nil
	}

	return true, nil
}

func (hrsi *SubscriberItem) processSubscription(indexFile *repo.IndexFile, hash string) error {
	repoURL := hrsi.Channel.Spec.Pathname
	klog.V(4).Info("Proecssing HelmRepo:", repoURL)

	if err := hrsi.manageHelmCR(indexFile, repoURL); err != nil {
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
		klog.V(5).Infof("s.HelmRepoConfig.Data %v", helmRepoConfigData)

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
			klog.V(5).Info("helmRepoConfigData[\"insecureSkipVerify\"] is empty")
		}
	} else {
		klog.V(5).Info("s.HelmRepoConfig is nil")
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

	klog.V(5).Info(req)
	resp, err := client.Do(req)

	if err != nil {
		klog.Error(err, "Http request failed: ", cleanRepoURL)

		return nil, "", err
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

func GetSubscriptionChartsOnHub(hubClt client.Client, sub *appv1.Subscription, insecureSkipVerify bool) ([]*releasev1.HelmRelease, error) {
	chn := &chnv1.Channel{}
	chnkey := utils.NamespacedNameFormat(sub.Spec.Channel)

	if err := hubClt.Get(context.TODO(), chnkey, chn); err != nil {
		return nil, gerr.Wrapf(err, "failed to get channel of subscription %v", sub)
	}

	repoURL := chn.Spec.Pathname
	klog.V(2).Infof("getting resource list of HelmRepo %v", repoURL)

	chSrt := &corev1.Secret{}

	if chn.Spec.SecretRef != nil {
		srtNs := chn.GetNamespace()

		chnSrtKey := types.NamespacedName{
			Name:      chn.Spec.SecretRef.Name,
			Namespace: srtNs,
		}

		if err := hubClt.Get(context.TODO(), chnSrtKey, chSrt); err != nil {
			return nil, gerr.Wrapf(err, "failed to get reference secret %v from channel", chnSrtKey.String())
		}
	}

	chnCfg := &corev1.ConfigMap{}

	if chn.Spec.ConfigMapRef != nil {
		cfgNs := chn.Spec.ConfigMapRef.Namespace

		if cfgNs == "" {
			cfgNs = chn.GetNamespace()
		}

		chnCfgKey := types.NamespacedName{
			Name:      chn.Spec.ConfigMapRef.Name,
			Namespace: cfgNs,
		}

		klog.V(2).Infof("getting cfg %v from channel %v", chnCfgKey.String(), chn)

		if err := hubClt.Get(context.TODO(), chnCfgKey, chnCfg); err != nil {
			return nil, gerr.Wrapf(err, "failed to get reference configmap %v from channel", chnCfgKey.String())
		}
	}

	httpClient, err := getHelmRepoClient(chnCfg, insecureSkipVerify)
	if err != nil {
		return nil, gerr.Wrapf(err, "Unable to create client for helm repo %v", repoURL)
	}

	indexFile, _, err := getHelmRepoIndex(httpClient, sub, chSrt, repoURL)
	if err != nil {
		return nil, gerr.Wrapf(err, "unable to retrieve the helm repo index %v", repoURL)
	}

	return ChartIndexToHelmReleases(hubClt, chn, sub, indexFile)
}

func ChartIndexToHelmReleases(hclt client.Client, chn *chnv1.Channel, sub *appv1.Subscription, indexFile *repo.IndexFile) ([]*releasev1.HelmRelease, error) {
	helms := make([]*releasev1.HelmRelease, 0)

	for pkgName, chartVer := range indexFile.Entries {
		releaseCRName, err := utils.PkgToReleaseCRName(sub, pkgName)
		if err != nil {
			return nil, gerr.Wrapf(err, "failed to generate releaseCRName of helm chart %v for subscription %v", pkgName, sub)
		}

		helm, err := utils.CreateOrUpdateHelmChart(pkgName, releaseCRName, chartVer, hclt, chn, sub)
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

//loadIndex loads data into a repo.IndexFile
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

func (hrsi *SubscriberItem) manageHelmCR(indexFile *repo.IndexFile, repoURL string) error {
	var doErr error

	hostkey := types.NamespacedName{Name: hrsi.Subscription.Name, Namespace: hrsi.Subscription.Namespace}
	syncsource := helmreposyncsource + hostkey.String()
	pkgMap := make(map[string]bool)

	dplUnits := make([]kubesynchronizer.DplUnit, 0)

	//Loop on all packages selected by the subscription
	for packageName, chartVersions := range indexFile.Entries {
		klog.V(5).Infof("chart: %s\n%v", packageName, chartVersions)

		dpl, err := utils.CreateHelmCRDeployable(
			repoURL, packageName, chartVersions, hrsi.synchronizer.GetLocalClient(), hrsi.Channel, hrsi.Subscription)

		if err != nil {
			klog.Error("failed to create a helmrelease CR deployable, err: ", err)

			doErr = err

			continue
		}

		unit := kubesynchronizer.DplUnit{Dpl: dpl, Gvk: helmGvk}
		dplUnits = append(dplUnits, unit)

		dplkey := types.NamespacedName{
			Name:      dpl.Name,
			Namespace: dpl.Namespace,
		}
		pkgMap[dplkey.Name] = true
	}

	if len(dplUnits) > 0 || (len(dplUnits) == 0 && doErr == nil) {
		if len(dplUnits) == 0 {
			klog.Warningf("The dplUnits length is 0, this might lead to deregistration for subscription %s/%s",
				hrsi.Subscription.Namespace, hrsi.Subscription.Name)
		}

		if err := dplpro.Units(hrsi.Subscription, hrsi.synchronizer, hostkey, syncsource, pkgMap, dplUnits); err != nil {
			klog.Warningf("failed to put helm deployables to cache (will retry), err: %v", err)
			doErr = err
		}
	}

	return doErr
}
