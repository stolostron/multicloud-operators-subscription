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

package metrics

import "github.com/prometheus/client_golang/prometheus"

var LocalDeploymentSuccessfulPullTime = *prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name: "local_deployment_successful_time",
	Help: "Histogram of successful local deployment latency",
}, []string{LabelSubscriptionNameSpace, LabelSubscriptionName})

var LocalDeploymentFailedPullTime = *prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name: "local_deployment_failed_time",
	Help: "Histogram of failed local deployment latency",
}, []string{LabelSubscriptionNameSpace, LabelSubscriptionName})

func init() {
	CollectorsForRegistration = append(CollectorsForRegistration, LocalDeploymentSuccessfulPullTime, LocalDeploymentFailedPullTime)
}
