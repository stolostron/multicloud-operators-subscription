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
	"crypto/sha1"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"
	dplutils "github.com/IBM/multicloud-operators-deployable/pkg/utils"
	releasev1alpha1 "github.com/IBM/multicloud-operators-subscription-release/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	kubesynchronizer "github.com/IBM/multicloud-operators-subscription/pkg/synchronizer/kubernetes"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

const (
	//Max is 52 chars but as helm add behind the scene extension -delete-registrations for some objects
	//The new limit is 31 chars
	maxNameLength = 52 - len("-delete-registrations")
	randomLength  = 5
	//minus 1 because we add a dash
	maxGeneratedNameLength = maxNameLength - randomLength - 1
)

// SubscriberItem - defines the unit of namespace subscription
type SubscriberItem struct {
	appv1alpha1.SubscriberItem

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
	repoURL := hrsi.Channel.Spec.PathName

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
	repoURL := hrsi.Channel.Spec.PathName
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
	cleanRepoURL := strings.TrimSuffix(repoURL, "/")
	req, err := http.NewRequest(http.MethodGet, cleanRepoURL+"/index.yaml", nil)

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

	err = hrsi.filterCharts(indexfile)

	return indexfile, hash, err
}

//loadIndex loads data into a repo.IndexFile
func loadIndex(data []byte) (*repo.IndexFile, error) {
	i := &repo.IndexFile{}
	if err := yaml.Unmarshal(data, i); err != nil {
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
	h := sha1.New()
	_, err := h.Write(b)

	if err != nil {
		klog.Error("Failed to hash key with error:", err)
	}

	return string(h.Sum(nil))
}

//filterCharts filters the indexFile by name, tillerVersion, version, digest
func (hrsi *SubscriberItem) filterCharts(indexFile *repo.IndexFile) error {
	//Removes all entries from the indexFile with non matching name
	err := hrsi.removeNoMatchingName(indexFile)
	if err != nil {
		klog.Error(err)
		return err
	}
	//Removes non matching version, tillerVersion, digest
	hrsi.filterOnVersion(indexFile)
	//Keep only the lastest version if multiple remains after filtering.
	err = takeLatestVersion(indexFile)
	if err != nil {
		klog.Error("Failed to filter on version with error: ", err)
	}

	return err
}

//removeNoMatchingName Deletes entries that the name doesn't match the name provided in the subscription
func (hrsi *SubscriberItem) removeNoMatchingName(indexFile *repo.IndexFile) error {
	if hrsi.Subscription != nil {
		if hrsi.Subscription.Spec.Package != "" {
			keys := make([]string, 0)
			for k := range indexFile.Entries {
				keys = append(keys, k)
			}

			for _, k := range keys {
				if k != hrsi.Subscription.Spec.Package {
					delete(indexFile.Entries, k)
				}
			}
		} else {
			return fmt.Errorf("subsciption.spec.name is missing for subscription: %s/%s", hrsi.Subscription.Namespace, hrsi.Subscription.Name)
		}
	}

	klog.V(5).Info("After name matching:", indexFile)

	return nil
}

//checkKeywords Checks if the charts has at least 1 keyword from the packageFilter.Keywords array
func (hrsi *SubscriberItem) checkKeywords(chartVersion *repo.ChartVersion) bool {
	var labelSelector *metav1.LabelSelector
	if hrsi.Subscription.Spec.PackageFilter != nil {
		labelSelector = hrsi.Subscription.Spec.PackageFilter.LabelSelector
	}

	return utils.KeywordsChecker(labelSelector, chartVersion.Keywords)
}

//filterOnVersion filters the indexFile with the version, tillerVersion and Digest provided in the subscription
//The version provided in the subscription can be an expression like ">=1.2.3" (see https://github.com/blang/semver)
//The tillerVersion and the digest provided in the subscription must be literals.
func (hrsi *SubscriberItem) filterOnVersion(indexFile *repo.IndexFile) {
	keys := make([]string, 0)
	for k := range indexFile.Entries {
		keys = append(keys, k)
	}

	for _, k := range keys {
		chartVersions := indexFile.Entries[k]
		newChartVersions := make([]*repo.ChartVersion, 0)

		for index, chartVersion := range chartVersions {
			if hrsi.checkKeywords(chartVersion) && hrsi.checkDigest(chartVersion) && hrsi.checkTillerVersion(chartVersion) && hrsi.checkVersion(chartVersion) {
				newChartVersions = append(newChartVersions, chartVersions[index])
			}
		}

		if len(newChartVersions) > 0 {
			indexFile.Entries[k] = newChartVersions
		} else {
			delete(indexFile.Entries, k)
		}
	}

	klog.V(5).Info("After version matching:", indexFile)
}

//checkDigest Checks if the digest matches
func (hrsi *SubscriberItem) checkDigest(chartVersion *repo.ChartVersion) bool {
	if hrsi.Subscription != nil {
		if hrsi.Subscription.Spec.PackageFilter != nil {
			if hrsi.Subscription.Spec.PackageFilter.Annotations != nil {
				if filterDigest, ok := hrsi.Subscription.Spec.PackageFilter.Annotations["digest"]; ok {
					return filterDigest == chartVersion.Digest
				}
			}
		}
	}

	klog.V(5).Info("Digest check passed for:", chartVersion)

	return true
}

//checkTillerVersion Checks if the TillerVersion matches
func (hrsi *SubscriberItem) checkTillerVersion(chartVersion *repo.ChartVersion) bool {
	if hrsi.Subscription != nil {
		if hrsi.Subscription.Spec.PackageFilter != nil {
			if hrsi.Subscription.Spec.PackageFilter.Annotations != nil {
				if filterTillerVersion, ok := hrsi.Subscription.Spec.PackageFilter.Annotations["tillerVersion"]; ok {
					tillerVersion := chartVersion.GetTillerVersion()
					if tillerVersion != "" {
						tillerVersionVersion, err := semver.ParseRange(tillerVersion)
						if err != nil {
							klog.Errorf("Error while parsing tillerVersion: %s of %s Error: %s", tillerVersion, chartVersion.GetName(), err.Error())
							return false
						}

						filterTillerVersion, err := semver.Parse(filterTillerVersion)

						if err != nil {
							klog.Error(err)
							return false
						}

						return tillerVersionVersion(filterTillerVersion)
					}
				}
			}
		}
	}

	klog.V(5).Info("Tiller check passed for:", chartVersion)

	return true
}

//checkVersion checks if the version matches
func (hrsi *SubscriberItem) checkVersion(chartVersion *repo.ChartVersion) bool {
	if hrsi.Subscription != nil {
		if hrsi.Subscription.Spec.PackageFilter != nil {
			if hrsi.Subscription.Spec.PackageFilter.Version != "" {
				version := chartVersion.GetVersion()
				versionVersion, err := semver.Parse(version)

				if err != nil {
					klog.V(3).Info("Skipping error in parsing version, taking it as not match. The error is:", err)
					return false
				}

				filterVersion, err := semver.ParseRange(hrsi.Subscription.Spec.PackageFilter.Version)

				if err != nil {
					klog.Error(err)
					return false
				}

				return filterVersion(versionVersion)
			}
		}
	}

	klog.V(5).Info("Version check passed for:", chartVersion)

	return true
}

//takeLatestVersion if the indexFile contains multiple versions for a given chart, then
//only the latest is kept.
func takeLatestVersion(indexFile *repo.IndexFile) (err error) {
	indexFile.SortEntries()

	for k := range indexFile.Entries {
		//Get return the latest version when version is empty but
		//there is a bug in the masterminds semver used by helm
		// "*" constraint is not working properly
		// "*" is equivalent to ">=0.0.0"
		chartVersion, err := indexFile.Get(k, ">=0.0.0")
		if err != nil {
			klog.Error(err)
			return err
		}

		indexFile.Entries[k] = []*repo.ChartVersion{chartVersion}
	}

	return nil
}

func (hrsi *SubscriberItem) getOverrides(packageName string) dplv1alpha1.Overrides {
	dploverrides := dplv1alpha1.Overrides{}

	for _, overrides := range hrsi.Subscription.Spec.PackageOverrides {
		if overrides.PackageName == packageName {
			klog.Infof("Overrides for package %s found", packageName)
			dploverrides.ClusterName = packageName
			dploverrides.ClusterOverrides = make([]dplv1alpha1.ClusterOverride, 0)

			for _, override := range overrides.PackageOverrides {
				clusterOverride := dplv1alpha1.ClusterOverride{
					RawExtension: runtime.RawExtension{
						Raw: override.RawExtension.Raw,
					},
				}
				dploverrides.ClusterOverrides = append(dploverrides.ClusterOverrides, clusterOverride)
			}

			return dploverrides
		}
	}

	return dploverrides
}

func (hrsi *SubscriberItem) manageHelmCR(indexFile *repo.IndexFile, repoURL string) error {
	var err error

	hostkey := types.NamespacedName{Name: hrsi.Subscription.Name, Namespace: hrsi.Subscription.Namespace}
	syncsource := helmreposyncsource + hostkey.String()
	pkgMap := make(map[string]bool)

	//Loop on all packages selected by the subscription
	for packageName, chartVersions := range indexFile.Entries {
		klog.V(5).Infof("chart: %s\n%v", packageName, chartVersions)

		helmReleaseNewName := packageName + "-" + hrsi.Subscription.Name + "-" + hrsi.Subscription.Namespace

		helmRelease := &releasev1alpha1.HelmRelease{}
		//Create a new helrmReleases
		//Try to retrieve the releases to check if we have to reuse the releaseName or calculate one.

		for i := range chartVersions[0].URLs {
			parsedURL, err := url.Parse(chartVersions[0].URLs[i])

			if err != nil {
				klog.Error("Failed to parse url with error:", err)
				return err
			}

			if parsedURL.Scheme == "local" {
				//make sure there is one and only one slash
				repoURL = strings.TrimSuffix(repoURL, "/") + "/"
				chartVersions[0].URLs[i] = strings.Replace(chartVersions[0].URLs[i], "local://", repoURL, -1)
			}
		}
		//Check if Update or Create
		err = hrsi.synchronizer.LocalClient.Get(context.TODO(),
			types.NamespacedName{Name: helmReleaseNewName, Namespace: hrsi.Subscription.Namespace}, helmRelease)

		if err != nil {
			if errors.IsNotFound(err) {
				klog.V(2).Infof("Create helmRelease %s", helmReleaseNewName)

				helmRelease = &releasev1alpha1.HelmRelease{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "app.ibm.com/v1alpha1",
						Kind:       "HelmRelease",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      helmReleaseNewName,
						Namespace: hrsi.Subscription.Namespace,
						OwnerReferences: []metav1.OwnerReference{{
							APIVersion: hrsi.Subscription.APIVersion,
							Kind:       hrsi.Subscription.Kind,
							Name:       hrsi.Subscription.Name,
							UID:        hrsi.Subscription.UID,
						}},
					},
					Spec: releasev1alpha1.HelmReleaseSpec{
						Source: &releasev1alpha1.Source{
							SourceType: releasev1alpha1.HelmRepoSourceType,
							HelmRepo: &releasev1alpha1.HelmRepo{
								Urls: chartVersions[0].URLs,
							},
						},
						ConfigMapRef: hrsi.Channel.Spec.ConfigMapRef,
						SecretRef:    hrsi.Channel.Spec.SecretRef,
						ChartName:    packageName,
						ReleaseName:  getReleaseName(helmReleaseNewName),
						Version:      chartVersions[0].GetVersion(),
					},
				}
			} else {
				klog.Error("Error in getting existing helm release", err)
				return err
			}
		} else {
			// set kind and apiversion, coz it is not in the resource get from k8s
			helmRelease.APIVersion = "app.ibm.com/v1alpha1"
			helmRelease.Kind = "HelmRelease"
			klog.V(2).Infof("Update helmRelease spec %s", helmRelease.Name)
			releaseName := helmRelease.Spec.ReleaseName
			helmRelease.Spec = releasev1alpha1.HelmReleaseSpec{
				Source: &releasev1alpha1.Source{
					SourceType: releasev1alpha1.HelmRepoSourceType,
					HelmRepo: &releasev1alpha1.HelmRepo{
						Urls: chartVersions[0].URLs,
					},
				},
				ConfigMapRef: hrsi.Channel.Spec.ConfigMapRef,
				SecretRef:    hrsi.Channel.Spec.SecretRef,
				ChartName:    packageName,
				ReleaseName:  releaseName,
				Version:      chartVersions[0].GetVersion(),
			}
		}

		err := hrsi.override(helmRelease)

		if err != nil {
			klog.Error("Failed to override ", helmRelease.Name, " err:", err)
			return err
		}

		dpl := &dplv1alpha1.Deployable{}
		if hrsi.Channel == nil {
			dpl.Name = hrsi.Subscription.Name + "-" + packageName + "-" + chartVersions[0].GetVersion()
			dpl.Namespace = hrsi.Subscription.Namespace
		} else {
			dpl.Name = hrsi.Channel.Name + "-" + packageName + "-" + chartVersions[0].GetVersion()
			dpl.Namespace = hrsi.Channel.Namespace
		}

		dpl.Spec.Template = &runtime.RawExtension{}
		dpl.Spec.Template.Raw, err = json.Marshal(helmRelease)

		if err != nil {
			klog.Error("Failed to mashall helm release", helmRelease)
			return err
		}

		dplanno := make(map[string]string)
		dplanno[dplv1alpha1.AnnotationLocal] = "true"
		dpl.SetAnnotations(dplanno)

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

func getReleaseName(base string) string {
	if len(base) > maxNameLength {
		//minus 1 because adding "-"
		base = base[:maxGeneratedNameLength]
		return fmt.Sprintf("%s-%s", base, utilrand.String(randomLength))
	}

	return base
}

func (hrsi *SubscriberItem) override(helmRelease *releasev1alpha1.HelmRelease) error {
	//Overrides with the values provided in the subscription for that package
	overrides := hrsi.getOverrides(helmRelease.Spec.ChartName)
	data, err := yaml.Marshal(helmRelease)

	if err != nil {
		klog.Error("Failed to mashall ", helmRelease.Name, " err:", err)
		return err
	}

	template := &unstructured.Unstructured{}
	err = yaml.Unmarshal(data, template)

	if err != nil {
		klog.Warning("Processing local deployable with error template:", helmRelease, err)
	}

	template, err = dplutils.OverrideTemplate(template, overrides.ClusterOverrides)

	if err != nil {
		klog.Error("Failed to apply override for instance: ")
		return err
	}

	data, err = yaml.Marshal(template)

	if err != nil {
		klog.Error("Failed to mashall ", helmRelease.Name, " err:", err)
		return err
	}

	err = yaml.Unmarshal(data, helmRelease)

	if err != nil {
		klog.Error("Failed to unmashall ", helmRelease.Name, " err:", err)
		return err
	}

	return nil
}
