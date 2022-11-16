package addon

import (
	"context"
	"embed"
	"fmt"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	appsubutils "open-cluster-management.io/multicloud-operators-subscription/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AppMgrAddonName = "application-manager"

	ChartDir = "manifests/chart"

	AgentImageEnv      = "OPERAND_IMAGE_MULTICLUSTER_OPERATORS_SUBSCRIPTION"
	OperatorVersionEnv = "OPERATOR_VERSION"
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

func toAddonResources(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	type resource struct {
		Memory string `json:"memory"`
	}

	type resources struct {
		Requests resource `json:"requests"`
		Limits   resource `json:"limits"`
	}

	jsonStruct := struct {
		Resources resources `json:"resources"`
	}{
		Resources: resources{
			Requests: resource{
				Memory: "128Mi",
			},
			Limits: resource{
				Memory: "2Gi",
			},
		},
	}

	for _, variable := range config.Spec.CustomizedVariables {
		if variable.Name == "RequestMemory" {
			jsonStruct.Resources.Requests.Memory = variable.Value
		}

		if variable.Name == "LimitsMemory" {
			jsonStruct.Resources.Limits.Memory = variable.Value
		}
	}

	values, err := addonfactory.JsonStructToValues(jsonStruct)
	if err != nil {
		return nil, err
	}

	return values, nil
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

	addonClient, err := addonv1alpha1client.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		klog.Errorf("unable to create kube client: %v", err)
		return addonMgr, err
	}

	addonGetter := addonfactory.NewAddOnDeloymentConfigGetter(addonClient)

	agentFactory := addonfactory.NewAgentAddonFactory(AppMgrAddonName, ChartFS, ChartDir).
		// register the supported configuration types
		WithConfigGVRs(
			schema.GroupVersionResource{Group: "addon.open-cluster-management.io", Version: "v1alpha1", Resource: "addondeploymentconfigs"},
		).
		WithGetValuesFuncs(
			getValue,
			addonfactory.GetValuesFromAddonAnnotation,
			// get the AddOnDeloymentConfig object and transform nodeSelector and toleration defined in spec.NodePlacement to Values object
			addonfactory.GetAddOnDeloymentConfigValues(
				addonGetter,
				addonfactory.ToAddOnNodePlacementValues,
			),
			// get the AddOnDeloymentConfig object and transform request/limit memory defined in Spec.CustomizedVariables to Values object
			addonfactory.GetAddOnDeloymentConfigValues(
				addonGetter,
				toAddonResources,
			),
		).
		WithAgentRegistrationOption(newRegistrationOption(kubeClient, AppMgrAddonName))

	if agentInstallAllStrategy {
		agentFactory.WithInstallStrategy(agent.InstallAllStrategy("open-cluster-management-agent-addon"))
	}

	agentAddon, err := agentFactory.BuildHelmAgentAddon()
	if err != nil {
		klog.Errorf("failed to build agent %v", err)
		return addonMgr, err
	}

	err = addonMgr.AddAgent(agentAddon)

	return addonMgr, err
}

func GetMchImage(kubeConfig *rest.Config) (string, error) {
	kubeClient, err := client.New(kubeConfig, client.Options{})
	if err != nil {
		klog.Errorf("Unable to kube client: %v", err)

		return "", err
	}

	_, found := os.LookupEnv(AgentImageEnv)
	if !found {
		klog.Info("ACM not detected")

		return "", nil
	}

	klog.V(2).Info("ACM detected, wait for MCH config map")

	// Get ACM version
	acmVersion, found := os.LookupEnv(OperatorVersionEnv)
	if !found {
		err = fmt.Errorf("%v env var not found", OperatorVersionEnv)
		klog.Error(err)

		return "", err
	}

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"ocm-configmap-type":  "image-manifest",
			"ocm-release-version": acmVersion,
		},
	}

	mchSelector, err := appsubutils.ConvertLabels(labelSelector)
	if err != nil {
		klog.Error("Failed to convert label", err)

		return "", err
	}

	mchImageCMList := &corev1.ConfigMapList{}

	for count := 0; count < 60; count++ {
		klog.Infof("Waiting for MCH config map, count: %v", count)

		err = kubeClient.List(context.TODO(), mchImageCMList, &client.ListOptions{LabelSelector: mchSelector})
		if err != nil {
			klog.Error(err, "Failed to get configmap for MCH images")

			return "", err
		}

		if len(mchImageCMList.Items) > 0 {
			data := mchImageCMList.Items[0].Data

			image := data["multicluster_operators_subscription"]
			if image == "" {
				err = fmt.Errorf("appsub image not found in MCH config map")
				klog.Warning(err)

				return "", err
			}

			klog.Info("MCH appsubimage: %v", image)

			return image, nil
		}

		// Not found, sleep ...
		time.Sleep(10 * time.Second)
	}

	err = fmt.Errorf("timed-out waiting for MCH config map for ACM version %v", acmVersion)
	klog.Warning(err)

	return "", err
}
