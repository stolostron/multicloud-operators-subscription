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

package deployable

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/deployable/utils"
)

func (r *ReconcileDeployable) createManagedDependencies(cluster types.NamespacedName, instance *appv1alpha1.Deployable,
	familymap map[string]*appv1alpha1.Deployable) (map[string]*appv1alpha1.Deployable, error) {
	if klog.V(utils.QuiteLogLel) {
		fnName := utils.GetFnName()
		klog.Infof("Entering: %v()", fnName)

		defer klog.Infof("Exiting: %v()", fnName)
	}

	var err error

	hosting := types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}
	// create or update child deployable
	for _, dependency := range instance.Spec.Dependencies {
		klog.V(10).Info("handling dependency:", dependency, "in cluster:", cluster)
		// This controller does not handle non-deployable kind objets
		// Handle deployable kind
		if dependency.Kind == instance.Kind || dependency.Kind == "" {
			depobj := &appv1alpha1.Deployable{}
			depobjkey := client.ObjectKey{
				Name:      dependency.Name,
				Namespace: dependency.Namespace,
			}

			if depobjkey.Namespace == "" {
				depobjkey.Namespace = instance.Namespace
			}

			err = r.Get(context.TODO(), depobjkey, depobj)

			if err != nil {
				return familymap, err
			}

			objann := depobj.GetAnnotations()

			if objann == nil {
				objann = make(map[string]string)
			}

			for k, v := range dependency.Annotations {
				objann[k] = v
			}

			depobj.SetAnnotations(objann)

			objlbl := depobj.GetLabels()

			if objlbl == nil {
				objlbl = make(map[string]string)
			}

			for k, v := range dependency.Labels {
				objlbl[k] = v
			}

			depobj.SetLabels(objlbl)

			if objann[appv1alpha1.AnnotationShared] == "true" {
				shareddeplist, err := r.getDeployableFamily(depobj)
				klog.Info("Got shared objs:", shareddeplist)

				if err != nil && !errors.IsNotFound(err) {
					klog.Error("failed to get shared dependency")
				}

				for _, dpl := range shareddeplist {
					if dpl.Namespace == cluster.Namespace {
						familymap[getDeployableTrueKey(dpl)] = dpl.DeepCopy()
					}
				}
			}

			familymap, err = r.createManagedDeployable(cluster, hosting, depobj, familymap)

			if err != nil {
				return familymap, err
			}
		}
	}

	return familymap, nil
}
