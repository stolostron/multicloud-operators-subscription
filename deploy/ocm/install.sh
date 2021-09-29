#!/bin/bash

set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}

rm -rf registration-operator

echo "############  Cloning registration-operator"
git clone https://github.com/open-cluster-management-io/registration-operator.git

cd registration-operator || {
  printf "cd failed, registration-operator does not exist"
  return 1
}

echo "############  Deploying hub"
$KUBECTL config use-context kind-hub
make deploy-hub
if [ $? -ne 0 ]; then
 echo "############  Failed to deploy"
 exit 1
fi

kind get kubeconfig --name hub --internal > ./.hub-kubeconfig

for i in {1..7}; do
  echo "############$i  Checking cluster-manager-registration-controller"
  RUNNING_POD=$($KUBECTL -n open-cluster-management-hub get pods | grep cluster-manager-registration-controller | grep -c "Running")
  if [ "${RUNNING_POD}" -ge 1 ]; then
    break
  fi

  if [ $i -eq 7 ]; then
    echo "!!!!!!!!!!  the cluster-manager-registration-controller is not ready within 3 minutes"
    $KUBECTL -n open-cluster-management-hub get pods

    exit 1
  fi
  sleep 30
done

for i in {1..7}; do
  echo "############$i  Checking cluster-manager-registration-webhook"
  RUNNING_POD=$($KUBECTL -n open-cluster-management-hub get pods | grep cluster-manager-registration-webhook | grep -c "Running")
  if [ "${RUNNING_POD}" -ge 1 ]; then
    break
  fi

  if [ $i -eq 7 ]; then
    echo "!!!!!!!!!!  the cluster-manager-registration-webhook is not ready within 3 minutes"
    $KUBECTL -n open-cluster-management-hub get pods
    exit 1
  fi
  sleep 30s
done

echo "############  Deploying managed cluster"
$KUBECTL config use-context kind-cluster1
make deploy-spoke
if [ $? -ne 0 ]; then
 echo "############  Failed to deploy"
 exit 1
fi

for i in {1..7}; do
  echo "############$i  Checking klusterlet-registration-agent"
  RUNNING_POD=$($KUBECTL -n open-cluster-management-agent get pods | grep klusterlet-registration-agent | grep -c "Running")
  if [ ${RUNNING_POD} -ge 1 ]; then
    break
  fi

  if [ $i -eq 7 ]; then
    echo "!!!!!!!!!!  the klusterlet-registration-agent is not ready within 3 minutes"
    $KUBECTL -n open-cluster-management-agent get pods
    exit 1
  fi
  sleep 30
done

$KUBECTL get ns open-cluster-management-agent-addon ; if [ $? -ne 0 ] ; then kubectl create ns open-cluster-management-agent-addon ; fi

echo "############  env is installed successfully!!"

$KUBECTL config use-context kind-hub

echo "############  Cleanup"
cd ../ || exist
rm -rf registration-operator

echo "############  Finished installation!!!"
