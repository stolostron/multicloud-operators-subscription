# Git repository channel subscription

You can subscribe to public or enterprise Git repositories that contain Kubernetes resource YAML files or Helm charts, or both. This document gives examples of connecting to a Git repository through a channel and subscribing to Kubernetes resources and Helm charts from the Git repository.

## Supported Git servers

- GitHub
- GitLab
- BitBucket
- Gogs (Gogs webhook is not supported )

## Prerequisite

Ensure that you have a Kubernetes cluster and this subscription operator running.
Ensure that you have a Kubernetes cluster that include a running instance of this subscription operator.

## Subscribing to a Helm chart from a public Git repository

Use the following example to create a channel that connects to a public Git repository and subscribes to a nginx Helm chart.

1. Clone this `multicloud-operators-subscription` GitHub repository.
1. In the root for your cloned repository, run the following command to create a namespace:

   ```shell
   kubectl apply -f ./examples/local-git-sub/00-namespace.yaml
   ```

   This command creates an `local-dev-chn-ns` namespace.
1. Run the following command to create an `gitops-chn` channel within the `local-dev-chn-ns` namespace.

   ```shell
   kubectl apply -f ./examples/local-git-sub/channel.yaml
   ```

   The following YAML content is used to define this `gitops` channel:

   ```yaml
   apiVersion: apps.open-cluster-management.io/v1
   kind: Channel
   metadata:
     name: gitops-chn
     namespace: local-dev-chn-ns
   spec:
     pathname: 'https://github.com/open-cluster-management-io/ multicloud-operators-subscription.git'
   type: Git
   ```

   The value for the `pathname` field is the Git repository HTTPS URL.
1. Run the following command to subscribe to the `gitops` channel:

   ```shell
   kubectl apply -f ./examples/local-git-sub/subscription.yaml
   ```

   When you review the `./examples/local-git-sub/subscription.yaml` file, the subscription has the following annotations.

   ```yaml
    annotations:
      apps.open-cluster-management.io/github-branch: main
      apps.open-cluster-management.io/github-path: examples/remote-git-sub-op
   ```

   The annotation `apps.open-cluster-management.io/github-path` indicates that the subscription subscribes to all Helm charts and Kubernetes resources that are in the `examples/remote-git-sub-op` directory for the Git repository channel. The subscription subscribes to `master` branch by default. If you want to subscribe to a different branch, you can use annotation `apps.open-cluster-management.io/github-branch`.
1. Run the following command to place the subscribed resources onto the local cluster:

   ```shell
   kubectl patch subscriptions.apps.open-cluster-management.io nginx-sub --type='json' -p='[{"op": "replace", "path": "/spec/placement/local", "value": true}]'
   ```

   After a couple of minutes, run the following command to check whether a `deployments.apps` CR is created for the nginx Helm chart:

   ```shell
   kubectl get deployments -n default
   ```


## Subscribing to Kubernetes resources from a Git repository

In the following example, you create a channel that connects to a Git repository and subscribes to a sample nginx deployment `examples/github-channel/sample-deployment.yaml` YAML file.

1. Clone this `multicloud-operators-subscription` Git repository.
1. Run the following command to create a `kuberesources`namespace:

   ```shell
   kubectl apply -f ./examples/github-channel/10-namespace.yaml
   ```

1. Run the following command to create a `sample-kube-resources-git` channel in the `kuberesources` namespace:

   ```shell
   kubectl apply -f ./examples/github-channel/11-channel.yaml
   ```

   The following YAML content is used to define this `sample-kube-resources-git` channel:

   ```yaml
   apiVersion: apps.open-cluster-management.io/v1
   kind: Channel
   metadata:
     name: sample-kube-resources-git
     namespace: kuberesources
   spec:
       type: Git
       pathname: https://github.com/open-cluster-management/multicloud-operators-subscription.git
   ```

   The value for the `pathname` field is the Git repository HTTPS URL.
1. Run the following command to subscribe to the `sample-kube-resources-git` channel:

   ```shell
   kubectl apply -f ./examples/github-channel/12-subscription.yaml
   ```

   When you review the `./examples/github-channel/12-subscription.yaml` file, the subscription has the following annotations.

   ```yaml
    annotations:
      apps.open-cluster-management.io/git-path: examples/github-channel
      apps.open-cluster-management.io/git-branch: branch1
   ```

   The annotation `apps.open-cluster-management.io/git-path` indicates that the subscription subscribes to all Helm charts and Kubernetes resources that are in the `examples/github-channel` directory of the Git repository channel.

   In `examples/github-channel`, there are multiple YAML files, however, only the `sample-deployment.yaml` file is applied. The `.kubernetesignore` file that is within the directory that is defined by the `data.path` field indicates that all other files are to be ignored. The subscription then applies only the `sample-deployment.yaml` file to the cluster.

   The subscription subscribes to `master` branch by default. If you want to subscribe to a different branch, you can use annotation `apps.open-cluster-management.io/git-branch`.
