module github.com/open-cluster-management/multicloud-operators-subscription

go 1.16

require (
	github.com/aws/aws-sdk-go-v2 v1.3.2
	github.com/aws/aws-sdk-go-v2/config v1.1.5
	github.com/aws/aws-sdk-go-v2/service/s3 v1.5.0
	github.com/blang/semver v3.5.1+incompatible
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/go-git/go-git/v5 v5.4.2
	github.com/go-logr/logr v0.4.0
	github.com/go-openapi/spec v0.19.5
	github.com/google/go-github/v32 v32.1.0
	github.com/johannesboyne/gofakes3 v0.0.0-20200218152459-de0855a40bc1
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.13.0
	github.com/open-cluster-management/api v0.0.0-20210513122330-d76f10481f05
	github.com/open-cluster-management/multicloud-operators-channel v1.2.4-0-20210817-0d8714a
	github.com/openshift/api v3.9.1-0.20190924102528-32369d4db2ad+incompatible
	github.com/operator-framework/operator-lib v0.5.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/sabhiram/go-gitignore v0.0.0-20180611051255-d3107576ba94
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	golang.org/x/crypto v0.0.0-20210421170649-83a5a9bb288b
	golang.org/x/net v0.0.0-20210428140749-89ef3d95e781
	gopkg.in/src-d/go-git.v4 v4.13.1
	helm.sh/helm/v3 v3.6.3
	k8s.io/api v0.21.3
	k8s.io/apiextensions-apiserver v0.21.3
	k8s.io/apimachinery v0.21.3
	k8s.io/cli-runtime v0.21.3
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20210305001622-591a79e4bda7
	sigs.k8s.io/controller-runtime v0.9.1
	sigs.k8s.io/kustomize/api v0.8.8
	sigs.k8s.io/wg-policy-prototypes v0.0.0-20210823142600-b09c9bb01d4c
)

replace (
	github.com/docker/distribution => github.com/docker/distribution v0.0.0-20191216044856-a8371794149d
	github.com/docker/docker => github.com/moby/moby v17.12.0-ce-rc1.0.20200618181300-9dc6525e6118+incompatible
	k8s.io/client-go => k8s.io/client-go v0.21.1

)
