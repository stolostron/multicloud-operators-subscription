# multicloud-operators-subscription

[![Build](https://api.travis-ci.com/open-cluster-management/multicloud-operators-subscription.svg?branch=main)](https://api.travis-ci.com/open-cluster-management/multicloud-operators-subscription.svg?branch=main)
[![GoDoc](https://godoc.org/github.com/open-cluster-management/multicloud-operators-subscription?status.svg)](https://godoc.org/github.com/open-cluster-management/multicloud-operators-subscription)
[![Go Report Card](https://goreportcard.com/badge/github.com/open-cluster-management/multicloud-operators-subscription)](https://goreportcard.com/report/github.com/open-cluster-management/multicloud-operators-subscription)
[![Sonarcloud Status](https://sonarcloud.io/api/project_badges/measure?project=open-cluster-management_multicloud-operators-subscription&metric=coverage)](https://sonarcloud.io/api/project_badges/measure?project=open-cluster-management_multicloud-operators-subscription&metric=coverage)
[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

------

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Overview](#overview)
- [Quick start](#quick-start)
    - [Subscribe a Helm chart](#subscribe-a-helm-chart)
    - [Troubleshooting](#troubleshooting)
- [Multicluster application subscription deployment](#multicluster-application-subscription-deployment)
- [Community, discussion, contribution, and support](#community-discussion-contribution-and-support)
- [Getting started](#getting-started)
    - [Prerequisites](#prerequisites)
- [Security response](#security-response)
- [References](#references)
    - [multicloud-operators repositories](#multicloud-operators-repositories)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## Overview

------

Subscribes resources from channels and applies them to Kubernetes 

## Quick start

------

### Subscribe a Helm chart

- Clone the [multicloud-operators-subscription GitHub repository](https://github.com/open-cluster-management/multicloud-operators-subscription).

```shell
mkdir -p "$GOPATH"/src/github.com/open-cluster-management
cd "$GOPATH"/src/github.com/open-cluster-management
git clone https://github.com/open-cluster-management/multicloud-operators-subscription.git
cd "$GOPATH"/src/github.com/open-cluster-management/multicloud-operators-subscription
```

- Set up the environment, and deploy the subscription operator.

```shell
kubectl apply -f ./deploy/standalone
```

- Create a Channel and a Subscription.

```shell
kubectl apply -f ./examples/helmrepo-channel
```

- Subscribe!

```shell
kubectl patch subscriptions.apps.open-cluster-management.io simple --type='json' -p='[{"op": "replace", "path": "/spec/placement/local", "value": true}]'
```

Find the nginx pods that are deployed to the current namespace. You should have 3 backend pods with the controller.

```shell
% kubectl get pods -l app=nginx-ingress
NAME                                             READY   STATUS    RESTARTS   AGE
nginx-ingress-controller-857f44797-7fx7c         1/1     Running   0          96s
nginx-ingress-default-backend-6b8dc9d88f-97pxz   1/1     Running   0          96s
nginx-ingress-default-backend-6b8dc9d88f-drt7c   1/1     Running   0          96s
nginx-ingress-default-backend-6b8dc9d88f-n26ls   1/1     Running   0          96s
```

Check the [Getting started](docs/getting_started.md) doc for more details.

### Troubleshooting

- Check operator availability

```shell
% kubectl get deploy,pods
NAME                                                READY     UP-TO-DATE   AVAILABLE   AGE
deployment.apps/multicloud-operators-subscription   1/1       1            1           99m

NAME                                                     READY     STATUS    RESTARTS   AGE
pod/multicloud-operators-subscription-557c676479-dh2fg   1/1       Running   0          24s
```

- Check the Subscription and its status.

```shell
% kubectl describe appsub simple
Name:         simple
Namespace:    default
Labels:       <none>
Annotations:  kubectl.kubernetes.io/last-applied-configuration:
                {"apiVersion":"apps.open-cluster-management.io/v1","kind":"Subscription","metadata":{"annotations":{},"name":"simple","namespace":"default"},"spec":{"ch...
API Version:  apps.open-cluster-management.io/v1
Kind:         Subscription
Metadata:
  Creation Timestamp:  2019-11-21T04:01:47Z
  Generation:          2
  Resource Version:    24045
  Self Link:           /apis/apps/v1/namespaces/default/subscriptions/simple
  UID:                 a35b6ef5-0c13-11ea-b4e7-00000a100ef8
Spec:
  Channel:  dev/dev-helmrepo
  Name:     nginx-ingress
  Package Overrides:
    Package Alias:  nginx-ingress-alias
    Package Name:  nginx-ingress
    Package Overrides:
      Path:   spec
      Value:  defaultBackend:
  replicaCount: 3

  Placement:
    Local:  true
Status:
  Last Update Time:  2019-11-21T04:02:38Z
  Phase:             Subscribed
  Statuses:
    /:
      Packages:
        dev-helmrepo-nginx-ingress-1.25.0:
          Last Update Time:  2019-11-21T04:02:38Z
          Phase:             Subscribed
          Resource Status:
            Last Update:  2019-11-21T04:02:24Z
            Phase:        Success
Events:                   <none>
```

### Multicluster application subscription deployment

- Setup a _hub_ cluster and a _managed_ cluster. See [open-cluster-management registration-operator](https://github.com/open-cluster-management/registration-operator#how-to-deploy) for more details.

- Deploy the subscription operator on the _hub_ cluster.

```shell
kubectl config use-context _hub_cluster_context_ # replace _hub_cluster_context_ with the hub cluster context name
git clone https://github.com/open-cluster-management/multicloud-operators-subscription
cd multicloud-operators-subscription
TRAVIS_BUILD=0
make deploy-community-hub # make deploy-community-hub GO_REQUIRED_MIN_VERSION:= # if you see warning about min version
```

- Deploy the subscription agent on the _managed_ cluster.

```shell
kubectl config use-context _managed_cluster_context_ # replace _managed_cluster_context_ with the managed cluster context name
export HUB_KUBECONFIG=_path_to_hub_kubeconfig_ # replace _path_to_hub_kubeconfig_ with the full path to the hub cluster kubeconfig
export MANAGED_CLUSTER_NAME=cluster1
make deploy-community-managed # make deploy-community-managed GO_REQUIRED_MIN_VERSION:= # if you see warning about min version
```

- Deploy an application subscription on the _hub_ cluster and it will propagate down to the _managed_ cluster

```shell
$ kubectl config use-context _hub_cluster_context_ # replace _hub_cluster_context_ with the hub cluster context name
$ kubectl apply -f examples/helmrepo-hub-channel
$ kubectl config use-context _managed_cluster_context_ # replace _managed_cluster_context_ with the managed cluster context name
$ sleep 60
$ kubectl get subscriptions.apps 
NAME        STATUS       AGE   LOCAL PLACEMENT   TIME WINDOW
nginx-sub   Subscribed   77s   true       
$ kubectl get pod
NAME                                                   READY   STATUS    RESTARTS   AGE
nginx-ingress-65f8e-controller-76fdf7f8bb-srfjp        1/1     Running   0          84s
nginx-ingress-65f8e-default-backend-865d66965c-ckq66   1/1     Running   0          84s

```

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repository.

------

## Getting started

### Prerequisites

Check the [Development Doc](docs/development.md) for information about how to contribute to the repository.

## Security response

Check the [Security Doc](SECURITY.md) if you find a security issue.

## References

### Multicloud-operators repositories 

- [multicloud-operators-application](https://github.com/open-cluster-management/multicloud-operators-application)
- [multicloud-operators-channel](https://github.com/open-cluster-management/multicloud-operators-channel)
- [multicloud-operators-deployable](https://github.com/open-cluster-management/multicloud-operators-deployable)
- [multicloud-operators-placementrule](https://github.com/open-cluster-management/multicloud-operators-placementrule)
- [multicloud-operators-subscription](https://github.com/open-cluster-management/multicloud-operators-subscription)
- [multicloud-operators-subscription-release](https://github.com/open-cluster-management/multicloud-operators-subscription-release)

