SHELL := /usr/bin/env bash
GO ?= go
CONTROLLER_GEN ?= controller-gen

.PHONY: test
test:
	@packages="$$( $(GO) list ./... )" || exit $$?; \
	if [[ -z "$$packages" ]]; then \
		echo "no packages to test"; \
	else \
		$(GO) test $$packages; \
	fi

.PHONY: generate
generate:
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: manifests
manifests:
	$(CONTROLLER_GEN) crd:allowDangerousTypes=true paths="./api/..." output:crd:artifacts:config=config/crds

.PHONY: build
build:
	$(GO) build -o bin/controller-manager ./cmd/controller-manager
	$(GO) build -o bin/csi-plugin ./cmd/csi-plugin
	$(GO) build -o bin/node-agent ./cmd/node-agent
	$(GO) build -o bin/sidecar ./cmd/sidecar
	$(GO) build -o bin/dashboard ./cmd/dashboard

.PHONY: kind-up
kind-up:
	scripts/dev-up.sh

.PHONY: kind-down
kind-down:
	scripts/dev-down.sh
