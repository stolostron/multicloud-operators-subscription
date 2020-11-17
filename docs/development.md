# Development Guide

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Development Guide](#development-guide)
    - [Required tools/Binaries](#required-tools/binaries)
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
