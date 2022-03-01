#!/bin/bash
###############################################################################
# Copyright Contributors to the Open Cluster Management project
###############################################################################

set -o nounset
set -o pipefail

### Setup
echo "SETUP ensure app addon is available"
kubectl config use-context kind-cluster1
if kubectl -n open-cluster-management-agent-addon wait --for=condition=available --timeout=300s deploy/multicluster-operators-subscription; then
    echo "App addon is available"
else
    echo "FAILED: App addon is not available"
    exit 1
fi

### 01-placement
echo "STARTING test case 01-placement"
kubectl config use-context kind-hub
kubectl label managedcluster cluster1 cluster.open-cluster-management.io/clusterset=app-demo --overwrite
kubectl label managedcluster cluster1 purpose=test --overwrite
kubectl apply -f test/e2e/cases/01-placement/
sleep 30

if kubectl get subscriptions.apps.open-cluster-management.io demo-subscription | grep Propagated; then 
    echo "01-placement: hub subscriptions.apps.open-cluster-management.io status is Propagated"
else
    echo "01-placement FAILED: hub subscriptions.apps.open-cluster-management.io status is not Propagated"
    exit 1
fi

kubectl config use-context kind-cluster1
if kubectl get subscriptions.apps.open-cluster-management.io demo-subscription | grep Subscribed; then 
    echo "01-placement: cluster1 subscriptions.apps.open-cluster-management.io status is Subscribed"
else
    echo "01-placement FAILED: cluster1 subscriptions.apps.open-cluster-management.io status is not Subscribed"
    exit 1
fi

if kubectl get pod | grep nginx-ingress-simple-controller | grep Running; then
    echo "01-placement: appsub deployment pod status is Running"
else
    echo "01-placement FAILED: appsub deployment pod status is Running"
    exit 1
fi

kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/01-placement/
sleep 30
kubectl config use-context kind-cluster1
if kubectl get pod | grep nginx-ingress-simple-controller; then
    echo "01-placement FAILED: appsub deployment pod is not deleted"
    exit 1
else
    echo "01-placement: appsub deployment pod is deleted"
fi
echo "PASSED test case 01-placement"

### 02-placementrule
echo "STARTING test 02-placementrule"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/02-placementrule/
sleep 30

if kubectl get subscriptions.apps.open-cluster-management.io nginx-sub | grep Propagated; then 
    echo "02-placementrule: hub subscriptions.apps.open-cluster-management.io status is Propagated"
else
    echo "02-placementrule FAILED: hub subscriptions.apps.open-cluster-management.io status is not Propagated"
    exit 1
fi

kubectl config use-context kind-cluster1
if kubectl get subscriptions.apps.open-cluster-management.io nginx-sub | grep Subscribed; then 
    echo "02-placementrule: cluster1 subscriptions.apps.open-cluster-management.io status is Subscribed"
else
    echo "02-placementrule FAILED: cluster1 subscriptions.apps.open-cluster-management.io status is not Subscribed"
    exit 1
fi

if kubectl get pod | grep nginx-ingress- | grep Running; then
    echo "02-placementrule: appsub deployment pod status is Running"
else
    echo "02-placementrule FAILED: appsub deployment pod status is Running"
    exit 1
fi

kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/02-placementrule/
sleep 30
kubectl config use-context kind-cluster1
if kubectl get pod | grep nginx-ingress-; then
    echo "02-placementrule FAILED: appsub deployment pod is not deleted"
    exit 1
else
    echo "02-placementrule: appsub deployment pod is deleted"
fi
echo "PASSED test case 02-placementrule"

### 03-keep-namespace
echo "STARTING test 03-keep-namespace"
kubectl config use-context kind-hub
kubectl create ns test-case-03
kubectl apply -f test/e2e/cases/03-keep-namespace/
sleep 30

kubectl config use-context kind-cluster1
if kubectl get ns test-case-03; then 
    echo "03-keep-namespace: cluster1 namespace 03-keep-namespace is created"
else
    echo "03-keep-namespace FAILED: cluster1 namespace 03-keep-namespace is not present"
    exit 1
fi

kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/03-keep-namespace/
sleep 30
kubectl config use-context kind-cluster1
if kubectl get ns test-case-03; then 
    echo "03-keep-namespace: cluster1 namespace 03-keep-namespace is still present"
else
    echo "03-keep-namespace FAILED: cluster1 namespace 03-keep-namespace is not present"
    exit 1
fi
echo "PASSED test case 03-keep-namespace"

### 04-helm-no-match
echo "STARTING test case 04-helm-no-match"
kubectl config use-context kind-hub
kubectl label managedcluster cluster1 cluster.open-cluster-management.io/clusterset=app-demo --overwrite
kubectl label managedcluster cluster1 purpose=test --overwrite
kubectl apply -f test/e2e/cases/04-helm-no-match/
sleep 10

if kubectl get subscriptions.apps.open-cluster-management.io demo-subscription | grep PropagationFailed; then 
    echo "04-helm-no-match: hub subscriptions.apps.open-cluster-management.io status is PropagationFailed"
else
    echo "04-helm-no-match FAILED: hub subscriptions.apps.open-cluster-management.io status is not PropagationFailed"
    exit 1
fi
if kubectl get subscriptions.apps.open-cluster-management.io demo-subscription -o yaml | grep "unable to find any matching Helm chart"; then 
    echo "04-helm-no-match: hub subscriptions.apps.open-cluster-management.io status contains proper error message"
else
    echo "04-helm-no-match FAILED: hub subscriptions.apps.open-cluster-management.io status does not contains proper error message"
    exit 1
fi
echo "PASSED test case 04-helm-no-match"
