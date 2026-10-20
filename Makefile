GO       ?= go
PNPM     ?= pnpm
BIN      ?= bin/ibcmon
WEBDIST  := internal/api/webdist

.PHONY: all build build-go build-web test test-go test-web lint lint-go lint-web fmt tidy run clean abis e2e

all: build

build: build-web build-go

build-go:
	$(GO) build -o $(BIN) ./cmd/ibcmon

# The console is built into internal/api/webdist and embedded in the binary.
build-web:
	cd web && $(PNPM) install --frozen-lockfile && $(PNPM) build
	rm -rf $(WEBDIST)/assets $(WEBDIST)/index.html
	cp -R web/dist/. $(WEBDIST)/

test: test-go test-web

test-go:
	$(GO) test -race -count=1 ./...

test-web:
	cd web && $(PNPM) test

lint: lint-go lint-web

lint-go:
	$(GO) vet ./...
	golangci-lint run ./...

lint-web:
	cd web && $(PNPM) lint

fmt:
	gofmt -w $$(git ls-files '*.go')
	cd web && $(PNPM) exec prettier --write src

tidy:
	$(GO) mod tidy

run: build-go
	$(BIN) serve --config ibcmon.yaml

clean:
	rm -rf bin web/dist $(WEBDIST)/assets $(WEBDIST)/index.html

# Refreshes abi/*.json from a Foundry build of gno-eth-ibc (contracts/out).
# GNO_ETH_IBC points at the checkout; run `make build` there first.
GNO_ETH_IBC ?= ../gno-eth-ibc
abis:
	python3 scripts/extract-abis.py $(GNO_ETH_IBC)/contracts/out abi

# Runs every check once against the local testnet + anvil of gno-eth-ibc
# (scripts/e2e-local.sh there). See docs/design.md §13.
e2e: build-go
	./scripts/e2e-local.sh
