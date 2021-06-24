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

HUB_KUBECONFIG ?= $(HOME)/hub-kubeconfig
MANAGED_CLUSTER_NAME ?= cluster1

LOCAL_OS := $(shell uname)
ifeq ($(LOCAL_OS),Linux)
    TARGET_OS ?= linux
    XARGS_FLAGS="-r"
else ifeq ($(LOCAL_OS),Darwin)
    TARGET_OS ?= darwin
    XARGS_FLAGS=
else
    $(error "This system's OS $(LOCAL_OS) isn't recognized/supported")
endif

SED_CMD:=sed
ifeq ($(GOHOSTOS),darwin)
	ifeq ($(GOHOSTARCH),amd64)
		SED_CMD:=gsed
	endif
endif

FINDFILES=find . \( -path ./.git -o -path ./.github \) -prune -o -type f
XARGS = xargs -0 ${XARGS_FLAGS}
CLEANXARGS = xargs ${XARGS_FLAGS}

IMG ?= $(shell cat COMPONENT_NAME 2> /dev/null)
REGISTRY = quay.io/open-cluster-management
VERSION ?= $(shell cat COMPONENT_VERSION 2> /dev/null)
IMAGE_NAME_AND_VERSION ?= $(REGISTRY)/$(IMG):$(VERSION)
export GOPACKAGES   = $(shell go list ./... | grep -v /manager | grep -v /bindata  | grep -v /vendor | grep -v /internal | grep -v /build | grep -v /test | grep -v /e2e )

.PHONY: build

build:
	@common/scripts/gobuild.sh build/_output/bin/$(IMG) ./cmd/manager
	@common/scripts/gobuild.sh build/_output/bin/uninstall-crd ./cmd/uninstall-crd

.PHONY: build-images

build-images: build
	@docker build -t ${IMAGE_NAME_AND_VERSION} -f build/Dockerfile .
	@docker tag ${IMAGE_NAME_AND_VERSION} $(REGISTRY)/$(IMG):latest

.PHONY: lint

lint: lint-all

.PHONY: lint-all

lint-all:lint-go

.PHONY: lint-go

lint-go:
	@${FINDFILES} -name '*.go' \( ! \( -name '*.gen.go' -o -name '*.pb.go' \) \) -print0 | ${XARGS} common/scripts/lint_go.sh

.PHONY: test

test:
	@build/run-tests.sh

.PHONY: deploy-standalone

deploy-standalone:
	kubectl get ns open-cluster-management ; if [ $$? -ne 0 ] ; then kubectl create ns open-cluster-management ; fi
	kubectl apply -f deploy/hub-common
	kubectl apply -f deploy/standalone

.PHONY: deploy-hub

deploy-hub:
	kubectl get ns open-cluster-management ; if [ $$? -ne 0 ] ; then kubectl create ns open-cluster-management ; fi
	kubectl apply -f deploy/hub-common
	kubectl apply -f deploy/hub

.PHONY: deploy-managed

deploy-managed:
	cp -f $(HUB_KUBECONFIG) /tmp/kubeconfig
	kubectl get ns open-cluster-management-agent-addon ; if [ $$? -ne 0 ] ; then kubectl create ns open-cluster-management-agent-addon ; fi
	kubectl -n open-cluster-management-agent-addon delete secret appmgr-hub-kubeconfig --ignore-not-found
	kubectl -n open-cluster-management-agent-addon create secret generic appmgr-hub-kubeconfig --from-file=kubeconfig=/tmp/kubeconfig
	kubectl apply -f deploy/managed-common
	$(SED_CMD) -e "s,managed_cluster_name,$(MANAGED_CLUSTER_NAME)," deploy/managed/operator.yaml | kubectl apply -f -
