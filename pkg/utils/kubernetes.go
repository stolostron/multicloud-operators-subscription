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

package utils

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	crdclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

// CheckAndInstallCRD checks if subscription belongs to this cluster
// managed cluster annotation matches or no managed cluster annotation (local)
func CheckAndInstallCRD(crdconfig *rest.Config, pathname string) error {
	var err error

	crdClient, err := crdclientset.NewForConfig(crdconfig)
	if err != nil {
		klog.Fatalf("Error building cluster registry clientset: %s", err.Error())
		return err
	}

	var crdobj crdv1.CustomResourceDefinition

	var crddata []byte

	crddata, err = ioutil.ReadFile(pathname) // #nosec G304 pathname is not user supplied. Used only by unit-test.

	if err != nil {
		klog.Fatal("Loading app crd file", err.Error())
		return err
	}

	err = yaml.Unmarshal(crddata, &crdobj)

	if err != nil {
		klog.Fatal("Unmarshal app crd ", err.Error(), "\n", string(crddata))
		return err
	}

	klog.V(10).Info("Loaded Application CRD: ", crdobj, "\n - From - \n", string(crddata))

	crd, err := crdClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crdobj.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		klog.Info("Installing SIG Application CRD from File: ", pathname)
		// Install sig app
		_, err = crdClient.ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), &crdobj, metav1.CreateOptions{})
		if err != nil {
			klog.Fatal("Creating CRD", err.Error())
			return err
		}
	} else {
		if !reflect.DeepEqual(crd.Spec, crdobj.Spec) {
			klog.Info("CRD ", crdobj.GetName(), " is being updated with ", pathname)
			crdobj.Spec.DeepCopyInto(&crd.Spec)
			_, err = crdClient.ApiextensionsV1().CustomResourceDefinitions().Update(context.TODO(), crd, metav1.UpdateOptions{})
			if err != nil {
				klog.Fatal("Updating CRD", err.Error())
				return err
			}
		} else {
			klog.Info("CRD ", crdobj.GetName(), " exists: ", pathname)
		}
		return err
	}

	return err
}

func NamespacedNameFormat(str string) types.NamespacedName {
	nn := types.NamespacedName{}

	if str != "" {
		strs := strings.Split(str, "/")
		if len(strs) != 2 {
			errmsg := "Illegal string, want namespace/name, but get " + str
			klog.Error(errmsg)

			return nn
		}

		nn.Name = strs[1]
		nn.Namespace = strs[0]
	}

	return nn
}

// ConvertLabels coverts label selector to lables.Selector
func ConvertLabels(labelSelector *metav1.LabelSelector) (labels.Selector, error) {
	if labelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(labelSelector)

		if err != nil {
			return labels.Nothing(), err
		}

		return selector, nil
	}

	return labels.Everything(), nil
}

func GetComponentNamespace() (string, error) {
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "open-cluster-management-agent-addon", err
	}

	return string(nsBytes), nil
}

func BuildKubeClient(kubeConfigPath string) (*kubernetes.Clientset, error) {
	hubRestConfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		klog.Errorf("failed to build kubeconfig. Error:%v", err)
		return nil, err
	}

	return kubernetes.NewForConfig(hubRestConfig)
}

// GetCheckSum generates a checksum of a kube config file
func GetCheckSum(kubeconfigfile string) ([32]byte, error) {
	content, err := ioutil.ReadFile(filepath.Clean(kubeconfigfile))
	if err != nil {
		return [32]byte{}, fmt.Errorf("read %s failed, %w", kubeconfigfile, err)
	}

	return sha256.Sum256(content), nil
}
