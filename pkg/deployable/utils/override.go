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
	"errors"
	"strings"

	jsonpatch "github.com/cameront/go-jsonpatch"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
)

// GenerateOverrides compare 2 deployable and generate array for overrides
func GenerateOverrides(src, dst *appv1alpha1.Deployable) (covs []appv1alpha1.ClusterOverride) {
	defer func() {
		if r := recover(); r != nil {
			klog.V(5).Infof("Failed to make patch between the source and target deployables: r: %v", r)

			ovmap := make(map[string]interface{})
			ovmap["path"] = "."
			ovmap["value"] = dst.Spec.Template.DeepCopy()

			patchb, err := json.Marshal(ovmap)

			if err != nil {
				klog.Info("Error in marshal target target subscription template spec.packageOverride ", ovmap, " with error:", err)

				covs = append(covs, appv1alpha1.ClusterOverride{})
			} else {
				clusterOverride := appv1alpha1.ClusterOverride{
					RawExtension: runtime.RawExtension{
						Raw: patchb,
					},
				}

				covs = append(covs, clusterOverride)
			}
		}
	}()

	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	klog.V(10).Info("Start Generating patch. Src Template:", string(src.Spec.Template.Raw), " dst:", string(dst.Spec.Template.Raw))

	srcobj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(src.Spec.Template)

	if err != nil {
		klog.Info("Failed to decode src template ", string(src.Spec.Template.Raw))
	}

	dstobj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dst.Spec.Template)

	if err != nil {
		klog.Info("Failed to decode dst template ", string(dst.Spec.Template.Raw))
	}

	patch, err := jsonpatch.MakePatch(srcobj, dstobj)

	if err != nil {
		klog.Info("Error in generating patch for", string(src.Spec.Template.Raw), " with error:", err)
		return covs
	}

	for _, p := range patch.Operations {
		ovmap := make(map[string]interface{})
		pathstr := p.Path

		if pathstr[0] == '/' {
			pathstr = pathstr[1:]
		}

		pathstr = strings.ReplaceAll(pathstr, "/", ".")
		ovmap["path"] = pathstr
		ovmap["value"] = p.Value
		patchb, err := json.Marshal(ovmap)

		if err != nil {
			klog.Info("Error in mashaing patch for", ovmap, " with error:", err)
			continue
		}

		covs = append(covs, appv1alpha1.ClusterOverride{RawExtension: runtime.RawExtension{Raw: patchb}})
	}

	klog.V(5).Info("Got clusteroverrides ", covs)

	return covs
}

// PrepareOverrides returns the overridemap for given deployable instance
func PrepareOverrides(cluster types.NamespacedName, instance *appv1alpha1.Deployable) ([]appv1alpha1.ClusterOverride, error) {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	if instance == nil || instance.Spec.Overrides == nil {
		return nil, nil
	}

	var overrides []appv1alpha1.ClusterOverride

	// go over clsuters to find matching override
	for _, ov := range instance.Spec.Overrides {
		if ov.ClusterName != cluster.Name && (ov.ClusterName != "/" || cluster.Name != "" || cluster.Namespace != "") {
			continue
		}

		overrides = ov.ClusterOverrides
	}

	return overrides, nil
}

// OverrideTemplate alter the given template with overrides
func OverrideTemplate(template *unstructured.Unstructured, overrides []appv1alpha1.ClusterOverride) (*unstructured.Unstructured, error) {
	if klog.V(QuiteLogLel) {
		fnName := GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	ovt := template.DeepCopy()

	if template == nil || overrides == nil {
		klog.V(10).Info("No Instance or no override for template")
		return ovt, nil
	}

	for _, override := range overrides {
		tmpOverride := override
		ovuobj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&tmpOverride)
		klog.V(10).Info("From Instance Converter", ovuobj, "with err:", err, " path: ", ovuobj["path"], " value:", ovuobj["value"])

		if err != nil {
			return nil, errors.New("can not parse override")
		}

		path, ok := ovuobj["path"].(string)

		if !ok {
			return nil, errors.New("can not convert path of override")
		}

		if path == "." {
			ovt.Object = ovuobj["value"].(map[string]interface{})
		} else {
			fields := strings.Split(path, ".")
			err = unstructured.SetNestedField(ovt.Object, ovuobj["value"], fields...)

			if err != nil {
				klog.V(5).Info("Error in setting nested field", err)
			}
		}
	}

	klog.V(10).Info("Finished overriding template:", ovt)

	return ovt, nil
}
