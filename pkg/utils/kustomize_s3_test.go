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
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"

	appv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	awsutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/aws"
)

func TestCategorizeObjectsForKustomizationAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Test case 1: Root level kustomization
	keys1 := []string{
		"kustomization.yaml",
		"configmap.yaml",
		"secret.yaml",
		"deployment.yaml",
	}

	kustomizeDirs1, regularFiles1 := CategorizeObjectsForKustomization(keys1)

	g.Expect(len(kustomizeDirs1)).To(gomega.Equal(1))
	g.Expect(kustomizeDirs1[""]).To(gomega.BeTrue()) // Root level kustomization
	g.Expect(len(regularFiles1)).To(gomega.Equal(0)) // All files are in kustomization directory

	// Test case 2: Subdirectory kustomization
	keys2 := []string{
		"app/kustomization.yaml",
		"app/configmap.yaml",
		"app/secret.yaml",
		"regular.yaml",
		"another-regular.yaml",
	}

	kustomizeDirs2, regularFiles2 := CategorizeObjectsForKustomization(keys2)

	g.Expect(len(kustomizeDirs2)).To(gomega.Equal(1))
	g.Expect(kustomizeDirs2["app"]).To(gomega.BeTrue())
	g.Expect(len(regularFiles2)).To(gomega.Equal(2))
	g.Expect(regularFiles2).To(gomega.ContainElement("regular.yaml"))
	g.Expect(regularFiles2).To(gomega.ContainElement("another-regular.yaml"))

	// Test case 3: Multiple kustomization directories
	keys3 := []string{
		"base/kustomization.yaml",
		"base/configmap.yaml",
		"overlay/kustomization.yml", // Test .yml extension
		"overlay/patch.yaml",
		"standalone.yaml",
	}

	kustomizeDirs3, regularFiles3 := CategorizeObjectsForKustomization(keys3)

	g.Expect(len(kustomizeDirs3)).To(gomega.Equal(2))
	g.Expect(kustomizeDirs3["base"]).To(gomega.BeTrue())
	g.Expect(kustomizeDirs3["overlay"]).To(gomega.BeTrue())
	g.Expect(len(regularFiles3)).To(gomega.Equal(1))
	g.Expect(regularFiles3).To(gomega.ContainElement("standalone.yaml"))

	// Test case 4: No kustomization files
	keys4 := []string{
		"configmap.yaml",
		"secret.yaml",
		"deployment.yaml",
	}

	kustomizeDirs4, regularFiles4 := CategorizeObjectsForKustomization(keys4)

	g.Expect(len(kustomizeDirs4)).To(gomega.Equal(0))
	g.Expect(len(regularFiles4)).To(gomega.Equal(3))

	// Test case 5: Nested directories
	keys5 := []string{
		"apps/frontend/kustomization.yaml",
		"apps/frontend/deployment.yaml",
		"apps/backend/kustomization.yaml",
		"apps/backend/service.yaml",
		"config/configmap.yaml",
	}

	kustomizeDirs5, regularFiles5 := CategorizeObjectsForKustomization(keys5)

	g.Expect(len(kustomizeDirs5)).To(gomega.Equal(2))
	g.Expect(kustomizeDirs5["apps/frontend"]).To(gomega.BeTrue())
	g.Expect(kustomizeDirs5["apps/backend"]).To(gomega.BeTrue())
	g.Expect(len(regularFiles5)).To(gomega.Equal(1))
	g.Expect(regularFiles5).To(gomega.ContainElement("config/configmap.yaml"))
}

func TestDownloadObjectToFileAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup fake S3 server
	server, awsHandler, _ := SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err := awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create test content
	testContent := "test file content"
	testObj := awsutils.DeployableObject{
		Name:    "test-file.yaml",
		Content: []byte(testContent),
	}
	err = awsHandler.Put(bucket, testObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "download-test-")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer os.RemoveAll(tempDir)

	// Test downloading file
	filePath := filepath.Join(tempDir, "subdir", "downloaded-file.yaml")
	err = DownloadObjectToFile("test-file.yaml", filePath, awsHandler, bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify file was created and has correct content
	content, err := os.ReadFile(filePath)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(string(content)).To(gomega.Equal(testContent))

	// Test error case - non-existent object
	err = DownloadObjectToFile("non-existent.yaml", filepath.Join(tempDir, "non-existent.yaml"), awsHandler, bucket)
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestProcessKustomizationAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup fake S3 server
	server, awsHandler, _ := SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err := awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test data - kustomization.yaml
	kustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
- secret.yaml
`

	// Test data - configmap.yaml
	configMapYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: test-ns
data:
  key: value
`

	// Test data - secret.yaml
	secretYaml := `
apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: test-ns
data:
  key: dmFsdWU=
`

	// Upload test files to fake S3
	kustomizeObj := awsutils.DeployableObject{
		Name:    "kustomization.yaml",
		Content: []byte(kustomizationYaml),
	}
	err = awsHandler.Put(bucket, kustomizeObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	configMapObj := awsutils.DeployableObject{
		Name:    "configmap.yaml",
		Content: []byte(configMapYaml),
	}
	err = awsHandler.Put(bucket, configMapObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	secretObj := awsutils.DeployableObject{
		Name:    "secret.yaml",
		Content: []byte(secretYaml),
	}
	err = awsHandler.Put(bucket, secretObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test ProcessKustomization
	allKeys := []string{"kustomization.yaml", "configmap.yaml", "secret.yaml"}
	kustomizeDir := ""

	resources, err := ProcessKustomization(kustomizeDir, allKeys, awsHandler, bucket, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(2)) // Should have ConfigMap and Secret

	// Verify resources
	resourceKinds := make(map[string]bool)
	for _, resource := range resources {
		resourceKinds[resource.GetKind()] = true

		g.Expect(resource.GetAPIVersion()).NotTo(gomega.BeEmpty())
		g.Expect(resource.GetName()).NotTo(gomega.BeEmpty())
	}

	g.Expect(resourceKinds["ConfigMap"]).To(gomega.BeTrue())
	g.Expect(resourceKinds["Secret"]).To(gomega.BeTrue())
}

func TestProcessKustomizationWithSubdirectoryAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup fake S3 server
	server, awsHandler, _ := SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err := awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test data in subdirectory
	kustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
`

	configMapYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: app-ns
data:
  app: myapp
`

	// Upload test files to subdirectory
	kustomizeObj := awsutils.DeployableObject{
		Name:    "app/kustomization.yaml",
		Content: []byte(kustomizationYaml),
	}
	err = awsHandler.Put(bucket, kustomizeObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	configMapObj := awsutils.DeployableObject{
		Name:    "app/configmap.yaml",
		Content: []byte(configMapYaml),
	}
	err = awsHandler.Put(bucket, configMapObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test ProcessKustomization with subdirectory
	allKeys := []string{"app/kustomization.yaml", "app/configmap.yaml"}
	kustomizeDir := "app"

	resources, err := ProcessKustomization(kustomizeDir, allKeys, awsHandler, bucket, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(1))

	// Verify resource
	resource := resources[0]
	g.Expect(resource.GetKind()).To(gomega.Equal("ConfigMap"))
	g.Expect(resource.GetName()).To(gomega.Equal("app-config"))
}

func TestProcessKustomizationWithPackageOverridesAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup fake S3 server
	server, awsHandler, _ := SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err := awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test data - kustomization.yaml
	kustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
`

	configMapYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: test-ns
data:
  key: value
`

	// Upload test files
	kustomizeObj := awsutils.DeployableObject{
		Name:    "kustomization.yaml",
		Content: []byte(kustomizationYaml),
	}
	err = awsHandler.Put(bucket, kustomizeObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	configMapObj := awsutils.DeployableObject{
		Name:    "configmap.yaml",
		Content: []byte(configMapYaml),
	}
	err = awsHandler.Put(bucket, configMapObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create package overrides
	packageOverrides := []*appv1.Overrides{
		{
			PackageName: "kustomization",
			PackageOverrides: []appv1.PackageOverride{
				{},
			},
		},
	}

	allKeys := []string{"kustomization.yaml", "configmap.yaml"}
	kustomizeDir := ""

	// Test ProcessKustomization with package overrides
	resources, err := ProcessKustomization(kustomizeDir, allKeys, awsHandler, bucket, packageOverrides)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(1))

	// Verify resource
	resource := resources[0]
	g.Expect(resource.GetKind()).To(gomega.Equal("ConfigMap"))
	g.Expect(resource.GetName()).To(gomega.Equal("test-config"))
}

func TestProcessKustomizationWithReferencedResourcesAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup fake S3 server
	server, awsHandler, _ := SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err := awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test data - kustomization.yaml with external reference
	kustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- configmap.yaml
- ../base/common.yaml
`

	configMapYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: overlay-config
  namespace: test-ns
data:
  key: overlay-value
`

	baseCommonYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: base-config
  namespace: test-ns
data:
  key: base-value
`

	// Upload test files
	kustomizeObj := awsutils.DeployableObject{
		Name:    "overlay/kustomization.yaml",
		Content: []byte(kustomizationYaml),
	}
	err = awsHandler.Put(bucket, kustomizeObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	configMapObj := awsutils.DeployableObject{
		Name:    "overlay/configmap.yaml",
		Content: []byte(configMapYaml),
	}
	err = awsHandler.Put(bucket, configMapObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	baseObj := awsutils.DeployableObject{
		Name:    "base/common.yaml",
		Content: []byte(baseCommonYaml),
	}
	err = awsHandler.Put(bucket, baseObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	allKeys := []string{"overlay/kustomization.yaml", "overlay/configmap.yaml", "base/common.yaml"}
	kustomizeDir := "overlay"

	// Test ProcessKustomization with referenced resources
	resources, err := ProcessKustomization(kustomizeDir, allKeys, awsHandler, bucket, nil)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(len(resources)).To(gomega.Equal(2)) // Should have both ConfigMaps

	// Verify resources
	resourceNames := make(map[string]bool)

	for _, resource := range resources {
		g.Expect(resource.GetKind()).To(gomega.Equal("ConfigMap"))

		resourceNames[resource.GetName()] = true
	}

	g.Expect(resourceNames["overlay-config"]).To(gomega.BeTrue())
	g.Expect(resourceNames["base-config"]).To(gomega.BeTrue())
}

func TestProcessKustomizationErrorAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup fake S3 server
	server, awsHandler, _ := SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err := awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Test case: Invalid kustomization content
	kustomizeDir := ""

	invalidKustomizationYaml := `
invalid yaml content [
`

	invalidObj := awsutils.DeployableObject{
		Name:    "kustomization.yaml",
		Content: []byte(invalidKustomizationYaml),
	}
	err = awsHandler.Put(bucket, invalidObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	allKeys2 := []string{"kustomization.yaml"}

	_, err = ProcessKustomization(kustomizeDir, allKeys2, awsHandler, bucket, nil)
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestDownloadReferencedResourcesAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup fake S3 server
	server, awsHandler, _ := SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err := awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "kustomize-test-")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer os.RemoveAll(tempDir)

	// Create kustomization file with references
	kustomizationContent := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../base/common.yaml
- config/app.yaml
bases:
- ../shared
`

	kustomizationFile := filepath.Join(tempDir, "kustomization.yaml")
	err = os.WriteFile(kustomizationFile, []byte(kustomizationContent), 0644)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Upload referenced files to S3
	baseCommonYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: base-common
data:
  key: base-value
`

	configAppYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  app: myapp
`

	sharedKustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- shared.yaml
`

	sharedYaml := `
apiVersion: v1
kind: Secret
metadata:
  name: shared-secret
data:
  key: dmFsdWU=
`

	// Upload files to S3
	baseObj := awsutils.DeployableObject{
		Name:    "base/common.yaml",
		Content: []byte(baseCommonYaml),
	}
	err = awsHandler.Put(bucket, baseObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	configObj := awsutils.DeployableObject{
		Name:    "overlay/config/app.yaml",
		Content: []byte(configAppYaml),
	}
	err = awsHandler.Put(bucket, configObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sharedKustomizeObj := awsutils.DeployableObject{
		Name:    "shared/kustomization.yaml",
		Content: []byte(sharedKustomizationYaml),
	}
	err = awsHandler.Put(bucket, sharedKustomizeObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	sharedResourceObj := awsutils.DeployableObject{
		Name:    "shared/shared.yaml",
		Content: []byte(sharedYaml),
	}
	err = awsHandler.Put(bucket, sharedResourceObj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	allKeys := []string{
		"base/common.yaml",
		"overlay/config/app.yaml",
		"shared/kustomization.yaml",
		"shared/shared.yaml",
	}
	downloadedFiles := make(map[string]bool)

	// Test downloadReferencedResources
	err = downloadReferencedResources(kustomizationFile, "overlay", allKeys, awsHandler, bucket, tempDir, downloadedFiles)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify files were downloaded
	g.Expect(downloadedFiles["base/common.yaml"]).To(gomega.BeTrue())
	g.Expect(downloadedFiles["overlay/config/app.yaml"]).To(gomega.BeTrue())
	g.Expect(downloadedFiles["shared/kustomization.yaml"]).To(gomega.BeTrue())
	g.Expect(downloadedFiles["shared/shared.yaml"]).To(gomega.BeTrue())

	// Verify files exist on disk
	g.Expect(filepath.Join(tempDir, "base/common.yaml")).To(gomega.BeAnExistingFile())
	g.Expect(filepath.Join(tempDir, "overlay/config/app.yaml")).To(gomega.BeAnExistingFile())
	g.Expect(filepath.Join(tempDir, "shared/kustomization.yaml")).To(gomega.BeAnExistingFile())
	g.Expect(filepath.Join(tempDir, "shared/shared.yaml")).To(gomega.BeAnExistingFile())
}

func TestDownloadResourcePathAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup fake S3 server
	server, awsHandler, _ := SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err := awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "download-path-test-")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer os.RemoveAll(tempDir)

	// Upload test files
	file1Content := "file1 content"
	file2Content := "file2 content"

	file1Obj := awsutils.DeployableObject{
		Name:    "base/file1.yaml",
		Content: []byte(file1Content),
	}
	err = awsHandler.Put(bucket, file1Obj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	file2Obj := awsutils.DeployableObject{
		Name:    "base/file2.yaml",
		Content: []byte(file2Content),
	}
	err = awsHandler.Put(bucket, file2Obj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	allKeys := []string{"base/file1.yaml", "base/file2.yaml", "other/file3.yaml"}
	downloadedFiles := make(map[string]bool)

	// Test downloadResourcePath
	err = downloadResourcePath("base", allKeys, awsHandler, bucket, tempDir, downloadedFiles)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify files were downloaded
	g.Expect(downloadedFiles["base/file1.yaml"]).To(gomega.BeTrue())
	g.Expect(downloadedFiles["base/file2.yaml"]).To(gomega.BeTrue())
	g.Expect(downloadedFiles["other/file3.yaml"]).To(gomega.BeFalse())

	// Verify files exist on disk
	g.Expect(filepath.Join(tempDir, "base/file1.yaml")).To(gomega.BeAnExistingFile())
	g.Expect(filepath.Join(tempDir, "base/file2.yaml")).To(gomega.BeAnExistingFile())

	// Test error case - non-existent resource path
	err = downloadResourcePath("nonexistent", allKeys, awsHandler, bucket, tempDir, downloadedFiles)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("no files found for resource path"))
}

func TestDownloadResourcePathDirectAWS(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup fake S3 server
	server, awsHandler, _ := SetupFakeS3Server()
	defer server.Close()
	g.Expect(awsHandler).NotTo(gomega.BeNil())

	bucket := "test-bucket"
	err := awsHandler.Create(bucket)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "download-direct-test-")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer os.RemoveAll(tempDir)

	// Upload test files
	file1Content := "direct file1 content"
	file2Content := "direct file2 content"

	file1Obj := awsutils.DeployableObject{
		Name:    "direct/file1.yaml",
		Content: []byte(file1Content),
	}
	err = awsHandler.Put(bucket, file1Obj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	file2Obj := awsutils.DeployableObject{
		Name:    "direct/file2.yaml",
		Content: []byte(file2Content),
	}
	err = awsHandler.Put(bucket, file2Obj)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	downloadedFiles := make(map[string]bool)

	// Test downloadResourcePathDirect
	err = downloadResourcePathDirect("direct", awsHandler, bucket, tempDir, downloadedFiles)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify files were downloaded
	g.Expect(downloadedFiles["direct/file1.yaml"]).To(gomega.BeTrue())
	g.Expect(downloadedFiles["direct/file2.yaml"]).To(gomega.BeTrue())

	// Verify files exist on disk
	g.Expect(filepath.Join(tempDir, "direct/file1.yaml")).To(gomega.BeAnExistingFile())
	g.Expect(filepath.Join(tempDir, "direct/file2.yaml")).To(gomega.BeAnExistingFile())

	// Test error case - non-existent resource path
	err = downloadResourcePathDirect("nonexistent", awsHandler, bucket, tempDir, downloadedFiles)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("no files found for resource path"))
}
