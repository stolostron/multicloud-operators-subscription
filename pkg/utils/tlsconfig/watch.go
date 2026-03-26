// Copyright 2025 The Kubernetes Authors.
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

package tlsconfig

import (
	"context"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

var apiserverGVR = schema.GroupVersionResource{
	Group:    "config.openshift.io",
	Version:  "v1",
	Resource: "apiservers",
}

// StartAPIServerTLSProfileWatch watches the OpenShift APIServer named "cluster".
// On Modified events, exits with code 0 so the pod can restart and reload TLS from InitClusterTLSConfig.
// If Watch fails (including on clusters without that API), logs at V(2) and retries after a short backoff.
func StartAPIServerTLSProfileWatch(ctx context.Context, cfg *rest.Config) {
	if cfg == nil {
		return
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		klog.V(2).Infof("tls profile watch: skip, dynamic client: %v", err)
		return
	}
	go watchAPIServerLoop(ctx, dyn)
}

func watchAPIServerLoop(ctx context.Context, dyn dynamic.Interface) {
	backoff := time.Second * 30
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		w, err := dyn.Resource(apiserverGVR).Watch(ctx, metav1.ListOptions{
			FieldSelector: "metadata.name=" + apiserverClusterName,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			klog.V(2).Infof("tls profile watch: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}
		func() {
			defer w.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-w.ResultChan():
					if !ok {
						return
					}
					if ev.Type == watch.Modified {
						klog.Info("APIServer cluster modified, exiting to reload TLS profile")
						os.Exit(0)
					}
				}
			}
		}()
	}
}
