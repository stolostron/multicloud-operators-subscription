# Getting Started

## Prerequisite

- Kubernetes v1.13+. Use following [link](https://kubernetes.io/docs/setup/#learning-environment) to find an environment or setup one.
- kubectl v1.11+ Use following [link](https://kubernetes.io/docs/tasks/tools/install-kubectl/) to download

## Build subscription

## Build and run the operator

### Run as deployment inside the cluster

```shell

cd $GOPATH/src/github.com/open-cluster-management/multicloud-operators-subscription

# Setup environemnt for subscription operator
# Set service account
kubectl apply -f deploy/service_account.yaml
# Set role and role binding in working namespace
kubectl create -f deploy/role.yaml
kubectl create -f deploy/role_binding.yaml
# Deploy subscrition operator
```

### Run locally outside the cluster
