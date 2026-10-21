package addon

import (
	"context"
	"testing"

	"github.com/openshift/library-go/pkg/assets"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
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

//nolint:all
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
			expectedCount:     10,
		},
		{
			name:              "case_2",
			cluster:           newCluster("local-cluster"),
			addon:             newAddon(AppMgrAddonName, "local-cluster", "test", ""),
			expectedNamespace: "test",
			expectedImage:     "quay.io/open-cluster-management/multicluster_operators_subscription:latest",
			expectedCount:     6,
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

type testConfigGetter struct {
	config *addonapiv1alpha1.AddOnDeploymentConfig
}

func newTestConfigGetter(config *addonapiv1alpha1.AddOnDeploymentConfig) *testConfigGetter {
	return &testConfigGetter{
		config: config,
	}
}

func (t *testConfigGetter) Get(_ context.Context, _ string, _ string) (*addonapiv1alpha1.AddOnDeploymentConfig, error) {
	return t.config, nil
}

func newAddonConfigWithNamespace(namespace string) *addonapiv1alpha1.AddOnDeploymentConfig {
	return &addonapiv1alpha1.AddOnDeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "test",
		},
		Spec: addonapiv1alpha1.AddOnDeploymentConfigSpec{
			AgentInstallNamespace: namespace,
		},
	}
}

func newAddonWithConfig(name, cluster, annotationValue string, config *addonapiv1alpha1.AddOnDeploymentConfig) *addonapiv1alpha1.ManagedClusterAddOn {
	specHash, _ := utils.GetAddOnDeploymentConfigSpecHash(config)
	addon := newAddon(name, cluster, "", annotationValue)
	addon.Status = addonapiv1alpha1.ManagedClusterAddOnStatus{
		ConfigReferences: []addonapiv1alpha1.ConfigReference{
			{
				ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
					Group:    utils.AddOnDeploymentConfigGVR.Group,
					Resource: utils.AddOnDeploymentConfigGVR.Resource,
				},
				DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
					SpecHash: specHash,
				},
			},
		},
	}

	return addon
}

func TestAddonInstallNamespaceFunc(t *testing.T) {
	tests := []struct {
		name              string
		addon             *addonapiv1alpha1.ManagedClusterAddOn
		cluster           *clusterv1.ManagedCluster
		config            *addonapiv1alpha1.AddOnDeploymentConfig
		expectedNamespace string
	}{
		{
			name:              "default namespace",
			addon:             newAddon("test", "cluster1", "", ""),
			cluster:           newCluster("cluster1"),
			expectedNamespace: "open-cluster-management-agent-addon",
		},
		{
			name:              "customized namespace",
			addon:             newAddonWithConfig("test", "cluster1", "", newAddonConfigWithNamespace("ns1")),
			cluster:           newCluster("cluster1"),
			config:            newAddonConfigWithNamespace("ns1"),
			expectedNamespace: "ns1",
		},
	}

	scheme := runtime.NewScheme()
	clusterv1.Install(scheme)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(test.cluster).Build()
			getter := newTestConfigGetter(test.config)
			nsFunc := AddonInstallNamespaceFunc(getter, client)
			namespace, _ := nsFunc(test.addon)

			if namespace != test.expectedNamespace {
				t.Errorf("namespace should be %s, but get %s", test.expectedNamespace, namespace)
			}
		})
	}
}

