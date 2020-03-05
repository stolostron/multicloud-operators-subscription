# GitHub repository channel subscription

You can subscribe to public or enterprise GitHub repositories that contain Kubernetes resource YAML files or Helm charts, or both. This document gives examples of connecting to a GitHub repository through a channel and subscribing to Kubernetes resources and Helm charts from the GitHub repository.

## Prerequisite

Ensure that you have a Kubernetes cluster and this subscription operator running.
Ensure that you have a Kubernetes cluster that include a running instance of this subscription operator.

## Subscribing to a Helm chart from a public GitHub repository

Use the following example to create a channel that connects to a public IBM GitHub repository and subscribes to a MongoDB Helm chart.

1. Clone this `multicloud-operators-subscription` GitHub repository.
1. In the root for your cloned repository, run the following command to create a namespace:

   ```shell
   kubectl apply -f ./examples/github-channel/00-namespace.yaml
   ```

   This command creates an `ibmcharts` namespace.
1. Run the following command to create an `ibm-charts-github` channel within the `ibmcharts` namespace.

   ```shell
   kubectl apply -f ./examples/github-channel/01-channel.yaml
   ```

   The following YAML content is used to define this `ibm-charts-github` channel:

   ```yaml
   apiVersion: apps.open-cluster-management.io/v1
   kind: Channel
   metadata:
   name: ibm-charts-github
   namespace: ibmcharts
   spec:
       type: GitHub
       pathname: https://github.com/IBM/charts.git
   ```

   The value for the `pathname` field is the GitHub repository HTTPS URL.
1. Run the following command to subscribe to the `ibm-charts-github` channel:

   ```shell
   kubectl apply -f ./examples/github-channel/02-subscription.yaml
   ```

   When you review the `./examples/github-channel/02-subscription.yaml` file, the `spec.packageFilter.filterRef` field references the following ConfigMap:

   ```yaml
   apiVersion: v1
   kind: ConfigMap
   metadata:
   name: ibm-mongodb-dev-cm
   data:
       path: stable/ibm-mongodb-dev
   ```

   The `data.path` field indicates that the subscription subscribes to all Helm charts and Kubernetes resources that are in the `stable/ibm-mongodb-dev` directory for the GitHub repository channel.
1. Run the following command to place the subscribed resources onto the local cluster:

   ```shell
   kubectl patch subscriptions.apps.open-cluster-management.io github-mongodb-subscription --type='json' -p='[{"op": "replace", "path": "/spec/placement/local", "value": true}]'
   ```

   After a couple of minutes, run the following command to check whether a `helmreleases.apps.open-cluster-management.io` CR is created for the MongoDB Helm chart:

   ```shell
   kubectl get helmreleases.apps.open-cluster-management.io --all-namespaces
   ```

   Then, run the following command in the same namespace as the MongoDB helmreleases.apps.open-cluster-management.io CR to find the deployment:

   ```shell
   kubectl get deployments
   ```

## Subscribing to Kubernetes resources from a GitHub repository

In the following example, you create a channel that connects to a GitHub repository and subscribes to a sample nginx deployment `examples/github-channel/sample-deployment.yaml` YAML file.

1. Clone this `multicloud-operators-subscription` GitHub repository.
1. Run the following command to create a `kuberesources`namespace:

   ```shell
   kubectl apply -f ./examples/github-channel/10-namespace.yaml
   ```

1. Run the following command to create a `sample-kube-resources-github` channel in the `kuberesources` namespace:

   ```shell
   kubectl apply -f ./examples/github-channel/11-channel.yaml
   ```

   The following YAML content is used to define this `sample-kube-resources-github` channel:

   ```yaml
   apiVersion: apps.open-cluster-management.io/v1
   kind: Channel
   metadata:
     name: sample-kube-resources-github
     namespace: kuberesources
   spec:
       type: GitHub
       pathname: https://github.com/open-cluster-management/multicloud-operators-subscription.git
   ```

   The value for the `pathname` field is the GitHub repository HTTPS URL.
1. Run the following command to subscribe to the `sample-kube-resources-github` channel:

   ```shell
   kubectl apply -f ./examples/github-channel/12-subscription.yaml
   ```

   When you review the `./examples/github-channel/12-subscription.yaml` file, the `spec.packageFilter.filterRef` field references the following ConfigMap:

   ```yaml
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: resource-filter-configmap
   data:
       path: examples/github-channel
   ```

   The `data.path` field indicates that the subscription subscribes to all Helm charts and Kubernetes resources that are in the `stable/ibm-mongodb-dev` directory of the GitHub repository channel.

     In `examples/github-channel`, there are multiple YAML files, however, only the `sample-deployment.yaml` file is applied. The `.kubernetesignore` file that is within the directory that is defined by the `data.path` field indicates that all other files are to be ignored. The subscription then applies only the `sample-deployment.yaml` file to the cluster.
