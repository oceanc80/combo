###########################
# Configuration Variables #
###########################
ORG := github.com/operator-framework
PKG := $(ORG)/combo
VERSION_PATH := $(PKG)/pkg/version
GIT_COMMIT := $(shell git rev-parse HEAD)
CONTROLLER_GEN := $(Q)go run sigs.k8s.io/controller-tools/cmd/controller-gen
GO_BUILD := $(Q)go build
PKGS := $(shell go list ./...)
COMBO_VERSION :=  $(shell git describe --match 'v[0-9]*' --tags --always)

# Binary build options
export KUBERNETES_VERSION=v0.22.2

# Container build options
export IMAGE_REPO ?= quay.io/operator-framework/combo-operator
export IMAGE_TAG ?= latest
IMAGE ?= $(IMAGE_REPO):$(IMAGE_TAG)

export BUNDLE_REPO ?= quay.io/operator-framework/combo-bundle
export BUNDLE_TAG ?= latest
BUNDLE ?= $(BUNDLE_REPO):$(BUNDLE_TAG)

# kernel-style V=1 build verbosity
ifeq ("$(origin V)", "command line")
  BUILD_VERBOSE = $(V)
endif

ifeq ($(BUILD_VERBOSE),1)
  Q =
else
  Q = @
endif


###############
# Help Target #
###############
.PHONY: help
help: ## Show this help screen
	@echo 'Usage: make <OPTIONS> ... <TARGETS>'
	@echo ''
	@echo 'Available targets are:'
	@echo ''
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


#################
# Build Targets #
#################
.PHONY: tidy generate format lint verify build-cli build-container build-bundle

tidy: ## Update dependencies
	$(Q)go mod tidy

generate: ## Generate code and manifests
	$(Q)go generate ./...

format: ## Format the source code
	$(Q)go fmt ./...

lint: ## Run golangci-lint
	$(Q)go run github.com/golangci/golangci-lint/cmd/golangci-lint run

verify: tidy generate format lint ## Verify the current code generation and lint
	git diff --exit-code

VERSION_FLAGS=-ldflags "-X $(VERSION_PATH).GitCommit=$(GIT_COMMIT) -X $(VERSION_PATH).ComboVersion=$(COMBO_VERSION) -X $(VERSION_PATH).KubernetesVersion=$(KUBERNETES_VERSION)"
build-cli: ## Build the CLI binary. Specify VERSION_PATH, GIT_COMMIT, or KUBERNETES_VERSION to change the binary version. You may also specify BUILD_OS and BUILD_ARCH to change the build's binary.
	$(Q) CGO_ENABLED=0 GOOS=$(BUILD_OS) GOARCH=$(BUILD_ARCH) go build $(VERSION_FLAGS) -o ./combo

build-container: BUILD_OS=linux
build-container: BUILD_ARCH=amd64
build-container: build-cli ## Build the Combo container from the Dockerfile. Accepts IMAGE_REPO and IMAGE_TAG overrides.
	docker build . -f Dockerfile -t $(IMAGE)

build-bundle: ## Build the Combo bundle. Accepts BUNDLE_REPO and BUNDLE_TAG overrides.
	docker build . -f Dockerfile.plainbundle -t $(BUNDLE)

################
# Test Targets #
################
.PHONY: test test-unit test-e2e

test: test-unit test-e2e ## Run both the unit and e2e tests

UNIT_TEST_DIRS=$(shell go list ./... | grep -v /test/)
test-unit: ## Run the unit tests
	$(Q)go test -count=1 -short $(UNIT_TEST_DIRS)

test-e2e: ## Run the e2e tests
	go run "github.com/onsi/ginkgo/ginkgo" run test/e2e

###################
# Running Targets #
###################
.PHONY: load-image deploy teardown run run-local run-e2e run-e2e-local

IMAGE_LOAD_COMMAND=kind load docker-image
load-image: ## Load-image loads the currently constructed image onto the cluster. IMAGE can be overridden to load the bundle image.
	$(IMAGE_LOAD_COMMAND) $(IMAGE)

deploy: generate ## Deploy the Combo operator to the current cluster
	kubectl apply -f manifests

teardown: ## Teardown the Combo operator to the current cluster
	kubectl delete -f manifests

run: build-container load-image deploy ## Run Combo on local cluster

run-local: build-container load-image deploy ## Run Combo on local environment with Dockerfile

run-e2e: run test-e2e ## Run Combo and trigger the e2e tests for it

run-e2e-local: run-local test-e2e ## Run Combo on local environment and trigger e2e tests for it using Dockerfile

###################
# Release Targets #
###################
export DISABLE_RELEASE_PIPELINE ?= true
substitute:
	envsubst < .goreleaser.template.yml > .goreleaser.yml

release: GORELEASER ?= goreleaser
release: GORELEASER_ARGS ?= --snapshot --rm-dist
release: substitute
	$(GORELEASER) $(GORELEASER_ARGS)
