# Arc route

One `ibcmon` binary serves either the existing Sepolia route or a separate
Arc route, selected by YAML. Each process monitors one route deployment.
Keep the Sepolia process on its current YAML and port. The Arc example uses
`:8081`; point `monitor-2.2-29-108-12.sslip.io` at that process in the reverse
proxy. It does not share chain state or deployment addresses with monitor.

```sh
cp ibcmon.arc.example.yaml ibcmon.arc.yaml
export IBCMON_ARC_GNO_RPC_URL_2=https://your-independent-test-arc-1-node
make build
bin/ibcmon serve -config ibcmon.arc.yaml
```

Run from this checkout so the example's relative deployment path resolves.
Deploy the Arc contracts first, then set `eth.deployment_file` to that
checkout's `contracts/deployments/5042002.json`. The existing JSON keys
`ics26Router`, `ics20Transfer`, `ibcAdmin`, `ibcAdminExec`, `gnoLightClient`
and `ugnotVoucher` supply the contract addresses. Explicit `eth.addresses`
entries override those keys. No Arc address is compiled into the monitor or
console. `history_from_deployment: true` reads `deployBlock` as the first
log block unless `history_from_block` is nonzero. When supplying addresses
without a deployment file, set `history_from_block` to a block at or before
the earliest deployment transaction. Include all migrations and recovery
disable events; a rolling lookback would lose that history.

The route settings are:

| YAML setting | Arc example |
|---|---|
| `gno.chain_id` | `test-arc-1` |
| `gno.client_type`, `gno.route_client_id` | `arc`, `arc-1` |
| `gno.rpc_url`, `gno.browser_rpc_url` | `https://rpc-2.2-29-108-12.sslip.io` |
| `gno.web_url` | `https://gnoweb-2.2-29-108-12.sslip.io` |
| `eth.chain_id` | `"5042002"` (wallet: `0x4cef52`) |
| `eth.chain_name`, `eth.network_name` | `Arc testnet` (display / wallet name) |
| `eth.explorer_url` | `https://testnet.arcscan.app` |
| `eth.browser_rpc_url` | `https://rpc.testnet.arc.io` |
| `eth.native_currency` | USDC, 18 decimals for the native gas balance |
| `eth.finality` | `tag` (`finalized`), no beacon URL |

`browser_rpc_url` fields are deliberately separate from private collector
RPCs. They must be public endpoints with browser CORS support and no secrets
in their paths. Only these public settings and the resolved IBCAdmin address
are sent through `/api/v1/info` (`network`) and `/api/v1/overview` (`info.network`).
The console loads them before mounting route pages or offering wallet
controls. It uses them for RPC reads, chain checks, Adena / EVM network
selection, transaction destinations, explorer links and chain labels.
Realm `NewEth*` builder names and API keys such as `ethereum` remain stable;
their labels use the configured chain name. Catalog addresses from the
Sepolia deployment are cleared from Arc form defaults; supply new targets
explicitly, and review every draft against live state.

Arc's `finalized` block is about one block (0.5 seconds) behind `latest`.
The example polls every 2 seconds and bounds head age at 30 seconds, provider
lag at 20 blocks and head progress at 1 minute. These remain configurable
under `monitor.freshness` using the existing `ethereum_*` keys. The default
Sepolia values (45 minutes / 64 blocks / 10 minutes) are unchanged.
`eth.log_chunk` bounds each log request (Arc example 1,000 blocks; existing
default 2,000). Log history starts at deployment and is read at the pin;
large histories may require a longer `collection_timeout` or a local RPC.
Both chains still require a second provider for header agreement.

Arc clients expose block heights, timestamps, BFT validator powers and the
registry storage root. Expiry is `last_consensus_state.timestamp +
client_state.trusting_period`, in seconds, with no beacon slots or sync
committees. The Clients page shows the validator count, exact total power,
registry storage root, drift, trust level, trusting period and expiry.
Tune `client_expiry.stale_after.arc` and warning / critical windows for the
deployed trust period. All transfer, acknowledgement, timeout, guardian,
IBCAdmin and recovery views use the same contract and realm readers.

The official Arc RPC has no `eth_getProof`; Blockdaemon supports it only at
latest. This monitor's current contract views and header agreement do not
require those proofs. Do not weaken pinning or silently query latest to
work around a missing historical proof. Future integrity checks requiring
pinned proofs will need suitable node support. USDC's ERC20 interface at
`0x3600000000000000000000000000000000000000` has **6** decimals; token reads
continue to use ERC20 decimals rather than the wallet's native 18 decimals.

After deployment:

```sh
bin/ibcmon check -config ibcmon.arc.yaml client_expiry/gno
curl -fsS http://localhost:8081/api/v1/info
curl -fsS http://localhost:8081/api/v1/clients
```

Verify `network` names test-arc-1 / Arc testnet and the deployed IBCAdmin,
that `arc-1` has a consensus timestamp and derived expiry, and that both
provider headers agree. Live packet and governance execution checks require
the new deployment and funded wallets; they are not performed by the test
suite. The gno governance channel must be bound to the Arc client.
