# Copyright 2022.
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

# If you update this file, please follow
# https://suva.sh/posts/well-documented-makefiles

# Ensure Make is run with bash shell as some syntax below is bash-specific
SHELL := /usr/bin/env bash

.DEFAULT_GOAL := help

VERSION ?= $(shell cat clusterctl-settings.json | jq .config.nextVersion -r)

#
# Go.
#
GO_VERSION ?= 1.20.7

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# Directories
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR := $(ROOT_DIR)/bin
TOOLS_DIR := $(ROOT_DIR)/hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/bin
export PATH := $(abspath $(TOOLS_BIN_DIR)):$(PATH)

# E2E

REPO_ROOT := $(shell git rev-parse --show-toplevel)
ARTIFACTS ?= ${REPO_ROOT}/_artifacts
E2E_CONF_FILE ?= "$(abspath test/e2e/config/elf-dev.yaml)"
E2E_TEMPLATE_DIR := "$(abspath test/e2e/data/infrastructure-elf/)"

GINKGO_FOCUS ?=
GINKGO_SKIP ?=
GINKGO_NODES ?= 1
GINKGO_TIMEOUT ?= 2h
GINKGO_POLL_PROGRESS_AFTER ?= 10m
GINKGO_POLL_PROGRESS_INTERVAL ?= 1m
SKIP_RESOURCE_CLEANUP ?= false
USE_EXISTING_CLUSTER ?= false
GINKGO_NOCOLOR ?= false

# to set multiple ginkgo skip flags, if any
ifneq ($(strip $(GINKGO_SKIP)),)
_SKIP_ARGS := $(foreach arg,$(strip $(GINKGO_SKIP)),-skip="$(arg)")
endif

# Allow overriding manifest generation destination directory
MANIFEST_ROOT ?= ./config
CRD_ROOT ?= $(MANIFEST_ROOT)/crd/bases
RBAC_ROOT ?= $(MANIFEST_ROOT)/rbac
RELEASE_DIR := out
BUILD_DIR := .build
OVERRIDES_DIR := $(HOME)/.cluster-api/overrides/infrastructure-elf/$(VERSION)

# Architecture variables
IMAGE_TAG ?= dev
ARCH ?= amd64
ALL_ARCH = amd64 arm64

# Set build time variables including version details
LDFLAGS := $(shell hack/version.sh $(IMAGE_TAG))

# Common docker variables
IMAGE_NAME ?= cape-manager
PULL_POLICY ?= Always

# Release docker variables
REGISTRY ?= docker.io/smartxworks
CONTROLLER_IMG ?= $(REGISTRY)/$(IMAGE_NAME)

## --------------------------------------
## Help
## --------------------------------------

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

## --------------------------------------
## Testing
## --------------------------------------

$(ARTIFACTS):
	mkdir -p $@

.PHONY: e2e-templates
e2e-templates: kustomize ## Generate e2e cluster templates
	$(MAKE) release-manifests
	cp $(RELEASE_DIR)/cluster-template.yaml $(E2E_TEMPLATE_DIR)/kustomization/base/cluster-template.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/kustomization/base > $(E2E_TEMPLATE_DIR)/cluster-template.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/kustomization/cluster-template-cp-ha > $(E2E_TEMPLATE_DIR)/cluster-template-cp-ha.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/kustomization/cluster-template-kcp-remediation > $(E2E_TEMPLATE_DIR)/cluster-template-kcp-remediation.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/kustomization/cluster-template-kcp-scale-in > $(E2E_TEMPLATE_DIR)/cluster-template-kcp-scale-in.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/kustomization/cluster-template-md-remediation > $(E2E_TEMPLATE_DIR)/cluster-template-md-remediation.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/kustomization/cluster-template-node-drain > $(E2E_TEMPLATE_DIR)/cluster-template-node-drain.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/kustomization/conformance > $(E2E_TEMPLATE_DIR)/cluster-template-conformance.yaml

test: generate ## Run tests.
	source ./hack/fetch_ext_bins.sh; fetch_tools; setup_envs; go test -v ./api/... ./webhooks/... ./controllers/... ./pkg/... -coverprofile=cover.out

