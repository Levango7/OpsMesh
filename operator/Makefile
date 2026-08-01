# Makefile for the opsmesh-operator.
#
# Targets:
#   manifests  - (re)generate CRD YAML from Go types via controller-gen (if installed)
#   build      - compile the manager binary into bin/manager
#   run        - run the manager locally against the current kubeconfig
#   test       - run the controller test suite
#   docker-build - build the container image
#   clean      - remove build artifacts

# Image URL to use for building the manager container.
IMG ?= opsmesh/operator:latest
# controller-gen version used by the manifests target.
CONTROLLER_GEN_VERSION ?= v0.14.0
# Get the currently used GOPATH.
GOPATH ?= $(shell go env GOPATH)

# Container engine to use (docker or podman).
CONTAINER_ENGINE ?= docker

.PHONY: all
all: build

# ---------------------------------------------------------------------------
# Tooling
# ---------------------------------------------------------------------------

CONTROLLER_GEN ?= $(GOPATH)/bin/controller-gen

$(CONTROLLER_GEN):
	$(info Installing controller-gen $(CONTROLLER_GEN_VERSION))
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

.PHONY: build
build:
	go build -trimpath -ldflags="-s -w" -o bin/manager ./cmd

.PHONY: run
run: build
	./bin/manager --leader-elect

.PHONY: test
test:
	go test -v ./...

# ---------------------------------------------------------------------------
# Manifests
# ---------------------------------------------------------------------------

# If controller-gen is installed we regenerate the CRD; otherwise we keep the
# hand-written YAML under config/crd/bases as the source of truth.
.PHONY: manifests
manifests: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) crd:crdVersions=v1 \
		paths="./api/..." \
		output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=manager-role \
		paths="./internal/controller/..." \
		output:rbac:artifacts:config=config/rbac

# Fallback that just prints a hint when controller-gen is unavailable.
.PHONY: manifests-check
manifests-check:
	@command -v $(CONTROLLER_GEN) >/dev/null 2>&1 || { \
		echo "controller-gen not found; using hand-written CRD under config/crd/bases"; \
		echo "run 'make manifests' after 'go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)' to regenerate"; \
	}

# ---------------------------------------------------------------------------
# Container
# ---------------------------------------------------------------------------

.PHONY: docker-build
docker-build:
	$(CONTAINER_ENGINE) build -t $(IMG) .

.PHONY: docker-push
docker-push:
	$(CONTAINER_ENGINE) push $(IMG)

# ---------------------------------------------------------------------------
# Misc
# ---------------------------------------------------------------------------

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: fmt vet
fmt:
	go fmt ./...

vet:
	go vet ./...