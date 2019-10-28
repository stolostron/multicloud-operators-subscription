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
	"encoding/json"
	"fmt"
	"testing"

	dplv1alpha1 "github.com/IBM/multicloud-operators-deployable/pkg/apis/app/v1alpha1"	
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func MarshalSecret() {
	s := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{"apiKey": []byte{}},
	}

	// we want to convert obj to template, which is used for dpl

	dpl := &dplv1alpha1.Deployable{}
	dpl.Name = s.GetName()
	dpl.Namespace = s.GetNamespace()
	dpl.Spec.Template = &runtime.RawExtension{}

	gvk := schema.GroupVersionKind{
		Kind:    "Secret",
		Version: "v1",
	}
	s.SetGroupVersionKind(gvk)
	sruntime := s.DeepCopyObject()
	dplTpl := dpl.Spec.Template
	runtime.Convert_runtime_Object_To_runtime_RawExtension(&sruntime, dplTpl, nil)

	fmt.Println(dpl.Spec.Template.Object)

	stpl := &unstructured.Unstructured{}
	tplByte, _ := dpl.Spec.Template.MarshalJSON()
	err := json.Unmarshal(tplByte, stpl)

	fmt.Println(err, stpl, dplTpl.Object.GetObjectKind())
}

func TestMarshalSecret(t *testing.T) {
	MarshalSecret()
}