.PHONY: e2e-image
e2e-image: docker-pull-prerequisites ## Build the e2e manager image. Docker ignores docker.io causing CAPI to fail to load local e2e image.
	docker build --tag="k8s.gcr.io/smartxworks/cape-manager:e2e" .

.PHONY: test-e2e
test-e2e: e2e-templates
test-e2e: ginkgo kustomize kind ## Run e2e tests
	$(GINKGO) -v --trace -poll-progress-after=$(GINKGO_POLL_PROGRESS_AFTER) \
		-poll-progress-interval=$(GINKGO_POLL_PROGRESS_INTERVAL) --tags=e2e --focus="$(GINKGO_FOCUS)" \
		$(_SKIP_ARGS) --nodes=$(GINKGO_NODES) --timeout=$(GINKGO_TIMEOUT) --no-color=$(GINKGO_NOCOLOR) \
		--output-dir="$(ARTIFACTS)" --junit-report="junit.e2e_suite.1.xml" $(GINKGO_ARGS) ./test/e2e -- \
		-e2e.artifacts-folder="$(ARTIFACTS)" \
		-e2e.config="$(E2E_CONF_FILE)" \
		-e2e.skip-resource-cleanup=$(SKIP_RESOURCE_CLEANUP) -e2e.use-existing-cluster=$(USE_EXISTING_CLUSTER)

## --------------------------------------
## Tooling Binaries
## --------------------------------------

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.7)

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.12.0)

GINKGO := $(shell pwd)/bin/ginkgo
ginkgo: ## Download ginkgo locally if necessary.
	$(call go-get-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo@v2.11.0)

KIND := $(shell pwd)/bin/kind
kind: ## Download kind locally if necessary.
	$(call go-get-tool,$(KIND),sigs.k8s.io/kind@v0.20.0)

GOLANGCI_LINT := $(shell pwd)/bin/golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary.
	$(call go-get-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint@v1.53.3)

## --------------------------------------
## Linting and fixing linter errors
## --------------------------------------

.PHONY: lint
lint: golangci-lint ## Lint codebase
	$(GOLANGCI_LINT) run -v --fast=false

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT) ## Lint the codebase and run auto-fixers if supported by the linter.
	GOLANGCI_LINT_EXTRA_ARGS=--fix $(MAKE) lint

## --------------------------------------
## Generate
## --------------------------------------

.PHONY: generate
generate: ## Generate code
	$(MAKE) generate-go
	$(MAKE) generate-manifests

.PHONY: generate-go
generate-go: controller-gen ## Runs Go related generate targets
	go generate ./...
	$(CONTROLLER_GEN) \
		paths=./api/... \
		paths=./webhooks/... \
		object:headerFile=./hack/boilerplate.go.txt

.PHONY: generate-manifests
generate-manifests: controller-gen ## Generate manifests e.g. CRD, RBAC etc.
	$(CONTROLLER_GEN) \
		paths=./api/... \
		paths=./webhooks/... \
		crd:crdVersions=v1 \
		output:crd:dir=$(CRD_ROOT) \
		webhook
	$(CONTROLLER_GEN) \
		paths=./controllers/... \
		output:rbac:dir=$(RBAC_ROOT) \
		rbac:roleName=manager-role

## --------------------------------------
## Release
## --------------------------------------

$(RELEASE_DIR):
	@mkdir -p $(RELEASE_DIR)

$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)

$(OVERRIDES_DIR):
	@mkdir -p $(OVERRIDES_DIR)

.PHONY: release
release: clean-release
	$(MAKE) docker-build-all
	$(MAKE) docker-push-all
	$(MAKE) release-manifests

.PHONY: release-manifests
release-manifests:
	$(MAKE) manifests MANIFEST_DIR=$(RELEASE_DIR) PULL_POLICY=IfNotPresent IMAGE=$(CONTROLLER_IMG):$(IMAGE_TAG)
	cp metadata.yaml $(RELEASE_DIR)/metadata.yaml

.PHONY: release-overrides
release-overrides:
	$(MAKE) manifests MANIFEST_DIR=$(OVERRIDES_DIR) PULL_POLICY=IfNotPresent IMAGE=$(CONTROLLER_IMG):$(IMAGE_TAG)

