# multicloud-operators-subscription

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

- [Overview](#overview)
- [Architecutre](#architecutre)
- [Stand-alone deployment](#stand-alone-deployment)
- [Multi-cluster deployment](#multi-cluster-deployment)
- [GitOps](docs/gitrepo_subscription.md)
- [Security response](#security-response)
- [References](#references)

## Overview

Subscribes resources from channels and apply them to Kubernetes clusters.

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

### Multi-cluster deployment

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

## Security response

Check the [Security Doc](SECURITY.md) if you find a security issue.

## References

- The `multicloud-operators-subscription` is part of the `open-cluster-management` community. For more information, visit: [open-cluster-management.io](https://open-cluster-management.io).
