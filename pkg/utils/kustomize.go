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
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	awsutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/aws"
	"sigs.k8s.io/kustomize/api/krusty"
	kustomizetypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
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
		Reorder:      krusty.ReorderOptionLegacy,
		PluginConfig: pluginConfig,
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
	if len(ov.PackageOverrides) < 1 {
		return errors.New("no PackageOverride is specified. Skipping to override kustomization")
	}

	return nil
}

func VerifyAndOverrideKustomize(packageOverrides []*appv1.Overrides, relativePath, kustomizeDir string) error {
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
					klog.Errorf("Failed to override kustomization. error: %v", err)
					return err
				}
			}
		}
	}

	return nil
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
		value, ok := ovuobj["value"].(map[string]interface{})
		if !ok {
			klog.Errorf("the value is not of type map[string]interface{}, Kustomize PackageOverride value: %v", ovuobj["value"])
			return fmt.Errorf("the value is not of type map[string]interface{}, Kustomize PackageOverride value: %v", ovuobj["value"])
		}

		override = value
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

	bs, err := os.ReadFile(kustomizeYamlFilePath) // #nosec G304 constructed filepath.Join(kustomizeDir, "kustomization.yaml")

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

	if err := os.WriteFile(kustomizeYamlFilePath, bs, 0600); err != nil {
		klog.Error("Failed to overwrite kustomize file ", " err: ", err)
		return err
	}

	return nil
}

// CategorizeObjectsForKustomization separates object keys into kustomization directories and regular files
func CategorizeObjectsForKustomization(keys []string) (map[string]bool, []string) {
	kustomizeDirs := make(map[string]bool)
	regularFiles := []string{}

	//identify kustomization directories
	for _, key := range keys {
		if strings.HasSuffix(key, "/kustomization.yaml") || strings.HasSuffix(key, "/kustomization.yml") ||
			key == "kustomization.yaml" || key == "kustomization.yml" {
			// Extract directory path
			dir := filepath.Dir(key)
			if dir != "." {
				kustomizeDirs[dir] = true
			} else {
				// Root level kustomization
				kustomizeDirs[""] = true
			}
		}
	}

	//identify all other regular files no located in the kustomization directories
	for _, key := range keys {
		isInKustomizeDir := false
		keyDir := filepath.Dir(key)

		// Check if this file is in any kustomization directory
		for kustomizeDir := range kustomizeDirs {
			if kustomizeDir == "" {
				// Root level kustomization - check if file is at root
				if keyDir == "." || keyDir == "" {
					isInKustomizeDir = true
					break
				}
			} else if strings.HasPrefix(key, kustomizeDir+"/") {
				isInKustomizeDir = true
				break
			}
		}

		if !isInKustomizeDir {
			regularFiles = append(regularFiles, key)
		}
	}

	return kustomizeDirs, regularFiles
}

