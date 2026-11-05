GO       ?= go
BIN      ?= bin/ibcmon

.PHONY: all build test lint fmt tidy run clean abis e2e

all: build

# The binary serves the API only; the console is the separate
# ibc-monitoring-ui app.
build:
	$(GO) build -o $(BIN) ./cmd/ibcmon

test:
	$(GO) test -race -count=1 ./...

lint:
	$(GO) vet ./...
	golangci-lint run ./...

fmt:
	gofmt -w $$(git ls-files '*.go')

tidy:
	$(GO) mod tidy

run: build
	$(BIN) serve --config ibcmon.yaml

clean:
	rm -rf bin

# Refreshes abi/*.json from a Foundry build of gno-eth-ibc (contracts/out).
# GNO_ETH_IBC points at the checkout; run `make build` there first.
GNO_ETH_IBC ?= ../gno-eth-ibc
abis:
	python3 scripts/extract-abis.py $(GNO_ETH_IBC)/contracts/out abi

# Runs every check once against the local testnet + anvil of gno-eth-ibc
# (scripts/e2e-local.sh there). See docs/design.md §13.
e2e: build
	./scripts/e2e-local.sh
