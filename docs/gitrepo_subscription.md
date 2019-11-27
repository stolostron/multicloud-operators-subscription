# GitHub repository channel subscription

You can subscribe to public or enterprise GitHub repositories containing kubernetes resource YAMLs and/or Helm charts. This document gives examples of connecting to a GitHub repository through a channel and subscribing to kubernetes resources and helm charts from the GitHub repository.

## Prerequisite

You should have a kubernetes cluster and this subscription operator running.

## Subscribing to a Helm chart from a public GitHub repository

In this example, we are going to create a channel that connects to a public IBM GitHub repository and subscribe to MongoDB helm chart.

1. Clone this subscription operator GitHub repository.
1. In the cloned repository root, run `kubectl apply -f ./examples/github-channel/00-namespace.yaml` to create a namespace. This creates namespace `ibmcharts`.
1. Run `kubectl apply -f ./examples/github-channel/01-channel.yaml` to create `ibm-charts-github` channel in `ibmcharts` namespace.

    ```yaml
    apiVersion: app.ibm.com/v1alpha1
    kind: Channel
    metadata:
    name: ibm-charts-github
    namespace: ibmcharts
    spec:
        type: GitHub
        pathname: https://github.com/IBM/charts.git
    ```

    `pathname` is the GitHub repository HTTPS URL.

1. Run `kubectl apply -f ./examples/github-channel/02-subscription.yaml` to subscribe to `ibm-charts-github` channel. Take a look at `./examples/github-channel/02-subscription.yaml`. `spec.packageFilter.filterRef` references this config map.

    ```yaml
    apiVersion: v1
    kind: ConfigMap
    metadata:
    name: ibm-mongodb-dev-cm
    data:
        path: stable/ibm-mongodb-dev
    ```

    `data.path` in this config map indicates that the subcription subscribes to all helm charts and kubernetes resources in `stable/ibm-mongodb-dev` directory of the GitHub repository that the channel
1. Run `kubectl patch subscriptions.app.ibm.com github-mongodb-subscription --type='json' -p='[{"op": "replace", "path": "/spec/placement/local", "value": true}]'` to place the subscribed items into the local cluster. After a couple of minutes, run `kubectl get helmrelease.app.ibm.com --all-namespaces` to check a helmrelease.app.ibm.com CR is created for the MongoDB helm chart. Also run `kubectl get deployments` in the same namespace as the MongoDB helmrelease.app.ibm.com CR to find the deployment.

## Subscribing to Kubernetes resources from a GitHub repository

In this example, we are going to create a channel that connects to this GitHub repository and subscribe to the sample nginx deployment YAML `examples/github-channel/sample-deployment.yaml`.

1. Clone this subscription operator GitHub repository.
1. Run `kubectl apply -f ./examples/github-channel/10-namespace.yaml` to create `kuberesources` namespace.
1. Run `kubectl apply -f ./examples/github-channel/11-channel.yaml` to create `sample-kube-resources-github` channel in `kuberesources` namespace.

    ```yaml
    apiVersion: app.ibm.com/v1alpha1
    kind: Channel
    metadata:
      name: sample-kube-resources-github
      namespace: kuberesources
    spec:
        type: GitHub
        pathname: https://github.com/IBM/multicloud-operators-subscription.git
    ```

    `pathname` is the GitHub repository HTTPS URL.

1. Run `kubectl apply -f ./examples/github-channel/12-subscription.yaml` to subscribe to `sample-kube-resources-github` channel. Take a look at `./examples/github-channel/12-subscription.yaml`. `spec.packageFilter.filterRef` references this config map.

    ```yaml
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: resource-filter-configmap
    data:
        path: examples/github-channel
    ```

    `data.path` in this config map indicates that the subcription subscribes to all helm charts and kubernetes resource in `stable/ibm-mongodb-dev` directory of the GitHub repository that the channel points to. In examples/github-channel, there are multiple YAML files but only `sample-deployment.yaml` will be applied because the `.kubernetesignore` file in the directory defined by `path` indicates to ignore all files except `sample-deployment.yaml`. The subscription will only apply this file to the cluster.
1. Run `kubectl patch subscriptions.app.ibm.com sample-kube-resources-subscription --type='json' -p='[{"op": "replace", "path": "/spec/placement/local", "value": true}]'` to place the subscribed items into the local cluster. After a couple of minutes, run `kubectl get deployment --all-namespaces` to check `sample-nginx-deployment` deployment is created.

## Subscribing to a Helm chart from an enterprise GitHub repository requiring authentication

In the previous example, the GitHub repository that the channel connects to is a public repository so it does not require authentication. If a GitHub repository requires authentication, you need to associate a channel with a kubernetes secret. Currently the `channel` and `subscription` supports only basic authentication. Set `user` with a GitHub user ID and `accessToken` with a GitHub personal access token.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-github-secret
  namespace: ibmcharts
data:
  user: dXNlcgo=
  accessToken: cGFzc3dvcmQK
---
apiVersion: app.ibm.com/v1alpha1
kind: Channel
metadata:
  name: ibm-charts-github
  namespace: ibmcharts
spec:
    type: GitHub
    pathname: https://github.com/IBM/charts.git
    secretRef:
      name: my-github-secret
```

## .kubernetesignore file

In a GitHub repository root or in the `data.path` directory which is specified in the config map of `spec.packageFilter.filterRef` described above, you can have `.kubernetesignore` file to specify patterns of files and/or subdirectories to ignore when the subscription processes and applies Kubernetes resource from the repository. You can use the `.kubernetesignore` as fine-grain filters to selectively apply Kubernetes resources. The pattern format of the `.kubernetesignore` is the same as `.gitignore`. If `data.path` is not defined in the config map of `spec.packageFilter.filterRef`, the subscription looks for `.kubernetesignore` in the reporitory root. If `data.path` is defined, it looks for `.kubernetesignore` in the `data.path` directory. It currently does not support `.kubernetesignore` in any other directory.

## Subscribing to a specific branch

This subscription operator subscribes to `master` branch of the GitHub repository by default. If you want to subscribe to a different branch, specify the branch name in the configmap of `spec.packageFilter.filterRef`. For example,

    ```yaml
    apiVersion: v1
    kind: ConfigMap
    metadata:
    name: ibm-mongodb-dev-cm
    data:
        path: stable/ibm-mongodb-dev
        branch: mybranch
    ```

## Limitations

* If subscribing to a Kubernetes resource configuration YAML files, one YAML should contain only one Kubernetes resource configuration. If there are multiple Kubernetes resource configurations, only the first one will get applied.

* Subscribing to a specific commit of a branch is not supported.