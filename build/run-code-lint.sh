#!/bin/bash

#
# Copyright 2019 The Kubernetes Authors.
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

set -o errexi
set -o nounset
set -o pipefail
set -o xtrace

# Prepare lint tools
# Install hadolint
HADOLINT_PATH="${HOME}"/hadolint
mkdir -p "${HADOLINT_PATH}"
wget -P "${HADOLINT_PATH}" https://github.com/hadolint/hadolint/releases/download/v1.17.5/hadolint-Linux-x86_64
mv "${HADOLINT_PATH}"/hadolint-Linux-x86_64 "${HADOLINT_PATH}"/hadolint
chmod +x "${HADOLINT_PATH}"/hadolint
export PATH="${HADOLINT_PATH}":"${PATH}"

# Install yamllint
pip install --user yamllint

# Install markdown lint
gem install mdl
gem install awesome_bot

# Install golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOPATH)"/bin v1.28.3

# Start lint task
make lint
