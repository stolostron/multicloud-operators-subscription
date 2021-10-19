#! /bin/bash

usage () {
    echo "getAppSubStatus.sh is to get the AppSub detailed Status from the given managed cluster"
    echo ""
    echo "Options:"
    echo "  -c     managed cluster name"
    echo "  -s     AppSub Namespace"
    echo "  -n     AppSub Name"
    echo "  -h     Help"
    echo ""
    echo "Example: ./getAppSubStatus.sh -c cluster1 -s appsubNS1 -n appsub1"
}

check_dependency () {
  which jq > /dev/null
  if [ $? -ne 0 ]; then
    echo "jq is not installed."
    exit 1
  fi
  which ruby > /dev/null
  if [ $? -ne 0 ]; then
    echo "ruby is not installed."
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

echo "Cluster Name: $cluster";
echo "App NameSpace: $appNs";
echo "App Name: $appName";

echo "==== Validating HUB Cluster Access ===="
kubectl cluster-info > /dev/null
if [ $? -ne 0 ]; then
    echo "hub cluster Not accessed."
    exit 1
fi

echo "==== Validating Managed cluster ===="
kubectl get managedcluster $cluster > /dev/null
if [ $? -ne 0 ]; then
    echo "Managed cluster Not found."
    exit 1
fi

kubectl apply -f - -o yaml > /dev/null << EOF
apiVersion: view.open-cluster-management.io/v1beta1
kind: ManagedClusterView
metadata:
  name: getappsubstatus
  namespace: ${cluster}
spec:
  scope:
    apiGroup: apps.open-cluster-management.io
    kind: SubscriptionStatus
    version: v1alpha1
    resource: subscriptionstatuses
    name: ${appName}
    namespace: ${appNs}
EOF

kubectl get managedclusterview -n ${cluster} getappsubstatus -o jsonpath='{.status.result}' | jq .statuses | ruby -ryaml -rjson -e 'puts YAML.dump(JSON.parse(STDIN.read))'

kubectl delete managedclusterview -n ${cluster} getappsubstatus --ignore-not-found=true  > /dev/null 2>&1

