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
	"io/ioutil"
	"reflect"

	"github.com/ghodss/yaml"
	crdv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	crdclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

// CheckAndInstallCRD checks if deployable belongs to this cluster
// managed cluster annotation matches or no managed cluster annotation (local)
func CheckAndInstallCRD(crdconfig *rest.Config, pathname string) error {
	var err error

	crdClient, err := crdclientset.NewForConfig(crdconfig)
	if err != nil {
		klog.Fatalf("Error building cluster registry clientset: %s", err.Error())
		return err
	}

	var crdobj crdv1beta1.CustomResourceDefinition

	var crddata []byte

	crddata, err = ioutil.ReadFile(pathname)

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

	crd, err := crdClient.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crdobj.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		klog.Info("Installing SIG Application CRD from File: ", pathname)
		// Install sig app
		_, err = crdClient.ApiextensionsV1beta1().CustomResourceDefinitions().Create(&crdobj)
		if err != nil {
			klog.Fatal("Creating CRD", err.Error())
			return err
		}
	} else {
		if !reflect.DeepEqual(crd.Spec, crdobj.Spec) {
			klog.Info("CRD ", crdobj.GetName(), " is being updated with ", pathname)
			crdobj.Spec.DeepCopyInto(&crd.Spec)
			_, err = crdClient.ApiextensionsV1beta1().CustomResourceDefinitions().Update(crd)
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
