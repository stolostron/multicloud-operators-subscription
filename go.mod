module github.com/open-cluster-management/multicloud-operators-subscription

go 1.15

require (
	github.com/aws/aws-sdk-go-v2 v0.18.0
	github.com/blang/semver v3.5.1+incompatible
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/go-logr/logr v0.3.0
	github.com/go-openapi/spec v0.19.5
	github.com/google/go-cmp v0.5.1
	github.com/google/go-github/v32 v32.1.0
	github.com/johannesboyne/gofakes3 v0.0.0-20200218152459-de0855a40bc1
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/open-cluster-management/ansiblejob-go-lib v0.1.12
	github.com/open-cluster-management/api v0.0.0-20200623215229-19a96fed707a
	github.com/open-cluster-management/multicloud-operators-channel v1.0.1-0.20200813201238-cf522bb8e5a9
	github.com/open-cluster-management/multicloud-operators-deployable v0.0.0-20200817152717-50b364f569c6
	github.com/open-cluster-management/multicloud-operators-placementrule v1.0.1-2020-06-08-14-28-27.0.20200817152733-689893a33a7f
	github.com/open-cluster-management/multicloud-operators-subscription-release v1.0.1-2020-06-08-14-28-27.0.20210104195656-6905d123e0ed
	github.com/operator-framework/operator-sdk v0.18.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.10.0
	github.com/sabhiram/go-gitignore v0.0.0-20180611051255-d3107576ba94
	github.com/spf13/pflag v1.0.5
	golang.org/x/net v0.0.0-20200822124328-c89045814202
	gopkg.in/src-d/go-git.v4 v4.13.1
	helm.sh/helm/v3 v3.4.1
	k8s.io/api v0.19.3
	k8s.io/apiextensions-apiserver v0.19.3
	k8s.io/apimachinery v0.19.3
	k8s.io/cli-runtime v0.19.3
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/helm v2.16.3+incompatible
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20200805222855-6aeccd4b50c6
	sigs.k8s.io/controller-runtime v0.6.3
	sigs.k8s.io/kustomize/api v0.6.0
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/client-go => k8s.io/client-go v0.19.3
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.6.2
)
