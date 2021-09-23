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
	"sigs.k8s.io/controller-runtime/pkg/client"
	policyReportV1alpha2 "sigs.k8s.io/wg-policy-prototypes/policy-report/pkg/api/wgpolicyk8s.io/v1alpha2"
)

// Create PropagatioFailed result in cluster policy report in the managed cluster namespace
func CreateFailedPolicyReportResult(client client.Client, cluster string, appsubNs, appsubName, statusMsg string) error {
	// Get cluster policy reports
	policyReport, err := getClusterPolicyReport(client, appsubNs, appsubName, cluster, true)
	if err != nil {
		klog.Errorf("Error getting cluster policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
		return err
	}

	// Update result in policy report
	prResultFoundIndex := -1
	prResultSource := appsubNs + "/" + appsubName
	for i, result := range policyReport.Results {
		if result.Source == prResultSource && result.Policy == "APPSUB_FAILURE" {
			prResultFoundIndex = i
			break
		}
	}
	klog.V(1).Infof("Update policy report: %v/%v, resultIndex:%v", policyReport.Namespace, policyReport.Name, prResultFoundIndex)

	if prResultFoundIndex < 0 {
		// Deploy failed but result not in policy report - add it
		klog.V(1).Infof("Add result (source:%v) to policy report", prResultSource)

		prFailedResult := &policyReportV1alpha2.PolicyReportResult{
			Source:    prResultSource,
			Policy:    "APPSUB_FAILURE",
			Result:    "fail",
			Timestamp: metaV1.Timestamp{Seconds: time.Now().Unix()},
		}
		policyReport.Results = append(policyReport.Results, prFailedResult)
	}

	if err := client.Update(context.TODO(), policyReport); err != nil {
		klog.Errorf("Error in updating on hub, policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
		return err
	}

	return nil
}

func getClusterPolicyReport(rClient client.Client, appsubNs, appsubName, clusterPolicyReportNs string, create bool) (*policyReportV1alpha2.PolicyReport, error) {
	policyReport := &policyReportV1alpha2.PolicyReport{}
	policyReport.Namespace = clusterPolicyReportNs
	policyReport.Name = "policyreport-appsub-status"
	klog.V(1).Infof("Get cluster policy report: %v/%v", policyReport.Namespace, policyReport.Name)

	if err := rClient.Get(context.TODO(),
		client.ObjectKey{Name: policyReport.Name, Namespace: policyReport.Namespace}, policyReport); err != nil {
		if errors.IsNotFound(err) {
			if create {
				klog.V(1).Infof("Policy report: %v/%v not found, create it.", policyReport.Namespace, policyReport.Name)

				labels := map[string]string{
					"apps.open-cluster-management.io/cluster": "true",
				}
				policyReport.Labels = labels

				if err := rClient.Create(context.TODO(), policyReport); err != nil {
					klog.Errorf("Error in creating on hub, policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
					return policyReport, err
				}
			} else {
				return policyReport, err
			}
		} else {
			klog.Errorf("Error getting policyReport:%v/%v, err:%v", policyReport.Namespace, policyReport.Name, err)
			return policyReport, err
		}
	}

	return policyReport, nil
}
