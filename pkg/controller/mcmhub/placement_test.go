// Copyright 2023 The Kubernetes Authors.
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

package mcmhub

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	appSubV1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
)

func TestReconcileSubscription_getClustersFromPlacementRef(t *testing.T) {
	tests := []struct {
		name   string
		appSub *appSubV1.Subscription
	}{
		{
			name: "PlacementRule with extra s at the end",
			appSub: &appSubV1.Subscription{
				Spec: appSubV1.SubscriptionSpec{
					Placement: &v1.Placement{
						PlacementRef: &corev1.ObjectReference{
							Kind:       "PlacementRules",
							APIVersion: "apps.open-cluster-management.io/v1",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ReconcileSubscription{}
			_, err := r.getClustersFromPlacementRef(tt.appSub)

			if (err != nil) != true {
				t.Errorf("ReconcileSubscription.getClustersFromPlacementRef() error = %v, wantErr %v", err, true)
				return
			}

			if !strings.Contains(err.Error(), "unsupported placement reference") {
				t.Errorf("ReconcileSubscription.getClustersFromPlacementRef() error = %v", err)
				return
			}
		})
	}
}
