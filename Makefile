# If you update this file, please follow
# https://suva.sh/posts/well-documented-makefiles

# Ensure Make is run with bash shell as some syntax below is bash-specific
SHELL := /usr/bin/env bash

.DEFAULT_GOAL := help

VERSION ?= $(shell cat clusterctl-settings.json | jq .config.nextVersion -r)

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
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# Directories
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR := $(ROOT_DIR)/bin
TOOLS_DIR := $(ROOT_DIR)/hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/bin
export PATH := $(abspath $(TOOLS_BIN_DIR)):$(PATH)

REPO_ROOT := $(shell git rev-parse --show-toplevel)
ARTIFACTS ?= ${REPO_ROOT}/_artifacts
E2E_CONF_FILE  ?= "$(abspath test/e2e/config/elf-dev.yaml)"
E2E_TEMPLATE_DIR := "$(abspath test/e2e/data/infrastructure-elf/)"

# Allow overriding manifest generation destination directory
MANIFEST_ROOT ?= ./config
CRD_ROOT ?= $(MANIFEST_ROOT)/crd/bases
RBAC_ROOT ?= $(MANIFEST_ROOT)/rbac
RELEASE_DIR := out
BUILD_DIR := .build
OVERRIDES_DIR := $(HOME)/.cluster-api/overrides/infrastructure-elf/$(VERSION)

# Architecture variables
ARCH ?= amd64
ALL_ARCH = amd64 arm arm64 ppc64le s390x

# Common docker variables
IMAGE_NAME ?= manager
PULL_POLICY ?= Always

# Release docker variables
RELEASE_REGISTRY := gcr.io/cluster-api-provider-elf/release
RELEASE_CONTROLLER_IMG := $(RELEASE_REGISTRY)/$(IMAGE_NAME)

# Development Docker variables
DEV_REGISTRY ?= gcr.io/$(shell gcloud config get-value project)
DEV_CONTROLLER_IMG ?= $(DEV_REGISTRY)/elf-$(IMAGE_NAME)
DEV_TAG ?= dev

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

.PHONY: cluster-templates
cluster-templates: kustomize ## Generate cluster templates
	cp $(RELEASE_DIR)/cluster-template.yaml $(E2E_TEMPLATE_DIR)/bases/cluster-template.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/cluster-template --load_restrictor none > $(E2E_TEMPLATE_DIR)/cluster-template.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/cluster-template-cp-ha --load_restrictor none > $(E2E_TEMPLATE_DIR)/cluster-template-cp-ha.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/cluster-template-kcp-remediation --load_restrictor none > $(E2E_TEMPLATE_DIR)/cluster-template-kcp-remediation.yaml
	$(KUSTOMIZE) build $(E2E_TEMPLATE_DIR)/cluster-template-md-remediation --load_restrictor none > $(E2E_TEMPLATE_DIR)/cluster-template-md-remediation.yaml

test: generate fmt vet ## Run tests.
	source ./hack/fetch_ext_bins.sh; fetch_tools; setup_envs; go test -v ./api/... ./controllers/... ./pkg/... -coverprofile cover.out

.PHONY: e2e-image
e2e-image: ## Build the e2e manager image
	docker build --tag="gcr.io/k8s-staging-cluster-api/cape-manager:e2e" .

.PHONY: e2e
e2e: e2e-image
e2e: ginkgo kustomize kind ## Run e2e tests
	$(MAKE) release-manifests
	$(MAKE) cluster-templates

	time $(GINKGO) -v ./test/e2e -- -e2e.config="$(E2E_CONF_FILE)" -e2e.artifacts-folder="$(ARTIFACTS)"

## --------------------------------------
## Tooling Binaries
## --------------------------------------

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.9.1)

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

GINKGO := $(shell pwd)/bin/ginkgo
ginkgo: ## Download ginkgo locally if necessary.
	$(call go-get-tool,$(GINKGO),github.com/onsi/ginkgo/ginkgo@v1.16.4)

KIND := $(shell pwd)/bin/kind
kind: ## Download kind locally if necessary.
	$(call go-get-tool,$(KIND),sigs.k8s.io/kind@v0.11.0)

## --------------------------------------
## Linting and fixing linter errors
## --------------------------------------

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

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
		object:headerFile=./hack/boilerplate.go.txt

.PHONY: generate-manifests
generate-manifests: controller-gen ## Generate manifests e.g. CRD, RBAC etc.
	$(CONTROLLER_GEN) \
		paths=./api/... \
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

.PHONY: release-manifests
release-manifests:
	$(MAKE) manifests MANIFEST_DIR=$(RELEASE_DIR) PULL_POLICY=IfNotPresent IMAGE=$(RELEASE_CONTROLLER_IMG):$(VERSION)
	cp metadata.yaml $(RELEASE_DIR)/metadata.yaml

.PHONY: release-overrides
release-overrides:
	$(MAKE) manifests MANIFEST_DIR=$(OVERRIDES_DIR) PULL_POLICY=IfNotPresent IMAGE=$(RELEASE_CONTROLLER_IMG):$(VERSION)

.PHONY: dev-manifests
dev-manifests:
	$(MAKE) manifests MANIFEST_DIR=$(OVERRIDES_DIR) PULL_POLICY=Always IMAGE=$(DEV_CONTROLLER_IMG):$(DEV_TAG)
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
## Development
## --------------------------------------

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

.PHONY: run
run: generate fmt vet ## Run a controller from your host.
	go run ./main.go

.PHONY: install
install: generate kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: generate kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

.PHONY: deploy
deploy: generate kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	sed -i'' -e 's@image: .*@image: '"$(DEV_CONTROLLER_IMG):$(DEV_TAG)"'@' config/default/manager_image_patch.yaml
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

## --------------------------------------
## Docker
## --------------------------------------

.PHONY: docker-build
docker-build: ## Build the docker image for controller-manager
	docker build --pull --build-arg ARCH=$(ARCH) . -t $(DEV_CONTROLLER_IMG):$(DEV_TAG)

.PHONY: docker-push
docker-push: ## Push the docker image
	docker push $(DEV_CONTROLLER_IMG):$(DEV_TAG)
