#! /bin/bash

usage () {
    echo "getLastUpdateTime.sh is to get the last update time for an ApspSub on a given managed cluster"
    echo ""
    echo "Options:"
    echo "  -c     managed cluster name"
    echo "  -s     AppSub Namespace"
    echo "  -n     AppSub Name"
    echo "  -h     Help"
    echo ""
    echo "Example: ./getLastUpdateTime.sh -c cluster1 -s appsubNS1 -n appsub1"
}

check_dependency () {
  which jq > /dev/null
  if [ $? -ne 0 ]; then
    echo "jq is not installed."
    exit 1
  fi
}

check_dependency

if [ "$#" -lt 1 ]; then
  usage
  exit 0
fi

while getopts "h:c:s:n:" arg; do
  case $arg in
    c)
      cluster="${OPTARG}"
      ;;
    s)
      appNs="${OPTARG}"
      ;;
    n)
      appName="${OPTARG}"
      ;;
    :)
      usage
      exit 0
      ;;
    *)
      usage
      exit 0
      ;;
  esac
done

echo "==== Validating HUB Cluster Access ===="
kubectl cluster-info > /dev/null
if [ $? -ne 0 ]; then
    echo "Hub cluster not accessible."
    exit 1
fi

echo "==== Validating Managed cluster: ${cluster} ===="
kubectl get managedcluster $cluster > /dev/null
if [ $? -ne 0 ]; then
    echo "Managed cluster '${cluster}'  not found."
    exit 1
fi

localcluster=($(oc get managedclusters -l local-cluster=true --no-headers=true -o name | awk -F "/" '{print $2}'))
if [ "$cluster" == "$localcluster" ]; then
  appName="${appName}-local"
fi

echo "==== Validating AppSub on Hub: ${appNs}/${appName} ===="
kubectl get appsub -n $appNs $appName > /dev/null
if [ $? -ne 0 ]; then
    echo "AppSub '${appNs}/${appName}' not found on the Hub."
    exit 1
fi

# Delete if there is an existing managedclusterview
kubectl get managedclusterview -n $cluster getappsub > /dev/null 2>&1 
if [ $? -eq 0 ]; then
    kubectl delete managedclusterview -n ${cluster} getappsub --ignore-not-found=true  > /dev/null 2>&1
fi


kubectl apply -f - -o yaml > /dev/null << EOF
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: getappsub
  namespace: ${cluster}
spec:
  scope:
    apiGroup: apps.open-cluster-management.io
    kind: Subscription
    version: v1alpha1
    resource: subscriptions
    name: ${appName}
    namespace: ${appNs}
EOF

# Check status of the managed cluster view
result=($(kubectl get managedclusterview -n ${cluster} getappsub -o jsonpath='{.status.conditions}' | jq --raw-output .[].status))
if [ "$result" = "False" ]; then
    echo "AppSub '${appNs}/${appName}' not found on managed cluster '${cluster}'"
    exit 1
fi

echo -n "LastUpdateTime: "
kubectl get managedclusterview -n ${cluster} getappsub -o jsonpath='{.status.result}' | jq --raw-output .status.lastUpdateTime 

kubectl delete managedclusterview -n ${cluster} getappsub --ignore-not-found=true  > /dev/null 2>&1

