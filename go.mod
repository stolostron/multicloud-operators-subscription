module github.com/open-cluster-management/multicloud-operators-subscription

go 1.13

require (
	github.com/MakeNowJust/heredoc v0.0.0-20171113091838-e9091a26100e // indirect
	github.com/aws/aws-sdk-go-v2 v0.18.0
	github.com/blang/semver v3.5.1+incompatible
	github.com/bugsnag/bugsnag-go v1.5.0 // indirect
	github.com/bugsnag/panicwrap v1.2.0 // indirect
	github.com/docker/go-metrics v0.0.0-20181218153428-b84716841b82 // indirect
	github.com/docker/libtrust v0.0.0-20160708172513-aabc10ec26b7 // indirect
	github.com/docker/spdystream v0.0.0-20181023171402-6480d4af844c // indirect
	github.com/emicklei/go-restful v2.11.1+incompatible // indirect
	github.com/garyburd/redigo v1.6.0 // indirect
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/go-openapi/spec v0.19.4
	github.com/gofrs/uuid v3.2.0+incompatible // indirect
	github.com/golangci/golangci-lint v1.20.0 // indirect
	github.com/google/go-cmp v0.4.0
	github.com/google/go-github/v28 v28.1.1
	github.com/gorilla/handlers v1.4.0 // indirect
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/johannesboyne/gofakes3 v0.0.0-20200218152459-de0855a40bc1
	github.com/mattn/go-colorable v0.1.4 // indirect
	github.com/onsi/ginkgo v1.12.0
	github.com/onsi/gomega v1.9.0
	github.com/open-cluster-management/multicloud-operators-channel v1.0.0-2020-05-12-22-38-29.0.20200514134758-a7e10a29ec07
	github.com/open-cluster-management/multicloud-operators-deployable v0.0.0-20200513210312-92eab4faeb2b
	github.com/open-cluster-management/multicloud-operators-placementrule v1.0.0-2020-05-12-22-38-29.0.20200513204034-766ba50d9664
	github.com/open-cluster-management/multicloud-operators-subscription-release v1.0.1-2020-05-28-18-29-00.0.20200603130746-0d0667fb4e33
	github.com/opencontainers/runc v1.0.0-rc2.0.20190611121236-6cc515888830 // indirect
	github.com/operator-framework/operator-sdk v0.17.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.9.1
	github.com/sabhiram/go-gitignore v0.0.0-20180611051255-d3107576ba94
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.5.1
	github.com/yvasiyarov/go-metrics v0.0.0-20150112132944-c25f46c4b940 // indirect
	github.com/yvasiyarov/gorelic v0.0.6 // indirect
	golang.org/x/net v0.0.0-20200226121028-0de0cce0169b
	gopkg.in/src-d/go-git.v4 v4.13.1
	helm.sh/helm/v3 v3.2.0
	k8s.io/api v0.18.0
	k8s.io/apiextensions-apiserver v0.18.0
	k8s.io/apimachinery v0.18.0
	k8s.io/cli-runtime v0.18.0
	k8s.io/client-go v13.0.0+incompatible
	k8s.io/helm v2.16.3+incompatible
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20200121204235-bf4fb3bd569c
	sigs.k8s.io/controller-runtime v0.5.2
	sigs.k8s.io/kustomize v2.0.3+incompatible
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/client-go => k8s.io/client-go v0.17.4 // Required by prometheus-operator
)
