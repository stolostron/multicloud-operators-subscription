#!/bin/bash -e
###############################################################################
# (c) Copyright IBM Corporation 2019, 2020. All Rights Reserved.
# Note to U.S. Government Users Restricted Rights:
# U.S. Government Users Restricted Rights - Use, duplication or disclosure restricted by GSA ADP Schedule
# Contract with IBM Corp.
# Copyright (c) Red Hat, Inc.
# Copyright Contributors to the Open Cluster Management project
###############################################################################

os=$(go env GOOS)
arch=$(go env GOARCH)

# download kubebuilder and extract it to tmp
rm -rf /tmp/kubebuilder
rm -rf /tmp/kubebuilder_2.3.1_${os}_${arch}
curl -L https://go.kubebuilder.io/dl/2.3.1/${os}/${arch} | tar -xz -C /tmp/
mv /tmp/kubebuilder_2.3.1_${os}_${arch} /tmp/kubebuilder
_test_bin_dir=/tmp/kubebuilder
