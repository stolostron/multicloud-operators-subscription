# Development Guide

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Development Guide](#development-guide)
  - [Required tools/Binaries](#required-toolsbinaries)
  - [Develop locally](#develop-locally)
  - [Launch Dev mode](#launch-dev-mode)
  - [Build a local image](#build-a-local-image)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## Required tools/Binaries

Multcloud-Operators projects are built with following tools, use following links to get them installed in your env.

Kubernetes:

- Version 1.13+. Use following [link](https://kubernetes.io/docs/setup/#learning-environment) to find an environment or setup one.

Build:

- [go v1.13+](https://golang.org/dl/)

Lint:

- [hadolint](https://github.com/hadolint/hadolint#install)
- [shellcheck](https://github.com/koalaman/shellcheck#installing)
- [yamllint](https://github.com/adrienverge/yamllint#installation)
- [helm client](https://helm.sh/docs/using_helm/#install-helm)
- [golangci-lint](https://github.com/golangci/golangci-lint#install)
- [autopep8](https://github.com/hhatto/autopep8#installation)
- [mdl](https://github.com/markdownlint/markdownlint#installation)
- [awesome_bot](https://github.com/dkhamsing/awesome_bot#installation)
- [sass-lint](https://github.com/sds/scss-lint#installation)
- [tslint](https://github.com/palantir/tslint#installation--usage)
- [prototool](https://github.com/uber/prototool/blob/dev/docs/install.md)
- goimports -  Run `go get -v golang.org/x/tools/cmd/goimports` to install

Test:

- [kubebuilder v1.0.8](https://github.com/kubernetes-sigs/kubebuilder/releases/tag/v1.0.8)

## Develop locally

1. Clone the repository

```shell
git clone git@github.com:open-cluster-management/multicloud-operators-subscription.git
cd multicloud-operators-subscription
```

2. Login to your hub cluster. Then build the local image

```shell
make local
```

3. Verify that the image was built successfully. The `ls -l build/_output/bin` command should return following values

```shell
ls -l build/_output/bin 
-rwxr-xr-x  1 user  group  74786048 Jun 14 13:53 appsubsummary
-rwxr-xr-x  1 user  group  60232176 Jun 14 13:53 multicluster-operators-placementrule
-rwxr-xr-x  1 user  group  90153648 Jun 14 13:52 multicluster-operators-subscription
-rwxr-xr-x  1 user  group  52879056 Jun 14 13:52 uninstall-crd
```

4. Edit hub subscription pod to make it invalid, this is the prerequisite to start your local image

Find the hub-sub pod

```shell
oc get pods -n open-cluster-management | grep hub-sub 
multicluster-operators-hub-subscription-${random-number}  
```

Edit hub-sub to make it invalid

```shell
oc edit pod multicluster-operators-hub-subscription-${random-number} -n open-cluster-management
```

Search for the image name in the edit mode. Add -invalid to the end of the image name or anything to make it no longer valid. Check the pod status to make it crash (InvalidImageName)

5. As subscription deployment has many scenarios: deploy to local, deploy to managed clusters, etc. We have four ways to run the image locally.

- Start subscription manager on hub cluster

```shell
export WATCH_NAMESPACE=
export KUBECONFIG=~/.kube/kubeconfig.hub
build/_output/bin/multicluster-operators-subscription --alsologtostderr --sync-interval=60 --v=1 --debug
```

- Standalone subscription

```shell
export WATCH_NAMESPACE=
export KUBECONFIG=~/.kube/kubeconfig.hub
build/_output/bin/multicluster-operators-subscription --alsologtostderr --standalone --sync-interval=60 --v=1
```

- Start subscription manager on local managed cluster (hub cluster is managed cluster)

```shell
export WATCH_NAMESPACE=
$ export KUBECONFIG=~/.kube/kubeconfig.hub
build/_output/bin/multicluster-operators-subscription --alsologtostderr --v=1 \
--hub-cluster-configfile=/Users/youruser/.kube/kubeconfig.hub \
--sync-interval=60 \
--cluster-name=local-cluster --debug
```


- Start subscription manager on remote managed cluster

```shell
export WATCH_NAMESPACE= 
export KUBECONFIG=~/.kube/kubeconfig.managed
build/_output/bin/multicluster-operators-subscription --alsologtostderr --v=1 \
--hub-cluster-configfile=/Users/philipwu/.kube/kubeconfig.hub \
--sync-interval=60 \
--cluster-name=managed-cluster-1 \
--cluster-namespace=managed-cluster-1 
```

## Launch Dev mode

```shell
git clone git@github.com:open-cluster-management/multicloud-operators-subscription.git
cd multicloud-operators-subscription
export GITHUB_USER=<github_user>
export GITHUB_TOKEN=<github_token>
make
make build
./build/_output/bin/multicluster-operators-subscription
```

## Build a local image

```shell
git clone git@github.com:open-cluster-management/multicloud-operators-subscription.git
cd multicloud-operators-subscription
export GITHUB_USER=<github_user>
export GITHUB_TOKEN=<github_token>
make
make build-images
```
