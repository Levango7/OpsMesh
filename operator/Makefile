# Makefile for the opsmesh-operator.
#
# Targets:
#   manifests    - (re)generate CRD YAML from Go types via controller-gen (if installed)
#   install      - apply the CRD into the current cluster (kubectl apply -f config/crd/bases)
#   deploy       - set the manager image and apply CRD + manager manifests via kustomize
#   build        - compile the manager binary into bin/manager
#   run          - run the manager locally against the current kubeconfig
#   test         - run the controller test suite
#   docker-build - build the container image
#   clean        - remove build artifacts

# Image URL to use for building the manager container.
IMG ?= opsmesh/operator:latest
# controller-gen version used by the manifests target.
CONTROLLER_GEN_VERSION ?= v0.14.0
# kustomize version used by the deploy target.
KUSTOMIZE_VERSION ?= v5.4.2
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

KUSTOMIZE ?= $(GOPATH)/bin/kustomize

# Ensure kustomize is available; install it into GOPATH/bin if missing.
.PHONY: kustomize
kustomize:
	@if ! command -v $(KUSTOMIZE) >/dev/null 2>&1; then \
		echo "Installing kustomize $(KUSTOMIZE_VERSION)"; \
		go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION); \
	fi
	@$(KUSTOMIZE) version

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
# Cluster deployment
# ---------------------------------------------------------------------------

# install applies the CRD(s) under config/crd/bases into the current cluster.
# It regenerates manifests first (best-effort) and ensures kustomize is present
# so that downstream `make deploy` works without an extra setup step.
.PHONY: install
install: manifests kustomize
	kubectl apply -f config/crd/bases

# deploy sets the manager image on config/manager, applies the CRD, and then
# renders + applies the default kustomize overlay (CRD + RBAC + manager
# Deployment + Service). Requires kubectl and kustomize.
.PHONY: deploy
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	kubectl apply -f config/crd/bases
	$(KUSTOMIZE) build config/default | kubectl apply -f -

# undeploy tears down everything deployed by `make deploy`.
.PHONY: undeploy
undeploy: kustomize
	$(KUSTOMIZE) build config/default | kubectl delete -f - --ignore-not-found
	kubectl delete -f config/crd/bases --ignore-not-found

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