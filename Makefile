# Makefile for pkm-sync
# ----------------------
# This file provides local commands to ensure code quality and correctness,
# mirroring the checks run in the CI/CD pipeline.

# Go parameters
BINARY_NAME := pkm-sync
GO_PACKAGES := ./...
GO_BUILD_CMD := go build -v $(GO_PACKAGES)
GO_TEST_CMD := go test -v -race $(GO_PACKAGES)
GOLANGCI_LINT := golangci-lint

# Install prefix (XDG-style user-local)
PREFIX := $(HOME)/.local

# Container image
REGISTRY := registry.jaggars.cc
IMAGE_NAME := pkm-sync-daily
IMAGE_TAG := latest
IMAGE := $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

# Default target: Run all CI checks.
.PHONY: all
all: ci

# Target: ci - Runs all the checks that are performed in the CI pipeline.
# This is the command you should run locally before pushing code.
.PHONY: ci
ci: lint test build
	@echo "✅ All CI checks passed."

# Target: lint - Runs the golangci-lint linter.
# It uses the .golangci.yml configuration file for its settings.
.PHONY: lint
lint: check-golangci-version
	@echo "🔍 Running linters..."
	@$(GOLANGCI_LINT) run ./... --timeout=5m

# Target: lint-full - Runs the golangci-lint linter with all issues shown.
.PHONY: lint-full
lint-full: check-golangci-version
	@echo "🔍 Running all linters..."
	@$(GOLANGCI_LINT) run ./... --max-issues-per-linter=0 --max-same-issues=0 --timeout=5m

# Target: test - Runs unit tests with the race detector.
.PHONY: test
test:
	@echo "🧪 Running unit tests..."
	@$(GO_TEST_CMD)

# Target: build - Compiles the Go project to ensure it builds correctly.
.PHONY: build
build:
	@echo "🏗️ Building project..."
	@$(GO_BUILD_CMD)

# Target: binary - Builds a named pkm-sync binary.
.PHONY: binary
binary:
	@echo "🏗️ Building binary..."
	@go build -o $(BINARY_NAME) ./cmd

# Target: fmt - Formats all Go source files.
.PHONY: fmt
fmt:
	@echo "🖊️ Formatting Go files..."
	@gofmt -w $(shell find . -name '*.go' -not -path './vendor/*')

# Target: install - Builds and installs the binary to $(PREFIX)/bin.
.PHONY: install
install: binary
	@echo "📦 Installing $(BINARY_NAME) to $(PREFIX)/bin..."
	@mkdir -p $(PREFIX)/bin
	@cp $(BINARY_NAME) $(PREFIX)/bin/$(BINARY_NAME)
	@echo "✅ Installed to $(PREFIX)/bin/$(BINARY_NAME)"

# Target: uninstall - Removes the installed binary.
.PHONY: uninstall
uninstall:
	@echo "🗑️ Removing $(PREFIX)/bin/$(BINARY_NAME)..."
	@rm -f $(PREFIX)/bin/$(BINARY_NAME)
	@echo "✅ Uninstalled $(BINARY_NAME)"

# Target: clean - Removes the built binary.
.PHONY: clean
clean:
	@echo "🧹 Cleaning built binary..."
	@rm -f $(BINARY_NAME)

# Target: tidy - Tidies up the go.mod and go.sum files.
.PHONY: tidy
tidy:
	@echo "🧹 Tidying go modules..."
	@go mod tidy

# Target: check-golangci-version - Ensures golangci-lint v2.0+ is installed.
# This is required for the v2 configuration format used in .golangci.yml.
.PHONY: check-golangci-version
check-golangci-version:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || { \
		echo "❌ golangci-lint not found. Please install golangci-lint v2.0+"; \
		echo "   Recommended: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		echo "   Alternative: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v2.4.0"; \
		exit 1; \
	}
	@version=$$($(GOLANGCI_LINT) --version | head -1 | cut -d' ' -f4); \
	if [ -z "$$version" ]; then \
		echo "❌ Could not parse golangci-lint version"; \
		$(GOLANGCI_LINT) --version; \
		exit 1; \
	fi; \
	major=$$(echo $$version | cut -d'.' -f1); \
	if [ $$major -lt 2 ]; then \
		echo "❌ golangci-lint v2.0+ required for v2 configuration format. Current version: $$version"; \
		echo "   The .golangci.yml file uses v2 features like 'formatters' that require golangci-lint v2.0+"; \
		echo "   Recommended upgrade: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		echo "   Alternative upgrade: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v2.4.0"; \
		exit 1; \
	fi; \
	echo "✅ golangci-lint version check passed ($$version)"

# Target: docker-build - Build the container image.
.PHONY: docker-build
docker-build:
	@echo "🐳 Building container image $(IMAGE)..."
	@docker build -t $(IMAGE) .

# Target: docker-push - Push the container image to the registry.
.PHONY: docker-push
docker-push:
	@echo "🐳 Pushing $(IMAGE)..."
	@docker push $(IMAGE)

# Target: help - Displays help for the Makefile targets.
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all                    - Run all CI checks (default)."
	@echo "  ci                     - Alias for 'all'."
	@echo "  lint                   - Run golangci-lint (requires v2.0+)."
	@echo "  lint-full              - Run golangci-lint with all issues shown."
	@echo "  test                   - Run unit tests."
	@echo "  build                  - Build the project."
	@echo "  binary                 - Build the pkm-sync binary."
	@echo "  fmt                    - Format all Go source files."
	@echo "  install                - Build and install binary to \$(PREFIX)/bin."
	@echo "  uninstall              - Remove installed binary from \$(PREFIX)/bin."
	@echo "  clean                  - Remove the built binary."
	@echo "  tidy                   - Tidy go modules."
	@echo "  docker-build           - Build the container image."
	@echo "  docker-push            - Push the image to the registry."
	@echo "  check-golangci-version - Verify golangci-lint v2.0+ is installed."
	@echo "  help                   - Show this help message."

