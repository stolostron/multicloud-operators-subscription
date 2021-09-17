#! /bin/bash

APPSUB_TOTAL=300

generateDeleteAppSubYaml () {
    APPSUB_NUM=1

    while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
        echo "oc delete appsub -n appsub-ns-${APPSUB_NUM} appsub-${APPSUB_NUM}" >> _output/delete_appsub.yaml
        (( APPSUB_NUM = APPSUB_NUM + 1 ))
    done
}

mkdir -p  _output
rm -fr _output/delete_appsub.yaml

generateDeleteAppSubYaml
