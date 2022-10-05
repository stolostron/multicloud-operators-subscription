#!/bin/bash

set -o nounset
set -o pipefail

waitForRes() {
    FOUND=1
    MINUTE=0
    resKinds=$1
    resName=$2
    resNamespace=$3
    ignore=$4
    metServName=$5
    metServPort=$6
    running="\([0-9]\+\)\/\1"
    printf "\n#####\nWait for ${resNamespace}/${resName} to reach running state (3min).\n"
    while [ ${FOUND} -eq 1 ]; do
        # Wait up to 3min, should only take about 20-30s
        if [ $MINUTE -gt 180 ]; then
            echo "Timeout waiting for the ${resNamespace}\/${resName}."
            echo "List of current resources:"
            oc get ${resKinds} -n ${resNamespace} ${resName}
            echo "You should see ${resNamespace}/${resName} ${resKinds}"
            if [ "${resKinds}" == "pods" ]; then
                APP_ADDON_POD=$($KUBECTL get pods -n ${resNamespace} |grep ${resName} |awk -F' ' '{print $1}')

                echo "##### Output the pod log\n"
                $KUBECTL logs -n ${resNamespace} $APP_ADDON_POD

                echo "##### Describe the pod status\n"
                $KUBECTL describe pods -n ${resNamespace} $APP_ADDON_POD
            fi
            exit 1
        fi
        if [ "$ignore" == "" ]; then
            operatorRes=`oc get ${resKinds} -n ${resNamespace} | grep ${resName}`
        else
            operatorRes=`oc get ${resKinds} -n ${resNamespace} | grep ${resName} | grep -v ${ignore}`
        fi
        if [[ $(echo $operatorRes | grep "${running}") ]]; then
            echo "* ${resName} is running"
            if [[ "$metServName" != "" && "$metServPort" != "" ]]; then
              # verify metrics service
              if `oc get svc -n ${resNamespace} ${metServName} -o custom-columns=n:.spec.ports[0].name,p:.spec.ports[0].targetPort | awk -v port=$metServPort 'NR==2{if ($1 == "metrics" && $2 == port){exit 0} exit 1}'`; then
                echo "* ${metServName} service is created successfully"
              else
                echo "* ${metServName} service is not created successfully, 'metrics' endpoint name and '$metServPort' target port are required"
                exit 1
              fi
            fi
            break
        elif [ "$operatorRes" == "" ]; then
            operatorRes="Waiting"
        fi
        echo "* STATUS: $operatorRes"
        sleep 5
        (( MINUTE = MINUTE + 5 ))
    done
}

KUBECTL=${KUBECTL:-kubectl}

if [ $1 == "hub" ]; then
  echo "############  access hub"
  $KUBECTL config use-context kind-hub

  waitForRes "pods" "multicluster-operators-subscription" "open-cluster-management" "" "hub-subscription-metrics" "8381"
  exit 0
elif [ $1 == "mc" ]; then
  echo "############  access cluster1"
  $KUBECTL config use-context kind-cluster1

  waitForRes "pods" "application-manager" "open-cluster-management-agent-addon" "" "mc-subscription-metrics" "8388"
  exit 0
else
  echo "no cluster type selected"
  exit 1
fi
