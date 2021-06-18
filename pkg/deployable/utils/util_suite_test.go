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

package utils

import (
	"context"
	stdlog "log"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis"
	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/deployable/v1"
)

var cfg *rest.Config
var c client.Client
var d *appv1alpha1.Deployable

var (
	dplname = "example-configmap"
	dplns   = "default"
)

var (
	payload = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "payload",
		},
	}
)

func TestMain(m *testing.M) {
	t := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "deploy", "crds"),
			filepath.Join("..", "..", "hack", "test", "crds"),
		},
	}

	apis.AddToScheme(scheme.Scheme)

	var err error
	if cfg, err = t.Start(); err != nil {
		stdlog.Fatal(err)
	}

	d, err = SetupTestDeployable()
	if err != nil {
		stdlog.Fatal(err)
	}

	code := m.Run()

	t.Stop()

	os.Exit(code)
}

func SetupTestDeployable() (*appv1alpha1.Deployable, error) {
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	if err != nil {
		return nil, err
	}

	c = mgr.GetClient()

	instance := &appv1alpha1.Deployable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dplname,
			Namespace: dplns,
		},
		Spec: appv1alpha1.DeployableSpec{
			Template: &runtime.RawExtension{
				Object: payload,
			},
		},
	}

	if err = c.Create(context.TODO(), instance); err != nil {
		return nil, err
	}

	return instance, nil
}
