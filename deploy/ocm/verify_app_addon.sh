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

echo "############  access cluster1"
$KUBECTL config use-context kind-cluster1

waitForRes "pods" "application-manager" "open-cluster-management-agent-addon" ""
exit 0