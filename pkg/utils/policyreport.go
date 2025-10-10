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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	appsubReportV1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Create PropagatioFailed result in cluster appsubReport in the managed cluster namespace
func CreateFailedAppsubReportResult(client client.Client, cluster string, appsubNs, appsubName, statusMsg string) error {
	// Get cluster appsub reports
	appsubReport, err := getClusterAppsubReport(client, cluster, true)
	if err != nil {
		klog.Errorf("Error getting cluster appsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)
		return err
	}

	// Update result in appsub report
	prResultFoundIndex := -1
	prResultSource := appsubNs + "/" + appsubName

	for i, result := range appsubReport.Results {
		if result.Source == prResultSource {
			prResultFoundIndex = i
			break
		}
	}

	klog.V(1).Infof("Update appsubReport: %v/%v, resultIndex:%v", appsubReport.Namespace, appsubReport.Name, prResultFoundIndex)

	if prResultFoundIndex < 0 {
		// Deploy failed but result not in appsub report - add it
		klog.V(1).Infof("Add result (source:%v) to appsubReport", prResultSource)

		prFailedResult := &appsubReportV1alpha1.SubscriptionReportResult{
			Source:    prResultSource,
			Result:    "propagationFailed",
			Timestamp: metaV1.Timestamp{Seconds: time.Now().Unix()},
		}
		appsubReport.Results = append(appsubReport.Results, prFailedResult)
	}

	if err := client.Update(context.TODO(), appsubReport); err != nil {
		klog.Errorf("Error in updating on hub, appsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)
		return err
	}

	return nil
}

func getClusterAppsubReport(rClient client.Client, clusterAppsubReportNs string,
	create bool) (*appsubReportV1alpha1.SubscriptionReport, error) {
	appsubReport := &appsubReportV1alpha1.SubscriptionReport{
		TypeMeta: metaV1.TypeMeta{
			Kind:       "SubscriptionReport",
			APIVersion: "apps.open-cluster-management.io/v1alpha1",
		},
	}
	appsubReport.Namespace = clusterAppsubReportNs
	appsubReport.Name = clusterAppsubReportNs

	klog.V(1).Infof("Get cluster appsubReport: %v/%v", appsubReport.Namespace, appsubReport.Name)

	if err := rClient.Get(context.TODO(),
		client.ObjectKey{Name: appsubReport.Name, Namespace: appsubReport.Namespace}, appsubReport); err != nil {
		if errors.IsNotFound(err) {
			if create {
				klog.V(1).Infof("appsubReport: %v/%v not found, create it.", appsubReport.Namespace, appsubReport.Name)

				labels := map[string]string{
					"apps.open-cluster-management.io/cluster": "true",
				}
				appsubReport.Labels = labels
				appsubReport.ReportType = "Cluster"

				if err := rClient.Create(context.TODO(), appsubReport); err != nil {
					klog.Errorf("Error in creating on hub, appsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)
					return appsubReport, err
				}
			} else {
				return appsubReport, err
			}
		} else {
			klog.Errorf("Error getting appsubReport:%v/%v, err:%v", appsubReport.Namespace, appsubReport.Name, err)
			return appsubReport, err
		}
	}

	return appsubReport, nil
}
