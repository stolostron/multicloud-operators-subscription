#! /bin/bash

CLUSTER_TOTAL=1000
APPSUB_TOTAL=15
PACKAGE_TOTAL=5
UPDATE_TOTAL=5

generateAppSubStatusYaml () {
    CLUSTER_NUM=1
    while [ $CLUSTER_NUM -le $CLUSTER_TOTAL ]; do
        APPSUB_NUM=1
        while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
            PACKAGE_NUM=1
            while [ $PACKAGE_NUM -le $PACKAGE_TOTAL ]; do
                UPDATE_NUM=1
                while [ $UPDATE_NUM -le $UPDATE_TOTAL ]; do
                    echo "CLUSTER_NUM: $CLUSTER_NUM, APPSUB_NUM: $APPSUB_NUM, PACKAGE_NUM: $PACKAGE_NUM, UPDATE_NUM:$UPDATE_NUM"
                    cat appSubStatus-tpl.yaml | sed -e "s/\<CLUSTER-NUM\>/${CLUSTER_NUM}/g; s/\<APPSUB-NUM\>/${APPSUB_NUM}/g; s/\<PACKAGE-NUM\>/${PACKAGE_NUM}/g; s/\<UPDATE-NUM\>/${UPDATE_NUM}/g" >> _output/appSubStatusResult-cluster-${CLUSTER_NUM}.yaml
                    (( UPDATE_NUM = UPDATE_NUM + 1 ))
                done
                (( PACKAGE_NUM = PACKAGE_NUM + 1 ))
            done
            (( APPSUB_NUM = APPSUB_NUM + 1 ))
        done
        (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
    done

}

generateAppSubYaml () {
    APPSUB_NUM=1

    while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
        cat appSub-tpl.yaml | sed -e "s/\<APPSUB-NUM\>/${APPSUB_NUM}/g" >> _output/appSubStatusResult-app-ns.yaml
        (( APPSUB_NUM = APPSUB_NUM + 1 ))
    done
}

generateManagedClusterYaml() {
    CLUSTER_NUM=1

    while [ $CLUSTER_NUM -le $CLUSTER_TOTAL ]; do
        cat cluster-tpl.yaml | sed -e "s/\<CLUSTER-NUM\>/${CLUSTER_NUM}/g" >> _output/appSubStatusResult-cluster-ns.yaml
        (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
    done
}

generateBatchCLI() {
    CLUSTER_NUM=1

    echo "oc apply -f appSubStatusResult-cluster-ns.yaml" >> _output/batchUpdateAppSubStatus.sh
    echo "oc delete -f appSubStatusResult-cluster-ns.yaml" >> _output/batchCleanup.sh

    echo "oc apply -f appSubStatusResult-app-ns.yaml" >> _output/batchUpdateAppSubStatus.sh
    echo "oc delete -f appSubStatusResult-app-ns.yaml" >> _output/batchCleanup.sh

    while [ $CLUSTER_NUM -le $CLUSTER_TOTAL ]; do
        echo "oc apply -f appSubStatusResult-cluster-${CLUSTER_NUM}.yaml&" >> _output/batchUpdateAppSubStatus.sh
        (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
    done

    chmod 755 _output/batchUpdateAppSubStatus.sh
    chmod 755 _output/batchCleanup.sh
}

rm -fr _output
mkdir -p _output/

generateManagedClusterYaml
generateAppSubYaml
generateAppSubStatusYaml
generateBatchCLI
