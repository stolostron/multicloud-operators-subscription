# Getting Started

## Prerequisite

- git
- go version v1.13+.
- operatorsdk version v0.10.0
- kubebuilder version v1.0.8
- Linting Tools
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
    - goimports - `go get -v golang.org/x/tools/cmd/goimports`
- Setup GIT_HOST to override the setting for your custom path (e.g. `github.com/<myaccount>`)

## Build subscription

## Build and run the operator

### Run as deployment inside the cluster

```shell

cd $GOPATH/src/github.com/IBM/multicloud-operators-subscription

# Setup environemnt for subscription operator
# Set service account
kubectl apply -f deploy/service_account.yaml
# Set role and role binding in working namespace
kubectl create -f deploy/role.yaml
kubectl create -f deploy/role_binding.yaml
# Deploy subscrition operator
```

### Run locally outside the cluster
