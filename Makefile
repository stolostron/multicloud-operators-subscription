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

MANAGED_CLUSTER_NAME ?= cluster1
TEST_TMP := /tmp
export KUBEBUILDER_ASSETS ?= $(TEST_TMP)/kubebuilder/bin
K8S_VERSION ?= 1.28.3
GOHOSTOS ?= $(shell go env GOHOSTOS)
GOHOSTARCH ?= $(shell go env GOHOSTARCH)
KB_TOOLS_ARCHIVE_NAME := kubebuilder-tools-$(K8S_VERSION)-$(GOHOSTOS)-$(GOHOSTARCH).tar.gz
KB_TOOLS_ARCHIVE_PATH := $(TEST_TMP)/$(KB_TOOLS_ARCHIVE_NAME)

# specify the tag for ocm foundation images used in e2e test
OCM_IMAGE_TAG ?= v0.13.0

LOCAL_OS := $(shell uname)
ifeq ($(LOCAL_OS),Linux)
    TARGET_OS ?= linux
    XARGS_FLAGS = "-r"
else ifeq ($(LOCAL_OS),Darwin)
    TARGET_OS ?= darwin
    XARGS_FLAGS =
else
    $(error "This system's OS $(LOCAL_OS) isn't recognized/supported")
endif

SED_CMD:=sed
ifeq ($(GOHOSTOS),darwin)
	ifeq ($(GOHOSTARCH),amd64)
		SED_CMD := gsed
	endif
endif

FINDFILES=find . \( -path ./.git -o -path ./.github \) -prune -o -type f
XARGS = xargs -0 ${XARGS_FLAGS}
CLEANXARGS = xargs ${XARGS_FLAGS}

REGISTRY = quay.io/stolostron
VERSION = latest
IMAGE_NAME_AND_VERSION ?= $(REGISTRY)/multicloud-operators-subscription:$(VERSION)
export GOPACKAGES = $(shell go list ./... | grep -v /manager | grep -v /bindata  | grep -v /vendor | grep -v /internal | grep -v /build | grep -v /test | grep -v /e2e )
export TEST_GIT_REPO_URL = github.com/stolostron/multicloud-operators-subscription

.PHONY: build

build:
	@common/scripts/gobuild.sh build/_output/bin/multicluster-operators-subscription ./cmd/manager
	@common/scripts/gobuild.sh build/_output/bin/uninstall-crd ./cmd/uninstall-crd
	@common/scripts/gobuild.sh build/_output/bin/appsubsummary ./cmd/appsubsummary
	@common/scripts/gobuild.sh build/_output/bin/multicluster-operators-placementrule ./cmd/placementrule

.PHONY: local

local:
	@GOOS=darwin common/scripts/gobuild.sh build/_output/bin/multicluster-operators-subscription ./cmd/manager
	@GOOS=darwin common/scripts/gobuild.sh build/_output/bin/uninstall-crd ./cmd/uninstall-crd
	@GOOS=darwin common/scripts/gobuild.sh build/_output/bin/appsubsummary ./cmd/appsubsummary
	@GOOS=darwin common/scripts/gobuild.sh build/_output/bin/multicluster-operators-placementrule ./cmd/placementrule

.PHONY: build-images

build-images:
	@docker build -t ${IMAGE_NAME_AND_VERSION} -f build/Dockerfile .

# build local linux/amd64 images on non-amd64 hosts such as Apple M3
# need to create the buildx builder as a fixed name and clean it up after usage
# Or a new builder is created everytime and it will fail docker buildx image build eventually.
build-images-non-amd64:
	docker buildx create --name local-builder --use
	docker buildx inspect local-builder --bootstrap
	docker buildx build --platform linux/amd64 -t ${IMAGE_NAME_AND_VERSION} -f build/Dockerfile --load .
	docker buildx rm local-builder

.PHONY: lint

lint: lint-all

.PHONY: lint-all

lint-all:lint-go

.PHONY: lint-go

lint-go:
	@${FINDFILES} -name '*.go' \( ! \( -name '*.gen.go' -o -name '*.pb.go' \) \) -print0 | ${XARGS} common/scripts/lint_go.sh

.PHONY: test

# download the kubebuilder-tools to get kube-apiserver binaries from it
ensure-kubebuilder-tools:
	$(info Downloading kube-apiserver into '$(KUBEBUILDER_ASSETS)')
	rm -fr '$(KUBEBUILDER_ASSETS)'
	mkdir -p '$(KUBEBUILDER_ASSETS)'
	curl -s -f -L https://storage.googleapis.com/kubebuilder-tools/$(KB_TOOLS_ARCHIVE_NAME) -o '$(KB_TOOLS_ARCHIVE_PATH)'
	tar -C '$(KUBEBUILDER_ASSETS)' --strip-components=2 -zvxf '$(KB_TOOLS_ARCHIVE_PATH)'

.PHONY: ensure-kubebuilder-tools

update:
	go-bindata -o pkg/addonmanager/bindata/bindata.go -pkg bindata deploy/managed-common deploy/managed

go-bindata:
	go install github.com/go-bindata/go-bindata/go-bindata@latest

test: ensure-kubebuilder-tools
	@echo ${TEST_GIT_REPO_URL}
	go test -timeout 300s -v ./addon/... -coverprofile=addon_coverage.out
	go test -timeout 300s -v ./pkg/... -coverprofile=coverage.out

.PHONY: deploy-standalone

deploy-standalone:
	kubectl get ns open-cluster-management ; if [ $$? -ne 0 ] ; then kubectl create ns open-cluster-management ; fi
	kubectl apply -f deploy/crds
	kubectl apply -f deploy/hub-common
	kubectl apply -f deploy/standalone

.PHONY: deploy-hub

deploy-ocm:
	IMAGE_TAG=$(OCM_IMAGE_TAG) deploy/ocm/install.sh

deploy-hub:
	kubectl get ns open-cluster-management ; if [ $$? -ne 0 ] ; then kubectl create ns open-cluster-management ; fi
	kubectl apply -f deploy/crds
	kubectl apply -f deploy/hub-common
	kubectl apply -f deploy/hub

.PHONY: deploy-addon

deploy-addon:
	$(SED_CMD) -e "s,managed_cluster_name,$(MANAGED_CLUSTER_NAME)," deploy/addon/addon.yaml | kubectl apply -f -


build-e2e:
	go test -c ./test/e2e

test-e2e: deploy-ocm deploy-hub build-e2e
	./e2e.test -test.v -ginkgo.v
	deploy/ocm/verify_app_addon.sh hub
	deploy/ocm/verify_app_addon.sh mc

test-e2e-kc:
	build/e2e-kc.sh

############################################################
# generate code and crd
############################################################
# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."



# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.5.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:crdVersions=v1beta1"

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) paths="./..." output:crd:artifacts:config=deploy/crds


############################################################
# run the e2e on a local kind
############################################################
export CONTAINER_NAME = e2e
e2e: build build-images
	build/run-e2e-tests.sh

e2e-setup: e2e
	build/set_up_e2e_local_dir.sh
