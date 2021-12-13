#! /bin/bash

APPSUB_TOTAL=60

generateAppSubYaml () {
    APPSUB_NUM=1

    while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
        cat appsub-tpl.yaml | sed -e "s/\<APPSUB-NUM\>/${APPSUB_NUM}/g" >> _output/appsub.yaml
        (( APPSUB_NUM = APPSUB_NUM + 1 ))
    done
}

mkdir -p  _output
rm -fr _output/appsub.yaml

generateAppSubYaml
