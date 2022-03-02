package addonmanager

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func newAddon(name, namespace string) *addonapiv1alpha1.ManagedClusterAddOn {
	return &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func newManagedCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func TestManifests(t *testing.T) {
	cluster := newManagedCluster("cluster1")

	addon := newAddon("application-manaager", "cluster1")

	fakeKubeClient := fakekube.NewSimpleClientset()

	agent := NewAgent("test", fakeKubeClient, false)

	objects, err := agent.Manifests(cluster, addon)

	if err != nil {
		t.Errorf("Expect no error but got %v", err)
	}

	if len(objects) != 10 {
		t.Errorf("Expect 10 objects bug got %d", len(objects))
	}

	for _, obj := range objects {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			validateDeployment(t, o)
		}
	}
}

func validateDeployment(t *testing.T, dep *appsv1.Deployment) {
	if dep.Spec.Template.Spec.Containers[0].Image != "test" {
		t.Errorf("Expect image name to be test but got %s", dep.Spec.Template.Spec.Containers[0].Image)
	}

	for _, c := range dep.Spec.Template.Spec.Containers[0].Command {
		if !strings.HasPrefix(c, "--cluster-name") {
			continue
		}

		if c != "--cluster-name=cluster1" {
			t.Errorf("Unexpected command for cluster name: %s", c)
		}
	}
}

func TestPermissionFunc(t *testing.T) {
	cluster := newManagedCluster("cluster1")

	addon := newAddon("application-manaager", "cluster1")

	fakeKubeClient := fakekube.NewSimpleClientset()

	agent := NewAgent("test", fakeKubeClient, false)

	err := agent.GetAgentAddonOptions().Registration.PermissionConfig(cluster, addon)

	if err != nil {
		t.Errorf("Expect no error but got %v", err)
	}

	actions := fakeKubeClient.Actions()

	if len(actions) != 2 {
		t.Errorf("Expect 2 actions, but got %v", actions)
	}

	createAction := actions[1].(clienttesting.CreateActionImpl)

	binding := createAction.Object.(*rbacv1.RoleBinding)

	if binding.RoleRef.Name != roleName {
		t.Errorf("Expect roleref to be  %s, but got %s", roleName, binding.RoleRef.Name)
	}
}
