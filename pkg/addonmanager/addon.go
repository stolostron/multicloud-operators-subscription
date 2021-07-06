package addonmanager

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/multicloud-operators-subscription/pkg/addonmanager/bindata"
)

const roleName = "application-manager-agent"

var (
	genericScheme    = runtime.NewScheme()
	genericCodecs    = serializer.NewCodecFactory(genericScheme)
	genericCodec     = genericCodecs.UniversalDeserializer()
	agentStaticFiles = []string{
		"deploy/managed-common/apps.open-cluster-management.io_deployables_crd.yaml",
		"deploy/managed-common/apps.open-cluster-management.io_helmreleases_crd.yaml",
		"deploy/managed-common/apps.open-cluster-management.io_subscriptions.yaml",
		"deploy/managed-common/clusterrole_binding.yaml",
		"deploy/managed-common/clusterrole.yaml",
		"deploy/managed-common/service_account.yaml",
		"deploy/managed-common/service.yaml",
	}

	agentDeploymentFile = "deploy/managed/operator.yaml"
)

func init() {
	_ = scheme.AddToScheme(genericScheme)
	_ = apiextv1.AddToScheme(genericScheme)
}

type subscriptionAgent struct {
	agentImage string
	kubeClient kubernetes.Interface
}

func NewAgent(agentImage string, kubeClient kubernetes.Interface) agent.AgentAddon {
	return &subscriptionAgent{
		agentImage: agentImage,
		kubeClient: kubeClient,
	}
}

func (s *subscriptionAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	objects := []runtime.Object{}

	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = "open-cluster-management-agent-addon"
	}

	for _, file := range agentStaticFiles {
		raw := bindata.MustAsset(file)
		obj, _, err := genericCodec.Decode(raw, nil, nil)

		if err != nil {
			return objects, fmt.Errorf(
				"error while decoding YAML file %s. Err was: %s", file, err)
		}

		objects = append(objects, obj)
	}

	// render deployment files
	deployementRaw := bindata.MustAsset(agentDeploymentFile)
	obj, _, err := genericCodec.Decode(deployementRaw, nil, nil)

	if err != nil {
		return objects, fmt.Errorf(
			"error while decoding YAML object file %s. Err was: %s", agentDeploymentFile, err)
	}

	deployment := obj.(*appsv1.Deployment)

	// Update image
	deployment.Spec.Template.Spec.Containers[0].Image = s.agentImage

	// Update cluster name
	for i := range deployment.Spec.Template.Spec.Containers[0].Command {
		if strings.HasPrefix(deployment.Spec.Template.Spec.Containers[0].Command[i], "--cluster-name") {
			deployment.Spec.Template.Spec.Containers[0].Command[i] = fmt.Sprintf("--cluster-name=%s", cluster.Name)
		}
	}

	objects = append(objects, deployment)

	return objects, nil
}

func (s *subscriptionAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: "application-manager",
		Registration: &agent.RegistrationOption{
			CSRConfigurations: agent.KubeClientSignerConfigurations("application-manager", "appmgr"),
			CSRApproveCheck: func(
				cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
				return true
			},
			PermissionConfig: s.permissionFunc,
		},
	}
}

func (s *subscriptionAgent) permissionFunc(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	groups := agent.DefaultGroups(cluster.Name, addon.Name)
	return s.ensureRoleBinding(groups[0], cluster.Name)
}

func (s *subscriptionAgent) ensureRoleBinding(group, cluster string) error {
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: cluster,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind: "Group",
				Name: group,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     roleName,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	existingBinding, err := s.kubeClient.RbacV1().RoleBindings(cluster).Get(
		context.Background(), roleName, metav1.GetOptions{})

	switch {
	case errors.IsNotFound(err):
		_, err := s.kubeClient.RbacV1().RoleBindings(cluster).Create(
			context.Background(), roleBinding, metav1.CreateOptions{})
		return err
	case err != nil:
		return err
	}

	if apiequality.Semantic.DeepEqual(existingBinding.Subjects, roleBinding.Subjects) &&
		apiequality.Semantic.DeepEqual(existingBinding.RoleRef, roleBinding.RoleRef) {
		return nil
	}

	_, err = s.kubeClient.RbacV1().RoleBindings(cluster).Update(
		context.Background(), roleBinding, metav1.UpdateOptions{})

	return err
}
