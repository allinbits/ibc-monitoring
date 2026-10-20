# ibc-monitoring

Monitoring, watch and governance console for IBC on gno.land, starting with the
gno.land ↔ Ethereum route: the monitors (T4) and the watch tool (T5) of the
[IBC v2 ADR](https://github.com/gnolang/gno/blob/feat/secp256k1-validators-ibc/gno.land/adr/pr6375_ibc_ethereum.md).

- **Design:** [`docs/design.md`](docs/design.md) — read it first. It is the
  specification this repository is built to.
- **Status:** phase 1 (see design §12). Read-only data layer for both chains,
  the clients / routes / governance dashboards, the monitor engine with the
  `client_expiry` liveness check end to end, log and webhook alert sinks, the
  worker's code-hash job. The integrity checks (supply identity, orphans),
  the wallet actions and the Docker-based build verification are marked
  `TODO(phase N)` in the code.

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

Against the local testnet and anvil of gno-eth-ibc (`scripts/e2e-local.sh`
there): set `eth.finality: depth:1` (anvil has no beacon chain) and point
`eth.deployment_file` at `contracts/deployments/local.json`; `make e2e` runs
every check once against it.

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
