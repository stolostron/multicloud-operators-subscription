## Resource reconciliation rate settings

The subscription operator compares currently deployed hash of helm chart to the hash from the source repository every 15 munites and apply changes to target clusters when there is change. The frequeny of resource reconciliation has impact on the performance of other application deployments and updates. For example, if there are hundreds of application subscriptions and you choose to reconcile all of these more frequently, the response time of reconcilication will be slower. Depending on the nature of kubernetes resources of the application, it will help to select appropriate reconciliation frequency for better performance.

### Reconcile frequency settings

- `Off` : The deployed resources are not automatically reconciled. A change in the subscription CR triggers a reconciliation. You can add or update a label or annotation.
- `Low` : The subscription operator compares currently deployed hash to the hash of the source repository every hour and apply changes to target clusters when there is change.
- `Medium`: This is the default setting. The subscription operator compares currently deployed hash to the hash of the source repository every 15 munites and apply changes to target clusters when there is change.
- `High`: The subscription operator compares currently deployed hash to the hash of the source repository every 2 munites and apply changes to target clusters when there is change.

You can set this using `apps.open-cluster-management.io/reconcile-rate` annotation in the channel CR that is referenced by subscription. Here is an example.

```yaml
---
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: helm-channel
  namespace: sample
  annotations:
    apps.open-cluster-management.io/reconcile-rate: low
spec:
  type: HelmRepo
  pathname: <Helm repo URL>
---
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: helm-subscription
spec:
  channel: sample/helm-channel
  name: nginx-ingress
  packageOverrides:
  - packageName: nginx-ingress
    packageAlias: nginx-ingress-simple
    packageOverrides:
    - path: spec
      value:
        defaultBackend:
          replicaCount: 3
  placement:
    local: true
```

In this example, all subscriptions that uses `sample/helm-channel` get `low` reconciliation frequency. 

Regardless of the reconcile-rate setting in the channel, a subscription can turn the auto-reconciliation `off` by specifying `apps.open-cluster-management.io/reconcile-rate: off` annotation in the subscription CR. For example, 

```yaml
---
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: helm-channel
  namespace: sample
  annotations:
    apps.open-cluster-management.io/reconcile-rate: high
spec:
  type: HelmRepo
  pathname: <Helm repo URL>
---
apiVersion: apps.open-cluster-management.io/v1
kind: Subscription
metadata:
  name: helm-subscription
  annotations:
    apps.open-cluster-management.io/reconcile-rate: "off"
spec:
  channel: sample/helm-channel
  name: nginx-ingress
  packageOverrides:
  - packageName: nginx-ingress
    packageAlias: nginx-ingress-simple
    packageOverrides:
    - path: spec
      value:
        defaultBackend:
          replicaCount: 3
  placement:
    local: true
```

In this example, the resources deployed by `helm-subscription` will never be automatically reconciled even if the `reconcile-rate` is set to `high` in the channel.