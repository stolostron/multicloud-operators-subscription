package addon

import (
	"context"
	"embed"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

	AgentImageEnv = "OPERAND_IMAGE_MULTICLUSTER_OPERATORS_SUBSCRIPTION"
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
	OnHubCluster      bool         `json:"onHubCluster,"`     // single hub cluster
	OnMulticlusterHub bool         `json:"onMulticlusterHub"` // regional hub cluster
	GlobalValues      GlobalValues `json:"global,"`
}

func getValue(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	addonValues := Values{
		OnHubCluster:      false,
		OnMulticlusterHub: false,
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

	annotations := addon.GetAnnotations()

	// set OnMulticlusterHub to true for regional hub clusters, so that 3 addon crds won't be cleaned up when the regional hub is detached.
	if val, ok := annotations["addon.open-cluster-management.io/on-multicluster-hub"]; ok {
		if strings.EqualFold(val, "true") {
			addonValues.OnMulticlusterHub = true
		}
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
		klog.Errorf("unable to kube client: %v", err)

		return "", err
	}

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"ocm-configmap-type": "image-manifest",
		},
	}

	mchSelector, err := appsubutils.ConvertLabels(labelSelector)
	if err != nil {
		klog.Error("failed to convert label", err)

		return "", err
	}

	mchImageCMList := &corev1.ConfigMapList{}

	err = kubeClient.List(context.TODO(), mchImageCMList, &client.ListOptions{LabelSelector: mchSelector})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Info("no mch configmap found - not ACM")

			return "", nil
		}

		klog.Error(err, "failed to get configmap for MCH images")

		return "", err
	}

	var image string

	var newestVersion *semver.Version

	for _, cm := range mchImageCMList.Items {
		_version := cm.Labels["ocm-release-version"]
		if _version == "" {
			continue
		}

		currentVersion, err := semver.NewVersion(_version)
		if err != nil {
			klog.Infof("invalid ocm-release-version %v in MCH configmap: %v", _version, cm.Name)

			continue
		}

		if newestVersion == nil || newestVersion.LessThan(currentVersion) {
			currentImage := cm.Data["multicluster_operators_subscription"]
			if currentImage == "" {
				klog.Infof("appsub image not found in MCH config map: %v", cm.Name)
			} else {
				newestVersion = currentVersion
				image = currentImage
			}
		}
	}

	klog.Infof("MCH appsubimage: %v", image)

	return image, nil
}
