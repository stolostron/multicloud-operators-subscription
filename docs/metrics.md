<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Accessing the Metrics Service](#accessing-the-metrics-service)
  - [Hub Cluster Metrics](#hub-cluster-metrics)
  - [Managed Cluster Metrics](#managed-cluster-metrics)
  - [Standalone Cluster Metrics](#standalone-cluster-metrics)
- [Scrapping with Prometheus](#scrapping-with-prometheus)
  - [Hub Cluster Scrapping](#hub-cluster-scrapping)
    - [Hub Cluster ServiceMonitor](#hub-cluster-servicemonitor)
    - [Hub Cluster Role](#hub-cluster-role)
    - [Hub Cluster RoleBinding](#hub-cluster-rolebinding)
    - [Hub Cluster Scrapping Verification](#hub-cluster-scrapping-verification)
  - [Managed Cluster Scrapping](#managed-cluster-scrapping)
    - [Managed Cluster ServiceMonitor](#managed-cluster-servicemonitor)
    - [Managed Cluster Role](#managed-cluster-role)
    - [Managed Cluster RoleBinding](#managed-cluster-rolebinding)
    - [Managed Cluster Scrapping Verification](#managed-cluster-scrapping-verification)
  - [Standalone Cluster Scrapping](#standalone-cluster-scrapping)
    - [Standalone Cluster ServiceMonitor](#standalone-cluster-servicemonitor)
    - [Standalone Cluster Role](#standalone-cluster-role)
    - [Standalone Cluster RoleBinding](#standalone-cluster-rolebinding)
    - [Standalone Cluster Scrapping Verification](#standalone-cluster-scrapping-verification)
- [Troubleshooting](#troubleshooting)
  - [Hub Cluster Metrics Service Missing](#hub-cluster-metrics-service-missing)
  - [Managed Cluster Metrics Service Missing](#managed-cluster-metrics-service-missing)
  - [Standalone Cluster Metrics Service Missing](#standalone-cluster-metrics-service-missing)
- [Custom Metrics](#custom-metrics)
  - [Hub Cluster Custom Metrics](#hub-cluster-custom-metrics)
  - [Managed Cluster Custom Metrics](#managed-cluster-custom-metrics)
  - [Collecting Custom Metrics for Observability](#collecting-custom-metrics-for-observability)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Accessing the Metrics Service

> This step's goal is to manually expose and visit the collected metrics,</br>
> exposing the services is not required for [Scrapping with Prometheus](#scrapping-with-prometheus).

## Hub Cluster Metrics

Note the *Service*:

```shell
$ oc get svc -n open-cluster-management hub-subscription-metrics

NAME                            TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)    AGE
hub-subscription-metrics   ClusterIP   10.96.101.40   <none>        8381/TCP   28m
```

> if missing, [create manually](#hub-cluster-metrics-service-missing).

Expose the *Service*:

```shell
oc port-forward -n open-cluster-management svc/hub-subscription-metrics 8381
```

Use *curl* or your browser to visit the metrics endpoint and see the various metrics collected:

```shell
$ curl http://localhost:8381/metrics

...
# HELP workqueue_adds_total Total number of adds handled by workqueue
# TYPE workqueue_adds_total counter
workqueue_adds_total{name="CSRApprovingController"} 10
workqueue_adds_total{name="addon-deploy-controller"} 15
workqueue_adds_total{name="addon-healthcheck-controller"} 16
workqueue_adds_total{name="addon-hook-deploy-controller"} 9
workqueue_adds_total{name="addon-install-controller"} 12
workqueue_adds_total{name="addon-registration-controller"} 10
workqueue_adds_total{name="cluster-management-addon-controller"} 9
workqueue_adds_total{name="mcmhub-subscription-controller"} 0
...
```

## Managed Cluster Metrics

Note the *Service*:

> Note the use of the non-conventional port 8388 (instead of the conventional 8381).

```shell
$ oc get svc -n open-cluster-management-agent-addon mc-subscription-metrics

NAME                          TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)    AGE
mc-subscription-metrics   ClusterIP   10.96.142.141   <none>        8388/TCP   74m
```

> if missing, [create manually](#managed-cluster-metrics-service-missing).

Expose the *Service*:

```shell
oc port-forward -n open-cluster-management-agent-addon svc/mc-subscription-metrics 8388
```

Use *curl* or your browser to visit the metrics endpoint and see the various metrics collected:

```shell
$ curl http://localhost:8388/metrics

...
# TYPE controller_runtime_max_concurrent_reconciles gauge
controller_runtime_max_concurrent_reconciles{controller="helmrelease-controller"} 10
controller_runtime_max_concurrent_reconciles{controller="klusterlet-token-controller"} 1
controller_runtime_max_concurrent_reconciles{controller="subscription-controller"} 1
# HELP controller_runtime_reconcile_errors_total Total number of reconciliation errors p
er controller
# TYPE controller_runtime_reconcile_errors_total counter
controller_runtime_reconcile_errors_total{controller="helmrelease-controller"} 0
controller_runtime_reconcile_errors_total{controller="klusterlet-token-controller"} 22
controller_runtime_reconcile_errors_total{controller="subscription-controller"} 0
...
```

## Standalone Cluster Metrics

Note the *Service*:

> Note the use of the non-conventional port 8389 (instead of the conventional 8381).

```shell
$ oc get svc -n open-cluster-management standalone-subscription-metrics

NAME                              TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)    AGE
standalone-subscription-metrics   ClusterIP   10.96.225.150   <none>        8389/TCP   6m16s
```

> if missing, [create manually](#standalone-cluster-metrics-service-missing).

Expose the *Service*:

```shell
oc port-forward -n open-cluster-management svc/standalone-subscription-metrics 8389
```

Use *curl* or your browser to visit the metrics endpoint and see the various metrics collected:

```shell
$ curl http://localhost:8389/metrics

...
# TYPE controller_runtime_max_concurrent_reconciles gauge
controller_runtime_max_concurrent_reconciles{controller="helmrelease-controller"} 10
controller_runtime_max_concurrent_reconciles{controller="subscription-controller"} 1
...
```

# Scrapping with Prometheus

## Hub Cluster Scrapping

### Hub Cluster ServiceMonitor

Create a *ServiceMonitor* for collecting services exposing metrics:

> Note: for *OCP* *metadata.namespace* should be `openshift-monitoring`.

```shell
cat << EOF | oc apply -f -
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: hub-subscription-metrics
  namespace: monitoring
spec:
  endpoints:
  - port: metrics
  namespaceSelector:
    matchNames:
    - open-cluster-management
  selector:
    matchLabels:
      app: hub-subscription-metrics
EOF
```

### Hub Cluster Role

Create a *Role* for setting the permissions for monitoring:

```shell
cat << EOF | oc apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: prometheus-k8s-monitoring
  namespace: open-cluster-management
rules:
- apiGroups:
  - ""
  resources:
  - services
  - endpoints
  - pods
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - extensions
  resources:
  - ingresses
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - networking.k8s.io
  resources:
  - ingresses
  verbs:
  - get
  - list
  - watch
EOF
```

### Hub Cluster RoleBinding

Create a *RoleBinding* for Binding the *Role* to Prometheus' monitoring *ServiceAccount*:

> Note: for *OCP* *subjects[0].namespace* should be `openshift-monitoring`.

```shell
cat << EOF | oc apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: prometheus-k8s-monitoring-binding
  namespace: open-cluster-management
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: prometheus-k8s-monitoring
subjects:
- kind: ServiceAccount
  name: prometheus-k8s
  namespace: monitoring
EOF
```

### Hub Cluster Scrapping Verification

In `Prometheus`'s dashboard, run the following query to look up metrics reported by the Subscription Operator Metrics Service:

```text
{service="hub-subscription-metrics"}
```

## Managed Cluster Scrapping

### Managed Cluster ServiceMonitor

Create a *ServiceMonitor* for collecting services exposing metrics:

> Note: for *OCP* *metadata.namespace* should be `openshift-monitoring`.

```shell
cat << EOF | oc apply -f -
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: mc-subscription-metrics
  namespace: monitoring
spec:
  endpoints:
  - port: metrics
  namespaceSelector:
    matchNames:
    - open-cluster-management-agent-addon
  selector:
    matchLabels:
      app: mc-subscription-metrics
EOF
```

### Managed Cluster Role

Create a *Role* for setting the permissions for monitoring:

```shell
cat << EOF | oc apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: prometheus-k8s-monitoring
  namespace: open-cluster-management-agent-addon
rules:
- apiGroups:
  - ""
  resources:
  - services
  - endpoints
  - pods
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - extensions
  resources:
  - ingresses
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - networking.k8s.io
  resources:
  - ingresses
  verbs:
  - get
  - list
  - watch
EOF
```

### Managed Cluster RoleBinding

Create a *RoleBinding* for Binding the *Role* to Prometheus' monitoring *ServiceAccount*:

> Note: for *OCP* *subjects[0].namespace* should be `openshift-monitoring`.

```shell
cat << EOF | oc apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: prometheus-k8s-monitoring-binding
  namespace: open-cluster-management-agent-addon
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: prometheus-k8s-monitoring
subjects:
- kind: ServiceAccount
  name: prometheus-k8s
  namespace: monitoring
EOF
```

### Managed Cluster Scrapping Verification

In `Prometheus`'s dashboard, run the following query to look up metrics reported by the Subscription Operator Metrics Service:

```text
{service="mc-subscription-metrics"}
```

## Standalone Cluster Scrapping

### Standalone Cluster ServiceMonitor

Create a *ServiceMonitor* for collecting services exposing metrics:

> Note: for *OCP* *metadata.namespace* should be `openshift-monitoring`.

```shell
cat << EOF | oc apply -f -
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: standalone-subscription-metrics
  namespace: monitoring
spec:
  endpoints:
  - port: metrics
  namespaceSelector:
    matchNames:
    - open-cluster-management
  selector:
    matchLabels:
      app: standalone-subscription-metrics
EOF
```

### Standalone Cluster Role

Create a *Role* for setting the permissions for monitoring:

```shell
cat << EOF | oc apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: prometheus-k8s-monitoring
  namespace: open-cluster-management
rules:
- apiGroups:
  - ""
  resources:
  - services
  - endpoints
  - pods
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - extensions
  resources:
  - ingresses
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - networking.k8s.io
  resources:
  - ingresses
  verbs:
  - get
  - list
  - watch
EOF
```

### Standalone Cluster RoleBinding

Create a *RoleBinding* for Binding the *Role* to Prometheus' monitoring *ServiceAccount*:

> Note: for *OCP* *subjects[0].namespace* should be `openshift-monitoring`.

```shell
cat << EOF | oc apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: prometheus-k8s-monitoring-binding
  namespace: open-cluster-management
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: prometheus-k8s-monitoring
subjects:
- kind: ServiceAccount
  name: prometheus-k8s
  namespace: monitoring
EOF
```

### Standalone Cluster Scrapping Verification

In `Prometheus`'s dashboard, run the following query to look up metrics reported by the Subscription Operator Metrics Service:

```text
{service="standalone-subscription-metrics"}
```

# Troubleshooting

## Hub Cluster Metrics Service Missing

Using *clusteradm* should create the metrics service for the *Hub Cluster*, if needed, create manually:

```shell
cat << EOF | oc apply -f -
apiVersion: v1
kind: Service
metadata:
  labels:
    app: hub-subscription-metrics
  name: hub-subscription-metrics
  namespace: open-cluster-management
spec:
  ports:
    - name: metrics
      port: 8381
      protocol: TCP
      targetPort: 8381
  selector:
    app: multicluster-operators-hub-subscription
  sessionAffinity: None
  type: ClusterIP
EOF
```

## Managed Cluster Metrics Service Missing

The *multicluster-subscription-operator* should create the metrics service for the *Managed  Cluster*, if needed, create manually:

> Note the use of the non-conventional port 8388 (instead of the conventional 8381).

```shell
cat << EOF | oc apply -f -
apiVersion: v1
kind: Service
metadata:
  labels:
    app: mc-subscription-metrics
  name: mc-subscription-metrics
  namespace: open-cluster-management-agent-addon
spec:
  ports:
  - name: metrics
    port: 8388
    protocol: TCP
    targetPort: 8388
  selector:
    component: application-manager
  sessionAffinity: None
  type: ClusterIP
EOF
```

## Standalone Cluster Metrics Service Missing

If you're running a *Standalone Cluster*, you'll need to manually create/edit the metric service resource:

```shell
cat << EOF | oc apply -f -
apiVersion: v1
kind: Service
metadata:
  labels:
    app: standalone-subscription-metrics
  name: standalone-subscription-metrics
  namespace: open-cluster-management
spec:
  ports:
    - name: metrics
      port: 8389
      protocol: TCP
      targetPort: 8389
  selector:
    app: multicluster-operators-standalone-subscription
  sessionAffinity: None
  type: ClusterIP
EOF
```

# Custom Metrics

Adding to the [default exported metrics by the controller-runtime](https://book.kubebuilder.io/reference/metrics-reference.html#default-exported-metrics-references).

## Hub Cluster Custom Metrics

The following metrics can be scrapped from *Hub Cluster*:

| Name                        | Help                                        | Labels |
| --------------------------- | ------------------------------------------- | ------ |
| propagation_successful_time | Histogram of successful propagation latency | *subscription_namespace*<br/>*subscription_name* |
| propagation_failed_time     | Histogram of failed propagation latency     | *subscription_namespace*<br/>*subscription_name* |

## Managed Cluster Custom Metrics

The following metrics can be scrapped from *Managed Clusters*:

| Name                             | Help                                             | Labels |
| -------------------------------- | ------------------------------------------------ | ------ |
| git_successful_pull_time         | Histogram of successful git pull latency         | *subscription_namespace*<br/>*subscription_name* |
| git_failed_pull_time             | Histogram of failed git pull latency             | *subscription_namespace*<br/>*subscription_name* |
| local_deployment_successful_time | Histogram of successful local deployment latency | *subscription_namespace*<br/>*subscription_name* |
| local_deployment_failed_time     | Histogram of failed local deployment latency     | *subscription_namespace*<br/>*subscription_name* |

## Collecting Custom Metrics for Observability

For the [Observability Operator](https://github.com/stolostron/multicluster-observability-operator) to collect the aforementioned metrics, we need to configure the `observability-metrics-custom-allowlist` *ConfigMap* in the `open-cluster-management-observability` namespace on the *Hub Cluster*.</br>
Note that for *Histogram* type metrics, we have a 3 metrics series created per each *Histogram*, the members of the series are identified by the *bucket*, *count*, and *sum* suffixes to the metric name. Here's an example of a working *ConfigMap*:

```yaml
apiVersion: v1
kind: ConfigMap
data:
  metrics_list.yaml: |
    names:
    - git_successful_pull_time_bucket
    - git_successful_pull_time_count
    - git_successful_pull_time_sum
    - git_failed_pull_time_bucket
    - git_failed_pull_time_count
    - git_failed_pull_time_sum
    - local_deployment_successful_time_bucket
    - local_deployment_successful_time_count
    - local_deployment_successful_time_sum
    - local_deployment_failed_time_bucket
    - local_deployment_failed_time_count
    - local_deployment_failed_time_sum
    - propagation_successful_time_bucket
    - propagation_successful_time_count
    - propagation_successful_time_sum
    - propagation_failed_time_bucket
    - propagation_failed_time_count
    - propagation_failed_time_sum
```
