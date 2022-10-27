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

package hub

import (
	"github.com/prometheus/client_golang/prometheus"
	ocmMetrics "open-cluster-management.io/multicloud-operators-subscription/pkg/utils/metrics"
	controllerMetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var PropagationSuccessfulPullTime = *prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name: "propagation_successful_time",
	Help: "Histogram of successful propagation latency",
}, ocmMetrics.SubscriptionVectorLabels)

var PropagationFailedPullTime = *prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name: "propagation_failed_time",
	Help: "Histogram of failed propagation latency",
}, ocmMetrics.SubscriptionVectorLabels)

func init() {
	controllerMetrics.Registry.MustRegister(PropagationSuccessfulPullTime, PropagationFailedPullTime)
}
