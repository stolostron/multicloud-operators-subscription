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

echo ">>> Installing Operator SDK"
echo ">>> >>> Downloading source code"
GO111MODULE=off go get -d -v github.com/operator-framework/operator-sdk

cd "$GOPATH"/src/github.com/operator-framework/operator-sdk || exit

echo ">>> >>> Checking out version 0.10.0"
git checkout v0.10.0

echo ">>> >>> Running make tidy"
make tidy

echo ">>> >>> Running make install"
make install