# multicloud-operators-subscription

[![Build](http://35.227.205.240/badge.svg?jobs=build_multicloud-operators-subscription)](http://35.227.205.240/?job=images_multicloud-operators-subscription_postsubmit)
[![GoDoc](https://godoc.org/github.com/IBM/multicloud-operators-subscription?status.svg)](https://godoc.org/github.com/IBM/multicloud-operators-subscription)
[![Go Report Card](https://goreportcard.com/badge/github.com/IBM/multicloud-operators-subscription)](https://goreportcard.com/report/github.com/IBM/multicloud-operators-subscription)
[![Code Coverage](https://codecov.io/gh/IBM/multicloud-operators-subscription/branch/master/graphs/badge.svg?branch=master)](https://codecov.io/gh/IBM/multicloud-operators-subscription?branch=master)
[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

------

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Overview](#overview)
- [Quick Start](#quick-start)
    - [Subscribe a helm chart](#subscribe-a-helm-chart)
    - [Trouble shooting](#trouble-shooting)
- [Community, discussion, contribution, and support](#community-discussion-contribution-and-support)
- [References](#references)
    - [multicloud-operators repositories](#multicloud-operators-repositories)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## Overview

------

Subscribes resources from Channels and apply them to kubernetes

## Quick Start

------

### Subscribe a helm chart

- Clone the subscription operator repository

```shell
mkdir -p "$GOPATH"/src/github.com/IBM
cd "$GOPATH"/src/github.com/IBM
git clone https://github.com/IBM/multicloud-operators-subscription.git
cd "$GOPATH"/src/github.com/IBM/multicloud-operators-subscription
```

- Setup environment and deploy subscription operator

```shell
kubectl apply -f ./deploy/standalone
```

- Create a Channel and Subscription

```shell
kubectl apply -f ./examples/helmrepo-channel
```

- Subscribe!

```shell
kubectl patch subscriptions.app.ibm.com simple --type='json' -p='[{"op": "replace", "path": "/spec/placement/local", "value": true}]'
```

Find the nginx pods deployed to current namespace, and the number of backend pods is overrided to 3

```shell
% kubectl get pods -l app=nginx-ingress
NAME                                                              READY     STATUS    RESTARTS   AGE
ngin-f3ts5xr8xpcml36hlkwq8tzfw-nginx-ingress-controller-68tf55x   1/1       Running   0          26s
ngin-f3ts5xr8xpcml36hlkwq8tzfw-nginx-ingress-default-backe95wfk   1/1       Running   0          26s
ngin-f3ts5xr8xpcml36hlkwq8tzfw-nginx-ingress-default-backen85m6   1/1       Running   0          26s
ngin-f3ts5xr8xpcml36hlkwq8tzfw-nginx-ingress-default-backew5p2n   1/1       Running   0          26s
```

Check the [Getting Started](docs/getting_started.md) doc for more details

### Trouble shooting

- Check operator availability

```shell
% kubectl get deploy,pods
NAME                                                READY     UP-TO-DATE   AVAILABLE   AGE
deployment.apps/multicloud-operators-subscription   1/1       1            1           99m

NAME                                                     READY     STATUS    RESTARTS   AGE
pod/multicloud-operators-subscription-557c676479-dh2fg   1/1       Running   0          24s
```

- Check Subscription and its status

```shell
% kubectl describe subscriptions simple
Name:         simple
Namespace:    default
Labels:       <none>
Annotations:  kubectl.kubernetes.io/last-applied-configuration={"apiVersion":"app.ibm.com/v1alpha1","kind":"Subscription","metadata":{"annotations":{},"name":"simple","namespace":"default"},"spec":{"channel":"dev/d...
API Version:  app.ibm.com/v1alpha1
Kind:         Subscription
Metadata:
  Creation Timestamp:  2019-10-20T00:43:54Z
  Generation:          14
  Resource Version:    39456
  Self Link:           /apis/app.ibm.com/v1alpha1/namespaces/default/subscriptions/simple
  UID:                 2abed0ce-78e0-42e9-bc25-39737bc50220
Spec:
  Channel:  dev/dev-helmrepo
  Name:     nginx-ingress
  Package Overrides:
    Package Name:  nginx-ingress
    Package Overrides:
      Path:   spec.values
      Value:  controller:
  replicaCount: 3

  Placement:
    Local:  true
Status:
  Last Update Time:  2019-10-20T02:46:25Z
  Phase:             Subscribed
  Statuses:
    /:
      Packages:
        Dev - Helmrepo - Nginx - Ingress - 1 . 24 . 3:
          Last Update Time:  2019-10-20T02:46:25Z
          Phase:             Subscribed
          Resource Status:
            Last Update:  2019-10-20T02:46:13Z
            Phase:        Success
Events:                   <none>
```

Please refer to [Trouble shooting documentation](docs/trouble_shooting.md) for further info.

## Community, discussion, contribution, and support

------

Check the [DEVELOPMENT Doc](docs/development.md) for how to build and make changes.

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

You can reach the maintainers of this by raising issues. Slack communication is coming soon

## References

------

### multicloud-operators repositories

- [multicloud-operators-deployable](https://github.com/IBM/multicloud-operators-deployable)
- [multicloud-operators-placementrule](https://github.com/IBM/multicloud-operators-placementrule)
- [multicloud-operators-channel](https://github.com/IBM/multicloud-operators-channel)
- [multicloud-operators-subscription](https://github.com/IBM/multicloud-operators-subscription)
- [multicloud-operators-subscription-release](https://github.com/IBM/multicloud-operators-subscription-release)

------

If you have any further questions, please refer to
[help documentation](docs/help.md) for further information.
