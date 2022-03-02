package addon

import (
	"context"
	"embed"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	AppMgrAddonName = "application-manager"

	ChartDir = "manifests/chart"
)

//nolint
//go:embed manifests
//go:embed manifests/chart
//go:embed manifests/chart/templates/_helpers.tpl
var ChartFS embed.FS

var AppMgrImage string

var agentPermissionFiles = []string{
	// role with RBAC rules to access resources on hub
	"manifests/permission/role.yaml",
	// rolebinding to bind the above role to a certain user group
	"manifests/permission/rolebinding.yaml",
}

type GlobalValues struct {
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,"`
	ImagePullSecret string            `json:"imagePullSecret"`
	ImageOverrides  map[string]string `json:"imageOverrides,"`
	NodeSelector    map[string]string `json:"nodeSelector,"`
	ProxyConfig     map[string]string `json:"proxyConfig,"`
}

type Values struct {
	OnHubCluster bool         `json:"onHubCluster,"`
	GlobalValues GlobalValues `json:"global,"`
}

func getValue(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	addonValues := Values{
		OnHubCluster: false,
		GlobalValues: GlobalValues{
			ImagePullPolicy: corev1.PullIfNotPresent,
			ImagePullSecret: "open-cluster-management-image-pull-credentials",
			ImageOverrides: map[string]string{
				"multicluster_operators_subscription": AppMgrImage,
			},
			NodeSelector: map[string]string{},
			ProxyConfig: map[string]string{
				"HTTP_PROXY":  "",
				"HTTPS_PROXY": "",
				"NO_PROXY":    "",
			},
		},
	}

	labels := cluster.GetLabels()
	if labels["local-cluster"] == "true" {
		addonValues.OnHubCluster = true
	}

	return addonfactory.JsonStructToValues(addonValues)
}

func newRegistrationOption(kubeClient *kubernetes.Clientset, addonName string) *agent.RegistrationOption {
	return &agent.RegistrationOption{
		CSRConfigurations: agent.KubeClientSignerConfigurations(addonName, addonName),
		CSRApproveCheck:   utils.DefaultCSRApprover(addonName),
		PermissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
			for _, file := range agentPermissionFiles {
				if err := applyManifestFromFile(file, cluster.Name, addon.Name, kubeClient); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

//nolint
func applyManifestFromFile(file, clusterName, addonName string, kubeClient *kubernetes.Clientset) error {
	groups := agent.DefaultGroups(clusterName, addonName)
	config := struct {
		ClusterName string
		Group       string
	}{
		ClusterName: clusterName,
		Group:       groups[0],
	}

	recorder := events.NewInMemoryRecorder("")
	results := resourceapply.ApplyDirectly(context.Background(),
		resourceapply.NewKubeClientHolder(kubeClient),
		recorder,
		resourceapply.NewResourceCache(),
		func(name string) ([]byte, error) {
			template, err := ChartFS.ReadFile(file)
			if err != nil {
				return nil, err
			}
			return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
		},
		file,
	)

	for _, result := range results {
		if result.Error != nil {
			return result.Error
		}
	}

	return nil
}

func NewAddonManager(kubeConfig *rest.Config, agentImage string, agentInstallAllStrategy bool) (addonmanager.AddonManager, error) {
	AppMgrImage = agentImage

	addonMgr, err := addonmanager.New(kubeConfig)
	if err != nil {
		klog.Errorf("unable to setup addon manager: %v", err)
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		klog.Errorf("unable to create kube client: %v", err)
		return addonMgr, err
	}

	agentFactory := addonfactory.NewAgentAddonFactory(AppMgrAddonName, ChartFS, ChartDir).
		WithGetValuesFuncs(getValue, addonfactory.GetValuesFromAddonAnnotation).
		WithAgentRegistrationOption(newRegistrationOption(kubeClient, AppMgrAddonName))

	if agentInstallAllStrategy {
		agentFactory.WithInstallStrategy(&agent.InstallStrategy{
			Type: agent.InstallAll,
		})
	}

	agentAddon, err := agentFactory.BuildHelmAgentAddon()
	if err != nil {
		klog.Errorf("failed to build agent %v", err)
		return addonMgr, err
	}

	err = addonMgr.AddAgent(agentAddon)

	return addonMgr, err
}
