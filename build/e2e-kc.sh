#!/bin/bash
###############################################################################
# Copyright Contributors to the Open Cluster Management project
###############################################################################

set -o nounset
set -o pipefail

### Setup
echo "SETUP ensure app addon is available"
kubectl config use-context kind-cluster1
if kubectl -n open-cluster-management-agent-addon wait --for=condition=available --timeout=300s deploy/application-manager; then
    echo "App addon is available"
else
    echo "FAILED: App addon is not available"
    kubectl -n open-cluster-management-agent-addon get deploy
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

if kubectl get subscriptionstatus.apps.open-cluster-management.io demo-subscription -o yaml | grep InstallSuccessful; then 
    echo "01-placement: found InstallSuccessful in subscription status output"
else
    echo "01-placement: FAILED: InstallSuccessful is not in the subscription status output"
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

if kubectl get subscriptionstatus.apps.open-cluster-management.io nginx-sub -o yaml | grep InstallSuccessful; then 
    echo "02-placementrule: found InstallSuccessful in subscription status output"
else
    echo "02-placementrule: FAILED: InstallSuccessful is not in the subscription status output"
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

### 05-ansiblejob
echo "STARTING test case 05-ansiblejob"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/05-ansiblejob/
sleep 10

if kubectl get subscriptions.apps.open-cluster-management.io ansible-hook -o yaml | grep lastprehookjob | grep prehook-test; then 
    echo "05-ansiblejob: found ansiblejob CR name in subscription output"
else
    echo "05-ansiblejob: FAILED: ansiblejob CR name is not in the subscription output"
    exit 1
fi
if kubectl get ansiblejobs.tower.ansible.com | grep prehook-test; then 
    echo "05-ansiblejob: found ansiblejobs.tower.ansible.com"
else
    echo "05-ansiblejob: FAILED: ansiblejobs.tower.ansible.com not found"
    exit 1
fi
kubectl delete -f test/e2e/cases/05-ansiblejob/
sleep 5
echo "PASSED test case 05-ansiblejob"

### 06-ansiblejob-post
echo "STARTING test case 06-ansiblejob-post"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/06-ansiblejob-post/
sleep 30

if kubectl get subscriptions.apps.open-cluster-management.io ansible-hook -o yaml | grep lastposthookjob | grep posthook-test; then 
    echo "06-ansiblejob-post: found ansiblejob CR name in subscription output"
else
    echo "06-ansiblejob-post: FAILED: ansiblejob CR name is not in the subscription output"
    exit 1
fi
if kubectl get ansiblejobs.tower.ansible.com | grep posthook-test; then 
    echo "06-ansiblejob-post: found ansiblejobs.tower.ansible.com"
else
    echo "06-ansiblejob-post: FAILED: ansiblejobs.tower.ansible.com not found"
    exit 1
fi
echo "PASSED test case 06-ansiblejob-post"

### 07-helm-install-error
echo "STARTING test case 07-helm-install-error"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/07-helm-install-error/
sleep 30
kubectl config use-context kind-cluster1
if kubectl get subscriptionstatus.apps.open-cluster-management.io ingress -o yaml | grep "phase: Failed"; then 
    echo "07-helm-install-error: found failed phase in subscription status output"
else
    echo "07-helm-install-error: FAILED: failed phase is not in the subscription status output"
    exit 1
fi
kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/07-helm-install-error/
sleep 10
echo "PASSED test case 07-helm-install-error"

### 08-helm-upgrade-error
echo "STARTING test case 08-helm-upgrade-error"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/08-helm-upgrade-error/install
sleep 30
kubectl config use-context kind-cluster1
if kubectl get subscriptionstatus.apps.open-cluster-management.io ingress -o yaml | grep "phase: Deployed"; then 
    echo "08-helm-upgrade-error: found deployed phase in subscription status output"
else
    echo "08-helm-upgrade-error: FAILED: deployed phase is not in the subscription status output"
    exit 1
fi
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/08-helm-upgrade-error/upgrade/hub
sleep 10
kubectl config use-context kind-cluster1
kubectl apply -f test/e2e/cases/08-helm-upgrade-error/upgrade/managed
sleep 10
if kubectl get subscriptionstatus.apps.open-cluster-management.io ingress -o yaml | grep "phase: Failed"; then 
    echo "08-helm-upgrade-error: found failed phase in subscription status output"
else
    echo "08-helm-upgrade-error: FAILED: failed phase is not in the subscription status output"
    exit 1