// DownloadObjectToFile downloads an object from the bucket and saves it to a local file
func DownloadObjectToFile(key, filePath string, awsHandler awsutils.ObjectStore, bucket string) error {
	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Download object content
	obj, err := awsHandler.Get(bucket, key)
	if err != nil {
		return fmt.Errorf("failed to get object %s: %w", key, err)
	}

	// Write to file
	if err := os.WriteFile(filePath, obj.Content, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	return nil
}

// ProcessKustomization downloads all files in a kustomization directory and runs kustomize build
// Returns the parsed resources as unstructured objects
func ProcessKustomization(kustomizeDir string, allKeys []string, awsHandler awsutils.ObjectStore,
	bucket string, packageOverrides []*appv1.Overrides) ([]*unstructured.Unstructured, error) {
	// Create temporary directory for kustomization
	tempDir, err := os.MkdirTemp("", "objectbucket-kustomize-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create the kustomization directory structure
	var kustomizePath string
	if kustomizeDir == "" {
		kustomizePath = tempDir
	} else {
		kustomizePath = filepath.Join(tempDir, kustomizeDir)
		if err := os.MkdirAll(kustomizePath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create kustomization directory: %w", err)
		}
	}

	// Download all files belonging to this kustomization directory
	downloadedFiles := make(map[string]bool)

	for _, key := range allKeys {
		var shouldDownload bool

		var relativePath string

		if kustomizeDir == "" {
			// Root level kustomization
			keyDir := filepath.Dir(key)
			if keyDir == "." || keyDir == "" {
				shouldDownload = true
				relativePath = key
			}
		} else if strings.HasPrefix(key, kustomizeDir+"/") {
			shouldDownload = true
			relativePath = strings.TrimPrefix(key, kustomizeDir+"/")
		}

		if shouldDownload {
			if err := DownloadObjectToFile(key, filepath.Join(kustomizePath, relativePath), awsHandler, bucket); err != nil {
				return nil, fmt.Errorf("failed to download %s: %w", key, err)
			}

			downloadedFiles[key] = true
		}
	}

	// Verify kustomization file exists
	kustomizationFile := filepath.Join(kustomizePath, "kustomization.yaml")
	if _, err := os.Stat(kustomizationFile); os.IsNotExist(err) {
		kustomizationFile = filepath.Join(kustomizePath, "kustomization.yml")
		if _, err := os.Stat(kustomizationFile); os.IsNotExist(err) {
			return nil, fmt.Errorf("no kustomization.yaml or kustomization.yml found in %s", kustomizePath)
		}
	}

	// Parse kustomization file to find referenced resources and download them
	if err := downloadReferencedResources(kustomizationFile, kustomizeDir, allKeys, awsHandler, bucket, tempDir, downloadedFiles); err != nil {
		klog.Warningf("Failed to download some referenced resources: %v", err)
	}

	// Apply package overrides if specified
	if packageOverrides != nil {
		relativePath := kustomizeDir
		if relativePath == "" {
			relativePath = "."
		}

		err := VerifyAndOverrideKustomize(packageOverrides, relativePath, kustomizePath)
		if err != nil {
			return nil, fmt.Errorf("failed to override kustomization: %w", err)
		}
	}

	// Run kustomize build
	klog.Info("Running kustomize build on ", kustomizePath)
	out, err := RunKustomizeBuild(kustomizePath)

	if err != nil {
		return nil, fmt.Errorf("failed to run kustomize build: %w", err)
	}

	// Parse the kustomize output into resources
	resourceYamls := ParseYAML(out)
	resources := []*unstructured.Unstructured{}

	for _, resourceYaml := range resourceYamls {
		resourceFile := []byte(strings.Trim(resourceYaml, "\t \n"))

		if len(resourceFile) == 0 {
			continue
		}

		// Parse the resource
		template := &unstructured.Unstructured{}
		err := yaml.Unmarshal(resourceFile, template)

		if err != nil {
			klog.Errorf("Failed to unmarshal kustomized resource: %v", err)
			continue
		}

		// Skip if not a valid Kubernetes resource
		if template.GetAPIVersion() == "" || template.GetKind() == "" {
			klog.Info("Skipping non-Kubernetes resource")
			continue
		}

		resources = append(resources, template)
	}

	klog.Infof("Successfully processed kustomization %s with %d resources", kustomizeDir, len(resources))

	return resources, nil
}

// KustomizationConfig represents the structure of a kustomization.yaml file
type KustomizationConfig struct {
	Resources []string `yaml:"resources,omitempty"`
	Bases     []string `yaml:"bases,omitempty"`
}

// downloadReferencedResources parses a kustomization file and downloads any referenced resources
func downloadReferencedResources(kustomizationFile, kustomizeDir string, allKeys []string,
	awsHandler awsutils.ObjectStore, bucket, tempDir string, downloadedFiles map[string]bool) error {
	// Read the kustomization file
	content, err := os.ReadFile(kustomizationFile)
	if err != nil {
		return fmt.Errorf("failed to read kustomization file: %w", err)
	}

	// Parse the kustomization file
	var config KustomizationConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return fmt.Errorf("failed to parse kustomization file: %w", err)
	}

	// Combine resources and bases (bases is deprecated but still supported)
	allResources := append(config.Resources, config.Bases...)

	for _, resource := range allResources {
		if resource == "" {
			continue
		}

		// Resolve the resource path relative to the kustomization directory
		var resourcePath string
		if kustomizeDir == "" {
			resourcePath = resource
		} else {
			// Handle relative paths like "../../base"
			resourcePath = filepath.Join(kustomizeDir, resource)
			// Clean the path to resolve .. and . components
			resourcePath = filepath.Clean(resourcePath)
		}

		klog.Infof("Processing referenced resource: %s -> %s", resource, resourcePath)

		// First try to download from the existing allKeys
		err := downloadResourcePath(resourcePath, allKeys, awsHandler, bucket, tempDir, downloadedFiles)
		if err != nil {
			klog.Warningf("Resource path %s not found in initial key list, attempting direct bucket listing", resourcePath)

			// If not found in allKeys, try to list and download directly from the bucket
			if err := downloadResourcePathDirect(resourcePath, awsHandler, bucket, tempDir, downloadedFiles); err != nil {
				klog.Warningf("Failed to download resource path %s: %v", resourcePath, err)
			}
		}
	}

	return nil
}

// downloadResourcePath downloads all files under a given resource path
func downloadResourcePath(resourcePath string, allKeys []string, awsHandler awsutils.ObjectStore,
	bucket, tempDir string, downloadedFiles map[string]bool) error {
	downloaded := false

	for _, key := range allKeys {
		// Check if this key belongs to the resource path
		if strings.HasPrefix(key, resourcePath+"/") || key == resourcePath {
			if downloadedFiles[key] {
				downloaded = true // Already downloaded
				continue
			}

			// Determine the local file path
			localPath := filepath.Join(tempDir, key)

			klog.Infof("Downloading referenced resource: %s -> %s", key, localPath)

			if err := DownloadObjectToFile(key, localPath, awsHandler, bucket); err != nil {
				return fmt.Errorf("failed to download %s: %w", key, err)
			}

			downloadedFiles[key] = true
			downloaded = true
		}
	}

	if !downloaded {
		return fmt.Errorf("no files found for resource path: %s", resourcePath)
	}

	return nil
}

// downloadResourcePathDirect lists and downloads files directly from the bucket for a given resource path
func downloadResourcePathDirect(resourcePath string, awsHandler awsutils.ObjectStore,
	bucket, tempDir string, downloadedFiles map[string]bool) error {
	klog.Infof("Attempting direct bucket listing for resource path: %s", resourcePath)

	// List objects in the bucket with the resource path as prefix
	keys, err := awsHandler.List(bucket, &resourcePath)
	if err != nil {
		return fmt.Errorf("failed to list objects with prefix %s: %w", resourcePath, err)
	}

	if len(keys) == 0 {
		return fmt.Errorf("no files found for resource path: %s", resourcePath)
	}

	klog.Infof("Found %d files for resource path %s: %v", len(keys), resourcePath, keys)

	downloaded := false

	for _, key := range keys {
		if downloadedFiles[key] {
			continue // Already downloaded
		}

		// Determine the local file path
		localPath := filepath.Join(tempDir, key)

		klog.Infof("Downloading referenced resource: %s -> %s", key, localPath)

		if err := DownloadObjectToFile(key, localPath, awsHandler, bucket); err != nil {
			return fmt.Errorf("failed to download %s: %w", key, err)
		}

		downloadedFiles[key] = true
		downloaded = true
	}

	if !downloaded {
		return fmt.Errorf("no new files downloaded for resource path: %s", resourcePath)
	}

	return nil
}
