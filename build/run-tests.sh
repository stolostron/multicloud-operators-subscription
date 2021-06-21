#!/bin/bash -e
###############################################################################
# Copyright Contributors to the Open Cluster Management project
###############################################################################

os=$(go env GOOS)
arch=$(go env GOARCH)

# download kubebuilder and extract it to tmp
rm -rf /tmp/kubebuilder
rm -rf /tmp/kubebuilder_2.3.1_${os}_${arch}
curl -L https://go.kubebuilder.io/dl/2.3.1/${os}/${arch} | tar -xz -C /tmp/
mv /tmp/kubebuilder_2.3.1_${os}_${arch} /tmp/kubebuilder

PATH=/tmp/kubebuilder/bin:${PATH}
export PATH
export KUBEBUILDER_ASSETS=/tmp/kubebuilder/bin

go test -timeout 300s -v ./pkg/... 
