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

package controller

import (
	"reflect"
	"runtime"
	"strings"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/utils"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AddToManagerMCMFuncs is a list of functions to add all MCM Controllers (with config to hub) to the Manager
var AddToManagerMCMFuncs []func(manager.Manager, *rest.Config, *types.NamespacedName, bool) error

// AddToManagerFuncs is a list of functions to add all Controllers to the Manager
var AddToManagerFuncs []func(manager.Manager) error

// AddHubToManagerFuncs is a list of functions to add all Hub Controllers to the Manager
var AddHubToManagerFuncs []func(manager.Manager) error

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager, cfg *rest.Config, syncid *types.NamespacedName, standalone bool) error {
	for _, f := range AddToManagerFuncs {
		// If remote subscription pod (appmgr) is running in hub, don't add helmrelease controller to the manager,
		// As there has been a helmrelease controller running in standalone subscription pod
		addFuncName := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
		klog.Infof("AddToManagerFunc: %s", addFuncName)

		if strings.Contains(addFuncName, "multicloud-operators-subscription-release/pkg/controller/helmrelease.Add") &&
			utils.IsHub(m.GetConfig()) &&
			!standalone {
			klog.Info("Don't add helmrelease controller to the manager as the remote subscription is running on hub cluster")
			continue
		}

		if err := f(m); err != nil {
			return err
		}
	}

	for _, f := range AddToManagerMCMFuncs {
		if err := f(m, cfg, syncid, standalone); err != nil {
			return err
		}
	}

	return nil
}

// AddHubToManager adds all Hub Controllers to the Manager
func AddHubToManager(m manager.Manager) error {
	for _, f := range AddHubToManagerFuncs {
		if err := f(m); err != nil {
			return err
		}
	}

	return nil
}
