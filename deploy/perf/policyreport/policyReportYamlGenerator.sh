#! /bin/bash

CLUSTER_NUM_START=1601
CLUSTER_NUM_END=2000
APPSUB_TOTAL=300
APPSUB_DEPLOYED_TOTAL=270 # Remaing appsubs failed to deploy
OUTPUT_DIR=_output
APPSUB_OUTPUT_DIR=_outputAppSub

generatePolicyReportYaml () {
    # Clean up
    rm -fr ${OUTPUT_DIR}
    mkdir -p ${OUTPUT_DIR}/

    CLUSTER_NUM=$CLUSTER_NUM_START
    while [ $CLUSTER_NUM -le $CLUSTER_NUM_END ]; do
        CLUSTER_NAME="cluster-${CLUSTER_NUM}"

	# Header for policy report
        cat policyReport-header-tpl.yaml | sed -e "s/\<CLUSTER-NAME\>/${CLUSTER_NAME}/g; s/\<APPSUB-NAME\>/${APPSUB_NAME}/g; s/\<APPSUB-NS\>/${APPSUB_NS}/g" >> ${OUTPUT_DIR}/policyReport-cluster-${CLUSTER_NUM}.yaml

        APPSUB_NUM=1
        while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
            APPSUB_NAME="guestbook-${APPSUB_NUM}"
            APPSUB_NS="${APPSUB_NAME}-ns"

            echo "CLUSTER_NAME: $CLUSTER_NAME, APPSUB: $APPSUB_NS/$APPSUB_NAME"
            cat policyReport-app-resources-tpl.yaml | sed -e "s/\<CLUSTER-NAME\>/${CLUSTER_NAME}/g; s/\<APPSUB-NAME\>/${APPSUB_NAME}/g; s/\<APPSUB-NS\>/${APPSUB_NS}/g" >> ${OUTPUT_DIR}/policyReport-cluster-${CLUSTER_NUM}.yaml

            # Add template for failed apps
            if [ $APPSUB_NUM -gt $APPSUB_DEPLOYED_TOTAL ]; then
                cat policyReport-app-failed-tpl.yaml | sed -e "s/\<CLUSTER-NAME\>/${CLUSTER_NAME}/g; s/\<APPSUB-NAME\>/${APPSUB_NAME}/g; s/\<APPSUB-NS\>/${APPSUB_NS}/g" >> ${OUTPUT_DIR}/policyReport-cluster-${CLUSTER_NUM}.yaml
            fi

            (( APPSUB_NUM = APPSUB_NUM + 1 ))
        done

	# Footer for policy report
        cat policyReport-footer-tpl.yaml | sed -e "s/\<CLUSTER-NAME\>/${CLUSTER_NAME}/g" >> ${OUTPUT_DIR}/policyReport-cluster-${CLUSTER_NUM}.yaml

        (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
    done
}

generateAppSubYaml () {
    # Clean up
    rm -fr ${APPSUB_OUTPUT_DIR}
    mkdir -p ${APPSUB_OUTPUT_DIR}/

    APPSUB_NUM=1
    while [ $APPSUB_NUM -le $APPSUB_TOTAL ]; do
        APPSUB_NAME="guestbook-${APPSUB_NUM}"
        APPSUB_NS="${APPSUB_NAME}-ns"

        echo "APPSUB: $APPSUB_NS/$APPSUB_NAME"
        cat appsub-tpl.yaml | sed -e "s/\<APPSUB-NAME\>/${APPSUB_NAME}/g; s/\<APPSUB-NS\>/${APPSUB_NS}/g" >> ${APPSUB_OUTPUT_DIR}/appSub.yaml

        (( APPSUB_NUM = APPSUB_NUM + 1 ))
    done
}

applyPolicyReportYaml () {
    CLUSTER_NUM=$CLUSTER_NUM_START
    while [ $CLUSTER_NUM -le $CLUSTER_NUM_END ]; do
        CLUSTER_FILE="${OUTPUT_DIR}/policyReport-cluster-${CLUSTER_NUM}.yaml"

        if [ -f $CLUSTER_FILE ]; then
            echo "Applying ${CLUSTER_FILE}"
            oc apply -f ${CLUSTER_FILE}
        fi

        (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
    done
}

deletePolicyReportYaml () {
    CLUSTER_NUM=$CLUSTER_NUM_START
    while [ $CLUSTER_NUM -le $CLUSTER_NUM_END ]; do
        CLUSTER_FILE="${OUTPUT_DIR}/policyReport-cluster-${CLUSTER_NUM}.yaml"

        if [ -f $CLUSTER_FILE ]; then
            echo "Deleting ${CLUSTER_FILE}"
            oc delete -f ${CLUSTER_FILE}
        fi 

        (( CLUSTER_NUM = CLUSTER_NUM + 1 ))
    done
}

usage () {
    echo "Polcy Report YAML Helper"
    echo ""
    echo "Options:"
    echo "g     Generate Policy Report YAMLs"
    echo "s     Generate AppSub YAMLs"
    echo "a     Apply Policy Report YAMLs"
    echo "d     Delete Policy Report YAMLs"
    echo "h     Help"
    echo ""
    echo "Example, to generate and apply a priority report: ./policyReportYamlGenerator.sh -ga"
}


if [ "$#" -lt 1 ]; then
  usage
  exit
fi

while getopts "hgads" arg; do

  case $arg in
    g)
      echo "#######Generate policy report YAMLs#######\n"
      generatePolicyReportYaml
      ;;
    s)
      echo "#######Generate AppSub YAMLs#######\n"
      generateAppSubYaml
      ;;
    a)
      echo "#######Apply policy report YAMLs#######\n"
      applyPolicyReportYaml
      ;;
    d)
      echo "#######Delete policy report YAMLs#######\n"
      deletePolicyReportYaml
      ;;
    :)
      usage
      ;;
    *) 
      usage
      ;;
  esac
done
