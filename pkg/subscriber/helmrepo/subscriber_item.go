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

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"

	appv1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
	kubesynchronizer "github.com/open-cluster-management/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
)

// SubscriberItem - defines the unit of namespace subscription
type SubscriberItem struct {
	appv1.SubscriberItem

	hash         string
	stopch       chan struct{}
	syncinterval int
	synchronizer *kubesynchronizer.KubeSynchronizer
}

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
				klog.V(1).Infof("Subcription %v/%v will de deploy after %v",
					hrsi.SubscriberItem.Subscription.GetNamespace(),
					hrsi.SubscriberItem.Subscription.GetName(), nextRun)
				return
			}
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

	httpClient, err := hrsi.getHelmRepoClient()

	if err != nil {
		klog.Error(err, "Unable to create client for helm repo", repoURL)
		return
	}

	_, hash, err := hrsi.getHelmRepoIndex(httpClient, repoURL)

	if err != nil {
		klog.Error(err, "Unable to retrieve the helm repo index", repoURL)
		return
	}

	klog.V(4).Infof("Check if helmRepo %s changed with hash %s", repoURL, hash)

	if hash != hrsi.hash {
		err = hrsi.processSubscription()
	}

	if err != nil {
		klog.Error("Failed to process helm repo subscription with error:", err)
	}
}

func (hrsi *SubscriberItem) processSubscription() error {
	repoURL := hrsi.Channel.Spec.Pathname
	klog.V(4).Info("Proecssing HelmRepo:", repoURL)

	httpClient, err := hrsi.getHelmRepoClient()
	if err != nil {
		klog.Error(err, "Unable to create client for helm repo", repoURL)
		return err
	}

	indexFile, hash, err := hrsi.getHelmRepoIndex(httpClient, repoURL)
	if err != nil {
		klog.Error(err, "Unable to retrieve the helm repo index", repoURL)
		return err
	}

	err = hrsi.manageHelmCR(indexFile, repoURL)

	if err != nil {
		return err
	}

	hrsi.hash = hash

	return nil
}

func (hrsi *SubscriberItem) getHelmRepoClient() (*http.Client, error) {
	client := http.DefaultClient
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
			InsecureSkipVerify: false,
		},
	}

	if hrsi.ChannelConfigMap != nil {
		helmRepoConfigData := hrsi.ChannelConfigMap.Data
		klog.V(5).Infof("s.HelmRepoConfig.Data %v", helmRepoConfigData)

		if helmRepoConfigData["insecureSkipVerify"] != "" {
			b, err := strconv.ParseBool(helmRepoConfigData["insecureSkipVerify"])
			if err != nil {
				klog.Error(err, "Unable to parse insecureSkipVerify: ", helmRepoConfigData["insecureSkipVerify"])
				return nil, err
			}

			transport.TLSClientConfig.InsecureSkipVerify = b
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
func (hrsi *SubscriberItem) getHelmRepoIndex(client rest.HTTPClient, repoURL string) (indexFile *repo.IndexFile, hash string, err error) {
	cleanRepoURL := strings.TrimSuffix(repoURL, "/") + "/index.yaml"
	req, err := http.NewRequest(http.MethodGet, cleanRepoURL, nil)

	if err != nil {
		klog.Error(err, "Can not build request: ", cleanRepoURL)
		return nil, "", err
	}

	if hrsi.ChannelSecret != nil && hrsi.ChannelSecret.Data != nil {
		if authHeader, ok := hrsi.ChannelSecret.Data["authHeader"]; ok {
			req.Header.Set("Authorization", string(authHeader))
		} else if user, ok := hrsi.ChannelSecret.Data["user"]; ok {
			if password, ok := hrsi.ChannelSecret.Data["password"]; ok {
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

	err = utils.FilterCharts(hrsi.Subscription, indexfile)

	return indexfile, hash, err
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
	var err error

	hostkey := types.NamespacedName{Name: hrsi.Subscription.Name, Namespace: hrsi.Subscription.Namespace}
	syncsource := helmreposyncsource + hostkey.String()
	pkgMap := make(map[string]bool)

	//Loop on all packages selected by the subscription
	for packageName, chartVersions := range indexFile.Entries {
		klog.V(5).Infof("chart: %s\n%v", packageName, chartVersions)

		dpl, skip, err := utils.CreateHelmCRDeployable(
			repoURL, packageName, chartVersions, hrsi.synchronizer.LocalClient, hrsi.Channel, hrsi.Subscription)

		if err != nil {
			klog.Error("Failed to create a helmrelease CR deployable, err: ", err)
			break
		}

		if skip {
			pkgMap[dpl.GetName()] = true
			continue
		}

		err = hrsi.synchronizer.RegisterTemplate(hostkey, dpl, syncsource)
		if err != nil {
			klog.Info("eror in registering :", err)
			err = utils.SetInClusterPackageStatus(&(hrsi.Subscription.Status), dpl.GetName(), err, nil)

			if err != nil {
				klog.Info("error in setting in cluster package status :", err)
			}

			pkgMap[dpl.GetName()] = true

			return err
		}

		dplkey := types.NamespacedName{
			Name:      dpl.Name,
			Namespace: dpl.Namespace,
		}
		pkgMap[dplkey.Name] = true
	}

	if utils.ValidatePackagesInSubscriptionStatus(hrsi.synchronizer.LocalClient, hrsi.Subscription, pkgMap) != nil {
		err = hrsi.synchronizer.LocalClient.Get(context.TODO(), hostkey, hrsi.Subscription)
		if err != nil {
			klog.Error("Failed to get and subscription resource with error:", err)
		}

		err = utils.ValidatePackagesInSubscriptionStatus(hrsi.synchronizer.LocalClient, hrsi.Subscription, pkgMap)
	}

	return err
}
