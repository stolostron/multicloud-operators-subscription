package addon

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
}

func newCluster(name string) *clusterv1.ManagedCluster {
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: clusterv1.ManagedClusterStatus{Version: clusterv1.ManagedClusterVersion{Kubernetes: "1.10.1"}},
	}

	if name == "local-cluster" {
		cluster.SetLabels(map[string]string{"local-cluster": "true"})
	}

	return cluster
}

func newAddon(name, cluster, installNamespace string, annotationValues string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster,
		},
		Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
			InstallNamespace: installNamespace,
		},
	}

	if annotationValues != "" {
		addon.SetAnnotations(map[string]string{"addon.open-cluster-management.io/values": annotationValues})
	}

	return addon
}

func newAgentAddon(t *testing.T) agent.AgentAddon {
	registrationOption := newRegistrationOption(nil, AppMgrAddonName)
	getValuesFunc := getValue

	agentAddon, err := addonfactory.NewAgentAddonFactory(AppMgrAddonName, ChartFS, ChartDir).
		WithScheme(scheme).
		WithGetValuesFuncs(getValuesFunc, addonfactory.GetValuesFromAddonAnnotation).
		WithAgentRegistrationOption(registrationOption).
		BuildHelmAgentAddon()
	if err != nil {
		t.Fatalf("failed to build agent %v", err)
	}

	return agentAddon
}

// nolint
func TestManifest(t *testing.T) {
	tests := []struct {
		name              string
		cluster           *clusterv1.ManagedCluster
		addon             *addonapiv1alpha1.ManagedClusterAddOn
		expectedNamespace string
		expectedImage     string
		expectedCount     int
	}{
		{
			name:              "case_1",
			cluster:           newCluster("cluster1"),
			addon:             newAddon(AppMgrAddonName, "cluster1", "", `{"global":{"nodeSelector":{"node-role.kubernetes.io/infra":""},"imageOverrides":{"multicluster_operators_subscription":"quay.io/test/multicluster_operators_subscription:test"}}}`),
			expectedNamespace: "open-cluster-management-agent-addon",
			expectedImage:     "quay.io/test/multicluster_operators_subscription:test",
			expectedCount:     8,
		},
		{
			name:              "case_2",
			cluster:           newCluster("local-cluster"),
			addon:             newAddon(AppMgrAddonName, "local-cluster", "test", ""),
			expectedNamespace: "test",
			expectedImage:     "quay.io/open-cluster-management/multicluster_operators_subscription:latest",
			expectedCount:     5,
		},
	}
	AppMgrImage = "quay.io/open-cluster-management/multicluster_operators_subscription:latest"
	agentAddon := newAgentAddon(t)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects, err := agentAddon.Manifests(test.cluster, test.addon)
			if err != nil {
				t.Errorf("failed to get manifests with error %v", err)
			}

			if len(objects) != test.expectedCount {
				t.Errorf("expected objects number is %d, got %d", test.expectedCount, len(objects))
			}

			for _, o := range objects {
				switch object := o.(type) {
				case *appsv1.Deployment:
					if object.Namespace != test.expectedNamespace {
						t.Errorf("expected namespace is %s, but got %s", test.expectedNamespace, object.Namespace)
					}
					if object.Spec.Template.Spec.Containers[0].Image != test.expectedImage {
						t.Errorf("expected image is %s, but got %s", test.expectedImage, object.Spec.Template.Spec.Containers[0].Image)
					}
				}
			}

			// output is for debug
			// output(t, test.name, objects...)
		})
	}
}
