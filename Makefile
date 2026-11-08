SHELL := /bin/bash

# Toolchain commands run through the flake's dev shell, so the Go, buf, gofmt,
# and protoc plugin versions come from flake.lock instead of the developer's
# PATH. `nix develop` already exports IN_NIX_SHELL, so skip the wrapper there.
ifeq ($(IN_NIX_SHELL),)
DEV := nix develop --command --
else
DEV :=
endif

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

GO_SOURCES := $(shell find . -name '*.go' -not -path './gen/*' -not -path './bin/*')

.PHONY: all build build-local run test test-race lint fmt fmt-check proto-lint generate check clean docs-generate docs-check docs-install docs-dev docs-lint docs-typecheck docs-build

all: build

# Build noket and noketd with nix; the binaries land in result/bin/.
build:
	nix build .#noket

build-local:
	mkdir -p bin
	$(DEV) go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/ ./cmd/noket ./cmd/noketd

run:
	$(DEV) go run ./cmd/noket up --foreground

test:
	$(DEV) go test ./...

test-race:
	$(DEV) go test -race ./...

lint:
	$(DEV) golangci-lint run

fmt:
	$(DEV) gofumpt -w $(GO_SOURCES)

fmt-check:
	test -z "$$($(DEV) gofumpt -l $(GO_SOURCES))"

proto-lint:
	$(DEV) buf lint

# Generate noket/tunnel/v1 and noket/v1; google/rpc and buf/validate come from BSR modules.
generate:
	rm -rf gen
	$(DEV) buf generate --template buf.gen.yaml --path proto/noket/tunnel/v1 --path proto/noket/v1

check: fmt-check lint test docs-check

# Regenerate the CLI and configuration reference from the command tree and the
# structs in internal/types. The output is committed.
docs-generate:
	$(DEV) go run ./tools/docgen

# Fails when a committed reference page is stale; part of `make check`.
docs-check:
	$(DEV) go run ./tools/docgen -check

docs-install:
	cd docs && npm ci

docs-dev:
	cd docs && npm run dev

# Nimbus authoring rules: frontmatter shape and internal links are errors.
docs-lint:
	cd docs && npm run lint:docs

docs-typecheck:
	cd docs && npm run typecheck

# Builds the static site into docs/dist. Run docs-lint first: it checks links.
docs-build: docs-generate
	cd docs && npm run build

clean:
	rm -rf bin result result-1 result-* docs/dist docs/.astro