1. Run the following command to place the subscribed resources onto the local cluster:

   ```shell
   kubectl patch subscriptions.apps.open-cluster-management.io sample-kube-resources-subscription --type='json' -p='[{"op": "replace", "path": "/spec/placement/local", "value": true}]'
   ```

   After a couple of minutes, run the following command to check whether a `sample-nginx-deployment` deployment is created:

   ```shell
   kubectl get deployment --all-namespaces
   ```

## Subscribing to a Helm chart from an enterprise Git repository that requires authentication

In the previous examples, the Git repository that the channel connects to is a public repository and did not require authentication. If a Git repository does require authentication to connect to the repository, you need to associate the channel with a Kubernetes secret.

The `channel` and `subscription` resources support only basic authentication.

Update your channel resource to reference a Kubernetes secret and define the YAML content to create the secret. Within your YAML content, set the `user` field to be a Git user ID and the `accessToken` field to be a Git personal access token.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-git-secret
  namespace: ibmcharts
data:
  user: dXNlcgo=
  accessToken: cGFzc3dvcmQK
---
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: ibm-charts-git
  namespace: ibmcharts
spec:
    type: Git
    pathname: https://github.com/IBM/charts.git
    secretRef:
      name: my-git-secret
```

## Subscribing to a self-hosted Git server with custom or self-signed TLS certificate

If a Git server has a custom or self-signed TLS certificate, you can use `insecureSkipVerify: true` in the channel spec. Otherwise, the connection to the Git server will fail with an error similar to the following.

```
x509: certificate is valid for localhost.com, not localhost
```

```
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: sample-channel
  namespace: sample
spec:
  type: GitHub
  pathname: <Git URL>
  insecureSkipVerify: true
```

## .kubernetesignore file

You can include a `.kubernetesignore` file within your Git repository root directory, or within the `data.path` directory that is specified in the ConfigMap that is defined for your subscription `spec.packageFilter.filterRef` field.

You can use this `.kubernetesignore` file to specify patterns of files or subdirectories, or both, to ignore when the subscription processes and applies Kubernetes resource from the repository.

You can also use the `.kubernetesignore` file for fine-grain filtering to selectively apply Kubernetes resources. The pattern format of the `.kubernetesignore` file is the same as a `.gitignore` file.

If the `data.path` field is not defined in the ConfigMap that is set for the subscription `spec.packageFilter.filterRef` field, the subscription looks for a `.kubernetesignore` file in the repository root directory. If the `data.path` field is defined, the subscription looks for the `.kubernetesignore` file in the `data.path` directory. Subscriptions do not, searching any other directory for a `.kubernetesignore` file.

## Kustomize

If there is `kustomization.yaml` or `kustomization.yml` file in a subscribed Git folder, kustomize will be applied.

You can use `spec.packageOverrides` to override `kustomization` at the subscription deployment time. For example,

```yaml
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: example-subscription
  namespace: default
spec:
  channel: some/channel
  packageOverrides:
  - packageName: kustomization
    packageOverrides:
    - value:
        patches:
          - target:
              kind: Deployment
              name: busybox
            patch: |-
              - op: replace
                path: /spec/replicas
                value: 2
the kustomization overrider YAML as string
```

`packageName: kustomization` is required. The override either adds new entries or updates existing entries. It does not remove existing entries.

## Subscribing to a specific branch

The subscription operator that is include in this `multicloud-operators-subscription` repository subscribes to the `master` branch of a Git repository by default. If you want to subscribe to a different branch, you need to specify the branch name annotation in the subscription.

The following example Subscription YAML shows how to specify a different branch:

```yaml
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: nginx-app-sub
  annotations:
    apps.open-cluster-management.io/git-path: examples/remote-git-sub-op
    apps.open-cluster-management.io/git-branch: branch1
```

## Subscribing to a specific commit

The subscription operator that is include in this `multicloud-operators-subscription` repository subscribes to the latest commit of specified branch of a Git repository by default. If you want to subscribe to a specific commit, you need to specify the desired commit annotation with the commit hash in the subscription.

The following example Subscription YAML shows how to specify a different commit:

```yaml
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: nginx-app-sub
  annotations:
    apps.open-cluster-management.io/git-path: examples/remote-git-sub-op
    apps.open-cluster-management.io/git-desired-commit: 92ebae0b2354c3c78f4318e8359cc8e4caf005c5
    apps.open-cluster-management.io/git-clone-depth: 100
