# ibc-monitoring: design

Monitoring, watch and governance console for IBC on gno.land, starting with the gno.land ↔ Ethereum route.

This document is the specification this repository is built to. It implements **T4 (monitors)** and **T5
(watch tool)** of the ADR [`gno.land/adr/pr6375_ibc_ethereum.md`](https://github.com/gnolang/gno/blob/feat/secp256k1-validators-ibc/gno.land/adr/pr6375_ibc_ethereum.md)
("the ADR" below; section numbers are its), against the gno.land realms of gno-realms `feat/ibc-controls`
([ADR 0004](https://github.com/allinbits/gno-realms/blob/feat/ibc-controls/docs/adrs/0004-ethereum-route-controls.md),
[runbook](https://github.com/allinbits/gno-realms/blob/feat/ibc-controls/docs/upgrade-runbook.md)) and the
Ethereum contracts of gno-eth-ibc `feat/ibc-controls`
([`docs/controls.md`](https://github.com/allinbits/gno-eth-ibc/blob/feat/ibc-controls/docs/controls.md)).
T6, the setup audit tool, is out of scope; §3.6 leaves it room.

Status: design accepted as the basis for implementation; §12 tracks what is built. Where this document and the
chains disagree, the chains are right and this document is wrong: fix it.

Contents

1. [Goals and non-goals](#1-goals-and-non-goals)
2. [Users and roles](#2-users-and-roles)
3. [Architecture](#3-architecture)
4. [Data-source inventory](#4-data-source-inventory)
5. [Dashboards](#5-dashboards)
6. [Actions](#6-actions)
7. [Monitor algorithms (T4)](#7-monitor-algorithms-t4)
8. [Alerting](#8-alerting)
9. [Watch/check worker (T5)](#9-watchcheck-worker-t5)
10. [Security](#10-security)
11. [Tech stack](#11-tech-stack)
12. [Phases and milestones](#12-phases-and-milestones)
13. [Testing](#13-testing)
14. [Open questions for the owner](#14-open-questions-for-the-owner)
15. [Suggested on-chain additions](#15-suggested-on-chain-additions)

---

## 1. Goals and non-goals

### Goals

- **One place to see the route.** Every IBC client, route, meter, flag, hold, decision, incident and recovery
  state on both chains, read from the chains and shown with its provenance (chain, height or block, node).
- **T4, the monitors (ADR §7.11).** A daemon that evaluates the supply identity at causally closed height pairs,
  detects confirmed orphans with the documented forgery-evidence tests, tells unobserved from fabricated proof
  heights, defers alarms when data is unavailable, and raises liveness alarms (stalls, client expiry approaching,
  float runway). Alerts go to pluggable sinks. Auto-flagging with the watch keys exists but is **off by default**.
- **T5, the watch tool (ADR §7.6, §7.11).** For pending kind 5, 6, 7 and 9 decisions and recovery candidates:
  compare the live code hash with the decision's pinned hash and with the deployment manifest, run the
  reproducible-build verification (`verify-codehashes.sh` under Foundry in Docker), and diff storage layouts.
- **Wallet-signed actions** for guardians (flags, recovery cancellation), GovDAO members (proposal drafts from the
  `New*Request` builders) and anyone (`Execute`, `Redispatch`, `drain()`, `resendEpochNotice`, `clearRecoveryBlock`,
  `Claim`). Keys stay in the user's wallet: Adena for gno.land, an injected EIP-1193 wallet for Ethereum.
- **Self-hostable and open.** Each guardian runs their own instance against their own nodes. A public read-only
  instance is optional and labelled non-authoritative.
- **Exact amounts.** gno is adding native `uint256` (`feat/native-int256`, `grc20/v1`); amounts may exceed int64.
  Every amount is a decimal string over the wire and a `*big.Int` (Go) or `bigint` (TypeScript) in code. JavaScript
  `Number` never holds an amount.

### Non-goals

- Relaying. The monitor never relays packets, updates clients or submits proofs; it reads and, when configured,
  flags.
- T6, the setup audit (reading every authority back, byte-for-byte creation checks). §3.6 reserves a place for it.
- Deciding for the user. The console shows evidence and the ADR's rule; it never says "cancel this" or "flag now"
  on its own authority (§10.4).
- Key custody. No private key of a guardian or member ever touches this software. The monitor's watch keys are
  the one exception (§8.4), and they never reach the frontend.
- Replacing the runbooks. The incident drill (runbook §8) is run by people; this tool gives them the numbers.
- Multiple Ethereum routes or other counterparty chains in the first release. One governance channel serves exactly
  one Ethereum route (ADR §7.6), and the data model is keyed by route so that a second route or chain is an
  addition, not a rewrite.

## 2. Users and roles

| Role | Who | Reads | Signs |
|---|---|---|---|
| **Operator** | Runs the monitor and worker for a guardian organisation, or the public instance | everything, plus monitor internals (check history, provider disagreements, worker jobs) | nothing (the monitor's watch keys sign auto-flags when enabled; the operator configures them) |
| **Guardian** | Holder of one of the four guardian seats on a chain (two of them outsiders) | dashboards, alarms, pending decisions, upgrade-target checks, recovery proposals | `SetFlag`/`ClearFlag` (gno.land), `setRouteFlag`/`clearRouteFlag`/`setRefundsFlag`/`clearRefundsFlag`/`setFlag`/`clearFlag` (Ethereum), `cancelRecovery`, recovery approval and withdrawal |
| **Watch key** | The monitor's two seats per chain | — | flags only, from the monitor, when auto-flagging is enabled |
| **GovDAO member** | Votes on gno.land proposals | the same as a guardian, plus the decision drafts | nothing here: the console drafts the `New*Request` call and the member proposes it through GovDAO's own flow; the draft is text they copy, with the exact arguments |
| **Governor** (before the yield) | Runs the testing phase | the same | `ibcgov.Govern(request)` with the same drafts |
| **Anyone** | Relayers, users, the curious | the public read-only instance | the permissionless calls (§6.3) |

Roles are not authenticated by the console: a wallet either holds a seat or it does not, and the chain refuses the
rest. The console reads the seats (`Seat`, `guardians()`) to show which actions the connected address can sign.

## 3. Architecture

### 3.1 Components

```
                 own gno node(s)         own Ethereum execution + beacon node(s)
                 second gno RPC          second Ethereum provider (or Helios)
                        │                               │
                        ▼                               ▼
        ┌──────────────────────────────────────────────────────────────┐
        │  ibcmon (Go)                                                  │
        │                                                               │
        │   gnorpc ──► gnoviews ─┐                                      │
        │                        ├──► model ──► store ──► api (HTTP/JSON)│──► console (browser)
        │   ethrpc ──► ethviews ─┘        ▲                 ▲           │
        │                                 │                 │           │
        │   monitor (T4): checks, closed pairs, alarms ─────┤           │
        │   alert sinks: log, webhook; auto-flag (opt-in)   │           │
        │   worker (T5): code hashes, build verification, layout diffs  │
        └──────────────────────────────────────────────────────────────┘
                                                  │
                                                  ▼ docker run (foundry image)
                                           verify-codehashes.sh / forge inspect
```

One Go binary, `ibcmon`, with subcommands:

- `ibcmon serve` — the read-only data layer and the HTTP API, serving the built console. Runs the monitor and
  the worker as well unless `--no-monitor` / `--no-worker`.
- `ibcmon monitor` — T4 only (headless; alerts to sinks; API for its results).
- `ibcmon worker` — T5 only.
- `ibcmon check <name>` — run one check once and print its result as JSON (operators, scripts, CI).
- `ibcmon record` — record the live responses the checks read into a fixture directory (§13).

Packages (Go module `github.com/allinbits/ibc-monitoring`):

| Package | Role |
|---|---|
| `internal/gnorpc` | Minimal tm2 JSON-RPC client over HTTP: `status`, `block`, `commit`, `block_results`, `abci_query` (`vm/qeval`, `vm/qrender`) **pinned to a height**. Returns the answering height with every response. No dependency on the gno module. |
| `internal/gnoviews` | Typed readers for the realms' views: parses `qeval` results (`("…" string)`, `(n int64)`) and the Render JSON pages, with pagination. One function per view of §4.1. |
| `internal/ethrpc` | go-ethereum `ethclient` plus raw calls: `eth_call` at a **pinned block** (`finalized` resolved once per evaluation, then by number), `eth_getProof`, `eth_getLogs` in bounded chunks, block headers by hash, code by address. |
| `internal/ethviews` | Typed readers over the contract ABIs (embedded JSON from the Foundry build of a named gno-eth-ibc revision): one function per view and event of §4.2. |
| `internal/model` | The shared types: `Amount` (decimal string ↔ `*big.Int`), `Provenance`, `Client`, `Route`, `Meter`, `Flag`, `Hold`, `Decision`, `SendRecord`, `Incident`, `Loss`, `Recovery`, … JSON tags are the API contract. |
| `internal/monitor` | The T4 engine: the `Check` interface, the scheduler, the closed-pair selection, the alarm state machine, auto-flag gating. |
| `internal/alert` | Alarm model and sinks: `log`, `webhook`; the `autoflag` sink (signs with the watch keys). |
| `internal/worker` | The T5 jobs: `codehash`, `buildverify` (Docker), `layoutdiff` (Docker). |
| `internal/store` | Results, alarms, job runs and their history. SQLite file (pure Go driver). |
| `internal/api` | HTTP handlers, `/api/v1/*`, and the embedded console. |
| `internal/config` | Configuration file and environment. |
| `cmd/ibcmon` | The CLI. |
| `web/` | The console (TypeScript). |

### 3.2 Data flow

1. **Poll.** Every `poll_interval` (default 10 s, the ADR's monitor cadence, §7.1) the scheduler runs the cheap
   views (clients, flags, holds, decisions, route totals) against the primary nodes and stores snapshots with their
   provenance. Expensive views (send-record pages, logs) are read incrementally with cursors.
2. **Cross-check.** For every pinned height or block, the block hash is read from the primary node and from the
   second provider (header agreement, §3.3). A disagreement, or a second provider that cannot answer, marks the
   snapshot `unverified` and raises a `provider_disagreement` alarm; the monitor's integrity checks skip unverified
   snapshots (ADR §7.11, "Deferred alarms"). Values the integrity checks act on are additionally authenticated
   (verified proofs, or read from both providers and equal).
3. **Evaluate.** Each check reads from the store (never from the chains directly), so a check's inputs are the
   same snapshots the dashboard shows. Results carry the provenance of every input.
4. **Alert.** A result that crosses a threshold becomes an alarm. Alarms are deduplicated by key, have severity
   and state (`raised`, `acknowledged`, `cleared`), and go to the sinks. Only the alarm kinds the ADR names as
   auto-flagging (§7.11, "What triggers what") can trigger the auto-flag sink, and only when enabled.
5. **Serve.** The API serves the latest snapshots, check results, alarms and job runs; the console polls it.

### 3.3 Trust boundaries

Two different things are verified, and the provenance names them apart:

- **Header agreement** (`verification`): the pinned block's hash agrees between the primary node and the second
  provider (`cross-checked`), or the primary is a light client (`light-client`). It authenticates *which block*
  the snapshot is about. It does **not** authenticate the values read at that block: a compromised primary can
  answer the right block hash and fabricated `eth_call`, `Render` or log results.
- **State authentication** (`state_verification`): a value was proven against that header — an `eth_getProof`
  account proof verified against the header's `stateRoot` and a storage proof verified against the account's
  storage root (`proof`); a gno.land store proof verified against the AppHash (`proof`, phase 4); or the same
  value read from the two providers and found equal (`agreed`); otherwise `none`.

Rules that follow:

- **Integrity-critical values are authenticated or not used.** The orphan tests, the root comparisons, the
  header record and the foreign-decision reconciliation (§7) act only on values whose `state_verification` is
  `proof` or `agreed`. A containment response is never built on a value one provider asserted.
- **Fail closed.** When a required check cannot run — the second provider is configured but unavailable, a proof
  cannot be fetched or does not verify, the beacon node disagrees with the execution node — the snapshot is
  `unverified`, the integrity checks skip it (§8.3), and nothing is flagged on it. A guardian instance without a
  second provider is a configuration error (`instance.require_cross_check`, default true; the local e2e target
  turns it off and says so).
- Liveness alarms (§7.8) may use header-agreed values: they only alarm.

| Boundary | Trust | Mitigation |
|---|---|---|
| gno.land primary RPC | the operator's own node | header agreement with the second RPC at every pinned height (fail closed); `catching_up` refuses a node as primary; integrity values proven or read from both |
| Ethereum execution RPC | the operator's own node, or a **Helios** light client endpoint (§3.5) | header agreement with the second provider at the finalized block (fail closed); `eth_call`/`eth_getProof`/`eth_getLogs` always pinned to that block, never `latest`; integrity values by verified proof, account proof included |
| Beacon API | the operator's own beacon node | its finalized execution block hash must equal the execution node's `finalized` block hash; a disagreement is `unverified` |
| The chains' views | the realms and contracts as deployed | views are read, never trusted blindly for *integrity* questions: the supply identity is reconstructed from packet records, not read from a total (ADR §7.11, T4 row); the exactness limits of the views are stated in §4 |
| The console | the browser of a guardian or member | reads only from the operator's API; all Render output is treated as untrusted text (§10.3); every signature is prepared in the browser and sent to the wallet, never to the API; a wallet preview shows what will be signed and authenticates no evidence |
| The API | the operator's own deployment | read-only; no write endpoint exists; the watch keys never leave the monitor process; secrets are redacted before anything is stored, logged or served (§10.2) |
| Docker jobs | the Foundry image and a checkout of gno-eth-ibc at a named revision | the job runs with no network beyond the configured RPC, mounts the checkout read-only, and its stdout is captured, never executed |

### 3.4 Deployment

- **Guardian instance (recommended).** `docker compose up`: `ibcmon` with its SQLite volume, pointed at the
  guardian's own gno node, Ethereum execution and beacon nodes, and a second provider URL. Secrets (RPC URLs with
  tokens, webhook URLs, watch-key mnemonics if auto-flagging is enabled) come from the environment; the compose
  file reads `.env`. The console is served by `ibcmon` itself on one port; put it behind the organisation's SSO or
  a private network. Nothing in it needs to be public.
- **Public read-only instance (optional).** The same binary with `public_instance: true`: the API hides monitor
  internals that would reveal the operator's node URLs, the auto-flag sink is refused by configuration, and the
  console shows a permanent banner: *"Public instance. Not authoritative: run your own against your own nodes
  before acting."*
- **Operator laptop.** `ibcmon check <name>` and `ibcmon serve --no-worker` work with no Docker; the worker needs
  Docker.

### 3.5 Light-client verification of Ethereum state

ADR §7.12 and the runbook (§8 step 7) require integrity evidence from an Ethereum node "trusted independently of
the incident", read at an "independently authenticated finalized block". Two levels are supported:

1. **Own node plus second provider** (default): the primary execution node is the operator's own; the finalized
   block hash is cross-checked against a second provider and against the beacon node's finalized checkpoint. This
   is what the ADR describes.
2. **Light-client verified** (when feasible): the primary execution RPC is a [Helios](https://github.com/a16z/helios)
   endpoint, which verifies `eth_call`, `eth_getProof`, `eth_getLogs` results against a sync-committee-verified
   header before answering. `ibcmon` needs nothing special for it: Helios speaks JSON-RPC. The compose file has
   an optional `helios` service with the checkpoint supplied by the operator. Helios trusts the sync committee,
   the same assumption as gno.land's own Ethereum client (ADR §8.1).

The monitor records which level each snapshot had (`provenance.verification: "provider" | "light-client"`), and
the dashboard shows it. Writing a Go Ethereum light client inside `ibcmon` is deliberately not done: the
go-ethereum `beacon/light` packages could do it, but a separate audited process is simpler to trust and to replace.

gno.land headers are read from the operator's own node; a tm2 light-client walk (verifying commits against
validator sets, as the relayer's `GnoLightClient` does) is phase 4 (§12).

### 3.6 Room for T6

The setup audit reads every authority and setting back (AccessManager roles from the `RoleGranted` log, every
proxy's implementation and beacon, `finishSetup`'s checklist, the gno-side authority members and relayer lists) and
compares them with a deployment manifest. It fits as `internal/worker/audit` with its own job kind and a
dashboard page; the deployment manifest (§9.2) is shared. Nothing in the data layer assumes it does not exist.

## 4. Data-source inventory

The exact reads. Each row says what it gives and how it is addressed. Pagination and limits are from the code.
Heights: every gno read is pinned (`abci_query` `height`), every Ethereum read is pinned to a block number
resolved from `finalized` once per evaluation.

Conventions:

- `qeval` means `abci_query` path `vm/qeval`, data `<pkgpath>.<Expr>`; the answer is one typed value per line in
  Go syntax, for a string `("…" string)` (Go-quoted), for an integer `(123 int64)`, for a bool `(true bool)`, for a
  slice built by the call `(slice[("a" string),("b" string)] []string)`. A slice read from persisted state renders
  as references (`slice[ref(…)]`) with no elements, so lists are read from Render JSON pages wherever one exists
  and the slice parser refuses references. The monitor unquotes strings and parses numbers; it never evaluates
  anything else. `vm/qeval_json` (amino JSON of the exported values) is not used.
- `qrender` means path `vm/qrender`, data `<pkgpath>:<path>`; the answer is the realm's `Render(path)` output,
  **Markdown or JSON depending on the path** (see each row). JSON paths are parsed; Markdown paths are shown as
  text only (§10.3).
- `view` means `eth_call` of the named function on the named contract at the pinned block; `event` means
  `eth_getLogs` of the named event; `proof` means `eth_getProof` of a storage slot.

### 4.1 gno.land

Always query the **proxy** pkgpaths; `impl/vN` is never queried directly. The pkgpaths and the port are
configuration (`gno.pkgpaths.core`, `.transfer`, `.ibcgov`), defaulting to `gno.land/r/aib/ibc/core`,
`gno.land/r/aib/ibc/apps/transfer`, `gno.land/r/aib/ibc/ibcgov`.

**Exactness limits of the views.** The parsers read every number as its raw token (`json.Number`) and convert to
`*big.Int` or `int64`, so nothing is lost *here*; but some values are already inexact *on the chain side*, and
the identity may not rest on them:

| Value | Where | Limit | Consequence |
|---|---|---|---|
| core heights, sequences, timestamps | core Render JSON | built from `float64`: exact to 2^53 | fine for heights; packet sequences are read from the controls pages and events instead |
| **`total_escrow` amounts** (per client and total) | transfer v0 `TotalEscrowForDenom` | `float64(int64)`: exact only to 2^53 (≈ 9.0 × 10^15 base units, 9 billion GNOT) | **not an identity term above 2^53**; the obligation is reconstructed from records (§7.1), or read exactly once §15 P1 lands |
| **voucher `total_supply`**, `decimals`, voucher balances | transfer v0 `VoucherJSON`, `VoucherBalance` | `float64` | the gno voucher supply of an Ethereum-native denom is **unavailable** as an identity term above 2^53 until §15 P1 (`VoucherBalanceOf` is exact but needs the holder set) |
| `in_flight`, `returning`, `queued_refunds`, `parked`, `proven_owed`, `unrecovered`, `unresolved` | controls `Totals` | **saturating int64 views** of wide counters (`arith.gno`): exact below 2^63, the maximum above | a term reading `9223372036854775807` is "saturated: unavailable", never a value; §7.1 reconstructs these from records |
| `lost`, `recapitalized`, `reduced`, `accepted` | controls `Totals` | saturating lifetime statistics, "never read by a check" | dashboard only |
| everything else in the controls JSON | `itoa(int64)` | exact | |
| Ethereum views | `uint256` | exact | |

Rows below are marked **exact**, **float64** or **saturating** where it matters.

#### Core (`gno.land/r/aib/ibc/core`), used by the Clients dashboard and the liveness checks

| Read | How | Gives |
|---|---|---|
| client list | `qeval ClientIDs() []string` | every client id, sorted |
| client | `qrender clients/<id>` → `ClientJSON` | `id`, `type` (`tendermint`/`ethereum`), `creator`, `status`, `counterparty_client_id`, `counterparty_merke_prefix` (sic: the key is misspelled in the realm), `client_state`, `last_consensus_state` |
| client list page | `qrender clients?page=N&limit=M` → `{items, page, total}` | the same, paginated (limit ≤ 100; `total` is the number of **pages**) |
| client status | `qrender clients/<id>/status` or `qeval ClientStatus(id) string` | `Active`, `Frozen`, `Expired`, `Unknown`, `Unauthorized` |
| client head | `qeval ClientLatestHeight(id) string` (`rev/height`), `qeval ClientLatestTimestamp(id) (uint64, bool)` | the head and its time (unix seconds) |
| verifier version | `qeval VerifierVersion(id) string` (e.g. `ethereum/v0`) | for the Clients dashboard |
| trust generation | `qeval ClientTrustGeneration(id) uint64` | voids proven outcomes when it changes |
| counterparty | `qeval CounterpartyClientID(id) string` | the Ethereum router client id |
| route identity | `qeval ClientRouteIdentity(id) (identity, router string)` | chain id, genesis validators root, router, commitment slot |
| retired | `qeval IsClientRetired(id) bool`, `IsClientRetiredFor(id, port) bool`, `IsClientRetiredAny(id) bool` | retirement per port (`transfer`, `gov`) |
| consensus states | `qrender clients/<id>/consensus_states?page&limit` and `.../consensus_states/<rev>/<h>` | Tendermint: `{height, timestamp (number, unix s), root, next_validators_hash}`; Ethereum: `{height, slot, timestamp (numbers), state_root, current_sync_committee, next_sync_committee_known, next_sync_committee?}`; below `TrustedFromSlot` an Ethereum state renders `{height, trusted:false}` |
| Ethereum client state | in `ClientJSON.client_state` | `chain_id`, `genesis_validators_root`, `genesis_time`, `genesis_slot`, `seconds_per_slot`, `slots_per_epoch`, `epochs_per_sync_committee_period` (decimal strings), `latest_height{revision_height = latest slot}`, `latest_execution_block_number`, `is_frozen`, `ibc_contract_address`, `ibc_commitment_slot`, `fork_parameters` |
| Tendermint client state | in `ClientJSON.client_state` | `chain_id`, `latest_height`, `frozen_height`, `trust_level`, `trusting_period`/`unbonding_period`/`max_clock_drift` (float seconds), `upgrade_path` |
| packet state | `qeval PacketCommitment(id, seq) []byte`, `PacketReceipt(id, seq) []byte`, `PacketAcknowledgement(id, seq) []byte`; `qrender clients/<id>/packet_commitments|packet_receipts|packet_acknowledgements[?page]` (`{sequence: string, data: base64}`) | the receipt the orphan test authenticates against |
| next sequence | `qrender clients/<id>/next_sequence_send` → `{"next_sequence_send": n}` | the gno-side "last allocated + 1" for the absence-by-sequence test when gno.land is the source |
| async acks | `qeval HasPendingAsyncAck(id, seq) bool`, `PendingAsyncAck(id, seq)` | the "asynchronous ack not yet written" case |
| admin | `qrender admin` (Markdown, text only), `qeval AuthorityMembers() []address`, `Relayers() []address`, `Paused() bool`, `ImplPath() string`, `ImplVersion() string`, `RegisteredImplPaths() []string`, `IsImplAddress(a) bool` | the T6 corner and the bridge-realm set |
| apps | `qrender apps` → `[{port_id, pkg_path, address, paused}]`, `apps/pending` | |
| events (block results) | `block_results` of each height, `send_packet`, `recv_packet`, `write_acknowledgement`, `acknowledge_packet`, `timeout_packet` (`packet_source_client`, `packet_dest_client`, `packet_sequence`, `packet_timeout_timestamp`, `encoded_packet_hex`, `encoded_acknowledgement_hex`), `create_client`/`update_client`/`client_misbehaviour`/`recover_client`/`migrate_client`, `client_retired`, `impl_updated` | the gno-side receive list (`recv_packet` with the packet bytes) for the orphan test; update cadence for the liveness check |

Expiry is **derived**, there is no getter: Tendermint `expiry = last_consensus_state.timestamp + trusting_period`;
Ethereum `expired when currentSlot > latestSlot + 8 · epochs_per_sync_committee_period · slots_per_epoch` with
`currentSlot = (now − genesis_time)/seconds_per_slot + genesis_slot` (`TrustingPeriods = 8`,
`p/aib/ibc/lightclient/ethereum/v0`), shown as a time by converting slots back to seconds.

#### Transfer (`gno.land/r/aib/ibc/apps/transfer`), used by the Routes dashboard and the integrity checks

| Read | How | Gives |
|---|---|---|
| routes | `qrender routes` → `[RouteJSON]`; `qrender routes/<R>` → `RouteJSON` or `null` | `route`, `client`, `replaced[]` (every retired client, in order), `enrolled` (unix), `activated` (height), `closed`, `loss_closed`, `fee_collector`, `refused_recipients[]`, `protected_recipients[]`, `unrecovered`, `unresolved`, `denoms[]` |
| denom | in `RouteJSON.denoms[]` | `denom`, `kind` (`unescrow`/`mint`), `cap_floor`, `launch_cap_floor`, `supply_cap`, `fee`, `fee_floor`, `send_max`, `send_min`, `refund_cap`, `remote` (the ERC20), `meter{level, at, epoch}`, `refund_meter{level, at}` (**stored** levels, as of the last refill), `waiting` (recipients with an open claim), `queued_refunds` (entries), `totals{credited, owed, withheld, swept, paid, fees, sweep_paid, outstanding, returning, parked, proven_owed, in_flight, sent, send_fees, refunded, queued_refunds, lost, recapitalized, reduced, accepted, unrecovered, unresolved}` |
| claims | `qrender routes/<R>/claims?from=<cursor>` → `{entries[], next}` (≤ 100 per page, `next == ""` at the end; cursor inclusive) | `key` (`client/<20-digit seq>`), `denom`, `to`, `amount`, `paid`, `reduced`, `fee`, `state` (`open`/`withheld`/`forged`/`done`), `by` (`packet`/`vote`), `at`, `height`, `seq` |
| sweeps | `qrender routes/<R>/sweeps?from=` | `key`, `denom`, `to`, `amount`, `reduced`, `reason`, `height`, `voided`, `paid`, `paid_to`, `from_claim?`, `claim_paid?` |
| sends (send records) | `qrender routes/<R>/sends?from=` | `key`, `packet{sequence, source_client, destination_client, timeout_timestamp, payloads[{source_port, destination_port, version, encoding, value (hex)}]}`, `commitment` (hex), `sender`, `receiver`, `denom`, `kind`, `amount`, `fee`, `sent_height`, `sent_at`, `outcome` (`""`/`success`/`error`/`timeout`), `processed_height`, `processed_at`, `payment` (`""`/`paid`/`queued`/`claimed`/`rejected`/`reconciled`), `refunded`, `settlement` (0–5), `reservation`, `proven`, `proven_at`, `proven_by`, `proven_gen`, `incident` |
| losses | `qrender routes/<R>/losses?from=` | `key`, `direction` (`recv`/`send`), `denom`, `amount`, `chain` (`gno`/`ethereum`: `ethereum` is a **mirror**), `incident`, `cause` |
| internal escrow accounting (the lock) | `qrender total_escrow/<denom>` → `{denom, amount, clients[{client (a route key or any other client id), amount}]}` (**float64**) | the realm's per-client escrow *accounting* (`IterateEscrow`), every client of the port included, Ethereum routes and clients outside the controls alike; **not** a physical balance |
| **physical escrow balance** | native coins: `abci_query bank/balances/<transfer realm address>` (amino JSON `Coins`, decimal strings: **exact**) at the pinned height; GRC20: `qeval BalanceOf(<realm address>) int64` on the token realm (exact) | the realm holds **one physical account for every client** (`unescrowNative`: "a single physical escrow account (no per-client addresses in Gno)"), so the cover check compares each (account, denom) balance with the **sum** of every obligation booked against it across all clients (§7.1) |
| denom trace | `qrender denoms/ibc/<hash>` → `{base, path, denom}`; `qrender denoms[?page]` | a minted denom's Ethereum token is its trace `base` (`DenomConfig.Validate`: `Remote` must be empty for `mint`, "a minted denom's Ethereum token is its base"); `path` names the hop (`transfer/<client>`), which must be a client of the route |
| vouchers | `qrender vouchers[?page]`, `voucher/ibc/<hash>` → `{denom, grc20reg_key, name, symbol, decimals, total_supply}` (**float64**); `qeval VoucherBalanceOf(ibcDenom, addr) int64` (exact) | the gno voucher supply of an Ethereum-native denom: an identity term only below 2^53 or once §15 P1 lands; shown with its limit |
| blocklist, members | `qeval BlockedAddresses() []address`, `AuthorityMembers()`, `ImplPath()`, `ImplVersion()`, `KeepsControls(path) bool` | |
| controls reader | `qeval ExtView("controls")` returns `controls.Reader` typed as `any`; it cannot be type-asserted from a `qeval` expression on the transfer pkgpath (the type is not in the package scope) — see the `ibcgov` row `view()` below | |
| events | `route_credit{route, packet, denom, receiver, amount, booked}`, `route_claim{route, packet, denom, to, amount, fee}`, `route_claim_swept`, `route_refund{route, packet, denom, to, amount}`, `route_settle{route, packet, outcome, payment, refunded}`, `route_proven{route, packet, outcome, height}`, `route_reopen{route, accepted, open}`, `route_prepare{route, reserved, done}`, `route_recapitalize{route, id, denom, amount, payer}`, `route_gov{op, route, note}`, `ibc_transfer{sender, receiver, denom, amount, memo}`, `fungible_token_packet`, `timeout`, `block`/`unblock`, `impl_updated` | the releases the accounted escrow subtracts (claims paid, refunds paid, recapitalizations in), the "continue with `ContinueReopen`" and "prepare the rest with `PrepareReplacement`" notes, the recapitalization ids |

#### Controls reader (`gno.land/p/aib/ibc/ics20/controls/v0` `Reader`), reached through `ibcgov`

`ibcgov` has an unexported package-level `view() controls.Reader` that returns the transfer app's reader already
type-asserted. `qeval` evaluates its expression in the package block, where package-level names (exported or not)
resolve and file-level imports do not, so `gno.land/r/aib/ibc/ibcgov.view().ReopenBlockers("R")` is expected to
work while `transfer.ExtView("controls").(controls.Reader)` cannot be written. **This is to be verified on the
local testnet in phase 1**; until then every reader method below has a fallback from the Render pages, and §15
asks for exported views.

| Reader method | Gives | Fallback |
|---|---|---|
| `Routes() []string`, `RouteOf(clientID)`, `RetiredRouteOf(clientID)`, `Client(route)` | route keys and aliases | `routes` page |
| `Closed(route)`, `LossClosed(route)`, `Unrecovered(route)`, `Unresolved(route)` | route state | `RouteJSON` |
| `Denoms(route)`, `DenomConfig(route, denom)`, `Totals(route, denom)` | per denom config and totals | `RouteJSON.denoms` |
| **`Meters(route, denom, to, now, epoch, epochAt, lock) (bridge, recipient int64)`** | the **live** bridge and recipient levels refilled to `now` on copies; `epoch, epochAt` from `ibcgov.PayoutsEpoch(route)`, `lock` from the escrow accounting (0 for `mint`), `to` a recipient or empty | **none that is exact.** `Meter.Refill` depends on state the JSON does not show: the payouts-epoch reset (the meter empties as of `epochAt` when the epoch changed), the fractional remainder `Frac`, and the rate and ceiling *stored by the last refill* (a cap change applies only from the next refill); a recipient's queue meter is not rendered at all. A client-side refill from `level, at` and the current cap reproduces none of that, so it is **not shown**: the live level is `unavailable` until the reader path is verified or §15 P1 lands; the stored level and its time are shown as such |
| `ReopenBlockers(route) []string`, `SuspectUnsettled(route, client) int64`, `OpenSends(route, client) int64`, `SuspectFrom(route, client) (int64, bool)` | reconciliation progress per retired client | derive from the sends page: open = outcome `""` or payment `queued`; suspect unsettled = `processed_height ≥ suspectFrom` and `settlement == 0`; `suspectFrom` only from the replacement decision's text |
| `Claim(route, key)`, `Sweep(route, key)`, `Send(route, key)`, `Voidable(route, key, forged)` | one record by key | the pages |
| `Recap(route, id) (Recap, bool)` | a recapitalization authorization | the `route_gov` event `note` carries the id |
| `IsRefused(route, addr)`, `IsFeeCollector(a)`, `FeeCollector(route)` | | `RouteJSON` |

#### ibcgov (`gno.land/r/aib/ibc/ibcgov`), used by the Governance dashboard, flags and holds

| Read | How | Gives |
|---|---|---|
| page | `qrender ""` (Markdown, text only) | governor, clock, channel, hold, seats, routes with flags (**enrolled routes only**), decisions (newest 100 pending and 100 settled per kind) |
| full ledger | `qrender decisions` → JSON array, oldest first, **unpaginated** | `{id, kind, text, state (pending/applied/cancelled/spent), made, recorded, due, applied, cancelled, note}` |
| decision | `qeval Decision(id int64) (decisions.Decision, bool)`, `LastDecision() int64` | the same fields (`Params` is dropped) |
| governor | `qeval Governor() address`, `Yielded() int64`, `GovDAOGoverns() bool` | |
| seats | `qeval Seat(n int) address` for `n` in 0..5 (0–3 guardians, 4–5 watch), `SeatOf(a) int`, `IsGuardianSeat(n)` | who holds which seat; which actions the connected wallet may sign |
| flags | `qeval Flags(routeOrClient string) []string`, each line `"<fn> seat <n> at <unix>[ naming k1,k2]"`; `Flagged(route, fn) bool`; `NamedSuspect(route, key) bool` | per route **and per client with no route** (counts toward the hold) |
| hold | `qeval Held() bool`, `HoldSince() int64` | the hold's start; it ends at `belowSince + 24 h`, where `belowSince` (when the guardian flags fell below two) is unexported: the **end** is only in the `HoldEnding{at}` event, never `HoldSince + 24 h` |
| payouts epoch | `qeval PayoutsEpoch(route) (epoch, at int64)` | for `Reader.Meters` |
| route identity | `qeval RouteIdentity(route) string`, `IsRoute(route) bool`, `BridgeRealm(a) string` | |
| channel | `qeval Epoch() uint32`, `EpochClient() string`, `LastSeq(e uint32) uint64`, `Tombstones() int`, `AwaitingGovRetirement() int` | the gno side of the governance channel |
| dispatch | `qrender dispatch/<e>/<seq>` → `{epoch, seq, kind, decision, body (hex), status, redispatch, in_flight, transport, sends, event}` or `null`; `qeval DispatchBody(e, seq) []byte`, `DispatchState(e, seq) (status uint8, redispatch, inFlight bool, sends int, event string)`, `DispatchTransport(e, seq) uint64` | every Ethereum-bound decision of an epoch, `seq = 1..LastSeq(e)`; the envelope bytes to decode the kind's params (§4.2, envelope) |
| codecs | `qeval ParseEnvelope(b) (e, seq, kind, params, ok)`, `ParseAck(b)`, `ParseEpochNotice(b)` | offline decoding is done in Go instead (same layout) |
| events | `Decision{id, kind, state: queued/spent/applied/cancelled/deferred, due, until}`, `Flag{route, fn, seat, packets}`, `Unflag`, `Hold{since}`, `HoldEnding{at}`, `SeatFlagsEnded{seat}`, `SeatSet{seat, address}`, `FlagsMoved{from, to, flags}`, `Yielded`, `GovPacketSent{epoch, seq, kind, client, sequence}`, `GovAck{epoch, seq, status}`, `GovTimeout`, `GovEpoch{epoch, client}`, `GovNotice{client, status: refused/proven/applied, reason, epoch}`, `GovIntent`, `GovExpect`, `GovUnexpect`, `GovPurge`, `GovTombstone` | the seat-change **deferral anchor** (`Decision ... deferred until`), hold end times, the expected-transition graph (no listing view exists) |

Constants: decision delay 7 d, unpause 24 h, proposal deadline 14 d, `PageMax` 100, `holdLinger` 24 h,
`holdReplacementWait` 21 d, `MaxKeys` 64, `MaxClaims` 20, `PageLines` 100, `ReopenBatch` 64, gov window 64,
`GovPacketTimeout` 6 h.

### 4.2 Ethereum

Addresses come from the deployment file (`contracts/deployments/<chain>.json` of gno-eth-ibc, keys `accessManager`,
`ics26Router`, `ics20Transfer`, `ics20Implementation`, `ics20Controls`, `gnoLightClient`, `ugnotVoucher`,
`ibcAdmin`, `ibcAdminExec`, `gnoChainId`, `clientId`, `counterpartyClientId`, `activationKind4Params`) and are
configuration here (`eth.addresses.*`). ABIs are embedded from the Foundry build of a named gno-eth-ibc revision
(`abi/`): the **transfer proxy's ABI is the union** of `GnoICS20Transfer`, `IGnoICS20Controls`,
`GnoICS20Reconcile` and `GnoICS20Evidence`, because the modules are reached by delegatecall through the proxy's
fallback (calling a module's own address reverts `NotDelegated`). `IBCAdmin` is a plain contract, not a proxy.

#### Transfer proxy (`ics20Transfer`), Routes dashboard and integrity checks

| Read | Gives |
|---|---|
| `routeKey(clientId) string`, `currentClient(routeKey) string`, `isRetired(clientId) bool`, `getEscrow(routeKey) address` | aliases and the escrow address per route |
| `routeClosed(c) bool`, `routeReopening(c) bool`, `routePaused(c) bool`, `refundsHeld(c) bool`, `recoveryBlocked(c) bool` | route state |
| `routeFlags(c) Flag[6]`, `refundsFlags(c) Flag[6]` (`Flag{blockNumber, time}`, zero = not standing), `routeHold(c) Flag`, `refundsHold(c) Flag`, `guardianKeys() address[6]` | flags per seat and the two holds of the route. A hold view gives its **start**; it ends 24 h after the guardian flags **dipped** below two (`Guardians.hold`: `dipTime + grace`), at once when a flag standing at the dip's start lost its seat, or on a key replacement — and `dipTime` is not exposed. The end is therefore **not derivable from the start**: the console shows the start, the dip reconstructed from the flag-cleared events when the history allows it, and `HoldEnded{start, end}` once it ended |
| `limits(c, token) LimitsView{set, limits{maxPerMessage, dayCap, refundCap, sendMax, sendMin}, receiveLevel, refundLevel}` | per token limits and the **live** bucket levels |
| `quarantineSeconds(c) uint64`, `quarantineEnd(c) uint256` | the route's quarantine |
| `sendRecord(c, seq) SendRecord{commitment, sender, token, escrow, timeoutTimestamp, sendBlock, processedBlock, processedIndex, burned, outcome (Pending/Success/ErrorAck/Timeout), payment (None/Paid/Queued/Claimed/Parked/Rejected), amount, refunded, settlement (0–4), open, proven (None/Delivered/Refundable/Collision), held, provenBy, provenGen}` | the permanently retained send record: the orphan test's evidence and the in-flight / unpaid-refund terms |
| `openRecords(c) uint64`, `suspectRecords(c) (from, open)`, `clientRecon(c) (open, incident, suspectStart, suspectOpen, processed)` | reconciliation progress per client id |
| `reserved(c, token) uint256`, `losses(c, token) uint256`, `acceptedDeficit(c, token) uint256`, `lossBooked(c, seq, direction) bool` | reservations and the loss ledger. **The identity's `unrecovered_losses` on Ethereum is `losses` itself**: Ethereum has no recapitalization or reduction (ADR §8.5 #33), and a kind 13 only raises `acceptedDeficit` to `losses` (`_acceptDeficits`: `acceptedDeficit += losses − acceptedDeficit`) without restoring backing — acceptance changes the reconciliation *status*, not the economics. `losses − acceptedDeficit` is "losses awaiting acceptance", a dashboard value |
| `incidents(c) (Incident[]{kind (1 replacement/2 late loss), status (1 open/2 reopening/3 resolved), client (keccak of the id), suspectFrom}, resolvedUpTo, pending)` | the incident list and `pendingIncidents` |
| `refundQueue(c, token) (head, tail)`, `receiveRecord(c, seq) (token, amount)` (amount 0 once booked as a forged receive; the historical amount is in `GnoSettled`) | queue bounds; per inbound sequence the receive |
| `gnoBusy() bool`, `gnoControlsVersion()`, `CONTROLS()`, `CONTROLS_HASH()`, `RECONCILE()`, `RECONCILE_HASH()`, `EVIDENCE()`, `EVIDENCE_HASH()`, `gnoSelf()`, `getEscrowBeacon()`, `getIBCERC20Beacon()`, `ics26()`, `authority()`, `paused()` | the delegate graph (worker §9) and the repair-critical `gnoBusy()` |
| `ibcERC20Contract(denom) address`, `ibcERC20Denom(token) string` | voucher lookup per denom (no voucher list exists; denoms come from kind 5 decisions) |
| events | `GnoSendRecorded{clientId, sequence, commitment, token, amount}`, `GnoOutcomeRecorded{clientId, sequence, outcome, payment}`, `GnoRefundPaid`, `GnoRefundQueued`, `GnoRefundParked`, `GnoRefundRejected`, `GnoOutcomeIgnored`, `GnoSettled{clientId, sequence, action, amount}`, `GnoLossRecorded{routeKey, token, sequence, amount}`, `GnoDeficitAccepted`, `GnoRouteReopened`, `GnoReopenAborted`, `GnoRouteClosedForLoss`, `GnoRouteMigrated{old, new, routeKey}`, `GnoRouteFlagSet/Cleared{routeKey, key, seat, refunds}`, `GnoRouteUnpaused`, `GnoGuardianKeysSet`, `GnoLimitsSet`, `GnoQuarantineSet`, `GnoRecoveryBlockSet`, `GnoOutcomeProven`, `GnoIncidentPrepared`, `GnoSenderNotified`, `GnoNotificationDue` | **enumeration**: send records are keyed by `(keccak(clientId), seq)` with no list, so the monitor indexes `GnoSendRecorded` (and the router's `SendPacket`) and then reads `sendRecord`; receives have no transfer event, so the router's `WriteAcknowledgement` plus `receiveRecord`; flags across routes from the flag events |

#### Router (`ics26Router`, eureka v3.0.2)

| Read | Gives |
|---|---|
| events `SendPacket{clientId (indexed: keccak), sequence, packet}`, `WriteAcknowledgement{clientId, sequence, packet, acknowledgements}`, `AckPacket{clientId, sequence, packet, acknowledgement}`, `TimeoutPacket{clientId, sequence, packet}`, `ICS02ClientAdded`, `ICS02ClientMigrated`, `ICS02ClientUpdated`, `ICS02MisbehaviourSubmitted` | sends with the full packet bytes (the send record keeps only the commitment); **receives** (there is no `RecvPacket` event: a receive is a `WriteAcknowledgement`); client lifecycle. The indexed `clientId` is a keccak topic: the id is recovered from the packet in the data |
| `getCommitment(keccak256(path)) bytes32` | the live commitment, receipt or ack commitment at `path = clientId ‖ kind ‖ seq_be` (kinds 1 commitment, 2 receipt, 3 ack); live, so **not** evidence of absence |
| `proof` of `prevSequenceSends[sourceClient]` | **no getter**: `eth_getProof` of slot `keccak256(bytes(sourceClient) ‖ uint256(0x1260944489272988d9df285149b5aa1b0f48f2136d6f416159f840a3e0747601))` on the router proxy (ERC-7201 `ibc.storage.IBCStore` base + 1), pinned to the evaluation block: the last allocated sequence of that client, every app's packets counted |
| `proof` of `retired[sourceClient]` | `keccak256(bytes(sourceClient) ‖ uint256(0xf3bec7e430c885c7577ab512690266d2e153034fd4fd64588d19040d9649610a))` on the transfer proxy (`gno.storage.GnoICS20Transfer` base + 10), the alternative to `isRetired` |

#### IBCAdmin (`ibcAdmin`), Governance dashboard and the governance watch

| Read | Gives |
|---|---|
| `epoch() uint32`, `lastSeq() uint64`, `govClient() string`, `lastRecordTime() uint64`, `gnoChainId()`, `setupDone() bool` | the channel's Ethereum side; `silence` eligibility of the recovery is `now − max(lastRecordTime, epoch opening)` |
| `decision(epoch, seq) Decision{hash, kind, state (0 none/1 buffered/2 pending/3 executed/4 spent/5 cancelled), bad, recordedTime, recordedBlock, params}` | the ledger, read for `seq = 1..lastSeq` and the buffer `lastSeq+1..lastSeq+64`; **`matureAt` is not stored**: `recordedTime + (kind == 2 ? 24 h : 7 d)`, or the `DecisionRecorded` event's 4th argument for a pending decision; **params are deleted once closed**: history is decoded from the delivery transaction's calldata or from `ibcgov`'s `dispatch/<e>/<seq>` body (the same bytes) |
| `isTombstoned(epoch, seq) bool`, `tombstones(epoch) uint64[2][]` | cancellations |
| `keys() address[6]`, `flags() Flag[6]`, `hold() Flag`, `holds() Hold[]{start, end, merged}` (≤ 8, oldest merged), `conflictBlock() uint64` | `IBCAdmin`'s flags, the conflict flag, the hold history anchors use |
| `replacementAnchor(epoch, seq) uint64` | the pinned seat-change/hand-over hold anchor; the kind 9 **flag anchor has no getter**: from `ReplacementAnchored{epoch, seq, notBefore}` |
| `exec() (address, bytes32)`, `IBCAdminExec.bindings() (router, accessManager, ics20)`, `CHECKS_HASH`, `RECOVERY_HASH`, `recoveryModule` | the executor pin and delegate graph for the worker |
| `recoveryConfig() {delay, threshold, cancelThreshold, silence, holdPeriod}`, `recovery() (proposal, openedAt, cancels, approved[4])` | the emergency recovery: approvals are the non-zero `approved` hashes; the proposal's **contents have no getter**: from the `approveRecovery` calldata (`RecoveryProposal{newClientId, counterpartyClientId, merklePrefix, lightClient, codeHash, suspectFrom}`, hash = `keccak256(abi.encode(p))`) |
| `epochNotice(epoch) bytes`, `epochNoticeAcked(epoch) bool` | the notice bytes (`0x01 ‖ epoch u32 ‖ clientId`) and its acknowledgement |
| events | `DecisionReceived{epoch, seq, kind, hash, status, redispatch}`, `DecisionRecorded{epoch, seq, kind, matureAt}` (for a decision closed at recording the 4th value is `recordedTime`), `DecisionExecuted`, `DecisionSpent{…, reason}`, `DecisionCancelled`, `Tombstoned`, `Conflict{epoch, seq, stored, received}`, `FlagSet/FlagCleared{key, seat}`, `HoldEnded{start, end}`, `ReplacementAnchored{epoch, seq, notBefore}`, `Unpaused`, `GuardiansSet{seat, key}`, `EpochOpened{epoch, clientId}`, `ExecReplaced{exec, codeHash}`, `RecoveryApproved{proposal, guardian, approvals}`, `RecoveryOpened{proposal, executableAt}`, `RecoveryCancelVote`, `RecoveryCancelled`, `RecoveryBlockKept`, `RecoveryExecuted{proposal, newClientId}`, `SetupDone`, `EpochNoticeSent{epoch, clientId, sequence}` (0 = refused, resend), `EpochNoticeAcknowledged` | a **halted** decision is not persisted: it stays `BUFFERED` and the delivery emits `DecisionReceived` with status 2; the monitor shows "buffered behind `lastSeq`, halted?" when `lastSeq+1` is buffered and a `drain()` call simulated with `eth_call` leaves it buffered |

Envelope: `version u8 (1) ‖ epoch u32 ‖ seq u64 ‖ kind u8 ‖ params` (big-endian), `params = abi.encode` of the
kind's tuple (ADR §5.4 table; vectors in gno-eth-ibc `contracts/test/fixtures/gov-vectors.json`, which are this
repository's decoder fixtures). Ack: `epoch u32 ‖ seq u64 ‖ status u8 ‖ redispatch u8`.

#### GnoLightClient (`gnoLightClient`, and any kind 6 / recovery candidate), Clients dashboard and liveness

| Read | Gives |
|---|---|
| `getClientState() bytes` → `ClientState{chainId, revisionNumber, latestHeight, trustLevel{numerator, denominator}, trustingPeriod (s), maxClockDrift, isFrozen}` | **no `latestHeight()`/`trustingPeriod()` getters**: decoded from this |
| `getConsensusState(height) ConsensusState{timestamp (ns), appHash, nextValidatorsHash}` | the head's time; `expiry = timestamp/1e9 + trustingPeriod` |
| `isFrozen() bool`, `isExpired() bool`, `liveEpoch() (validatorsHash, nextValidatorsHash)`, `lastTransition() (id, time)`, `lastTransitionTime()`, `quarantineEnd(duration)`, `isQuarantined(duration)` | status and the quarantine source |
| events `ClientUpdated{height, blockHash, appHash}`, `LiveTransition{height, validatorsHash, nextValidatorsHash, transitionTime}`, `Frozen{height}`, `ValidatorSetCached` | update cadence (there is no list of stored heights); validator-set transitions |
| `FrozenLightClient`: `isFrozen()` true, everything else reverts `ClientRetired` | a retired id |

#### GnoVoucher (per registered denom)

`totalSupply()`, `decimals()`, `ics20()`, `fullDenomPath()`, `balanceOf(escrow)`; ERC20 `Transfer` events. The
voucher list is not enumerable on chain: it is built from executed kind 5 decisions and `ibcERC20Contract(denom)`.

#### ERC20 (Ethereum-native tokens)

`balanceOf(escrow)` at the pinned block; `decimals()`, `symbol()`.

### 4.3 Chain level

| Read | Gives |
|---|---|
| gno `status` | `latest_block_height`, `latest_block_hash`, `latest_block_time`, `catching_up` |
| gno `block`/`commit` at `h` | header (`app_hash`, `time`, `validators_hash`, `next_validators_hash`), block hash: the cross-check key and the AppHash proof of state at `h−1` |
| gno `block_results` at `h` | the events of §4.1 |
| Ethereum `eth_getBlockByNumber("finalized")`, `eth_getBlockByHash` | the finalized block, its hash, `stateRoot`, `timestamp` |
| beacon `/eth/v1/beacon/light_client/finality_update`, `/eth/v1/beacon/headers/finalized` | the finalized execution block the beacon node vouches for |
| Ethereum `eth_getCode`, `eth_getProof`, `eth_getLogs` (chunked, bounded ranges) | code hashes (worker), storage proofs (orphan tests), the event indexes |

### 4.4 What the data layer stores

Snapshots keyed by `(chain, height|block)`: clients, routes, flags, holds, decisions, recovery, incidents; the
packet index (send records and receives per route, both chains, with the heights they were first seen at and the
heights their outcomes changed at); the loss ledger; the per-evaluation closed pair and identity terms. Every
snapshot carries `Provenance{chain, height, block_hash, node, read_at, verification}`.

## 5. Dashboards

Every number on every dashboard has a provenance badge: `gno.land @ height H via <node>` or `Ethereum @ block B
(finalized, <verification>) via <node>`, and the time it was read. A number whose cross-check failed is shown
struck through with "unverified". The console never computes an amount with `Number`.

### 5.1 Clients

One row per IBC client on gno.land (`ClientIDs()`), plus the Ethereum `GnoLightClient`s: the route's current
client, every enrolled kind 6 target, and an open recovery's candidate.

| Column | gno.land client | Ethereum `GnoLightClient` |
|---|---|---|
| status | `ClientStatus` | `isFrozen`/`isExpired`, `Frozen`/`Expired`/`Active` |
| type and verifier | `type`, `VerifierVersion` | contract address, code hash (worker verdict when known) |
| latest head | `ClientLatestHeight` (slot or height), `latest_execution_block_number` | `latestHeight` from `getClientState` |
| head time | `ClientLatestTimestamp` | `getConsensusState(latestHeight).timestamp` |
| trusting period and expiry countdown | derived (§4.1) | `trustingPeriod`, derived |
| last update | newest `update_client` event | newest `ClientUpdated` |
| trust generation | `ClientTrustGeneration` | the route's `verifierGen` has no getter: shown as "bumped at" from `GnoRouteMigrated`/kind 6 executions |
| counterparty | `CounterpartyClientID`, `counterparty_merke_prefix` | the launch record / kind 6 params |
| route identity | `ClientRouteIdentity` | `gnoChainId` |
| retired for | `IsClientRetiredFor(id, "transfer"|"gov")` | `isRetired` |
| live epoch and last transition | — | `liveEpoch`, `lastTransition`, quarantine end |

### 5.2 Routes

One page per route key, both chains side by side.

- **Header:** route key, current client on each chain, `replaced[]`, closed / reopening / loss-closed, recovery
  block, enrolled and activated, fee collector, identity.
- **Denoms table** (one row per denom, both accountings): kind, the Ethereum token (`remote` for `unescrow`, the
  trace base for `mint`), caps and fees, the **stored** meter level with its time and the **live** level only when
  an exact source gives it (`Reader.Meters`), otherwise "live: unavailable" with the reason; refund meter likewise;
  Ethereum `limits` with live `receiveLevel` and `refundLevel` (exact); the Ethereum escrow balance and voucher
  supply; the gno physical balance (exact), the escrow accounting (`total_escrow`, marked float64) and the voucher
  supply (marked float64); `totals` with saturated values marked.
- **Quarantine:** armed, duration, `quarantineEnd`, the last `LiveTransition`.
- **Claims:** open, withheld, forged, done, paged; sweep entries; per recipient the live recipient meter.
- **Refunds:** queued (gno `queued_refunds`; Ethereum `refundQueue` and the queued/parked records), parked, held by
  a `refunds` hold, with ages.
- **Sends and send records:** paged, filterable by outcome, payment, settlement, proven state; the orphan test
  verdict per receive once the monitor evaluated it.
- **Losses and deficits:** the loss ledger per denom with `chain` (authoritative vs mirror), recapitalizations
  (`Recap`), reductions, accepted deficits, `unrecovered`, `unresolved`; Ethereum `losses`/`acceptedDeficit`.
- **Incidents and reconciliation:** `incidents()` with status, `clientRecon` per retired client (open, suspect
  open, processed), `ReopenBlockers`, `SuspectUnsettled`, `OpenSends`, `SuspectFrom`, preparation done
  (`GnoIncidentPrepared`, `route_prepare{done}`), the "continue with `ContinueReopen`" and "prepare the rest" notes.
- **Monitor panel:** the latest closed pair and every identity term for this route and denom, residuals, surplus,
  and the two-evaluation break counter.

### 5.3 Flags and holds

Per chain: the six seats (holder, role, generation changes from `SeatSet`/`GuardiansSet`), every standing flag with
its function or object, seat, block and time, named suspect packets (gno `payouts` flags), the hold with its start
and its **end time where it is known** (gno: only from `HoldEnding{at}`; Ethereum: `routeHold`, `refundsHold`,
`IBCAdmin.hold()` give the start, the dip is reconstructed from the flag-cleared events when the history allows,
`HoldEnded{start, end}` gives the end once over, and the hold history with merged intervals; a key replacement
ends a hold at once, shown from `GuardiansSet`/`GnoGuardianKeysSet`; never start + 24 h), and the
**deferral anchors**: every pending seat change or hand-over with its anchor and landing time
(`replacementAnchor`, `ReplacementAnchored`, gno `Decision ... deferred until`). Flags on a gno client with no route
are listed (they count toward the global hold).

### 5.4 Governance ledger

- **gno.land:** every decision from `decisions` (with `Decision(id)` detail): state, kind, text, made, recorded,
  due, applied, cancelled, note; pending ones grouped by maturity; which are deferred by the hold; the 14-day
  proposal deadline; Ethereum-bound dispatches per epoch (`dispatch/<e>/<seq>`) with status, redispatch flag,
  in-flight, transport sequence, the decoded envelope (kind and params).
- **Ethereum:** `IBCAdmin` decisions per epoch with state, kind, decoded params (from `decision().params` while
  pending; from the dispatch body otherwise), `recordedTime`, maturity, deferral cause (hold, flag, conflict,
  recovery suspension for kinds 3 and 6–10), tombstones, **buffered** sequences behind `lastSeq` (and whether a
  `drain()` simulation leaves them buffered: a halted decision), conflicts, the executor pin.
- **Epochs and notices:** `Epoch()`/`EpochClient()` against `epoch()`/`govClient()`; each epoch's notice bytes,
  `epochNoticeAcked`, `EpochNoticeSent` sequences, gno `GovNotice` events (refused/proven/applied), the expected
  transitions from `GovExpect`/`GovIntent`/`GovUnexpect`/`GovPurge` events, `Tombstones()`,
  `AwaitingGovRetirement()`.
- **Watch verdicts** next to every kind 5, 6, 7 and 9 decision and recovery candidate (§9).

### 5.5 Recovery

`recoveryConfig` (enabled or not, thresholds, delay, silence, hold period), eligibility now (client frozen/expired,
silence elapsed, hold stood `holdPeriod`), the open proposal (hash, decoded contents, `openedAt`, executable at,
cancel votes, which seats approved), the candidate verifier's freshness (§7.8), the route's recovery block, and
the gno side's `NewExpectRecoveryRequest` state.

### 5.6 Monitor and worker

The checks with their last result, schedule, consecutive skips and history; alarms with state and evidence; worker
jobs with logs and verdicts; provider cross-check status. On a public instance this page hides node URLs.

## 6. Actions

The console prepares a transaction, shows exactly what will be signed (realm, function, every argument; or
contract, function, decoded arguments), and hands it to the wallet. It never holds a key, never submits anything
the user did not sign, and never retries on the user's behalf.

### 6.1 Guardians

| Action | gno.land (Adena, `MsgCall`) | Ethereum (EIP-1193) |
|---|---|---|
| set a flag | `ibcgov.SetFlag(route, fn, keys)`; `fn` ∈ `sends`, `credits`, `payouts`, `refunds`; `keys` only on a guardian's `payouts` flag: comma-separated `client/<20-digit seq>`, ≤ 64 | transfer proxy `setRouteFlag(clientId)` (pause), `setRefundsFlag(clientId)`; `IBCAdmin.setFlag()` (its own object; refused before `finishSetup`) |
| clear a flag | `ibcgov.ClearFlag(route, fn)` (own flag only) | `clearRouteFlag`, `clearRefundsFlag`, `IBCAdmin.clearFlag()` |
| recovery | — (no gno-side vote exists) | `IBCAdmin.approveRecovery(proposal)` (seats 0–3), `IBCAdmin.cancelRecovery()` (withdraws the approval before opening; a cancel vote once open) |

The console shows the seat the connected address holds (`SeatOf`, `guardianKeys()`/`keys()`) and the flags it
already has standing; it never sends a flag for a seat the address does not hold.

### 6.2 GovDAO members and the governor

`ibcgov`'s builders return a `Request` struct whose fields are unexported, so a proposal is made in a **`MsgRun`
script** that calls the builder and `dao.MustCreateProposal(cross(cur), req.Proposal())` (after the yield), or
`ibcgov.Govern(cur, req)` (the governor, before it). The console drafts that script from a form per builder, shows
it verbatim, and offers two paths: sign the `MsgRun` with Adena (members whose wallet supports `MsgRun`), or copy
the script for `gnokey maketx run`. Every builder of ADR 0004 is a form: the gno-side kinds (`NewCancelRequest`,
`NewSetSeatsRequest`, `NewUnpauseRouteRequest`, `NewSetDenomRequest`, `NewVoidRequest`, `NewSettleRequest`,
`NewReplaceRouteRequest`, `NewReopenRouteRequest`, `NewRecordLossRequest`, `NewRecapitalizeRequest`,
`NewReduceRequest`, `NewAcceptDeficitRequest`, `NewExpectRecoveryRequest`, `NewRecoverClientRequest`, …) and the
Ethereum-bound kinds 1–13 (`NewEthCancelRequest` … `NewEthReopenRequest`). The form validates what the builder
validates (ranges, seat counts, item counts, address grammar) and shows the delay the decision will wait and, for
an Ethereum kind, the envelope it will produce (decoded back for a second look).

### 6.3 Permissionless

| Action | gno.land | Ethereum |
|---|---|---|
| execute a due decision | `ibcgov.Execute(id)` | `IBCAdmin.execute(seq)` |
| re-send a dispatch | `ibcgov.Redispatch(e, seq)` | — |
| drain buffered decisions | — | `IBCAdmin.drain()` |
| resend an epoch notice | — | `IBCAdmin.resendEpochNotice(epoch)` |
| apply a recorded notice | `ibcgov.ApplyEpochNotice(e, client, path)` | — |
| clear the recovery block | — | `IBCAdmin.clearRecoveryBlock()` |
| execute a recovery | — | `IBCAdmin.executeRecovery()` |
| pay claims / refunds | `transfer.Claim(client, denom, n ≤ 20)`, `transfer.ClaimRefunds(client, denom, n)` | `claim(clientId, token, maxCount ≤ 20)`, `claimParked(clientId, sequence, gasBudget)` |
| reconciliation steps | `transfer.PrepareReplacement(route, n ≤ 64)`, `transfer.ContinueReopen(route)`, `transfer.Recapitalize(route, id)` (with the coins) | `prepareIncident(clientId, n)`, `continueReopen(clientId, n ≤ 16)` |
| housekeeping | `ibcgov.PruneFlags(n)`, `ibcgov.PruneGovRetirements(n)` | `retryNotification`, `notifySettled` |
| keep a recovery candidate fresh | — | `GnoLightClient.updateClient(bytes)` on the candidate (the console only links to the relayer's command; building the header is the relayer's job) |

`proveOutcome`/`ProveOutcome` need proofs a relayer builds; the console shows what a record still needs proven and
links the runbook step, it does not build proofs (phase 4 may add it on the Ethereum side, where `eth_getProof`
is enough).

### 6.4 What the console checks before signing

The chain's own refusal conditions, where readable, so that a refused transaction is caught before gas is spent:
`Execute` only for a `pending` decision past `due` and not deferred; `execute` only for state 2 past maturity, not
tombstoned, not suspended by an open recovery, `gnoBusy()` false; `drain()` only when `lastSeq+1` is buffered;
`resendEpochNotice` only for a recorded epoch; `claim` only with a non-empty queue and no `refunds` hold. These
are conveniences; the transaction preview always shows the raw call.

## 7. Monitor algorithms (T4)

The monitor implements ADR §7.11 rule by rule. Each check is a `Check` with a name, a schedule, inputs read from the
store, and a result that lists every input's provenance. This section maps each rule to its check. Every value an
integrity check acts on is state-authenticated (§3.3); header-agreed values alone only alarm.

### 7.0 Conventions: heights, roots and packet scope

**Heights.** Four different numbers are never mixed:

- a gno.land **state height** `q`: the state after block `q` (what `abci_query` at `height = q` answers);
- a gno.land **header height** `h`: header `h` carries `AppHash`, the root of the state after block `h − 1`. The
  proof height a receive on Ethereum references is a header height: `GnoLightClient.getConsensusState(h).appHash`
  is header `h`'s AppHash, i.e. the root of state `q = h − 1`. The relayer queries state at `q` and submits proof
  height `q + 1`. A root at proof height `h` is therefore compared with **header `h`** of the own node, never with
  header `h + 1`;
- a **beacon slot** `s`: the gno Ethereum client's heights are `Height{0, slot}`, and its consensus state at `s`
  holds the **execution** `state_root` of the execution payload of the beacon block at `s`;
- an **execution block number** `n`: what `eth_call`, `eth_getProof` and `eth_getLogs` are pinned to.

The slot → execution block mapping is authenticated from the beacon node (the beacon block at `s`, its
`execution_payload.block_number` and `block_hash`), cross-checked against the execution node's block of that
number by hash. A mapping that cannot be authenticated leaves the comparison `unresolved`.

**Packet scope.** Every send and receive is classified by **port and payload** before any transfer rule applies:
port `transfer` with a decodable ICS20 payload is a transfer packet; port `gov` → `ibcgov` (or `ibcgov` → `gov`
for a notice) with the governance envelope is a governance packet; anything else is foreign. The router's
`SendPacket`/`WriteAcknowledgement` events, the gno `send_packet`/`recv_packet` events and the commitments pages
carry the packets of **every app** on a client, so a governance packet has, rightly, no transfer send record. The
transfer tests (§7.2–§7.5) run on transfer packets only; governance packets are reconciled by §7.7; a foreign
packet on the route's client is reported (informational), never as an orphan.

### 7.1 Terms

Per route and denom (gno-native: a gno denom with an escrow on gno.land and a voucher on Ethereum; Ethereum-native:
an ERC20 escrowed on Ethereum with a voucher on gno.land), all amounts as `*big.Int`:

| Term | Source | Note |
|---|---|---|
| `accounted_escrow` | **reconstructed from packet records**, never read as a total: every authenticated transfer send's amount (gno-native: the route's send records on gno.land; Ethereum-native: `sendRecord`s on Ethereum), **plus every paid recapitalization** (gno-native: `route_recapitalize{route, id, denom, amount, payer}` matched to an executed `NewRecapitalizeRequest` authorization by id and amount; Ethereum has none), **minus** every release: a receive paid out of escrow (Ethereum-native, on receive; gno-native, each claim installment and sweep payment), a refund paid (`route_refund`, `GnoRefundPaid`) | ADR §7.9: a paid recapitalization is "credited to accounted backing and debited from the unrecovered losses"; §7.11 first bullet. An **unpaid** authorization changes nothing, and a plain transfer into an escrow is surplus (§7.11), never backing |
| `voucher_supply` | `GnoVoucher.totalSupply()` at `E` (gno-native, exact); the gno voucher's supply at `G` (Ethereum-native: **float64 on chain**, usable below 2^53 only, else the term is `unavailable`, §4.1) | |
| `in_flight` | transfer sends whose receive is not at or below the other pin, or received with an error ack or past timeout whose ack/timeout the source has not processed at its pin | from the send records' outcome fields on both chains, reconstructed per packet (the `in_flight` total is a saturating view) |
| `unpaid_refunds` | refund outcomes not yet paid: queued, parked, held by a `refunds` hold, re-mints pending | per record (`payment` queued/parked, `refunded < amount`); ADR: "a refund leaves this term only when paid" |
| `claims` | gno.land claims open, owed or withheld, plus sweep entries owed | per entry from the claims and sweeps pages (`amount − paid − reduced` for open/withheld claims; unpaid sweep entries); the `Owed + Withheld + Swept` totals are exact and used as a cross-check |
| `unrecovered_losses` | the loss ledger's open balance on the **payout chain** only, per (packet, token), never a mirror: gno.land, each loss of the losses page with `chain: gno`, less the paid recapitalizations and reductions booked against its incident (an accepted deficit lowers `unresolved`, not `unrecovered`); Ethereum, **`losses(route, token)` itself** (§4.2: no remedy exists there, and acceptance does not reduce it) | ADR §7.11, §7.12 "Cross-chain loss ownership" |
| `balance` | the **physical** balance: gno.land `bank/balances/<realm>` or GRC20 `BalanceOf(realm)` (exact); Ethereum `ERC20.balanceOf(escrow)` | the "balances must cover it" rule |

Identity (gno-native):
`accounted_escrow_gno == voucher_supply_eth + in_flight + unpaid_refunds + claims_gno − unrecovered_losses`.
Identity (Ethereum-native):
`accounted_escrow_eth == voucher_supply_gno + in_flight + unpaid_refunds + mint_claims_gno − unrecovered_losses`.

**Cover, per physical account.** gno.land keeps **one** escrow account (the transfer realm's address) for every
client of the port, so a per-route comparison can hide a shortfall (a balance of 100 "covers" either of two
obligations of 100). The cover is therefore evaluated per (account, denom):
`balance ≥ Σ accounted_escrow over the Ethereum routes + Σ escrow accounting of every other client` (the latter
from `total_escrow/<denom>` per client, float64-limited: an amount at or above 2^53 there makes the cover
`unavailable` until §15 P1). On Ethereum each route has its own escrow contract, so the cover is per route and
token. `balance − obligations` is `surplus` (alarm only); a negative value is a **shortfall**, an integrity break.

An identity or cover whose terms include an `unavailable` value (a saturated total, a float64 supply beyond 2^53,
an unauthenticated record) is **not evaluated**: the check skips with the reason, which counts toward the deferred
alarm (§8.3). A term is never guessed.

### 7.2 Closed pairs (`closed_pair`)

Rule: "Consistent cut" (§7.11). The check selects `(G, E)`, over transfer packets only (§7.0):

1. `G₀` = the latest committed gno.land height whose block hash the primary and the second gno RPC agree on.
   `E₀` = the execution block number the beacon node reports as finalized, whose hash the primary execution node,
   the second provider and (when present) Helios agree on.
2. Load every transfer receive on Ethereum at or below `E` whose gno send height (from the packet's send record on
   gno.land, looked up by `(sourceClient, sequence)`) is above `G`: lower `E` to one below the lowest such receive
   block. Load every transfer receive on gno.land at or below `G` whose Ethereum send block is above `E`: lower `G`
   likewise.
3. Repeat until neither moves. Both only decrease; the loop ends at worst at the previous evaluation's pair, or the
   route's launch heights.
4. If a send record a receive names is **absent** at the other pin, the receive is **not yet visible** (rewind
   past it, step 2) unless the orphan test (§7.4) is conclusive at that pin.

The pair, every rewind and its cause are part of the result (the dashboard shows "E lowered from 123 to 120:
receive seq 41 of client-0 has its send at gno height 9001 > G").

If no pair can be established — a record missing, a provider disagreeing, the pinned height not final, or the pair
falling behind the heads by more than `max_pair_lag` — the evaluation is **skipped** and retried; a
`closed_pair_unavailable` alarm is raised only after `deferred_alarm_failures` consecutive skips (default 6, one
minute at the default cadence).

### 7.3 Supply identity (`supply_identity`)

Rule: the identity and the cover rule, evaluated **only at a closed pair** from `closed_pair`. Every term is read
at exactly `G` or `E`. The result holds every term, the identity's residual (`lhs − rhs`), the cover's residual
(`balance − obligations`), and the list of packets in each term (bounded; the full list is in the store).

- residual `≠ 0` → **identity break**; cover residual `< 0` → **shortfall**: both are integrity breaks.
- cover residual `> 0` → `surplus`, alarm only, never a flag.
- An integrity break persists over **two consecutive evaluations at closed pairs** → the `integrity_break` alarm is
  eligible for auto-flag (§8.4). One evaluation alone raises a `warning` alarm.

### 7.4 Confirmed orphans (`orphan_receives`)

Rule: "Confirmed orphans are breaks, not skew" (§7.11) with the evidence tests of §7.12. A **transfer** receive on
chain D naming source client `c` and sequence `s` is checked against the source's **permanently retained send
record** at an **independently authenticated finalized** source state `S` (its pin, or a later one). Every
evidence value below is state-authenticated: on Ethereum by a verified `eth_getProof` (account proof against the
header's `stateRoot`, storage proof against the account's storage root); on gno.land, until store proofs land
(phase 4), by reading it from both providers and requiring equality.

| Test | Conclusive when | Evidence read |
|---|---|---|
| **mismatch** | the send record at `(c, s)` exists and holds a different packet (Ethereum: `sendRecord(c, s).commitment ≠ CommitPacket(P)`; gno.land: the record's `commitment ≠ CommitPacket(P)`, the record's own commitment having been checked against its packet bytes) | `proof` of the record's slots at `S`; gno: the sends page at `S`, agreed |
| **absence by sequence** | no record and the router's `prevSequenceSends[c] ≥ s` at `S` (Ethereum source) | `proof` of the `IBCStore` mapping slot (no getter) at `S`; gno source: the core's next-sequence view at `S`, agreed |
| **absence under a retired client** | no record and `isRetired(c)` on the Ethereum transfer app at `S` | `proof` of `retired[c]` (namespace `gno.storage.GnoICS20Transfer`, base + 10) at `S` |
| **absence by timeout** | no record and `S.timestamp ≥ P.timeoutTimestamp` | the header at `S` (header-agreed) |

The packet `P` is taken from the destination's receive (the gno.land `recv_packet` event's `encoded_packet_hex`,
or the Ethereum `WriteAcknowledgement` log) and authenticated against the destination's receipt
(`CommitPacketReceipt(P)` on gno.land against `PacketReceipt(c, s)` agreed; `keccak256(abi.encode(P))` on Ethereum
against the router's receipt slot by proof) before any test. A receive that fails every test stays **unresolved**
(it is rewound past in §7.2, and shown as such); one that passes any test is a **confirmed orphan**, an integrity
break flagged at once — not rewound past, not deferred — with the containment response (§7.10).

### 7.5 Unobserved versus fabricated proof heights (`proof_heights`)

Rule: §7.11, fourth bullet. For every transfer receive, the referenced source proof height is compared with the
monitor's **observed, authenticated source head**:

- above it → `unresolved`: the monitor refreshes its source view and re-evaluates; it never alarms for this alone
  (an authenticated head proves a block exists, not that no later one does);
- at or below it → the root the destination's client holds for that height is compared with the source's
  canonical root, by the conventions of §7.0:
  - a receive **on Ethereum** at gno proof height `h`: `GnoLightClient.getConsensusState(h).appHash` (proven)
    against the own gno node's **header `h`** `app_hash` (header-agreed with the second provider at `h`);
  - a receive **on gno.land** at slot `s`: the gno Ethereum client's consensus state at `s` (`state_root`, agreed)
    against the `stateRoot` of the execution block the authenticated slot mapping names (header-agreed).

  A **different root** is **conclusive** (ADR §7.11: "a different canonical state root at that height in the
  source's independently authenticated history"): flagged at once with the containment response (§7.10), as is
  a positive send-record forgery test (§7.4). A mapping or header that cannot be authenticated leaves the receive
  `unresolved`.

### 7.6 Header record (`header_record`)

ADR's T4 row names the header record beside the supply identity: a poisoned client can be fed headers the honest
chain never had and stay balanced for as long as nothing is received through it, so the imported history is
reconciled **continuously**, not only when a receive triggers it.

- **Ethereum's clients of gno.land** (the route's `GnoLightClient`, every enrolled kind 6 target, an open
  recovery's candidate): every `ClientUpdated{height, blockHash, appHash}` since the last evaluation, and the
  consensus state it installed (`getConsensusState(height)`, proven), is compared with the own gno node's header
  at `height` (block hash and `app_hash`, header-agreed with the second provider). Every `LiveTransition{height,
  validatorsHash, nextValidatorsHash}` is compared with the own node's `validators_hash`/`next_validators_hash` at
  that height. A height at or below the authenticated gno head with a **different** hash or root, or a transition
  the honest chain never had, is a **poisoned client**: conclusive, contained at once (§7.10). A height above the
  observed head is `unresolved` and re-checked as the head advances; an update that stays unresolved longer than
  `unresolved_after` (default 1 h, longer than any honest relay lag) alarms `critical`.
- **gno.land's clients of Ethereum** (every Ethereum client of the core, the route's and the expectation graph's):
  every new consensus state (`consensus_states` pages, agreed) at slot `s` with `state_root` is compared with the
  execution block the authenticated slot mapping names (§7.0). A different root at an authenticated slot is a
  poisoned gno-side verifier: conclusive, contained at once (§7.10) — and, since such a verifier can also
  authenticate forged epoch notices (ADR §8.5 R2), the governance dashboard shows the finding on the epoch.
- A trust-generation change (`ClientTrustGeneration`, `GnoRouteMigrated`, kind 6 executions) resets the record for
  that client; the dashboard shows the last authenticated height per client.

### 7.7 Foreign decisions (`foreign_decisions`)

The governance channel is proven by the same clients. The T4 row's "foreign decisions" are reconciled against the
source's own dispatch ledger, bytes for bytes:

- **Ethereum against gno.land.** For the current epoch `e` and every `seq ≤ lastSeq` (and every buffered `seq`
  in the window): `IBCAdmin.decision(e, seq).hash` (proven) must equal `keccak256(DispatchBody(e, seq))` read
  from the own gno node (agreed), and every `Conflict{e, seq, stored, received}` event's `received` hash must be
  the hash of a gno dispatch of that number. A decision recorded or buffered at a number gno.land never dispatched,
  or with other bytes, is a **foreign decision** (ADR §5.4: "forged governance packets … can occupy sequence
  numbers with the attacker's bytes"): conclusive, contained at once (§7.10), **and** the response includes a
  watch-key flag on `IBCAdmin` itself (its own `setFlag()`), which defers every 7-day kind until guardians act.
  A conflict whose `received` bytes are gno.land's and whose `stored` bytes are not is the same finding.
- **gno.land against Ethereum.** A dispatch gno.land shows as answered `recorded`, `buffered` or `duplicate`
  (statuses 1–3) must exist on `IBCAdmin` at `(e, seq)` at a finalized block at or past the block the ack was
  proven against; one that does not is a **forged success acknowledgement** (ADR §8.5 R8), conclusive: the
  gno-side verifier of Ethereum proved a state Ethereum never had, so it is contained at once (§7.10) and the
  dashboard shows `Redispatch(e, seq)` as the ADR's remedy. A dispatch with no answer yet is a stall (§7.8).
- **Epochs and notices.** gno.land's `Epoch()`/`EpochClient()` must correspond to an `EpochOpened{epoch,
  clientId}` on `IBCAdmin` (proven), and every notice gno.land recorded as proven or applied (`GovNotice`) to an
  `epochNotice(epoch)` with those bytes. A gno epoch Ethereum never opened is a **forged notice** (ADR §8.5 R2):
  conclusive, contained at once; the ADR's only repair is Ethereum's emergency recovery, which the dashboard says.
- Everything else here only alarms: a halted or buffered decision behind `lastSeq` (`drain()` simulated by
  `eth_call` leaves it buffered), a conflict flag, a notice refused (`GovNotice{refused}`), a dispatch answered
  with an error ack or `redispatch`.

### 7.8 Liveness alarms (`liveness_*`)

Alarm only; never a flag. Thresholds from ADR §9 unless configured otherwise.

| Check | Condition | Default |
|---|---|---|
| `client_expiry` | for every light client on either chain: `expiry = latest trusted time + trusting period`; alarm when `expiry − now < warn_before` (and `critical` when `< critical_before`); also `Expired` or `Frozen` status; also "no update for" longer than the cadence alarm (3 d for `GnoLightClient`, 24 h for the gno Ethereum client) | warn 3 d / 24 h; critical 12 h |
| `recovery_candidate` | an open emergency recovery proposal's candidate verifier: same expiry rule, plus the recovery's own timeline (opened at, executable at, cancel votes) | daily updates expected (§7.12 step 4) |
| `relay_stall` | sends with no receive at the destination beyond `stall_after`, acks/timeouts not processed beyond it, governance dispatches without an answer, epoch notices unacknowledged | 6 h (packets), 12 h (governance) |
| `claims_age` | the oldest open claim's age, the oldest queued refund's age, the oldest parked refund | 24 h |
| `float_runway` | per denom: the meter's refill rate (the cap, exact) against the open claims (gno) and queued refunds (both): hours until the queue drains at the cap's rate; alarm when a queue grows faster than the cap refills for `runway_window`. The live level is used only when an exact source gives it (§4.1) | 24 h |
| `quarantine` | a route quarantine starts or ends; a validator-set transition observed | informational |
| `maturity` | a decision becomes executable (either chain), a seat-change landing time pinned (`ReplacementAnchored`), a recovery becomes executable, a hold reaches day 14 or 21 | informational |
| `escrow_surplus` | from §7.3 | informational |

### 7.9 Governance watch (`gov_ledger`)

Alarm only. Pending decisions on both chains with their maturity, deferral cause (hold, flag, conflict,
suspension by an open recovery), halted or buffered entries, conflicts (`IBCAdmin`'s conflict flag and the
`Conflict` event), unacknowledged epoch notices, decisions on gno.land answered with an error ack or needing a
`Redispatch`, and the worker's triggers (§9.1): new or changed kind 5/6/7/9 candidates and recovery proposals,
every **executed** decision, and a changed implementation, beacon or executor pin.

### 7.10 What triggers what

From ADR §7.11, the **only** findings eligible for the auto-flag sink (§8.4), all of severity `break`:

1. a **confirmed orphan** transfer receive (§7.4);
2. an **integrity break** — identity residual or cover shortfall — **persisting over two consecutive closed-pair
   evaluations** (§7.3);
3. a **conclusive root contradiction**: a receive proven against a root that differs from the source's
   authenticated canonical root (§7.5), or a poisoned client in the header record (§7.6) — the ADR flags these
   "at once, with the same containment";
4. a **foreign decision**, a forged success acknowledgement or a forged epoch notice (§7.7), which prove the same
   kind of forgery through the governance channel.

The response is the ADR's containment: on gno.land `credits` and `payouts` on the route; on Ethereum the route's
pause flags (receives and escrowed sends); the route's `refunds` flag on **both** chains; and, for (4) when the
forgery is on the Ethereum side, `IBCAdmin`'s own flag. Everything else — stalls, ages, runways, failure to find a
closed pair, surplus, provider disagreement, unresolved heights, a single-evaluation break — alarms only.

### 7.11 Readiness and freshness

Every snapshot carries the collection it came from. A check evaluates only subjects whose snapshots are
**fresh** (from the latest collection, whose chain collected without error and whose pin is header-agreed);
subjects whose chain failed to collect keep their last snapshot, marked `stale` in the API, and are **skipped**
with the reason rather than evaluated on old data. A check with nothing fresh to evaluate is `skipped`; a check
that errors is counted like a skip (§8.3). The pins snapshot records, per chain, the last successful collection,
the last error (redacted, §10.2) and the number of consecutive failures; the dashboard shows all three.

## 8. Alerting

### 8.1 Alarm model

```
Alarm {
  key        string   // dedup key, e.g. "client_expiry/gno/ethereum-1"
  check      string
  severity   info | warning | critical | break
  state      raised | acknowledged | cleared
  first_seen, last_seen, cleared_at
  title, detail
  evidence   []Provenance + the result excerpt that triggered it
  autoflag   bool     // eligible for the auto-flag sink (only `break` from §7.10)
}
```

An alarm is raised once per key, updated while its condition holds, cleared when a later evaluation no longer
meets it (with hysteresis for thresholds). Every transition is stored and sent to the sinks as an
`AlarmEvent{type: raised | updated | cleared, at, alarm}`. The `autoflag` eligibility travels on the **event**,
not only on the alarm: it is set only on `raised` and `updated` events of a `break` alarm, and is always false on
`cleared` (an incident ending never triggers anything).

### 8.2 Sinks

| Sink | Phase | Note |
|---|---|---|
| `log` | 1 | structured JSON on stdout |
| `webhook` | 1 | POST of the event JSON to a URL with an optional HMAC signature header (`X-Ibcmon-Signature: sha256=<hmac of the body>`); retries with backoff; Slack/Discord/PagerDuty adapters are thin templates over it. A receiver verifies the signature, treats the body's `at` and the alarm key plus `transitions` counter as its replay protection (a resend carries the same values), and never acts on an event older than its own freshness bound |
| `autoflag` | 3 | §8.4 |
| `email`, `pagerduty` native | later | if asked |

**Delivery is asynchronous and bounded.** Each sink has its own queue (default 256 events) and worker; the
evaluation loop enqueues and never waits on a sink, so a slow webhook cannot delay the next evaluation or another
sink. A full queue drops the oldest event for that sink, counts the drop and raises `sink_backlog` (the alarm
state itself is in the store, nothing is lost but the notification). The `autoflag` sink is **not** behind the
notification queues: it has its own queue, is fed first, and its backlog is an alarm of its own. Webhook retries
are in-memory in phase 1 and persisted in the store from phase 2, so a restart does not lose them.

### 8.3 Deferred alarms

A check that cannot evaluate (data unavailable or stale, provider disagreement, an unauthenticated value, pair
not final, pair too far behind) returns `skipped` with a reason; a check that errors counts the same way. The
scheduler counts consecutive non-evaluations per check and raises `<check>_unavailable` only after
`deferred_alarm_failures`. The counter resets on the next successful evaluation. A single transient failure never
pages anyone; a chain that stays unreadable does, through this path, since §7.11 turns stale data into skips.

### 8.4 Auto-flagging (watch keys)

- Off by default. `autoflag.enabled: true` in the configuration file **and** the watch-key secrets in the
  environment (`IBCMON_GNO_WATCH_MNEMONIC`, `IBCMON_ETH_WATCH_PRIVATE_KEY`) are both required; either missing
  refuses to start with `autoflag.enabled`. The public-instance mode refuses the sink.
- **Rollout (§12):** the first production milestone is alarm-only with the sink in dry run; live signing is gated
  on the conditions of §12, not on a review of "a period".
- **What the sink acts on**, every condition checked at the sink boundary, never assumed from the caller:
  1. the event is a `raised` or `updated` transition — **never** `cleared`;
  2. the alarm's check is one of §7.10's four and its severity is `break`;
  3. the evidence is **fresh and complete**: every provenance of the finding is from the latest collection, header-
     agreed and state-authenticated, and names the deployment this instance is configured for (chain ids, router
     and `IBCAdmin` addresses, the route key) — an event that does not carry that is refused and logged;
  4. the loaded signer **currently holds watch seat 4 or 5** on the chain it would flag, read from the chain at
     the pinned block (`ibcgov.Seat(4|5)`, `guardianKeys()[4|5]` / `IBCAdmin.keys()[4|5]`); a signer that lost its
     seat signs nothing;
  5. the flag is not already standing for that seat (the chain's view), so a repeated event does not spend gas.
- **What it does:** signs exactly the ADR's containment flags (§7.10) with the watch keys and records every
  transaction hash in the store **before** broadcasting, keyed by (alarm key, chain, flag), so a restart or a
  re-delivery never signs the same flag twice (durable deduplication). It confirms each transaction (gno.land:
  the tx in a committed block; Ethereum: the receipt at a finalized block) and retries an unconfirmed or failed
  one with backoff; a containment that stays partial (one chain flagged, the other not) is an alarm of its own
  (`containment_partial`, `critical`) until every flag stands.
- It never clears a flag, never sets a `sends` flag, never votes, never signs anything but `SetFlag`,
  `setRouteFlag`, `setRefundsFlag` and `IBCAdmin.setFlag`.
- The watch keys are loaded into the monitor process only; the API never exposes them, their addresses are shown
  as the seats they hold (read from the chain).
- Dry-run mode (`autoflag.dry_run: true`) performs every check above and logs the transactions it would sign.

## 9. Watch/check worker (T5)

### 9.1 Jobs

| Job | Input | What it does | Output |
|---|---|---|---|
| `codehash` | a pending kind 5, 6, 7 or 9 decision, or a recovery proposal | reads `extcodehash`/`eth_getCode` of the candidate address at the finalized block, compares with the decision's pinned `codeHash`, with the manifest entry if the component is known, and checks the address is code (not an EIP-7702 designator); for a kind 7 transfer candidate reads its `gnoControlsVersion()`, `gnoSelf()` and module hashes | pass/fail per check, with provenance |
| `buildverify` | a candidate (`CANDIDATES_ONLY=true`) or the live system per release (`COMPONENTS=…`) | runs `scripts/verify-codehashes.sh` from a clean checkout of the named gno-eth-ibc revision inside the Foundry image, with the RPC and addresses from the configuration and the manifest; captures the output | pass/fail, log, the rebuilt hashes |
| `layoutdiff` | a kind 7 implementation or kind 9 executor candidate with its source revision, and the live component's revision from the manifest | the storage of these contracts is mostly **namespaced** (ERC-7201 `GnoTransferStorage.Layout` at `gno.storage.GnoICS20Transfer`, `IBCAdminStorage.Layout` at `gno.storage.IBCAdmin`, eureka's `ibc.storage.ICS20Transfer` and `ibc.storage.IBCStore`), reached through fixed base slots that `forge inspect <Contract> storage-layout` does not cover. The job therefore compares, at both revisions: the ordinary contract layout **and** every namespaced `Layout` struct (its base slot constant, each field's slot, offset, packing and nested types, recursively). A changed or removed field, a changed type or offset at an existing position, or a changed base slot fails; appended fields pass. A namespace the job cannot locate at either revision gives an **incomplete** verdict, never a pass | the diff and the verdict (an aid to the human append-only review, which stays required, not a replacement) |
| `manifest` | the deployment manifest | checks that every live component's code hash equals the manifest's and that nothing runs on chain the manifest does not list (the proxies' implementations, the beacons, `IBCAdmin`'s executor and its delegate graph) | pass/fail per component |

**Triggers** (ADR §7.6: "after every deployment and executed decision and for every pending candidate within its
own delay"): the worker runs `codehash` and `manifest` (a) at start-up and after every change of a live pin
(implementation, beacon, executor, verifier: `Upgraded`, `ExecReplaced`, `ICS02ClientMigrated`,
`GnoRouteMigrated`), (b) after **every executed decision** and recovery, and (c) whenever the governance watch
(§7.9) finds a new or changed pending kind 5/6/7/9 candidate or recovery proposal; `buildverify` and
`layoutdiff` once per candidate within its delay (and again on request), and `buildverify` per release after (a)
and (b).

**Review gates** that a job reports next to its verdict, each one `blocked` until a person records it (the worker
never passes them by itself):

- **envelope**: a pending candidate's `IBCAdmin.decision(e, seq).hash` equals the hash of gno.land's
  `DispatchBody(e, seq)` (§7.7), so what is verified is what governance sent;
- **`gnoBusy()`** (kind 7 transfer candidates): the repair-critical review that the candidate's `gnoBusy()` is
  functional and answers false outside an operation (ADR §8.5 R14) — the job runs the static call on the candidate
  and records the answer, the review is human;
- **unsupported verifier**: a kind 6 or recovery candidate that is not a `GnoLightClient` build needs the manual
  reproducible-build check (ADR §7.6);
- **legacy release**: a component of a release predating `COMPONENTS` (af6259d and earlier) is verified by hand;
- **layout**: the append-only storage review, with the `layoutdiff` output as its input.

Each run is a job in the store with status, timestamps, log and verdict; the console shows them next to the
decision with the gates' state.

### 9.2 Deployment manifest

A YAML file kept by the operators beside the deployment (ADR §7.6, "mixed releases"), one entry per component:

```yaml
components:
  - name: transfer           # as VerifyCodehashes' COMPONENTS names it
    address: "0x…"
    source_revision: "72a41de"
    compiler: { solc: "0.8.28", optimizer_runs: 200, evm_version: "cancun" }
    constructor_bindings: { … }
    code_hash: "0x…"
```

The worker verifies per release: it groups components by `source_revision` and runs `buildverify` from a clean
checkout of each. After an executed kind 7 or 9 the operators update one entry; the worker flags a live component
whose hash the manifest does not name.

### 9.3 Docker

The job runner mounts a checkout directory (cloned by the worker at the named revision, submodules included) and
runs `ghcr.io/foundry-rs/foundry:<pinned>` with `--network` limited to the configured RPC host. The script is run
as the image's user; nothing from the job is executed on the host. Jobs have a timeout and a concurrency limit of
one.

## 10. Security

### 10.1 Keys and secrets

- No user key ever reaches the server or the console: Adena and the EIP-1193 wallet sign in the browser.
- The watch keys (§8.4) are the only keys the software holds; they are loaded from the environment into the monitor
  process, never written to the store, never logged, never served.
- RPC URLs with tokens and webhook URLs come from the environment; `.env` is git-ignored and `.env.example` lists
  every variable with a placeholder.
- The store contains chain data and monitor results only.

### 10.2 Hosting

- The API is read-only (no POST changes state; the worker's "run again" is a POST that enqueues a job and is
  disabled on a public instance). Put the guardian instance behind the organisation's access control.
- **Redaction happens centrally, before storage.** Every error from an RPC client, a sink or a job passes through
  `redact.Error` at the boundary (the clients wrap their own errors): URLs are reduced to scheme and host, user
  info, query strings and path segments that look like tokens are removed, and known secret values (the configured
  RPC URLs, webhook URLs and secrets, watch keys) are replaced wherever they occur. Nothing unredacted is stored,
  logged or served, so an outage cannot leak a credential-bearing URL through a dashboard, a webhook or a log.
- **Public mode filters responses centrally.** With `instance.public`, one filter applied to every response drops
  the operator-only fields: node labels become `primary`/`second`, collection error details become
  `unavailable`, finding values and check reasons are kept only when they carry chain data, worker job logs are
  dropped, and the alarm events history is served without detail. The filter is a single function with a test
  that walks every endpoint.
- CORS: the API allows only the console's own origin by default.

### 10.3 Render output is untrusted

Realm `Render` output is Markdown written by a realm whose inputs include user-supplied strings (packet memos,
receivers, client ids, incident names). The console **never renders Markdown to HTML**. JSON paths are parsed
into typed models; every string from a chain is shown as text (React escapes by default; `dangerouslySetInnerHTML`
is banned by lint). Markdown paths, where shown at all, are shown verbatim in a `<pre>`. Links are built by the
console from typed fields, never taken from the chain. The same holds for Ethereum strings (client ids, denoms,
revert reasons).

### 10.4 A dashboard never decides for the user

The console shows state, the ADR's rule that applies and the evidence, with provenance. It drafts a transaction
only when the user opens the action and chooses the parameters; it never pre-fills a destructive choice (a cancel,
a forged void, a settlement action) from a monitor result. Monitor results that *could* justify an action
("integrity break persisting 2 evaluations", "code hash mismatch on candidate 12") are labelled as findings, next
to the rule, and leave the decision and the signature to the person. The auto-flag sink is the one automatic
actor, and it is off by default and limited to what the ADR assigns to the watch keys.

### 10.5 Supply-chain

- Go dependencies are pinned in `go.sum`; the frontend lockfile is committed; CI runs with `--frozen-lockfile`.
- The Foundry image is pinned by tag and digest; the gno-eth-ibc revision for ABIs and build verification is a
  configuration value, recorded with every job.
- No telemetry.

## 11. Tech stack

| Choice | Why |
|---|---|
| **Go 1.25** for the backend | the relayer, gno and tm2 are Go; go-ethereum gives `eth_call`, `eth_getProof`, ABI, RLP and keccak; one static binary ships the daemon, the worker and the console |
| **No dependency on the gno module** | the monitor reads through JSON-RPC (`abci_query`) with a 300-line client; importing `gnolang/gno` would pin this repository to a fork and pull in the VM. The relayer's gno client (gno-eth-ibc `relayer/gno`) is not imported either: its module path does not resolve and it depends on the gno module |
| **go-ethereum v1.16** | `ethclient`, `gethclient.GetProof`, `accounts/abi`; ABIs embedded as JSON from the Foundry build of a named gno-eth-ibc revision (`abi/`), refreshed by `make abis` |
| **SQLite via `modernc.org/sqlite`** | history and job logs need a file, not a server; pure Go, no cgo, one volume |
| **stdlib `net/http` and `log/slog`** | enough; no framework to keep up with |
| **Vite + React 18 + TypeScript** for the console | the simplest widely-known stack; static build embedded in the binary; React escapes output by default (§10.3) |
| **TanStack Query** | polling and cache of the API with no state framework |
| **viem** | Ethereum wallet (EIP-1193), ABI encoding, `bigint` amounts natively |
| **@adena-wallet/sdk** | Adena connection and `MsgCall` signing for gno.land |
| **No CSS framework** | a small hand-written stylesheet; dark/light by `prefers-color-scheme` |
| **Vitest + Testing Library** | component and API-client tests against recorded fixtures |
| **golangci-lint, eslint, tsc** | CI gates |
| **GitHub Actions** | build, lint, test on every push and pull request |

Alternatives considered: SvelteKit (fewer people know it in this team), Next.js (a server runtime for nothing),
a Go-templated server-rendered console (wallet integration needs a JavaScript app anyway), Postgres (a second
service for a single-operator tool), Prometheus-only alerting (the evidence and provenance do not fit a metric;
a `/metrics` endpoint is phase 2 for the operators who want it).

## 12. Phases and milestones

| Phase | Deliverable | Status |
|---|---|---|
| **0 Design** | this document | done |
| **1 Scaffold and read-only data layer** | repo, CI, `gnorpc`, `ethrpc`, typed views of §4 with fixtures; dashboards: clients, routes, governance ledger; monitor skeleton with `client_expiry` end to end (store, API, log and webhook sinks); worker skeleton | **built**: Go module with CI and lint; `gnorpc` (height-pinned `qeval`/`qrender`, block results) and `gnoviews` (clients with derived expiry, routes with stored meters and the live level marked unavailable, denom traces, send-record pages with payloads and commitment checks, ibcgov ledger, seats, flags, hold, dispatches); `ethrpc` (finalized/depth/latest pinning, calls, code, logs, proofs with account proofs and local verification) and `ethviews` (IBCAdmin ledger and buffer, flags, holds, recovery; transfer route state, flags, limits with live levels, incidents, `clientRecon`, send records; GnoLightClient); fake RPC servers and fixtures for both; the in-memory store; the alert manager with asynchronous bounded sinks (log, webhook) and the deferred-alarm rule counting errors and stale data; the collector with fail-closed header agreement and per-chain readiness; central secret redaction and public-mode filtering; `client_expiry`; the read-only API; the worker's `codehash` job; the console with Overview, Clients, Routes and Governance pages. Not yet: SQLite, `ibcmon record`, the Ethereum route's verifier resolved from router logs (the configured address is read), the `Reader.Meters` path through `ibcgov.view()` (to verify on a devnet), gno store proofs |
| **2 Full read model** | every view of §4 incrementally (send-record pages, logs, receives by port), every dashboard of §5 including incidents, reconciliation and recovery; `/metrics`; `ibcmon record` fixtures; the governance watch (§7.9); liveness alarms (§7.8) complete; persisted webhook retries | next |
| **3 Integrity (T4)** | packet scope, closed pairs, supply identity with per-account cover, orphans, proof heights, header record, foreign decisions; alarm state machine; auto-flag sink **in dry run only** with every gate of §8.4; drill against `e2e-local.sh` with injected forgeries and an outage drill (a provider down, a provider lying) | |
| **4 Actions** | Adena and EIP-1193 wallets; guardian actions; permissionless actions; GovDAO drafts from the builders; light-client mode (Helios) documented and tested; tm2 light-client walk and gno store proofs for gno values | |
| **5 Worker (T5)** | `manifest`, `buildverify` in Docker, namespaced `layoutdiff`, the triggers and review gates of §9.1; worker dashboard | |
| **6 Hardening** | public-instance mode audit, access control notes, audit of the console's untrusted-text handling and of the redaction filter, soak on Sepolia with the testnet drills (ADR §13 milestone 5) | |

**Release gates.** The **first production milestone is alarm-only**, with the auto-flag sink in dry run, and
stays so until every one of these holds, each with a recorded drill:

1. inputs are authenticated and complete: header agreement fails closed, integrity values are proven or agreed,
   readiness and freshness are enforced (§3.3, §7.11), and the e2e forgery and outage drills pass (phase 3);
2. the accounting is the corrected one of §7.1 and reconciles on the testnet through a full incident drill
   (runbook §8: replacement, settlement, recapitalization, reopening) with no false break;
3. the sink safeguards of §8.2 and §8.4 are in place: asynchronous queues, the seat check, durable
   deduplication, confirmation and retry, dry-run output reviewed against the drills;
4. the secret-redaction filter and the console's untrusted-text handling have been audited (phase 6);
5. T5's `codehash`, `manifest` and `buildverify` run, or an explicit manual verification procedure is written and
   followed for every pending candidate until they do.

Only then is `autoflag.dry_run: false` allowed on a guardian instance, and the owner records the decision.

## 13. Testing

- **Unit tests with recorded fixtures.** `internal/gnoviews` and `internal/ethviews` are tested against recorded
  JSON-RPC exchanges (`testdata/`): a fake tm2 RPC and a fake Ethereum JSON-RPC server answer from the recordings.
  The ABI encoder of the tests is go-ethereum's, so an Ethereum fixture is the ABI-encoded return data plus the
  block it was read at. `ibcmon record` writes new fixtures from a live deployment.
- **Check tests.** Each check is tested as a pure function of store snapshots: given these clients, these records,
  this pair → this result and these alarms. The closed-pair selection and the orphan tests have table tests for
  every rule of §7.2 and §7.4, including the "not yet visible" and "unresolved" cases.
- **Local end to end.** `scripts/e2e-local.sh` of gno-eth-ibc brings up the 3-validator gno testnet
  (`testnet/up.sh`, chain id `gno-eth-dev`, RPC `127.0.0.1:26657`) and anvil in Docker with the contracts
  deployed (`contracts/deployments/local.json`). `make e2e` here points `ibcmon` at both, runs every check once,
  and asserts the identity holds and no alarm is raised after the script's transfers; a second target injects a
  forgery (a plain transfer into the escrow, a receive without a send through anvil's `setStorageAt`) and asserts
  the right alarm. anvil has no beacon chain: the Ethereum "finalized" pin falls back to `latest − N` in local
  mode, which is explicit in the configuration (`eth.finality: "beacon" | "tag" | "depth:N"`); the local target
  also sets `instance.require_cross_check: false` and the second provider to the same node, so header agreement
  is exercised but not independent — the script says so in its output.
- **Frontend.** Vitest for the API client (bigint parsing, provenance), the amount formatter and the untrusted-text
  components; a Playwright smoke test against the fixture-backed API is phase 2.
- **CI.** Build, vet, lint and test of both halves on every push; the Docker-based worker jobs are tested with a
  stub runner in CI and for real in the e2e target.

## 14. Open questions for the owner

1. **Licence.** The repository has no `LICENSE`, as the sibling repositories have none. The ported files in
   gno-eth-ibc carry `LicenseRef-GNO-Network-GPL`, whose copyleft the controls documentation says extends to works
   that use them; this repository does not copy those files, but a choice is needed before anything is published.
2. **Public hosting.** Whether to run a public read-only instance, who operates it, and under which domain. The
   design supports it (§3.4) with the non-authoritative banner; it is not required.
3. **Auto-flag rollout.** When to enable the auto-flag sink on a guardian instance, which instance holds the watch
   keys (one per seat, or every guardian's instance holding both?), and whether the dry-run period is a fixed time
   or a review.
4. **Stack choices.** Go + SQLite backend; Vite + React + TypeScript console; viem and the Adena SDK for wallets;
   Helios as the optional light-client endpoint. Any of these can change now at little cost.
5. **Deployment manifest ownership.** Where the operators keep the manifest (this repository's `deployments/`,
   or gno-eth-ibc's), and who updates it after an executed kind 7 or 9.
6. **Second providers.** Which second gno RPC and Ethereum provider each guardian uses; the design requires one
   of each.
7. **The ADR's visibility gaps** (§15): which of the **prerequisites** to build on chain, and in which order; the
   tool states what stays `unavailable` until each lands.

## 15. Suggested on-chain additions

Found while writing §4 and reviewing §7. They are in two groups: **prerequisites**, without which a stated part
of the monitor cannot be exact and is reported `unavailable` (never approximated), and **conveniences**, which
simplify or speed the tool up. The owner decides which to build.

### Prerequisites (correctness)

**P1. Exact amounts in the transfer realm's JSON (gno-realms).** `total_escrow/<denom>` (per client and total)
and `vouchers`/`voucher/ibc/<hash>` (`total_supply`, balances) emit `float64`-built numbers; emit them as
integers or decimal strings. Until then the escrow accounting and the gno voucher supply are identity terms only
below 2^53, and the cover check for a denom whose accounting reaches 2^53 is `unavailable`.

**P2. Exact or reconstructible liabilities (gno-realms).** `Totals.InFlight`, `Returning`, `QueuedRefunds`,
`Parked`, `ProvenOwed`, `OpenUnrecovered` and `OpenUnresolved` are saturating int64 views of wide counters. Either
emit the wide values (two limbs, or a decimal string), or keep every per-packet record complete enough to replay
them (today §7.1 replays them from the sends, claims, sweeps and losses pages, which works while nothing is pruned
and every entry is exact). Also emit the incident balances (`IncidentTotals`: recapitalized, reduced, accepted per
incident) so `unrecovered_losses` can be checked against the realm's own figure.

**P3. Live meters from the realm (gno-realms).** Export `Meters(route, denom, to)` (computing `now`, the payouts
epoch and the lock itself) on the transfer realm, or put `meter.live`, `refund_meter.live` and the recipient
meters in `RouteJSON`. `Meter.Refill` depends on the epoch reset, `Frac` and the stored rate and ceiling, which no
view exposes, so the live level cannot be reproduced off chain; until then it is shown as `unavailable` (§4.1),
and `float_runway` uses the cap, not the level.

**P4. Hold end times and seat generations (both chains).** Expose the dip (`belowSince` on gno.land, `dipTime`
on Ethereum) or the computed end on the hold views, and the seat generations (`seatGen`, `Keys.gen`), so the
dashboard does not reconstruct them from event history (which a pruned or incomplete log can lack).

**P5. Decision params after close and halts (gno-eth-ibc).** Keep a decision's `params` after it closes, or emit
them in `DecisionExecuted`/`DecisionSpent`, and emit an event when a drain **halts**: the foreign-decision
reconciliation (§7.7) and the worker's envelope gate (§9.1) need the bytes of every decision, and a halted
decision is indistinguishable from a buffered one without simulating `drain()`.

**P6. A receive event on the transfer app (gno-eth-ibc)** (`GnoReceived{clientId, sequence, token, amount}`) and
a `route_send` event on gno.land, so the packet indexes (§7.0) can be built from the apps' own events rather than
from the router's and core's events of every app plus a port classification.

### Conveniences

**gno.land (gno-realms)**

1. **Export the controls reader's other progress views on the transfer realm**: `ReopenBlockers(route)`,
   `SuspectUnsettled(route, client)`, `OpenSends(route, client)`, `SuspectFrom(route, client)`, `Recap(route, id)`.
   Today they live only on `controls.Reader`, reached through `ExtView("controls")` typed as `any`, which a `qeval`
   expression on the transfer pkgpath cannot type-assert; the runbook's steps depend on them. `ibcgov.view()`
   (unexported) may serve as a workaround.
2. **Paginate `Render("decisions")`** (today an unbounded JSON array) with a cursor, and add a per-kind,
   per-state filter; keep `Decision(id)`.
3. **Expose the seat-change deferral anchor and landing time** on `Decision(id)` (today only in the one-time
   `Decision ... deferred until` event).
4. **A flag listing across routes and route-less clients** (`AllFlags()`), since the ibcgov page lists flags under
   enrolled routes only and `Flags(clientID)` needs the id.
5. **Listings for the governance graph**: expected transitions (pending, active, tombstoned) with their owners and
   generations, proven-but-unapplied notices with the path they would apply along, and `awaitingGov` members;
   today only `Tombstones()` and `AwaitingGovRetirement()` counts exist. Also a `Dispatches(e)` listing and the
   `retransmit` authorization flag on `dispatch/<e>/<seq>`.
6. **Exact numbers in the core's JSON**: heights, sequences and timestamps are emitted as `float64`-built JSON
   numbers; emit them as integers (or decimal strings), and fix the `counterparty_merke_prefix` key (keeping the old
   one for a release).
7. **`RouteJSON` omissions**: `reopening`, `replaced_at`, prepared / preparation cursor, `pending_sends`, the
   recapitalization list; `SendJSON` omits `proven_reserve`.
8. **Trusting period and expiry getters** on the core (`ClientExpiry(id) (uint64, bool)`), computed by the
   verifier, instead of each tool re-deriving them per client type.
9. **A store-proof query for realm values** (`abci_query` with `prove: true` on the controls' pages is not
   possible today, since they are computed by `Render`): a per-record query path under the `.store/main/key` tree,
   or exporting the records' storage keys, would let the gno-side evidence of §7.4 be proven instead of agreed.

**Ethereum (gno-eth-ibc)**

10. **A `setCustomERC20` event** (voucher registrations are invisible except through kind 5 decisions).
11. **Getters for `prevSequenceSends`** on the router (eureka upstream) — today an `eth_getProof` of a derived slot
    — and for `retired[]`'s companion fields on the transfer app: `verifierGen`, `unprepared`, `quarantineArmed`,
    `quarantineDeadline`, the preparation cursor (`prepCursor`, `prepared`), `lossTokens`, the refund queue entries
    (only head and tail are exposed).
12. **`IBCAdmin` ledger completeness**: store or expose `matureAt`, `flagAnchor(epoch, seq)`, the recovery
    proposal's contents (`recovery.params[hash]`), `enrolled` client ids and `appliedSeq`.
13. **`GnoLightClient.latestHeight()` and `trustingPeriod()` getters** (today decoded from `getClientState()`
    bytes), and a view of stored consensus heights.

**Both**

14. **A canonical packet identity** in every event and view: the ADR keys losses and records by
    `(sending chain's client id, sequence)`; the gno pages use `client/<20-digit seq>` keys while Ethereum uses
    `(keccak(clientId), seq)` topics. A shared textual key in events would remove a class of join bugs.
15. **Pruning policy** (ADR §13 open question 7): the monitor's packet index is unbounded as long as the chains'
    records are; a retention rule would let the index follow it.
