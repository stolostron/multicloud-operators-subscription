module github.com/IBM/multicloud-operators-subscription

require (
	github.com/IBM/multicloud-operators-channel v0.0.0-20191018155400-51cc80e57e62
	github.com/IBM/multicloud-operators-deployable v0.0.0-20191018152900-19f89453db82
	github.com/IBM/multicloud-operators-placementrule v0.0.0-20191018144800-6bbe25f1404c
	github.com/IBM/multicloud-operators-subscription-release v0.0.0-20191015202259-62d844a07129
	github.com/blang/semver v3.5.1+incompatible
	github.com/ghodss/yaml v1.0.0
	github.com/go-openapi/spec v0.19.0
	github.com/jstemmer/go-junit-report v0.9.1 // indirect
	github.com/onsi/gomega v1.4.3
	github.com/operator-framework/operator-sdk v0.10.0
	github.com/prometheus/common v0.2.0
	github.com/spf13/pflag v1.0.3
	golang.org/x/net v0.0.0-20190724013045-ca1201d0de80
	k8s.io/api v0.0.0-20190612125737-db0771252981
	k8s.io/apiextensions-apiserver v0.0.0-20190228180357-d002e88f6236
	k8s.io/apimachinery v0.0.0-20190612125636-6a5db36e93ad
	k8s.io/apiserver v0.0.0-20181213151703-3ccfe8365421
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/helm v2.13.1+incompatible
	k8s.io/klog v0.3.1
	k8s.io/kube-openapi v0.0.0-20190603182131-db7b694dc208
	sigs.k8s.io/controller-runtime v0.1.12
	sigs.k8s.io/controller-tools v0.1.10
)

// Pinned to kubernetes-1.13.4
replace (
	k8s.io/api => k8s.io/api v0.0.0-20190222213804-5cb15d344471
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190228180357-d002e88f6236
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190221213512-86fb29eff628
	k8s.io/apiserver => k8s.io/apiserver v0.0.0-20190228174905-79427f02047f
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.0.0-20190228180923-a9e421a79326
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190228174230-b40b2a5939e4
	k8s.io/code-generator => k8s.io/code-generator v0.0.0-20181117043124-c2090bec4d9b
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20190228175259-3e0149950b0e
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20180711000925-0cf8f7e6ed1d
	k8s.io/kubernetes => k8s.io/kubernetes v1.13.4
)

replace (
	git.apache.org/thrift.git => github.com/apache/thrift v0.0.0-20180902110319-2566ecd5d999
	github.com/coreos/prometheus-operator => github.com/coreos/prometheus-operator v0.29.0
	k8s.io/kube-state-metrics => k8s.io/kube-state-metrics v1.6.0
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.1.12
	sigs.k8s.io/controller-tools => sigs.k8s.io/controller-tools v0.1.11-0.20190411181648-9d55346c2bde
)

go 1.13
