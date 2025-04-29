package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E suite")
}

var (
	managedClusterName string
	hubKubeClient      kubernetes.Interface
	hubAddOnClient     addonclient.Interface
	hubClusterClient   clusterclient.Interface
	hubWorkClient      workclient.Interface
	clusterCfg         *rest.Config
)

var _ = ginkgo.Describe("E2E", func() {
	ginkgo.It(`ginkgo v2 no longer runs BeforeSuite if all tests are skipped 
or do not exist`, func() {})
})

// This suite is sensitive to the following environment variables:
//
// - MANAGED_CLUSTER_NAME sets the name of the cluster
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = ginkgo.BeforeSuite(func() {
	kubeconfig := os.Getenv("KUBECONFIG")
	managedClusterName = os.Getenv("MANAGED_CLUSTER_NAME")
	if managedClusterName == "" {
		managedClusterName = "cluster1"
	}
	err := func() error {
		var err error
		clusterCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err
		}

		hubKubeClient, err = kubernetes.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubAddOnClient, err = addonclient.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubWorkClient, err = workclient.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubClusterClient, err = clusterclient.NewForConfig(clusterCfg)

		return err
	}()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	var csrs *certificatesv1.CertificateSigningRequestList
	// Waiting for the CSR for ManagedCluster to exist
	err = wait.PollUntilContextTimeout(context.TODO(), 1*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		var err error
		csrs, err = hubKubeClient.CertificatesV1().CertificateSigningRequests().List(
			context.TODO(), metav1.ListOptions{LabelSelector: fmt.Sprintf(
				"open-cluster-management.io/cluster-name = %v", managedClusterName),
			})
		if err != nil {
			return false, err
		}

		if len(csrs.Items) >= 1 {
			return true, nil
		}

		return false, nil
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	// Approving all pending CSRs
	for i := range csrs.Items {
		csr := &csrs.Items[i]
		if !strings.HasPrefix(csr.Name, managedClusterName) {
			continue
		}

		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			csr, err = hubKubeClient.CertificatesV1().CertificateSigningRequests().Get(
				context.TODO(), csr.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			csr.Status.Conditions = append(
				csr.Status.Conditions,
				certificatesv1.CertificateSigningRequestCondition{
					Type:    certificatesv1.CertificateApproved,
					Status:  corev1.ConditionTrue,
					Reason:  "Approved by E2E",
					Message: "Approved as part of Loopback e2e",
				})
			_, err := hubKubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(
				context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
			return err
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	var managedCluster *clusterv1.ManagedCluster
	// Waiting for ManagedCluster to exist
	err = wait.PollUntilContextTimeout(context.TODO(), 1*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		var err error
		managedCluster, err = hubClusterClient.ClusterV1().ManagedClusters().Get(
			context.TODO(), managedClusterName, metav1.GetOptions{})

		if errors.IsNotFound(err) {
			return false, nil
		}

		if err != nil {
			return false, err
		}
		return true, nil
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	// Accepting ManagedCluster
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error
		managedCluster, err = hubClusterClient.ClusterV1().ManagedClusters().Get(
			context.TODO(), managedCluster.Name, metav1.GetOptions{})

		if err != nil {
			return err
		}

		managedCluster.Spec.HubAcceptsClient = true
		managedCluster.Spec.LeaseDurationSeconds = 5
		_, err = hubClusterClient.ClusterV1().ManagedClusters().Update(
			context.TODO(), managedCluster, metav1.UpdateOptions{})
		return err
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// Ensure cluster namespace exists
	err = wait.PollUntilContextTimeout(context.TODO(), 1*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		var err error
		_, err = hubKubeClient.CoreV1().Namespaces().Get(
			context.TODO(), managedClusterName, metav1.GetOptions{},
		)

		if errors.IsNotFound(err) {
			return false, nil
		}

		if err != nil {
			return false, err
		}

		return true, nil
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// Ensure managed cluster Available
	err = wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 300*time.Second, true, func(ctx context.Context) (bool, error) {
		var err error
		managedCluster, err = hubClusterClient.ClusterV1().ManagedClusters().Get(
			context.TODO(), managedClusterName, metav1.GetOptions{})

		if err != nil {
			return false, err
		}

		if !meta.IsStatusConditionTrue(managedCluster.Status.Conditions, "ManagedClusterConditionAvailable") {
			return false, nil
		}

		return true, nil
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// In the new addon-framework, it is required to create the application-manager clustermanagementAddon
	// Or just creating managedClusterAddon won't generate the addon manifestwork as expected.
	cma := &addonapiv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: "application-manager",
		},
		Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
			AddOnMeta: addonapiv1alpha1.AddOnMeta{
				Description: "Synchronizes application on the managed clusters from the hub",
				DisplayName: "Application Manager",
			},
			InstallStrategy: addonapiv1alpha1.InstallStrategy{
				Type: "Manual",
			},
		},
	}

	_, err = hubAddOnClient.AddonV1alpha1().ClusterManagementAddOns().Create(
		context.TODO(), cma, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// Then create the managedClusterAddon, expect the addon manifestwork to be generated in the cluster NS
	addon := &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "application-manager",
			Namespace: managedClusterName,
		},
		Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
			InstallNamespace: "open-cluster-management-agent-addon",
		},
	}

	_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(
		context.TODO(), addon, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 600*time.Second, true, func(ctx context.Context) (bool, error) {
		var err error
		addon, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(
			context.TODO(), "application-manager", metav1.GetOptions{})

		klog.Infof("addon: %#v", addon.Status)

		if err != nil {
			return false, err
		}

		appAddonManifestWorks, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).List(
			context.TODO(), metav1.ListOptions{})

		klog.Infof("appAddonManifestWorks items: %v, err: %v", len(appAddonManifestWorks.Items), err)

		if err != nil {
			return false, err
		}

		if len(appAddonManifestWorks.Items) > 0 {
			appAddonManifestWork, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(
				context.TODO(), "addon-application-manager-deploy-0", metav1.GetOptions{})

			klog.Infof("app Addon ManifestWork created. status: %#v", appAddonManifestWork.Status)

			if err != nil {
				return false, err
			}

			return true, nil
		}

		if !meta.IsStatusConditionTrue(addon.Status.Conditions, "Available") {
			return false, nil
		}

		return true, nil
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