// TestSecurityCheckPermissionTemplatesRender exercises the same template
// rendering path as applyManifestFromFile (without needing a real
// *kubernetes.Clientset) to catch Go-template syntax errors early: a bad
// {{ .Field }} reference in these manifests would otherwise only surface as a
// panic inside MustCreateAssetFromTemplate at addon-registration time on a real
// cluster. It also pins down the two RBAC decisions this fix depends on:
//   - the ClusterRole name is static (shared across all managed clusters)
//   - the ClusterRoleBinding name is templated per-cluster (so registering a
//     second managed cluster can never overwrite the first cluster's subject)
func TestSecurityCheckPermissionTemplatesRender(t *testing.T) {
	clusterName := "test-cluster-1"
	group := agent.DefaultGroups(clusterName, AppMgrAddonName)[0]

	config := struct {
		ClusterName string
		Group       string
	}{
		ClusterName: clusterName,
		Group:       group,
	}

	t.Run("clusterrole-security-check.yaml", func(t *testing.T) {
		file := "manifests/permission/clusterrole-security-check.yaml"

		template, err := ChartFS.ReadFile(file)
		if err != nil {
			t.Fatalf("failed to read %s: %v", file, err)
		}

		rendered := assets.MustCreateAssetFromTemplate(file, template, config)

		cr := &rbacv1.ClusterRole{}
		if err := yaml.Unmarshal(rendered.Data, cr); err != nil {
			t.Fatalf("rendered %s did not decode as a ClusterRole: %v\n%s", file, err, rendered.Data)
		}

		if cr.Name == "" || contains(cr.Name, "{{") {
			t.Errorf("ClusterRole name looks unrendered/empty: %q", cr.Name)
		}

		var hasWebhookRule, hasClusterRoleBindingRule bool

		for _, r := range cr.Rules {
			for _, g := range r.APIGroups {
				switch g {
				case "admissionregistration.k8s.io":
					hasWebhookRule = true
				case "rbac.authorization.k8s.io":
					hasClusterRoleBindingRule = true
				}
			}
		}

		if !hasWebhookRule {
			t.Errorf("expected a rule for admissionregistration.k8s.io, got rules: %+v", cr.Rules)
		}

		if !hasClusterRoleBindingRule {
			t.Errorf("expected a rule for rbac.authorization.k8s.io, got rules: %+v", cr.Rules)
		}
	})

	t.Run("clusterrolebinding-security-check.yaml", func(t *testing.T) {
		file := "manifests/permission/clusterrolebinding-security-check.yaml"

		template, err := ChartFS.ReadFile(file)
		if err != nil {
			t.Fatalf("failed to read %s: %v", file, err)
		}

		rendered := assets.MustCreateAssetFromTemplate(file, template, config)

		crb := &rbacv1.ClusterRoleBinding{}
		if err := yaml.Unmarshal(rendered.Data, crb); err != nil {
			t.Fatalf("rendered %s did not decode as a ClusterRoleBinding: %v\n%s", file, err, rendered.Data)
		}

		if contains(crb.Name, "{{") {
			t.Errorf("ClusterRoleBinding name looks unrendered: %q", crb.Name)
		}

		if want := "open-cluster-management:addons:application-manager:cluster-admin-check:" + clusterName; crb.Name != want {
			t.Errorf("ClusterRoleBinding name = %q, want %q (must be unique per cluster)", crb.Name, want)
		}

		if crb.RoleRef.Name != "open-cluster-management:addons:application-manager:cluster-admin-check" {
			t.Errorf("ClusterRoleBinding roleRef.name = %q, want the static ClusterRole name", crb.RoleRef.Name)
		}

		if len(crb.Subjects) != 1 || crb.Subjects[0].Name != group {
			t.Errorf("ClusterRoleBinding subjects = %+v, want a single subject named %q", crb.Subjects, group)
		}

		// Rendering for a second cluster must produce a different binding name,
		// proving two managed clusters can never collide/overwrite each other.
		secondClusterName := "test-cluster-2"
		secondConfig := struct {
			ClusterName string
			Group       string
		}{
			ClusterName: secondClusterName,
			Group:       agent.DefaultGroups(secondClusterName, AppMgrAddonName)[0],
		}

		rendered2 := assets.MustCreateAssetFromTemplate(file, template, secondConfig)
		crb2 := &rbacv1.ClusterRoleBinding{}

		if err := yaml.Unmarshal(rendered2.Data, crb2); err != nil {
			t.Fatalf("rendered %s did not decode as a ClusterRoleBinding: %v\n%s", file, err, rendered2.Data)
		}

		if crb2.Name == crb.Name {
			t.Errorf("two different clusters produced the same ClusterRoleBinding name %q; "+
				"the second cluster's registration would overwrite the first cluster's subject", crb.Name)
		}
	})
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
