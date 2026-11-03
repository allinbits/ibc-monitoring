# ibc-monitoring

Monitoring, watch and governance console for IBC on gno.land, starting with the
gno.land ↔ Ethereum route: the monitors (T4) and the watch tool (T5) of the
[IBC v2 ADR](https://github.com/gnolang/gno/blob/feat/secp256k1-validators-ibc/gno.land/adr/pr6375_ibc_ethereum.md).

- **Design:** [`docs/design.md`](docs/design.md) — read it first. It is the
  specification this repository is built to.
- **Status:** phase 1 (see design §12). Read-only data layer for both chains,
  the clients / routes / governance dashboards, the monitor engine with the
  `client_expiry` liveness check end to end, asynchronous log and webhook
  alert sinks, the worker's code-hash job. The integrity checks (supply
  identity, orphans, header record, foreign decisions), the wallet actions
  and the Docker-based build verification are marked `TODO(phase N)` in the
  code. The first production milestone is alarm-only (design §12, release
  gates).

One Go binary, `ibcmon`, runs the monitor, the worker and the API, and serves
the TypeScript console. Self-host it against your own nodes; a public instance
is optional and labels itself non-authoritative.

## Run it

Requirements: Go 1.25, Node 24 with pnpm (console), Docker (worker jobs only).

```sh
cp .env.example .env            # endpoints and secrets, git-ignored
cp ibcmon.example.yaml ibcmon.yaml
make build                      # console + binary (bin/ibcmon)
bin/ibcmon serve                # API and console on :8080
```

Other commands:

```sh
bin/ibcmon monitor              # monitor and API, no worker
bin/ibcmon check client_expiry  # one check, once, as JSON
make build-go                   # binary only (the console answers with a notice)
make test lint                  # both halves
cd web && pnpm dev              # console with hot reload, proxying /api to :8080
```

Configuration lives in `ibcmon.yaml` (non-secret) with `${VAR}` references
expanded from the environment (`.env`): see `ibcmon.example.yaml` and
`.env.example`. Ethereum addresses come from the `eth.addresses` block or from
gno-eth-ibc's `contracts/deployments/<chain>.json` (`eth.deployment_file`).

A guardian instance needs a **second provider per chain** (`gno.rpc_url_2`,
`eth.rpc_url_2`): header agreement fails closed, and a snapshot the second
provider cannot confirm is served as `unverified` and never flagged on
(design §3.3). The start-up refuses a chain without one unless
`instance.require_cross_check: false` is set, which only the local e2e target
does.

Against the local testnet and anvil of gno-eth-ibc (`scripts/e2e-local.sh`
there): set `eth.finality: depth:1` (anvil has no beacon chain) and point
`eth.deployment_file` at `contracts/deployments/local.json`; `make e2e` runs
every check once against it, with anvil as its own second provider and the
cross-check requirement off (it says so in its output).

Live meter levels are shown only when an exact source gives them; today the
realm exposes the stored level and the console says "live: unavailable" and
why (design §4.1, §15 P3). Secrets in RPC and webhook URLs are redacted at
the source before anything is stored, logged or served; a public instance
additionally drops node labels, error texts and worker logs from every
response (design §10.2).

## Arc instance

The same binary also monitors **test-arc-1 ↔ Arc testnet**. Start from
[`ibcmon.arc.example.yaml`](ibcmon.arc.example.yaml); see
[`docs/arc.md`](docs/arc.md) for configuration, deployment and verification.
The existing YAML keeps its Sepolia defaults.

## Layout

| Path | What |
|---|---|
| `docs/design.md` | the design: goals, architecture, data-source inventory, dashboards, actions, monitor algorithms, alerting, worker, security, stack, phases, testing, open questions, suggested on-chain additions |
| `cmd/ibcmon` | the CLI |
| `internal/gnorpc`, `internal/gnoviews` | tm2 JSON-RPC client (height-pinned `qeval`/`qrender`) and the typed views of the core, transfer and ibcgov realms |
| `internal/ethrpc`, `internal/ethviews` | pinned `eth_call` / `eth_getProof` / `eth_getLogs` client and the typed views of IBCAdmin, the transfer app and GnoLightClient |
| `internal/model` | the shared types; their JSON is the API contract (amounts are decimal strings) |
| `internal/monitor` | the T4 engine: collector with second-provider cross-checks, checks, scheduler |
| `internal/alert` | alarm state machine and sinks (log, webhook) |
| `internal/store` | snapshots, results, alarms (in-memory; SQLite is phase 2) |
| `internal/api` | the read-only HTTP API and the embedded console |
| `internal/worker` | T5 jobs: code hashes (built), build verification and layout diffs (stubs) |
| `abi/` | contract ABIs from a Foundry build of gno-eth-ibc, with their revision |
| `web/` | the console (Vite + React + TypeScript) |

## Security notes

- No user key ever touches this software: wallet actions (phase 4) sign in
  the browser. The monitor's watch keys (auto-flagging, off by default) are
  loaded from the environment into the monitor process only.
- Every string from a chain is untrusted text. The console never renders
  Markdown or HTML from a chain; lint forbids `dangerouslySetInnerHTML`.
- Amounts are never JavaScript `Number`s: decimal strings over the wire,
  `*big.Int` in Go, `bigint` in TypeScript.
- The API is read-only. Put a guardian instance behind your own access
  control; it needs no public exposure.

## Licence

None yet: the owner has not chosen one (design §14). Until then all rights are
reserved.
