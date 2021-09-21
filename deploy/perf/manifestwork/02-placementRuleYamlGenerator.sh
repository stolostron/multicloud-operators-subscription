#! /bin/bash

APPSUB_TOTAL=60

generatePlacementRuleYaml () {
    APPSUB_NUM=1

    while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
        cat placementrule-tpl.yaml | sed -e "s/\<APPSUB-NUM\>/${APPSUB_NUM}/g" >> _output/placementrule.yaml
        (( APPSUB_NUM = APPSUB_NUM + 1 ))
    done
}

mkdir -p  _output
rm -fr _output/placementrule.yaml

generatePlacementRuleYaml