fi
kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/08-helm-upgrade-error/install
sleep 10
echo "PASSED test case 08-helm-upgrade-error"

### 09-helm-missing-phase
echo "STARTING test case 09-helm-missing-phase"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/09-helm-missing-phase/
sleep 30
kubectl config use-context kind-cluster1
if kubectl get subscriptionstatus.apps.open-cluster-management.io preinstall-hook -o yaml | grep "kind: Deployment"; then 
    echo "09-helm-missing-phase: found deployment kind in subscription status output"
else
    echo "09-helm-missing-phase: FAILED: deployment kind is not in the subscription status output"
    exit 1
fi
if kubectl get subscriptionstatus.apps.open-cluster-management.io preinstall-hook -o yaml | grep "phase"; then 
    echo "09-helm-missing-phase: FAILED: found phase in the subscription status output"
    exit 1
else
    echo "09-helm-missing-phase: phase is not in subscription status output"
fi
kubectl config use-context kind-hub
echo "PASSED test case 09-helm-missing-phase"

### 10-cluster-override-ns
echo "STARTING test 10-cluster-override-ns"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/10-cluster-override-ns/
sleep 30
kubectl config use-context kind-cluster1
if kubectl -n test-10 get pod | grep nginx-placement | grep Running; then
    echo "10-cluster-override-ns: appsub deployment pod status is Running"
else
    echo "10-cluster-override-ns FAILED: appsub deployment pod status is Running"
    exit 1
fi
kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/10-cluster-override-ns/
sleep 30
kubectl config use-context kind-cluster1
if kubectl -n test-10 get pod | grep nginx-placement; then
    echo "10-cluster-override-ns FAILED: appsub deployment pod is not deleted"
    exit 1
else
    echo "10-cluster-override-ns: appsub deployment pod is deleted"
fi
echo "PASSED test case 10-cluster-override-ns"

### 11-helm-hub-dryrun
echo "STARTING test 11-helm-hub-dryrun"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/11-helm-hub-dryrun/
sleep 30
if kubectl get subscriptions.apps.open-cluster-management.io ingress-appsub | grep Propagated; then
    echo "11-helm-hub-dryrun: ingress-appsub status is Propagated"
else
    echo "11-helm-hub-dryruns FAILED:  ingress-appsub status is not Propagated"
    exit 1
fi
kubectl config use-context kind-cluster1
if kubectl get subscriptionstatus.apps.open-cluster-management.io ingress-appsub -o yaml | grep InstallError; then
    echo "11-helm-hub-dryrun: found InstallError in subscription status output"
else
    echo "11-helm-hub-dryrun: FAILED: InstallError is not in the subscription status output"
    exit 1
fi
kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/11-helm-hub-dryrun/
sleep 20
echo "PASSED test case 11-helm-hub-dryrun"

### 12-helm-update
echo "STARTING test 12-helm-update"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/12-helm-update/install
sleep 30
if kubectl get subscriptions.apps.open-cluster-management.io nginx-helm-sub | grep Propagated; then
    echo "12-helm-update: nginx-helm-sub status is Propagated"
else
    echo "12-helm-updates FAILED: nginx-helm-sub status is not Propagated"
    exit 1
fi
kubectl config use-context kind-cluster1
if kubectl get deploy nginx-ingress-simple-default-backend| grep "2/2"; then
    echo "12-helm-update: found 2/2 in deploy nginx-ingress-simple-default-backend"
else
    echo "12-helm-update: FAILED: 2/2 is not in in deploy nginx-ingress-simple-default-backend"
    exit 1
fi
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/12-helm-update/upgrade
sleep 120
kubectl config use-context kind-cluster1
if kubectl get deploy nginx-ingress-simple-default-backend| grep "1/1"; then
    echo "12-helm-update: found 1/1 in deploy nginx-ingress-simple-default-backend"
else
    echo "12-helm-update: FAILED: 1/1 is not in in deploy nginx-ingress-simple-default-backend"
    exit 1
fi
kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/12-helm-update/install
echo "PASSED test case 12-helm-update"

### 13-git-res-name
echo "STARTING test 13-git-res-name"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/13-git-res-name/
sleep 30
if kubectl get subscriptions.apps.open-cluster-management.io git-app-sub | grep Propagated; then
    echo "13-git-res-name: hub subscriptions.apps.open-cluster-management.io status is Propagated"
else
    echo "13-git-res-name FAILED: hub subscriptions.apps.open-cluster-management.io status is not Propagated"
    exit 1
