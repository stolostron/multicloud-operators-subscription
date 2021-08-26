#! /bin/bash

CLUSTER_TOTAL=1000
APPSUB_TOTAL=15

CLUSTER_NUM=1
while [ $CLUSTER_NUM -le $CLUSTER_TOTAL ]; do
    APPSUB_NUM=1
    while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
        CLUSTER=test-cluster-${CLUSTER_NUM}
        APPSUBSTATUS_NAME=appsub-ns-${APPSUB_NUM}.appsub-${APPSUB_NUM}.status
        oc patch appsubstatus -n ${CLUSTER} ${APPSUBSTATUS_NAME} --type merge -p '{"statuses":{"packages":{"clusterrole-5":{"phase":"Failed"}}}}'
        (( APPSUB_NUM = APPSUB_NUM + 1 ))
    done
    (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
done
