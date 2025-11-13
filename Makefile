# # /bin/sh does not support source command needed in make test
#SHELL := /bin/bash

ROOT_DIR=$(shell git rev-parse --show-toplevel)

# Platforms supported
PLATFORMS ?= linux/amd64,linux/arm64

VERSION ?= 2.4.0-dev2
# Image URL to use all building/pushing aerospike-kubernetes-init image targets
IMG ?= aerospike/aerospike-kubernetes-init-nightly:${VERSION}


# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.6.1

.PHONY: golanci-lint
golanci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

go-lint: golanci-lint ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run

##@ Build

.PHONY: build
build: fmt vet ## Build akoinit binary.
	go build -o bin/akoinit main.go

.PHONY: run
run: fmt vet ## Run a akoinit from your host.
	go run ./main.go

.PHONY: docker-buildx-build
docker-buildx-build: ## Build docker image for the init container for cross-platform support
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	docker buildx build --load --no-cache --provenance=false --tag ${IMG} --build-arg VERSION=$(VERSION) .
	- docker buildx rm project-v3-builder

.PHONY: docker-buildx-build-push
docker-buildx-build-push: ## Build and push docker image for the init container for cross-platform support
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	docker buildx build --push --no-cache --provenance=false --platform=$(PLATFORMS) --tag ${IMG} --build-arg VERSION=$(VERSION) .
	- docker buildx rm project-v3-builder

.PHONY: docker-buildx-build-push-openshift
docker-buildx-build-push-openshift: ## Build and push docker image for the init container for openshift cross-platform support
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	docker buildx build --push --no-cache --provenance=false --platform=$(PLATFORMS) --tag ${IMG} --build-arg VERSION=$(VERSION) --build-arg USER=1001 .
	- docker buildx rm project-v3-builder

.PHONY: enable-pre-commit
enable-pre-commit:
	pip3 install pre-commit
	pre-commit install

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef