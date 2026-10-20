#!/usr/bin/env bash
# Runs every ibcmon check once against the local gno.land testnet and anvil
# that gno-eth-ibc's scripts/e2e-local.sh brings up (docs/design.md §13).
#
# Prerequisites: `make e2e-local` in ../gno-eth-ibc (or GNO_ETH_IBC) has run,
# so the testnet answers on 127.0.0.1:26657, anvil on 127.0.0.1:8545 and
# contracts/deployments/local.json exists.
#
# TODO(phase 3): the forgery scenarios (a plain transfer into the escrow, a
# receive without a send through anvil's setStorageAt) and the assertions on
# the integrity alarms.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$HERE/.."
GNO_ETH_IBC="${GNO_ETH_IBC:-$ROOT/../gno-eth-ibc}"
DEPLOYMENT="$GNO_ETH_IBC/contracts/deployments/local.json"
BIN="${BIN:-$ROOT/bin/ibcmon}"

[[ -x "$BIN" ]] || { echo "build first: make build-go" >&2; exit 1; }
[[ -f "$DEPLOYMENT" ]] || { echo "no deployment at $DEPLOYMENT: run make e2e-local in gno-eth-ibc" >&2; exit 1; }

CFG="$(mktemp -t ibcmon-e2e.XXXXXX.yaml)"
trap 'rm -f "$CFG"' EXIT
cat > "$CFG" <<EOF
gno:
  rpc_url: ${GNO_RPC:-http://127.0.0.1:26657}
  rpc_url_2: ${GNO_RPC_2:-http://127.0.0.1:26667}
eth:
  rpc_url: ${ETH_RPC_URL:-http://127.0.0.1:8545}
  finality: depth:1
  deployment_file: $DEPLOYMENT
EOF

OUT="$("$BIN" check -config "$CFG")"
echo "$OUT"
# Every check must have evaluated (no skip, no error) and the collection must
# have no chain error.
if echo "$OUT" | grep -qE '"(gno_error|eth_error)": "'; then
	echo "collection failed" >&2
	exit 1
fi
if echo "$OUT" | grep -qE '"status": "(skipped|error)"'; then
	echo "a check could not evaluate" >&2
	exit 1
fi
echo "e2e: ok"
