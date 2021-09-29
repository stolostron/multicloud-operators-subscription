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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"
	kustomizetypes "sigs.k8s.io/kustomize/api/types"
)

// RunKustomizeBuild runs kustomize build and returns the build output
func RunKustomizeBuild(kustomizeDir string) ([]byte, error) {
	fSys := filesys.MakeFsOnDisk()

	// Allow external plugins when executing Kustomize. This is required to support the policy
	// generator. This builtin plugin loading option is the default value and is recommended
	// for production use-cases.
	pluginConfig := kustomizetypes.MakePluginConfig(
		kustomizetypes.PluginRestrictionsNone,
		kustomizetypes.BploUseStaticallyLinked,
	)
	options := &krusty.Options{
		DoLegacyResourceSort: true,
		PluginConfig:         pluginConfig,
	}

	k := krusty.MakeKustomizer(options)
	mapOut, err := k.Run(fSys, kustomizeDir)

	if err != nil {
		return nil, err
	}

	byteOut, err := mapOut.AsYaml()
	if err != nil {
		return nil, err
	}

	return byteOut, nil
}

func CheckPackageOverride(ov *appv1.Overrides) error {
	if ov.PackageOverrides == nil || len(ov.PackageOverrides) < 1 {
		return errors.New("no PackageOverride is specified. Skipping to override kustomization")
	}

	return nil
}

func VerifyAndOverrideKustomize(packageOverrides []*appv1.Overrides, relativePath, kustomizeDir string) {
	for _, ov := range packageOverrides {
		ovKustomizeDir := strings.Split(ov.PackageName, "kustomization")[0]

		//If the full kustomization.yaml path is specified but different than the current kustomize dir, egnore
		if !strings.EqualFold(ovKustomizeDir, relativePath) && !strings.EqualFold(ovKustomizeDir, "") {
			continue
		} else {
			err := CheckPackageOverride(ov)

			if err != nil {
				klog.Error("Failed to apply kustomization, error: ", err.Error())
			} else {
				klog.Info("Overriding kustomization ", kustomizeDir)

				pov := ov.PackageOverrides[0] // there is only one override for kustomization.yaml
				err := OverrideKustomize(pov, kustomizeDir)

				if err != nil {
					klog.Error("Failed to override kustomization.")
					break
				}
			}
		}
	}
}

func OverrideKustomize(pov appv1.PackageOverride, kustomizeDir string) error {
	kustomizeOverride := appv1.ClusterOverride(pov)
	ovuobj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&kustomizeOverride)

	klog.Info("Kustomize parse : ", ovuobj, "with err:", err, " path: ", ovuobj["path"], " value:", ovuobj["value"])

	if err != nil {
		klog.Error("Kustomize parse error: ", ovuobj, "with err:", err, " path: ", ovuobj["path"], " value:", ovuobj["value"])
		return err
	}

	if ovuobj["value"] == nil {
		klog.Error("Kustomize PackageOverride has no value")
		return nil
	}

	str := fmt.Sprintf("%v", ovuobj["value"])

	var override map[string]interface{}

	if strings.EqualFold(reflect.ValueOf(ovuobj["value"]).Kind().String(), "string") {
		if err := yaml.Unmarshal([]byte(str), &override); err != nil {
			klog.Error("Failed to override kustomize with error: ", err)
			return err
		}
	} else {
		override = ovuobj["value"].(map[string]interface{})
	}

	kustomizeYamlFilePath := filepath.Join(kustomizeDir, "kustomization.yaml")

	if _, err := os.Stat(kustomizeYamlFilePath); os.IsNotExist(err) {
		kustomizeYamlFilePath = filepath.Join(kustomizeDir, "kustomization.yml")
		if _, err := os.Stat(kustomizeYamlFilePath); os.IsNotExist(err) {
			klog.Error("Kustomization file not found in ", kustomizeDir)
			return err
		}
	}

	err = mergeKustomization(kustomizeYamlFilePath, override)
	if err != nil {
		return err
	}

	return nil
}

func mergeKustomization(kustomizeYamlFilePath string, override map[string]interface{}) error {
	var master map[string]interface{}

	bs, err := ioutil.ReadFile(kustomizeYamlFilePath) // #nosec G304 constructed filepath.Join(kustomizeDir, "kustomization.yaml")

	if err != nil {
		klog.Error("Failed to read file ", kustomizeYamlFilePath, " err: ", err)
		return err
	}

	if err := yaml.Unmarshal(bs, &master); err != nil {
		klog.Error("Failed to unmarshal kustomize file ", " err: ", err)
		return err
	}

	for k, v := range override {
		master[k] = v
	}

	bs, err = yaml.Marshal(master)

	if err != nil {
		klog.Error("Failed to marshal kustomize file ", " err: ", err)
		return err
	}

	if err := ioutil.WriteFile(kustomizeYamlFilePath, bs, 0600); err != nil {
		klog.Error("Failed to overwrite kustomize file ", " err: ", err)
		return err
	}

	return nil
}
