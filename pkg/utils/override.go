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
	"errors"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	appsubv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
)

// PrepareOverrides returns the overridemap for given subscription instance.
func PrepareOverrides(cluster types.NamespacedName, appsub *appsubv1.Subscription) ([]appsubv1.ClusterOverride, error) {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	if appsub == nil || appsub.Spec.Overrides == nil {
		return nil, nil
	}

	var overrides []appsubv1.ClusterOverride

	// go over clsuters to find matching override
	for _, ov := range appsub.Spec.Overrides {
		if ov.ClusterName == cluster.Name || (ov.ClusterName == "/" && cluster.Name != "" && cluster.Namespace != "") {
			overrides = ov.ClusterOverrides

			break
		}
	}

	klog.Infof("get overrides: %#v", overrides)

	return overrides, nil
}

// OverrideTemplate alter the given template with overrides.
func OverrideTemplate(template *unstructured.Unstructured, overrides []appsubv1.ClusterOverride) (*unstructured.Unstructured, error) {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	ovt := template.DeepCopy()

	if template == nil || overrides == nil {
		klog.Info("No Instance or no override for template")
		return ovt, nil
	}

	for _, override := range overrides {
		ovuobj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&override) // #nosec G601 requires "k8s.io/apimachinery/pkg/runtime" object
		klog.V(1).Info("From Instance Converter", ovuobj, "with err:", err, " path: ", ovuobj["path"], " value:", ovuobj["value"])

		if err != nil {
			return nil, errors.New("can not parse override")
		}

		path, ok := ovuobj["path"].(string)

		if !ok {
			return nil, errors.New("can not convert path of override")
		}

		fields := strings.Split(path, ".")
		err = unstructured.SetNestedField(ovt.Object, ovuobj["value"], fields...)

		if err != nil {
			klog.Error("Failed to set nested field for overriding template with error:", err)
		}
	}

	klog.V(1).Info("Finished overriding template:", ovt)

	return ovt, nil
}
