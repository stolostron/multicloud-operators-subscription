#!/bin/bash
#
# Copyright 2021 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8

export GOLANGCI_LINT_CACHE=/tmp/golangci-cache
export GOROOT=$(go env GOROOT)
export GOPATH=$(go env GOPATH)
rm -rf $GOLANGCI_LINT_CACHE
GOGC=25 "${GOPATH}/bin/golangci-lint" run -c ./common/config/.golangci.yml