```

The `git-clone-depth` annotation is optional and set to 20 by default which means the subscription controller retrieves the previous 20 commit history from the Git repository. If you specify much older `git-desired-commit`, you need to specify `git-clone-depth` accordingly for the desired commit.

## Subscribing to a specific tag

The subscription operator that is include in this `multicloud-operators-subscription` repository subscribes to the latest commit of specified branch of a Git repository by default. If you want to subscribe to a specific tag, you need to specify the tag annotation in the subscription.

The following example Subscription YAML shows how to specify a tag:

```yaml
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: nginx-app-sub
  annotations:
    apps.open-cluster-management.io/git-path: examples/remote-git-sub-op
    apps.open-cluster-management.io/git-tag: v0.10.0
    apps.open-cluster-management.io/git-clone-depth: 100
```

Note: If both Git desired commit and tag annotations are specified, the tag will be ignored.

The `git-clone-depth` annotation is optional and set to 20 by default which means the subscription controller retrieves the previous 20 commit history from the Git repository. If you specify much older `git-tag`, you need to specify `git-clone-depth` accordingly for the desired commit of the tag.

## Resource reconciliation rate settings

The subscription operator compares currently deployed commit ID to the latest commit ID of the source repository every 3 munites and apply changes to target clusters when there is change. Every 15 minutes, it re-applies all resources from the source Git repository to the target clusters even if there is no change in the repository. The frequeny of resource reconciliation has impact on the performance of other application deployments and updates. For example, if there are hundreds of application subscriptions and you choose to reconcile all of these more frequently, the response time of reconcilication will be slower. Depending on the nature of kubernetes resources, it will help to select appropriate reconciliation frequency for better performance.

### Reconcile frequency settings

- `Off` : The deployed resources are not automatically reconciled. A change in the subscription CR triggers a reconciliation. You can add or update a label or annotation.
- `Low` : The deployed resources are automatically reconciled every hour even if there is no change in the source Git repository.
- `Medium`: This is the default setting. The subscription operator compares currently deployed commit ID to the latest commit ID of the source repository every 3 munites and apply changes to target clusters when there is change. Every 15 minutes, it re-applies all resources from the source Git repository to the target clusters even if there is no change in the repository.
- `High`: The deployed resources are automatically reconciled every two minutes even if there is no change in the source Git repository.

You can set this using `apps.open-cluster-management.io/reconcile-rate` annotation in the channel CR that is referenced by subscription. Here is an example.

```yaml
---
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: git-channel
  namespace: sample
  annotations:
    apps.open-cluster-management.io/reconcile-rate: low
spec:
  type: GitHub
  pathname: <Git URL>
---
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: git-subscription
  annotations:
    apps.open-cluster-management.io/git-path: application1
    apps.open-cluster-management.io/git-branch: branch1
spec:
  channel: sample/git-channel
  placement:
    local: true
```

In this example, all subscriptions that uses `sample/git-channel` get `low` reconciliation frequency. 

Regardless of the reconcile-rate setting in the channel, a subscription can turn the auto-reconciliation `off` by specifying `apps.open-cluster-management.io/reconcile-rate: off` annotation in the subscription CR. For example, 

```yaml
---
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: git-channel
  namespace: sample
  annotations:
    apps.open-cluster-management.io/reconcile-rate: high
spec:
  type: GitHub
  pathname: <Git URL>
---
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: git-subscription
  annotations:
    apps.open-cluster-management.io/git-path: application1
    apps.open-cluster-management.io/git-branch: branch1
    apps.open-cluster-management.io/reconcile-rate: "off"
spec:
  channel: sample/git-channel
  placement:
    local: true
```

In this example, the resources deployed by `git-subscription` will never be automatically reconciled even if the `reconcile-rate` is set to `high` in the channel.

## Enabling Git WebHook

By default, a Git channel subscription clones the Git repository specified in the channel every minute and applies changes when the commit ID has changed. Alternatively, you can configure your subscription to apply changes only when the Git repository sends repo PUSH and PULL webhook event notifications.

In order to configure webhook in a Git repository, you need a target webhook payload URL and optionally a secret.

### Payload URL

Create a route (ingress) to expose the subscription operator's webhook event listener service where `<operator namespace>` is the namespace where the subscription operator runs in.

  ```shell
  oc create route passthrough --service=multicluster-operators-subscription -n <operator namespace>
  ```

Then, use `oc get route multicluster-operators-subscription -n <operator namespace>` command to find the externally-reachable hostname. The webhook payload URL is `https://<externally-reachable hostname>/webhook`.

### WebHook secret

WebHook secret is optional. Create a Kubernetes secret in the channel namespace. The secret must contain `data.secret`. For example,

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-git-webhook-secret
data:
  secret: cm9rZWpAY2EuaWJtLmNvbQo=
```

The value of `data.secret` is the base-64 encoded WebHook secret you are going to use.

* Using a unique secret per Git repository is recommended.

### Configuring WebHook in Git repository

Use the payload URL and webhook secret to configure WebHook in your Git repository.

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

