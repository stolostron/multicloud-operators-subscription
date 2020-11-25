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

package namespace

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ReconcileAppMgrToken syncid is the namespaced name of this managed cluster.
// host is the API server URL of this managed cluster.
type AgentTokenReconciler struct {
	*NsSubscriber
	client.Client
	hubclient client.Client
	scheme    *runtime.Scheme
	syncid    types.NamespacedName
	host      string
}

// NewKlusterletTokenReconciler returns a new klusterlet agent token reconciler
func NewKlusterletTokenReconciler(mgr manager.Manager, hubclient client.Client, syncid types.NamespacedName, host string) *AgentTokenReconciler {
	rec := &AgentTokenReconciler{
		Client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		hubclient: hubclient,
		syncid:    syncid,
		host:      host,
	}

	return rec
}

// Reconciles <clusterName>-cluster-secret secret in the managed cluster's namespace
// on the hub cluster to the klusterlet-addon-appmgr service account's token secret.
func (r *AgentTokenReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.Info("=====================NewKlusterletTokenReconciler==========================")
	klog.Info("request.Name" + request.Name)
	klog.Info("request.Namespace" + request.Namespace)
	klog.Info("request..String()" + request.String())
	klog.Info("request.NamespacedName" + request.NamespacedName.String())
	klog.Info("===========================================================================")
	return reconcile.Result{}, nil
}
