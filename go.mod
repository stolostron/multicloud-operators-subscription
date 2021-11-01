module open-cluster-management.io/multicloud-operators-subscription

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
	open-cluster-management.io/addon-framework v0.0.0-20210624140015-b26f9964526c
	open-cluster-management.io/api v0.0.0-20210629235044-d779373b7f7d
	open-cluster-management.io/multicloud-operators-channel v0.4.1-0.20211019174739-f906eb7328a6
	sigs.k8s.io/controller-runtime v0.9.1
	sigs.k8s.io/kustomize/api v0.8.8
)

replace k8s.io/client-go => k8s.io/client-go v0.21.3