.PHONY: dev-manifests
dev-manifests:
	$(MAKE) manifests MANIFEST_DIR=$(OVERRIDES_DIR) PULL_POLICY=Always IMAGE=$(CONTROLLER_IMG):$(IMAGE_TAG)
	cp metadata.yaml $(OVERRIDES_DIR)/metadata.yaml

.PHONY: manifests
manifests: $(MANIFEST_DIR) $(BUILD_DIR) kustomize
	rm -rf $(BUILD_DIR)/config
	cp -R config $(BUILD_DIR)
	cp templates/cluster-template.yaml $(MANIFEST_DIR)/cluster-template.yaml
	sed -i'' -e 's@imagePullPolicy: .*@imagePullPolicy: '"$(PULL_POLICY)"'@' $(BUILD_DIR)/config/default/manager_pull_policy.yaml
	sed -i'' -e 's@image: .*@image: '"$(IMAGE)"'@' $(BUILD_DIR)/config/default/manager_image_patch.yaml
	$(KUSTOMIZE) build $(BUILD_DIR)/config/default > $(MANIFEST_DIR)/infrastructure-components.yaml

## --------------------------------------
## Cleanup / Verification
## --------------------------------------

.PHONY: clean-release
clean-release: ## Remove the release folder
	rm -rf $(RELEASE_DIR)

## --------------------------------------
## Development
## --------------------------------------

.PHONY: build
build: generate ## Build manager binary.
	go build -o bin/manager main.go

.PHONY: run
run: generate ## Run a controller from your host.
	go run ./main.go --leader-election-namespace default -v 2

.PHONY: install
install: generate kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: generate kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

.PHONY: deploy
deploy: generate kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	sed -i'' -e 's@image: .*@image: '"$(CONTROLLER_IMG):$(IMAGE_TAG)"'@' config/default/manager_image_patch.yaml
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

## --------------------------------------
## Docker
## --------------------------------------

.PHONY: docker-build
docker-build: docker-pull-prerequisites ## Build the docker image for controller-manager
	docker build --build-arg ARCH=$(ARCH) --build-arg ldflags="$(LDFLAGS)" . -t $(CONTROLLER_IMG)-$(ARCH):$(IMAGE_TAG)

.PHONY: docker-push
docker-push: ## Push the docker image
	docker push $(CONTROLLER_IMG)-$(ARCH):$(IMAGE_TAG)

.PHONY: docker-pull-prerequisites
docker-pull-prerequisites:
	docker pull docker.io/docker/dockerfile:1.1-experimental
	docker pull gcr.io/distroless/static:latest

## --------------------------------------
## Docker — All ARCH
## --------------------------------------

.PHONY: docker-build-all ## Build all the architecture docker images
docker-build-all: $(addprefix docker-build-,$(ALL_ARCH))

docker-build-%:
	$(MAKE) ARCH=$* docker-build

.PHONY: docker-push-all ## Push all the architecture docker images
docker-push-all: $(addprefix docker-push-,$(ALL_ARCH))
	$(MAKE) docker-push-manifest

docker-push-%:
	$(MAKE) ARCH=$* docker-push

.PHONY: docker-push-manifest
docker-push-manifest: ## Push the fat manifest docker image.
	## Minimum docker version 18.06.0 is required for creating and pushing manifest images.
	docker manifest create --amend $(CONTROLLER_IMG):$(IMAGE_TAG) $(shell echo $(ALL_ARCH) | sed -e "s~[^ ]*~$(CONTROLLER_IMG)\-&:$(IMAGE_TAG)~g")
	@for arch in $(ALL_ARCH); do docker manifest annotate --arch $${arch} ${CONTROLLER_IMG}:${IMAGE_TAG} ${CONTROLLER_IMG}-$${arch}:${IMAGE_TAG}; done
	docker manifest push --purge ${CONTROLLER_IMG}:${IMAGE_TAG}


## --------------------------------------
## Helpers
## --------------------------------------

##@ helpers:

go-version: ## Print the go version we use to compile our binaries and images
	@echo $(GO_VERSION)
