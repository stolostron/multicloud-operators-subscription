/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createPropagatioFailedAppSubPackageStatus creates an appsubpackagestatus with the phase PropagationFailed
// in the managed cluster namespace
func CreatePropagatioFailedAppSubPackageStatus(statusClient client.Client, cluster string, isClusterLocal bool, appSubNs, appSubName, statusMsg string) error {
	packagesStatuses := &v1alpha1.SubscriptionUnitStatus{
		Phase:   v1alpha1.PackagePropagationFailed,
		Message: statusMsg,
		LastUpdateTime: metaV1.Time{
			Time: time.Now(),
		},
	}

	return CreateAppSubPackageStatus(statusClient, cluster, cluster, appSubNs, appSubName, []v1alpha1.SubscriptionUnitStatus{*packagesStatuses})
}

func CreateAppSubPackageStatus(statusClient client.Client, cluster, packageStatusNs, appSubNs, appSubName string, packagesStatuses []v1alpha1.SubscriptionUnitStatus) error {
	pkgstatus := &v1alpha1.SubscriptionStatus{}

	// Check if appsubpackagestatus already exists
	listopts := &client.ListOptions{}
	psSelector := &metaV1.LabelSelector{
		MatchLabels: map[string]string{
			"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appSubNs+"."+appSubName),
		},
	}

	psLabels, err := ConvertLabels(psSelector)
	if err != nil {
		klog.Error("Failed to convert managed appsubstatus label selector, err:", err)

		return err
	}

	listopts.LabelSelector = psLabels
	listopts.Namespace = packageStatusNs
	pkgstatusList := &v1alpha1.SubscriptionStatusList{}

	err = statusClient.List(context.TODO(), pkgstatusList, listopts)
	if err != nil {
		klog.Error("Failed to list appsubpackagestatus, err:", err)

		return err
	}

	if len(pkgstatusList.Items) > 0 {
		// appsubpackagestatus already exists - only update package statuses
		pkgstatus = &pkgstatusList.Items[0]
		pkgstatus.Statuses.SubscriptionStatus = packagesStatuses

		klog.Infof("Updating appsubpackagestatus: %v/%v", pkgstatus.Namespace, pkgstatus.Name)

		err = statusClient.Update(context.TODO(), pkgstatus)
		if err != nil {
			klog.Error(err, "Error in updating appsubpackagestatus for cluster:", cluster)
			return err
		}

		return nil
	}

	// Create new appsubpackagestatus
	pkgstatus.Name = appSubName
	pkgstatus.Namespace = packageStatusNs

	klog.Infof("Creating new appsubpackagestatus: %v/%v", pkgstatus.Namespace, pkgstatus.Name)

	labels := map[string]string{
		"apps.open-cluster-management.io/cluster":              cluster,
		"apps.open-cluster-management.io/hosting-subscription": fmt.Sprintf("%.63s", appSubNs+"."+appSubName),
	}
	pkgstatus.Labels = labels

	pkgstatus.Statuses.SubscriptionStatus = packagesStatuses

	err = statusClient.Create(context.TODO(), pkgstatus)
	if err != nil {
		klog.Error(err, "Error in creating appsubpackagestatus for cluster:", cluster)
		return err
	}

	return nil
}
