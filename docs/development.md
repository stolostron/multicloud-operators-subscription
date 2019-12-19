# Development Guide

If you would like to contribute to Multicloud-Operators projects, this guide will help you get started.

## Before you start

--------------------

### Developer Certificate of Origin

All Multicloud-Operators repositories built with [probot](https://github.com/probot/probot) that enforces the [Developer Certificate of Origin](https://developercertificate.org/) (DCO) on Pull Requests. It requires all commit messages to contain the `Signed-off-by` line with an email address that matches the commit author.

### Contributing A Patch

1. Submit an issue describing your proposed change to the repo in question.
1. The [repo owners](OWNERS) will respond to your issue promptly.
1. Fork the desired repository, develop and test your code changes.
1. Commit your changes with DCO to the forked repository
1. Submit a pull request to main repository

### Issue and Pull Request Management

Anyone may comment on issues and submit reviews for pull requests. However, in order to be assigned an issue or pull request, you must be a member of the [IBM](https://github.com/ibm) GitHub organization.

Repo maintainers can assign you an issue or pull request by leaving a `/assign <your Github ID>` comment on the issue or pull request.

### Required tools/Binaries

Multcloud-Operators projects are built with following tools, use following links to get them installed in your env.

Kubernetes:

- Version 1.13+. Use following [link](https://kubernetes.io/docs/setup/#learning-environment) to find an environment or setup one.

Build:

- [go v1.13+](https://golang.org/dl/)

- [operator-sdk v0.10.0](https://github.com/operator-framework/operator-sdk/blob/master/doc/user/install-operator-sdk.md#install-the-operator-sdk-cli)

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

## Build & Run

--------------------

Set following variables to build your local image

- `GO111MODULE=on` to enable go module
- `GIT_HOST=<your personal org>` override the org from main repo. e.g: `github.com/<your_account>`
- `BUILD_LOCALLY=1` to build locally
- `REGISTRY=<your registry>` to set registry of your image, default is `quay.io/multicloudlab`
- `IMG=<you image name>` to set your image name, tags are generated automatically, default name is `multicloud-operators-subscription`

Use `make` to lint, test, and build your images locally. Official image built from master branch is pushed to quay.io/multicloudlab/multicloud-operators-subscription.

Multicloud-Operators repositories follow general [operator-sdk practice](https://github.com/operator-framework/operator-sdk/blob/master/doc/user-guide.md#build-and-run-the-operator) to run the operator.

Before running the operator, required CRDs must be registered with Kubernetes apiserver:

```shell
% kubectl apply -f deploy/crds
```

Once this is done, there are 2 ways to run the operator

- As a go program in development environment outside a Kubernetes cluster
- As a Deployment inside a Kubernetes cluster

### Run locally outside the cluster

Use following operator-sdk command to launch operator locally.

```shell
export OPERATOR_NAME=multicloud-operators-subscription
% operator-sdk up local
```

### Run as a Deployment inside the cluster

Use the following kubectl command to launch operator as a deployment in a Kubernetes cluster.

```shell
% kubectl apply -f deploy
```

Verify the deployment and pods with following command:

```shell
% kubectl get deploy,pods -l name=multicloud-operators-subscription
NAME                                              READY     UP-TO-DATE   AVAILABLE   AGE
deployment.apps/multicloud-operators-subscription   1/1       1            1           15m

NAME                                                   READY     STATUS    RESTARTS   AGE
pod/multicloud-operators-subscription-78c9874dff-f64pg   1/1       Running   0          15m

```