1. Run the following command to place the subscribed resources onto the local cluster:

   ```shell
   kubectl patch subscriptions.apps.open-cluster-management.io sample-kube-resources-subscription --type='json' -p='[{"op": "replace", "path": "/spec/placement/local", "value": true}]'
   ```

   After a couple of minutes, run the following command to check whether a `sample-nginx-deployment` deployment is created:

   ```shell
   kubectl get deployment --all-namespaces
   ```

## Subscribing to a Helm chart from an enterprise GitHub repository that requires authentication

In the previous examples, the GitHub repository that the channel connects to is a public repository and did not require authentication. If a GitHub repository does require authentication to connect to the repository, you need to associate the channel with a Kubernetes secret.

The `channel` and `subscription` resources support only basic authentication.

Update you channel resource to reference a Kubernetes secret and define the YAML content to create the secret. Within your YAML content, set the `user` field to be a GitHub user ID and the `accessToken` field to be a GitHub personal access token.

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
apiVersion: apps.open-cluster-management.io/v1
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

You can include a `.kubernetesignore` file within your GitHub repository root directory, or within the `data.path` directory that is specified in the ConfigMap that is defined for your subscription `spec.packageFilter.filterRef` field.

You can use this `.kubernetesignore` file to specify patterns of files or subdirectories, or both, to ignore when the subscription processes and applies Kubernetes resource from the repository.

You can also use the `.kubernetesignore` file for fine-grain filtering to selectively apply Kubernetes resources. The pattern format of the `.kubernetesignore` file is the same as a `.gitignore` file.

If the `data.path` field is not defined in the ConfigMap that is set for the subscription `spec.packageFilter.filterRef` field, the subscription looks for a `.kubernetesignore` file in the repository root directory. If the `data.path` field is defined, the subscription looks for the `.kubernetesignore` file in the `data.path` directory. Subscriptions do not, searching any other directory for a `.kubernetesignore` file.

## Subscribing to a specific branch

The subscription operator that is include in this `multicloud-operators-subscription` repository subscribes to the `master` branch of a GitHub repository by default. If you want to subscribe to a different branch, you need to specify the branch name within the ConfigMap that is specified in the subscription `spec.packageFilter.filterRef` field.

The following example ConfigMap YAML shows how to specify a different branch:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
name: ibm-mongodb-dev-cm
data:
    path: stable/ibm-mongodb-dev
    branch: mybranch
```

## Enabling GitHub WebHook

By default, a GitHub channel subscription clones the GitHub repository specified in the channel every minute and applies changes when the commit ID has changed. Alternatively, you can configure your subscription to apply changes only when the GitHub repository sends repo PUSH and PULL webhook event notifications.

In order to configure webhook in a GitHub repository, you need a target webhook payload URL and optionally a secret.

### Payload URL

Create a route (ingress) to expose the subscription operator's webhook event listener service where `<operator namespace>` is the namespace where the subscription operator runs in.

  ```shell
  oc create route passthrough --service=multicloud-operators-subscription -n <operator namespace>
  ```

Then, use `oc get route multicloud-operators-subscription -n <operator namespace>` command to find the externally-reachable hostname. The webhook payload URL is `https://<externally-reachable hostname>/webhook`.

### WebHook secret

WebHook secret is optional. Create a Kubernetes secret in the channel namespace. The secret must contain `data.secret`. For example,

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-github-webhook-secret
data:
  secret: cm9rZWpAY2EuaWJtLmNvbQo=
```

The value of `data.secret` is the base-64 encoded WebHook secret you are going to use.

* Using a unique secret per GitHub repository is recommended.

### Configuring WebHook in GitHub repository

Use the payload URL and webhook secret to configure WebHook in your GitHub repository.

### Enable WebHook event notification in channel

Annotate the subscription's channel.

```shell
oc annotate channel.apps.open-cluster-management.io <channel name> apps.open-cluster-management.io/webhook-enabled="true"
```

If you used a secret to configure WebHook, annotate the channel with this as well where `<the_secret_name>` is the kubernetes secret name containing webhook secret.

```shell
oc annotate channel.apps.open-cluster-management.io <channel name> apps.open-cluster-management.io/webhook-secret="<the_secret_name>"
```

### Subscriptions of webhook-enabled channel

No webhook specific configuration is needed in subscriptions.

## Limitations

* If you are subscribing to Kubernetes resource configuration YAML files, include only one Kubernetes resource definition in each YAML file. If a file includes multiple resource definitions, only the first definition is applied.

* A kubernetes resource YAML file cannot start with `---` line.

* You cannot subscribe to a specific commit of a branch.
