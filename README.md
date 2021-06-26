# multicloud-operators-subscription

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

- [Overview](#overview)
- [Architecutre](#architecutre)
- [Stand-alone deployment](#stand-alone-deployment)
- [Multi-cluster deployment](#multi-cluster-deployment)
- [GitOps](#gitops)
- [Community, discussion, contribution, and support](#community,-discussion,-contribution,-and-support)

## Overview

Subscriptions (subscription.apps.open-cluster-management.io) allow clusters to subscribe to a source repository [channel](https://github.com/open-cluster-management-io/multicloud-operators-channel) that can be the following types: Git repository, Helm release registry, or Object storage repository.

Subscriptions can point to a channel for identifying new or updated resource templates. The subscription operator can then download directly from the storage location and deploy to targeted managed clusters without checking the hub cluster first. With a subscription, the subscription operator can monitor the channel for new or updated resources instead of the hub cluster.

## Architecutre

![architecture](images/architecture.png)

## Stand-alone deployment

Deploy the subscription operator.

```shell
$ make deploy-standalone
$ kubectl -n open-cluster-management get deploy  multicloud-operators-subscription
NAME                                READY   UP-TO-DATE   AVAILABLE   AGE
multicloud-operators-subscription   1/1     1            1           21m
```

Create a Helm channel and subscribe to it.

```shell
kubectl apply -f ./examples/helmrepo-channel
```

Find the nginx pods that are deployed to the current namespace. You should have 3 backend pods with the controller.

```shell
$ kubectl get pods -l app=nginx-ingress
NAME                                                    READY   STATUS    RESTARTS   AGE
nginx-ingress-simple-controller-6b57886cf8-pmqs5        1/1     Running   0          21m
nginx-ingress-simple-default-backend-666d7d77fc-cgfwn   1/1     Running   0          21m
nginx-ingress-simple-default-backend-666d7d77fc-q8gdg   1/1     Running   0          21m
nginx-ingress-simple-default-backend-666d7d77fc-wls8f   1/1     Running   0          21m
```

## Multi-cluster deployment

Setup a _hub_ cluster and a _managed_ cluster using [clusteradm](https://github.com/open-cluster-management-io/clusteradm#quick-start).

Deploy the subscription operator on the _hub_ cluster.

```shell
$ make deploy-hub
$ kubectl -n open-cluster-management get deploy  multicloud-operators-subscription
NAME                                READY   UP-TO-DATE   AVAILABLE   AGE
multicloud-operators-subscription   1/1     1            1           25s
```

Deploy the subscription agent on the _managed_ cluster.

```shell
$ make deploy-managed 
$ kubectl -n open-cluster-management-agent-addon get deploy  multicloud-operators-subscription
NAME                                READY   UP-TO-DATE   AVAILABLE   AGE
multicloud-operators-subscription   1/1     1            1           103s
```

Deploy an application subscription on the _hub_ cluster and it will propagate down to the _managed_ cluster

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

## GitOps

You can subscribe to public or enterprise Git repositories that contain Kubernetes resource YAML files or Helm charts, or both. See [Git repository channel subscription](docs/gitrepo_subscription.md) for more details.

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

### Communication channels

Slack channel: [#open-cluster-mgmt](http://slack.k8s.io/#open-cluster-mgmt)

## License

This code is released under the Apache 2.0 license. See the file LICENSE for more information.
