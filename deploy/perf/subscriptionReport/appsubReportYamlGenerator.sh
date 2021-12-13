#! /bin/bash

CLUSTER_TOTAL=10
APPSUB_TOTAL=20
OUTPUT_DIR=_output

# appsub failure possibility is 1/5
APPSUB_FAIL=5

# appsub propagation failure possibility is 1/10
APPSUB_PROPAGATION_FAIL=10

# Clean up
rm -fr ${OUTPUT_DIR}
mkdir -p ${OUTPUT_DIR}/

# genereate managed cluster yaml
CLUSTER_NUM=1
while [ $CLUSTER_NUM -le $CLUSTER_TOTAL ]; do
    CLUSTER_NAME="cluster-${CLUSTER_NUM}"

    cat cluster-tpl.yaml | sed -e "s/\<CLUSTER\>/${CLUSTER_NAME}/g" >> ${OUTPUT_DIR}/appsubReport.yaml
    (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
done

# genereate appsub yaml
APPSUB_NUM=1
while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
    APPSUB_NAME="appsub-${APPSUB_NUM}"
    APPSUB_NS="${APPSUB_NAME}-ns"

    cat appsub-tpl.yaml | sed -e "s/\<APPSUB-NS\>/${APPSUB_NS}/g; s/\<APPSUB-NAME\>/${APPSUB_NAME}/g" >> ${OUTPUT_DIR}/appsubReport.yaml
    (( APPSUB_NUM = APPSUB_NUM + 1 ))
done


# genereate app appsubreport yaml
APPSUB_NUM=1
while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
    APPSUB_NAME="appsub-${APPSUB_NUM}"
    APPSUB_NS="${APPSUB_NAME}-ns"

    cat app-appsubreport-tpl.yaml | sed -e "s/\<APPSUB-NS\>/${APPSUB_NS}/g; s/\<APPSUB-NAME\>/${APPSUB_NAME}/g; s/\<CLUSTER-TOTAL\>/${CLUSTER_TOTAL}/g" >> ${OUTPUT_DIR}/appsubReport.yaml
    (( APPSUB_NUM = APPSUB_NUM + 1 ))
done


# genereate cluster appsubreport yaml
CLUSTER_NUM=1
while [ $CLUSTER_NUM -le $CLUSTER_TOTAL ]; do
    CLUSTER_NAME="cluster-${CLUSTER_NUM}"

    cat cluster-appsubreport-header-tpl.yaml | sed -e "s/\<CLUSTER\>/${CLUSTER_NAME}/g" >> ${OUTPUT_DIR}/appsubReport.yaml

    APPSUB_NUM=1
    while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
        APPSUB_NAME="appsub-${APPSUB_NUM}"
        APPSUB_NS="${APPSUB_NAME}-ns"

        CLUSTER_STATUS="deployed"

        if [ $(($RANDOM % $APPSUB_FAIL)) -eq 0 ]; then 
            CLUSTER_STATUS="failed"
        elif [ $(($RANDOM % $APPSUB_PROPAGATION_FAIL)) -eq 0 ]; then
            CLUSTER_STATUS="propagationFailed"
        fi

        cat cluster-appsubreport-result-tpl.yaml | sed -e "s/\<APPSUB-NS\>/${APPSUB_NS}/g; s/\<APPSUB-NAME\>/${APPSUB_NAME}/g; s/\<CLUSTER-STATUS\>/${CLUSTER_STATUS}/g" >> ${OUTPUT_DIR}/appsubReport.yaml

        (( APPSUB_NUM = APPSUB_NUM + 1 ))
    done

    (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
done

# genereate cluster/appsub delete yaml
CLUSTER_NUM=1
while [ $CLUSTER_NUM -le $CLUSTER_TOTAL ]; do
    CLUSTER_NAME="cluster-${CLUSTER_NUM}"

    echo "oc delete namespace ${CLUSTER_NAME}" >> ${OUTPUT_DIR}/delete-app.sh
    (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
done

APPSUB_NUM=1
while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
    APPSUB_NAME="appsub-${APPSUB_NUM}"
    APPSUB_NS="${APPSUB_NAME}-ns"

    echo "oc delete namespace ${APPSUB_NS}" >> ${OUTPUT_DIR}/delete-app.sh
    (( APPSUB_NUM = APPSUB_NUM + 1 ))
done


