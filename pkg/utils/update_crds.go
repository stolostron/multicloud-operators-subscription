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

package utils

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
)

var (
	DefaultRepos = map[string]map[string]string{
		"ansiblejob": {
			"url":     "https://raw.githubusercontent.com/open-cluster-management/ansiblejob-go-lib/master/config/crd/bases/tower.ansible.com_ansiblejobs.yaml", // nolint:lll // fix url
			"crdName": "tower.ansible.com_ansiblejobs_crd.yaml",
		},
		"channel": {
			"url":     "https://raw.githubusercontent.com/open-cluster-management/multicloud-operators-channel/master/deploy/crds/apps.open-cluster-management.io_channels_crd.yaml", // nolint:lll // fix url
			"crdName": "apps.open-cluster-management.io_channel_crd.yaml",
		},
		"subscription": {
			"url":     "https://raw.githubusercontent.com/open-cluster-management/multicloud-operators-subscription/master/deploy/crds/apps.open-cluster-management.io_subscriptions.yaml", // nolint:lll // fix url
			"crdName": "apps.open-cluster-management.io_subscriptions_crd.yaml",
		},
		"deployable": {
			"url":     "https://raw.githubusercontent.com/open-cluster-management/multicloud-operators-deployable/master/deploy/crds/apps.open-cluster-management.io_deployables_crd.yaml", // nolint:lll // fix url
			"crdName": "apps.open-cluster-management.io_deployable_crd.yaml",
		},
		"placementrule": {
			"url":     "https://raw.githubusercontent.com/open-cluster-management/multicloud-operators-placementrule/master/deploy/crds/apps.open-cluster-management.io_placementrules_crd.yaml", // nolint:lll // fix url
			"crdName": "apps.open-cluster-management.io_placementrules_crd.yaml",
		},
		"helmrelease": {
			"url":     "https://raw.githubusercontent.com/open-cluster-management/multicloud-operators-subscription-release/master/deploy/crds/apps.open-cluster-management.io_helmreleases_crd.yaml", // nolint:lll // fix url
			"crdName": "apps.open-cluster-management.io_helmrelease_crd.yaml",
		},
		"managedcluster": {
			"url":     "https://raw.githubusercontent.com/open-cluster-management/api/master/cluster/v1/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml", // nolint:lll // fix url
			"crdName": "clusters.open-cluster-management.io_managedclusters.crd.yaml",
		},
	}
)

func UpdateCRDs(p string, tRepo []string, logger logr.Logger) error {
	if len(p) == 0 || len(tRepo) == 0 {
		return nil
	}

	if err := createOrCleanUpDir(p); err != nil {
		logger.Error(err, "failed to create directory")
		return err
	}

	if err := download(tRepo, p, logger); err != nil {
		return err
	}

	return nil
}

func createOrCleanUpDir(p string) error {
	_ = os.RemoveAll(p)

	_, err := os.Stat(p)

	if os.IsNotExist(err) {
		return os.Mkdir(p, 0700)
	}

	return nil
}

func download(repos []string, out string, logger logr.Logger) error {
	for _, repo := range repos {
		u, ok := DefaultRepos[repo]
		if !ok {
			return fmt.Errorf("the %v is not in the default list", repo)
		}

		resp, err := http.Get(u["url"])
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// Write the body to file

		fp := fmt.Sprintf("%s/%s", out, u["crdName"])
		fmt.Println(fp)

		fh, err := os.OpenFile(filepath.Clean(fp), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}

		_, err = io.Copy(fh, resp.Body)
		if err != nil {
			return err
		}
	}

	logger.Info("CRD updated")

	return nil
}