fi
kubectl delete -f test/e2e/cases/13-git-res-name/
echo "PASSED test case 13-git-res-name"

### 14-helm-appsubstatus
echo "STARTING test 14-helm-appsubstatus"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/14-helm-appsubstatus/install
sleep 30
if kubectl get subscriptionreport.apps.open-cluster-management.io nginx-helm-sub | grep nginx-helm-sub; then
    echo "14-helm-appsubstatus: nginx-helm-sub subscriptionreport is found"
else
    echo "14-helm-appsubstatus FAILED: nginx-helm-sub subscriptionreport is not found"
    exit 1
fi
kubectl delete subscriptionreport.apps.open-cluster-management.io nginx-helm-sub
kubectl -n cluster1 delete subscriptionreport.apps.open-cluster-management.io cluster1
kubectl config use-context kind-cluster1
if kubectl get subscriptionstatus.apps.open-cluster-management.io nginx-helm-sub | grep nginx-helm-sub; then
    echo "14-helm-appsubstatus: nginx-helm-sub subscriptionstatus is found"
else
    echo "14-helm-appsubstatus FAILED: nginx-helm-sub subscriptionstatus is not found"
    exit 1
fi
kubectl delete subscriptionstatus.apps.open-cluster-management.io nginx-helm-sub
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/14-helm-appsubstatus/upgrade
sleep 120
if kubectl get subscriptionreport.apps.open-cluster-management.io nginx-helm-sub | grep nginx-helm-sub; then
    echo "14-helm-appsubstatus: nginx-helm-sub subscriptionreport is found"
else
    echo "14-helm-appsubstatus FAILED: nginx-helm-sub subscriptionreport is not found"
    exit 1
fi
kubectl config use-context kind-cluster1
if kubectl get subscriptionstatus.apps.open-cluster-management.io nginx-helm-sub | grep nginx-helm-sub; then
    echo "14-helm-appsubstatus: nginx-helm-sub subscriptionstatus is found"
else
    echo "14-helm-appsubstatus FAILED: nginx-helm-sub subscriptionstatus is not found"
    exit 1
fi
kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/14-helm-appsubstatus/install
echo "PASSED test case 14-helm-appsubstatus"

### 15-git-helm
echo "STARTING test 15-git-helm"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/15-git-helm/install
sleep 30
if kubectl get subscriptions.apps.open-cluster-management.io git-app-sub | grep Propagated; then
    echo "15-git-helm: hub subscriptions.apps.open-cluster-management.io status is Propagated"
else
    echo "15-git-helm FAILED: hub subscriptions.apps.open-cluster-management.io status is not Propagated"
    exit 1
fi
kubectl apply -f test/e2e/cases/15-git-helm/update
sleep 120
kubectl config use-context kind-cluster1
if kubectl get helmrelease.apps.open-cluster-management.io | grep mortgage; then
    echo "15-git-helm FAILED: helmrelease.apps.open-cluster-management.io still showing mortgage app"
    exit 1
else
    echo "15-git-helm: hub helmrelease.apps.open-cluster-management.io is not showing mortgage app"
fi
kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/15-git-helm/install
echo "PASSED test case 15-git-helm"

### 16-helm-recreate
echo "STARTING test 16-helm-recreate"
kubectl config use-context kind-hub
kubectl apply -f test/e2e/cases/16-helm-recreate
sleep 30
if kubectl get subscriptions.apps.open-cluster-management.io nginx-helm-sub | grep Propagated; then
    echo "16-helm-recreate: nginx-helm-sub status is Propagated"
else
    echo "16-helm-recreate FAILED: nginx-helm-sub status is not Propagated"
    exit 1
fi
kubectl config use-context kind-cluster1
if kubectl delete service nginx-ingress-simple-controller; then
    echo "16-helm-recreate: service nginx-ingress-simple-controller is deleted"
else
    echo "16-helm-recreate FAILED: service nginx-ingress-simple-controller is not deleted"
    exit 1
fi
sleep 10
if kubectl get service nginx-ingress-simple-controller; then
    echo "16-helm-recreate: service nginx-ingress-simple-controller is recreated"
else
    echo "16-helm-recreate FAILED: service nginx-ingress-simple-controller is not recreated"
    exit 1
fi

kubectl config use-context kind-hub
kubectl delete -f test/e2e/cases/16-helm-recreate
echo "PASSED test case 16-helm-recreate"
