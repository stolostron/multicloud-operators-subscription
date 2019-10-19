# Getting Started

## Prerequisite

- git
- go version v1.13+.
- operatorsdk version v0.10.0
- kubebuilder version v1.0.8
- Linting Tools: hadolint, shellcheck, yamllint, helm, golangci-lint, autopep8, mdl, awesome_bot, sass-lint, tslint, prototool, goimports

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
