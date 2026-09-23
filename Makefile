# Makefile for github.com/malivvan/editor
GO       ?= go
GOFLAGS  ?=
PKG      ?= ./...
GOTESTSUM ?= $(shell which gotestsum 2>/dev/null || echo "/home/malivvan/go/bin/gotestsum")

# Detect gotestsum presence; fall back to plain go test if missing
ifneq ($(shell command -v $(GOTESTSUM) 2>/dev/null),)
  TEST_RUNNER   ?= $(GOTESTSUM)
  TEST_FLAGS    ?= --format testdox --format-icons hivis --
else
  TEST_RUNNER   ?= $(GO) test
  TEST_FLAGS    ?=
endif

COVERAGE_OUT ?= coverage.out

.PHONY: all build vet lint test test-race test-verbose cover cover-html fmt tidy demo clean help

all: build

build: ## Compile all packages
	$(GO) build $(GOFLAGS) $(PKG)

vet: ## Run go vet
	$(GO) vet $(GOFLAGS) $(PKG)

lint: ## Run linters (go vet + gofmt)
	$(GO) vet $(GOFLAGS) $(PKG)
	@unformatted=$$(gofmt -s -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

test: ## Run unit tests (with gotestsum if available)
	$(TEST_RUNNER) $(TEST_FLAGS) $(GOFLAGS) $(PKG)

test-race: ## Run unit tests with race detector
	$(TEST_RUNNER) $(TEST_FLAGS) -race $(GOFLAGS) $(PKG)

test-verbose: ## Run unit tests with verbose output
	$(TEST_RUNNER) $(TEST_FLAGS) -v $(GOFLAGS) $(PKG)

cover: ## Run tests with coverage report
	$(TEST_RUNNER) $(TEST_FLAGS) -covermode=atomic -coverprofile=$(COVERAGE_OUT) $(GOFLAGS) $(PKG)
	$(GO) tool cover -func=$(COVERAGE_OUT)

cover-html: cover ## Open the HTML coverage report
	$(GO) tool cover -html=$(COVERAGE_OUT)

bench: ## Run benchmarks
	$(GO) test -run=^$$ -bench=. -benchmem $(GOFLAGS) $(PKG)

fmt: ## Format Go sources
	$(GO) fmt $(PKG)
	gofmt -s -w .

tidy: ## Tidy go.mod
	$(GO) mod tidy

# Mirrored by the "Build demo" step of .github/workflows/ci.yml, which drives
# `go` directly because a CI runner image is not guaranteed to have make.
demo: ## Build the demo application
	$(GO) build -o /dev/null ./demo

clean: ## Remove build artifacts
	rm -f $(COVERAGE_OUT)
	$(GO) clean -cache -testcache

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
