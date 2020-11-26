module github.com/open-cluster-management/multicloud-operators-subscription

go 1.15

require (
	github.com/MakeNowJust/heredoc v0.0.0-20171113091838-e9091a26100e // indirect
	github.com/aws/aws-sdk-go-v2 v0.18.0
	github.com/blang/semver v3.5.1+incompatible
	github.com/docker/spdystream v0.0.0-20181023171402-6480d4af844c // indirect
	github.com/emicklei/go-restful v2.11.1+incompatible // indirect
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/go-logr/logr v0.3.0
	github.com/go-openapi/spec v0.19.5
	github.com/google/go-cmp v0.5.1
	github.com/google/go-github/v32 v32.1.0
	github.com/johannesboyne/gofakes3 v0.0.0-20200218152459-de0855a40bc1
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/open-cluster-management/ansiblejob-go-lib v0.1.12
	github.com/open-cluster-management/api v0.0.0-20200903203421-64b667f5455c
	github.com/open-cluster-management/multicloud-operators-channel v1.0.1-0.20201103204539-9d9004e1f5e6
	github.com/open-cluster-management/multicloud-operators-deployable v0.0.0-20200817152717-50b364f569c6
	github.com/open-cluster-management/multicloud-operators-placementrule v1.0.1-2020-06-08-14-28-27.0.20200817152733-689893a33a7f
	github.com/open-cluster-management/multicloud-operators-subscription-release v0.2.2-pre
	github.com/operator-framework/operator-sdk v0.18.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.10.0
	github.com/sabhiram/go-gitignore v0.0.0-20180611051255-d3107576ba94
	github.com/spf13/pflag v1.0.5
	golang.org/x/net v0.0.0-20200822124328-c89045814202
	gopkg.in/src-d/go-git.v4 v4.13.1
	helm.sh/helm/v3 v3.4.1
	k8s.io/api v0.19.4
	k8s.io/apiextensions-apiserver v0.19.3
	k8s.io/apimachinery v0.19.4
	k8s.io/cli-runtime v0.19.4
	k8s.io/client-go v13.0.0+incompatible
	k8s.io/helm v2.17.0+incompatible
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20201113171705-d219536bb9fd
	sigs.k8s.io/controller-runtime v0.6.4
	sigs.k8s.io/kustomize/api v0.6.0
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	github.com/go-logr/logr => github.com/go-logr/logr v0.2.1
	k8s.io/client-go => k8s.io/client-go v0.19.0
)
