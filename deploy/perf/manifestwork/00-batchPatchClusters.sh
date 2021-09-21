#!/bin/bash


APPSUB_IMAGE="quay.io\/xiangjingli\/multicluster-operators-subscription:latest"
CLUSTER_NUM=0

for cluster in $(oc get managedclusters | awk '{print $1}' | grep -v "NAME\|local-cluster")
do
  (( CLUSTER_NUM = CLUSTER_NUM + 1 ))

  if [ $(($CLUSTER_NUM%10)) -eq 0 ]; then
    echo "========"
    echo "Cluster count: $CLUSTER_NUM..."
  fi

  echo "========"
  echo "Deploy clusterRole and appsub status CRD to cluster $cluster"
  cat manifestwork-tpl.yaml | sed -e "s/\<CLUSTER-NAME\>/${cluster}/g" | kubectl apply -f - > /dev/null

  echo "Pause klusterletaddonconfig for cluster ${cluster}"
  kubectl annotate klusterletaddonconfig -n ${cluster} ${cluster} klusterletaddonconfig-pause=true --overwrite=true > /dev/null

  echo "Override appmgr addon image for cluster ${cluster}"
  kubectl get manifestwork -n ${cluster} ${cluster}-klusterlet-addon-appmgr -o yaml \
  | sed -e "/.*multicluster_operators_subscription.*/s/:.*$/: ${APPSUB_IMAGE}/g" \
  | kubectl apply -f - > /dev/null
done

echo "========"
echo "Total clusters: $CLUSTER_NUM."
