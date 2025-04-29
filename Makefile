# # /bin/sh does not support source command needed in make test
#SHELL := /bin/bash

ROOT_DIR=$(shell git rev-parse --show-toplevel)

# Platforms supported
PLATFORMS ?= linux/amd64,linux/arm64

VERSION ?= 2.2.5
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
GOLANGCI_LINT_VERSION ?= v1.54.0

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(LOCALBIN) $(GOLANGCI_LINT_VERSION)

go-lint: golangci-lint ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run

.PHONY: go-lint-fix
go-lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

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
