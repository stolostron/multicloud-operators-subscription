# Troubleshooting Guide

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Troubleshooting Guide](#troubleshooting-guide)
    - [Hub Subscription Pod](#hub-subscription-pod)
    - [Managed Subscription Pod](#managed-subscription-pod)
    - [How subscription status is reported](#how-subscription-status-is-reported)
    - [Hub Backend CLI](#hub-backend-cli)
    - [Set up ImageContentSourcePolicy when installing ACM downstream build on the managed cluster](#set-up-imagecontentsourcepolicy-when-installing-acm-downstream-build-on-the-managed-cluster)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## Hub Subscription Pod

### What is the hub subscrioption pod doing

- Dry Run the hub Application Subscription resource (aka AppSub) to get the resource list

Dry run is a pre-deployment validation step for the application subscription. It retrieves the full
list of resources that will be deployed and saves it to the app subscriptionReport in the
application namespace.

- Propagate the hub appsub to all managed clusters via ACM ManifestWork api. 

### Troubleshoot the hub subscrioption pod

- Find the hub subscription pod in the ACM system namespace E.g. `open-cluster-management`

```
% oc get pods -n open-cluster-management |grep hub-subscription
multicluster-operators-hub-subscription-75b75c8697-5t2hs          1/1     Running            1 (3d22h ago)    3d22h
```

- Check the hub subscription pod log on the hub

```
% oc logs -n open-cluster-management multicluster-operators-hub-subscription-75b75c8697-5t2hs
...
I0207 22:59:38.415347       1 mcmhub_controller.go:468] subscription-hub-reconciler/secondsub/second-level-sub "msg"="entry MCM Hub Reconciling subscription: secondsub/second-level-sub"  
I0207 22:59:38.415358       1 mcmhub_controller.go:503] Subscription: secondsub/second-level-sub is gone
I0207 22:59:38.415367       1 mcmhub_controller.go:504] Clean up all the manifestWorks owned by appsub: secondsub/second-level-sub
I0207 22:59:38.422554       1 propagate_manifestwork.go:376] manifestWork deleted: local-cluster/secondsub-second-level-sub
I0207 22:59:38.422581       1 mcmhub_controller.go:721] subscription-hub-reconciler "msg"="Enter finalCommit..."  
I0207 22:59:38.422593       1 mcmhub_controller.go:725] subscription-hub-reconciler "msg"="instace is delete, don't run update logic"  
I0207 22:59:38.422603       1 mcmhub_controller.go:726] subscription-hub-reconciler "msg"="Exit finalCommit..."  
I0207 22:59:38.422618       1 mcmhub_controller.go:518] subscription-hub-reconciler/secondsub/second-level-sub "msg"="exit Hub Reconciling 
...
```
### Set up log level for the hub subscription pod

- Open the ACM csv, append the log level to 1, save the csv 

```
% oc edit csv -n open-cluster-management advanced-cluster-management.v2.5.0 

      - name: multicluster-operators-hub-subscription
              containers:
              - command:
                - /usr/local/bin/multicluster-operators-subscription
                - --sync-interval=60
                - --v=1
```

- Make sure the hub subscription pod is restarted to run.
- Check more details from the hub subscription pod log

### Set up memory limit for the hub subscription pod

- Open the ACM csv, search the `multicluster-operators-hub-subscription` container, update the memory limit, save the csv

```
% oc edit csv -n open-cluster-management advanced-cluster-management.v2.5.0

      - name: multicluster-operators-hub-subscription
        spec:
          replicas: 1
          selector:
            matchLabels:
              app: multicluster-operators-hub-subscription
              ......

                resources:
                  limits:
                    cpu: 750m
                    memory: 2Gi                 ================> this is the hub subscription pod memory limit, update it to 4Gi for example.
                  requests:
                    cpu: 150m
                    memory: 128Mi

```
- verify the hub subscription pod should be restarted with the new memory limit. It could take a while for OLM to be reconciled to do so.

```
% oc get pods -n open-cluster-management |grep hub-sub
multicluster-operators-hub-subscription-58858c488f-c52zt          1/1     Running     2 (28h ago)      27d
```



## Managed Subscription Pod

### What is the Managed Subscrioption pod doing

- Local deployment on the managed cluster
  - Connect, download git repo / helm repo / object bucket
  - Deploy resources on the managed cluster
  - Create/Update the status of all deployed resources to the subscriptionStatus in the
  - application NameSpace on the managed cluster
  - Create/Update the overview of the cluster status for the app to the cluster subscriptionReport in the managed cluster namespace on the hub

### Troubleshoot the Managed Subscrioption pod

- Find the managed subscription pod in the ACM addon namespace on the managed cluster `open-cluster-management-agent-addon`

```
% oc get pods -n open-cluster-management-agent-addon |grep appmgr
klusterlet-addon-appmgr-646b87594d-wm82f                    1/1     Running   0            4d1h
```

- Check the managed cluster subscription pod log on the managed cluster

```
% oc logs -n open-cluster-management-agent-addon klusterlet-addon-appmgr-646b87594d-wm82f 
...
I0204 02:25:56.524973       1 spoke_token_controller.go:116] Reconciling open-cluster-management-agent-addon/klusterlet-addon-appmgr
I0204 02:25:56.525571       1 subscription_controller.go:188] Standalone/Endpoint Reconciling subscription: open-cluster-management/application-chart-sub
I0204 02:25:56.525705       1 subscription_controller.go:305] Exit Reconciling subscription: open-cluster-management/application-chart-sub
I0204 02:25:56.525736       1 subscription_controller.go:188] Standalone/Endpoint Reconciling subscription: open-cluster-management/grc-sub
I0204 02:25:56.525838       1 subscription_controller.go:305] Exit Reconciling subscription: open-cluster-management/grc-sub
I0204 02:25:56.525892       1 subscription_controller.go:188] Standalone/Endpoint Reconciling subscription: open-cluster-management/hive-clusterimagesets-subscription-fast-0
I0204 02:25:56.525961       1 subscription_controller.go:305] Exit Reconciling subscription: open-cluster-management/hive-clusterimagesets-subscription-fast-0
I0204 02:25:56.525992       1 subscription_controller.go:188] Standalone/Endpoint Reconciling subscription: open-cluster-management/console-chart-sub
...
```

### Set up log level for the managed subscription pod

- Find the managed cluster Name ${CLUSTER_NAME}
```
% oc get managedclusters
NAME               HUB ACCEPTED   MANAGED CLUSTER URLS                                          JOINED   AVAILABLE   AGE
local-cluster      true           https://api.playback-next.demo.red-chesterfield.com:6443      True     True        4d2h
playback-3node-1   true           https://api.playback-3node-1.demo.red-chesterfield.com:6443   True     True        3d13h
```

- On the hub cluster, stop Reconcile
To patch the managed subscription pod on the managed cluster, you need to first stop reconcile of the KlusterletAddonConfig on hub
```
% oc annotate klusterletaddonconfig -n ${CLUSTER_NAME} ${CLUSTER_NAME} klusterletaddonconfig-pause=true --overwrite=true
```

- On the hub cluster,scale Down klusterlet-addon-operator
```
% oc edit manifestwork -n ${CLUSTER_NAME}  ${CLUSTER_NAME}-klusterlet-addon-operator

search for Deployment. Set spec.replicas to 0:
```
- On the managed cluster, make sure the klusterlet-addon-operator pod is terminated.
```
% oc get pods -n open-cluster-management-agent-addon |grep klusterlet-addon-operator
```

- On the managed cluster, edit the appmgr addon deployment to set the log level to 1, save the deployment

```
% oc edit deployments -n open-cluster-management-agent-addon  klusterlet-addon-appmgr
    spec:
      containers:
      - args:
        - --alsologtostderr
        - --cluster-name=local-cluster
        - --hub-cluster-configfile=/var/run/klusterlet/kubeconfig
        - --v=1
```

- Make sure the managed subscription pod is restarted to run.
```
% oc get pods -n open-cluster-management-agent-addon  |grep klusterlet-addon-appmgr
klusterlet-addon-appmgr-794d76bcbf-tbsn5                     1/1    Running    0          14s
```

- Check more details from the managed subscription pod log.

### Set up memory limit for the managed subscription pod

- Find the managed cluster Name ${CLUSTER_NAME}
```
% oc get managedclusters
NAME               HUB ACCEPTED   MANAGED CLUSTER URLS                                          JOINED   AVAILABLE   AGE
local-cluster      true           https://api.playback-next.demo.red-chesterfield.com:6443      True     True        4d2h
playback-3node-1   true           https://api.playback-3node-1.demo.red-chesterfield.com:6443   True     True        3d13h
```

- On the hub cluster, stop Reconcile
To patch the managed subscription pod on the managed cluster, you need to first stop reconcile of the KlusterletAddonConfig on hub
```
% oc annotate klusterletaddonconfig -n ${CLUSTER_NAME} ${CLUSTER_NAME} klusterletaddonconfig-pause=true --overwrite=true
```

- On the hub cluster,scale Down klusterlet-addon-operator
```
% oc edit manifestwork -n ${CLUSTER_NAME}  ${CLUSTER_NAME}-klusterlet-addon-operator

search for Deployment. Set spec.replicas to 0:
```
- On the managed cluster, make sure the klusterlet-addon-operator pod is terminated.
```
% oc get pods -n open-cluster-management-agent-addon |grep klusterlet-addon-operator
```

- On the managed cluster, find and replace the memory limit to your desired value
```
% oc edit deployments -n open-cluster-management-agent-addon  klusterlet-addon-appmgr
...
        resources:
          limits:
            memory: 2Gi               ================> this is the managed subscription pod memory limit, update it to 3Gi for example.
          requests:
            memory: 128Mi
```

- Make sure the managed subscription pod is restarted wit the new memory limit.
```
% oc get pods -n open-cluster-management-agent-addon  |grep klusterlet-addon-appmgr
klusterlet-addon-appmgr-794d76bcbf-tbsn5                     1/1    Running    0          14s
```


## How subscription status is reported

The ACM subscription status is reported in the following three levels

### Package level AppSub status

In the appsub Namespace on the managed cluster

For every appsub deployed to a managed cluster, there is a SubscriptionStatus CR created in the appsub Namespace on the managed cluster, where every resource is reported with detailed error if exists.
```
apiVersion: apps.open-cluster-management.io/v1alpha1
kind: SubscriptionStatus
metadata:
  labels:
    apps.open-cluster-management.io/cluster: managed-k3s-cluster-bd7a7772
    apps.open-cluster-management.io/hosting-subscription: test-ns-2.git-gb-subscription-1
  name: git-gb-subscription-1
  namespace: test-ns-2                        // appsub namespace
statuses:
  packages:
  - apiVersion: v1
    kind: Service
    lastUpdateTime: "2021-09-13T20:12:34Z"
    Message: <detailed error. visible only if the package fails>
    name: frontend
    namespace: test-ns-2
    phase: Deployed 
  - apiVersion: apps/v1
    kind: Deployment
    lastUpdateTime: "2021-09-13T20:12:34Z"
    name: frontend
    namespace: test-ns-2
    phase: Deployed
  - apiVersion: v1
    kind: Service
    lastUpdateTime: "2021-09-13T20:12:34Z"
    name: redis-master
    namespace: test-ns-2
    phase: Deployed
  - apiVersion: apps/v1
    kind: Deployment
    lastUpdateTime: "2021-09-13T20:12:34Z"
    name: redis-master
    namespace: test-ns-2
    phase: Deployed
  - apiVersion: v1
    kind: Service
    lastUpdateTime: "2021-09-13T20:12:34Z"
    name: redis-slave
    namespace: test-ns-2
    phase: Deployed
  - apiVersion: apps/v1
    kind: Deployment
    lastUpdateTime: "2021-09-13T20:12:34Z"
    name: redis-slave
    namespace: test-ns-2
    phase: Deployed
```

### Cluster level AppSub status

In each cluster namespace on the hub

There is a subscriptionReport in each cluster Namespace on the hub, where the status of each appSub that is deployed to the cluster is displayed. The status could be 
  - Deployed
  - Failed
  - propagationFailed

```
apiVersion: apps.open-cluster-management.io/v1alpha1
kind: subscriptionReport
metadata:
  labels:
    apps.open-cluster-management.io/cluster: "true"
  name: cluster-1
  namespace: cluster-1                              // cluster namespace 
reportType: Cluster
results:
- result: deployed
  source: appsub-1-ns/appsub-1                     // appsub 1 namespaced name
  timestamp:
    nanos: 0
    seconds: 1634137362
- result: failed
  source: appsub-2-ns/appsub-2                     // appsub 2 namespaced name
  timestamp:
    nanos: 0
    seconds: 1634137362
- result: propagationFailed
  source: appsub-3-ns/appsub-3                     // appsub 3 namespaced name
  timestamp:
    nanos: 0
    seconds: 1634137362
```

### App level AppSub status

In the parent AppSub namespace on the hub cluster

There is another subscriptionReport in the AppSub Namespace on the hub, where the status of each cluster is shown based on the cluster subscriptionReport. Also we provide the resource list for the AppSub, and an overview summary for showing the total, deployed, failed, propagationFailed and inProgress counts.  

Note that inProcess = total - deployed - failed - propagationFailed

```
apiVersion: apps.open-cluster-management.io/v1alpha1
kind: subscriptionReport
metadata:
  labels:
    apps.open-cluster-management.io/hosting-subscription: appsub-1-ns.appsub-1
  name: appsub-1
  namespace: appsub-1-ns
reportType: Application
resources:
- apiVersion: v1
  kind: Service
  name: redis-master2
  namespace: playback-ns-2
- apiVersion: apps/v1
  kind: Deployment
  name: redis-master2
  namespace: playback-ns-2
- apiVersion: v1
  kind: Service
  name: redis-slave2
  namespace: playback-ns-2
- apiVersion: apps/v1
  kind: Deployment
  name: redis-slave2
  namespace: playback-ns-2
- apiVersion: v1
  kind: Service
  name: frontend2
  namespace: playback-ns-2
- apiVersion: apps/v1
  kind: Deployment
  name: frontend2
  namespace: playback-ns-2
results:
- result: deployed
  source: cluster-1                            //cluster 1 status
  timestamp:
    nanos: 0
    seconds: 0
- result: failed
  source: cluster-3                            //cluster 2 status
  timestamp:
    nanos: 0
    seconds: 0
- result: propagationFailed
  source: cluster-4                            //cluster 3 status
  timestamp:
    nanos: 0
    seconds: 0
summary:
  deployed: 8
  failed: 1
  inProgress: 0
  propagationFailed: 1
  clusters: 10
```

## Hub Backend CLI 

The CLI is for getting the package level AppSub Status on a given managed cluster

As a result, either the cluster level or the app level subscription Report doesn’t directly provide the detailed status for an application. It turns out holding such detailed status for all applications in the cluster level subscriptionReport increases the size of the cluster subscriptionReport
dramatically. Accordingly it impacts the whole performance of the hub cluster. It is necessary to provide a backend CLI, so that the end users can get the detailed status for an application deployed on a specific cluster.
```
% getAppSubStatus.sh -c <managed cluster Name> -s <AppSub Namespace> -n <Appsub Name>
// the relative package level AppSub status CR on the managed cluster will be fetched and displayed.
```
This CLI, uses identity details in the Application subscriptionReport, to create a managedClusterView resource, to see the managed cluster application SubscriptionStatus so the user can identify exactly what is wrong with the application.

The CLI can be downloaded here:

https://github.com/open-cluster-management-io/multicloud-operators-subscription/blob/main/cmd/scripts/getAppSubStatus.sh


## Set up ImageContentSourcePolicy when installing ACM downstream build on the managed cluster

### Issue

When creating a managedcluster, the application-manager addon has an image pull error that is caused by the ImageContentSourcePolicy not being deployed there

Steps to reproduce:

1. Tried to create an aws managedcluster
2. Waited for cluster addons to be ready but the application-manager addon wasn't
3. Logged into the managed cluster to find the imagecontentsourcepolicy was missing
4. check the application manager addon pod status on the managed cluster, see the image pull error
```
% oc get pods -n open-cluster-management-agent-addon |grep application-manager
```

### Root Cause

The downstream image that the application manager addon component uses is not public in the development stage during each release. The issue won't happen after each ACM GA.

```
image: registry.redhat.io/rhacm2/multicluster-operators-subscription-rhel8@sha256:2b7ae3d0c36833becce5d996fbc04e91a720c05e7997fb2aad86ee77a40d1e72  
```

### Solution

Make sure the ImageContentSourcePolicy CR exists on the managed cluster before getting it imported.

```
% oc get imagecontentsourcepolicy rhacm-repo  -o yaml 
apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: rhacm-repo
spec:
  repositoryDigestMirrors:
  - mirrors:
    - quay.io:443/acm-d
    source: registry.redhat.io/rhacm2
  - mirrors:
    - quay.io:443/acm-d
    source: registry.redhat.io/multicluster-engine
  - mirrors:
    - registry.redhat.io/openshift4/ose-oauth-proxy
    source: registry.access.redhat.com/openshift4/ose-oauth-proxy
```
