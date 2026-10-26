// The typed client of ibcmon's read-only API (internal/api). Amounts arrive
// as decimal strings and are parsed to bigint here; nothing in the console
// turns an amount into a Number.

export type Chain = "gno" | "ethereum";

export interface Provenance {
  chain: Chain;
  height: number;
  block_hash?: string;
  node: string;
  read_at: string;
  verification: "provider" | "cross-checked" | "light-client" | "unverified";
  finality?: string;
  /** Whether the value itself was authenticated against the header (design §3.3). */
  state_verification: "none" | "agreed" | "proof";
  collection?: number;
  /** Not from the chain's latest successful, header-agreed collection. */
  stale: boolean;
}

/** A uint64 identifier (sequence, decision number): a decimal string, never a Number. */
export type Seq = string;

export interface Client {
  chain: Chain;
  id: string;
  address?: string;
  kind: "ethereum" | "tendermint" | "gno-light-client";
  status: string;
  role?: string;
  verifier_version?: string;
  trust_generation?: number;
  counterparty_client_id?: string;
  chain_id?: string;
  latest_height: number;
  latest_revision?: number;
  latest_execution_block?: number;
  head_time: string;
  trusting_period_ns: number;
  expiry: string;
  frozen: boolean;
  retired_for_transfer?: boolean;
  retired_for_gov?: boolean;
  provenance: Provenance;
}

export interface Meter {
  level: string;
  at: number;
  epoch?: number;
  /** Present only when an exact source gave it (the realm's reader). */
  live?: string;
  live_source: "reader" | "unavailable";
  live_reason?: string;
}

export interface Totals {
  credited: string;
  owed: string;
  withheld: string;
  swept: string;
  paid: string;
  fees: string;
  sweep_paid: string;
  outstanding: string;
  returning: string;
  proven_owed: string;
  in_flight: string;
  sent: string;
  send_fees: string;
  refunded: string;
  queued_refunds: string;
  lost: string;
  recapitalized: string;
  reduced: string;
  accepted: string;
  unrecovered: string;
  unresolved: string;
}

export interface Denom {
  denom: string;
  kind: "unescrow" | "mint";
  cap_floor: string;
  launch_cap_floor: string;
  supply_cap: string;
  fee: string;
  fee_floor: string;
  send_max: string;
  send_min: string;
  refund_cap: string;
  remote: string;
  /** The denom's ERC20: `remote` for unescrow, the trace base for mint. */
  token: string;
  trace?: string;
  meter: Meter;
  refund_meter: Meter;
  waiting: number;
  queued_refunds: number;
  totals: Totals;
  /** The governed decimals of the voucher (0 for an unescrowed, gno-native denom). */
  decimals: number;
  display_decimals?: number;
  day_cap?: string;
  supply?: string;
  awaiting_first_receive?: boolean;
  /** The realm's escrow accounting (total_escrow), exact. */
  lock?: string;
}

export interface GnoRoute {
  route: string;
  client: string;
  replaced: string[];
  enrolled: string;
  activated: number;
  closed: boolean;
  loss_closed: boolean;
  fee_collector: string;
  refused_recipients: string[];
  protected_recipients: string[];
  unrecovered: string;
  unresolved: string;
  denoms: Denom[];
  claims?: Claim[];
  sweeps?: Sweep[];
  sends?: Send[];
  losses?: Loss[];
  provenance: Provenance;
}

export interface Claim {
  key: string;
  denom: string;
  to: string;
  amount: string;
  paid: string;
  reduced: string;
  fee: string;
  state: "open" | "withheld" | "forged" | "done";
  by: string;
  at: number;
  height: number;
  seq: number;
}

export interface Sweep {
  key: string;
  denom: string;
  to: string;
  amount: string;
  reduced: string;
  sweep_reason: string;
  height: number;
  voided: boolean;
  paid: boolean;
  paid_to: string;
  from_claim?: boolean;
  claim_paid?: string;
}

export interface Send {
  key: string;
  sequence: Seq;
  source_client: string;
  destination_client: string;
  timeout_timestamp: number;
  commitment_ok: boolean;
  sender: string;
  receiver: string;
  denom: string;
  kind: string;
  amount: string;
  fee: string;
  sent_height: number;
  sent_at: number;
  outcome: string;
  processed_height: number;
  processed_at: number;
  payment: string;
  refunded: string;
  settlement: number;
  reservation: string;
  proven: number;
  proven_at: number;
  proven_by: string;
  proven_gen: number;
  incident: string;
}

export interface Loss {
  key: string;
  direction: string;
  denom: string;
  amount: string;
  chain: string;
  incident: string;
  cause: string;
}

export interface EthFlag {
  seat: number;
  key: string;
  block_number: number;
  time: number;
}

export interface EthHold {
  start: number;
  end?: number;
  merged?: boolean;
}

export interface EthLimits {
  token: string;
  set: boolean;
  max_per_message: string;
  day_cap: string;
  refund_cap: string;
  send_max: string;
  send_min: string;
  receive_level: string;
  refund_level: string;
  receive_available?: string;
  refund_available?: string;
  origin?: "ethereum" | "gno";
  decimals?: number;
  escrow_balance?: string;
  voucher_supply?: string;
  reserved: string;
  losses: string;
  accepted_deficit: string;
}

export interface EthIncident {
  kind: number;
  status: number;
  client_hash: string;
  suspect_from: number;
}

export interface EthClientRecon {
  client_id: string;
  open: number;
  incident: number;
  suspect_start: number;
  suspect_open: number;
  processed: number;
  retired: boolean;
}

export interface EthMigration {
  old: string;
  new: string;
  block: number;
}

export interface EthRoute {
  route_key: string;
  /** Ethereum's own currentClient(routeKey) at the pin. */
  current_client: string;
  /** Client generations from Ethereum's own history (GnoRouteMigrated). */
  migrations: EthMigration[];
  /** The gno.land route matched to this one and the counterparty it names; the relationship is a separate comparison. */
  gno_route?: string;
  gno_counterparty?: string;
  relationship_mismatch?: string;
  /** The gno collection the cross-chain inputs came from (0: none). */
  gno_discovery: number;
  /** What the snapshot lacks without a gno discovery. */
  incomplete?: string[];
  escrow: string;
  closed: boolean;
  reopening: boolean;
  paused: boolean;
  refunds_held: boolean;
  recovery_blocked: boolean;
  route_flags: EthFlag[];
  refunds_flags: EthFlag[];
  route_hold?: EthHold;
  refunds_hold?: EthHold;
  guardian_keys: string[];
  quarantine_seconds: number;
  quarantine_end: number;
  limits: EthLimits[];
  incidents: EthIncident[];
  resolved_up_to: number;
  pending_incidents: number;
  client_recon: EthClientRecon[];
  provenance: Provenance;
}

/** The route_relationship check's latest verdict: a derived value with two dependencies. */
export interface RelationshipView {
  deferred: boolean;
  mismatch: boolean;
  severity?: string;
  title?: string;
  detail?: string;
  evaluated_at: string;
  /** Both inputs' provenance: the Ethereum route, then the gno discovery. */
  provenance: Provenance[];
}

export interface RouteView {
  key: string;
  /** Absent for an Ethereum route no gno.land route names. */
  gno?: GnoRoute;
  ethereum?: EthRoute;
  relationship?: RelationshipView;
}

export interface Seat {
  index: number;
  role: "guardian" | "watch";
  address: string;
}

export interface GnoFlag {
  route: string;
  function: string;
  seat: number;
  at: number;
  naming?: string[];
}

export interface GnoDecision {
  id: Seq;
  kind: string;
  text: string;
  state: "pending" | "applied" | "cancelled" | "spent";
  made: number;
  recorded: number;
  due: number;
  applied: number;
  cancelled: number;
  note: string;
}

export interface GnoDispatch {
  epoch: number;
  seq: Seq;
  kind: number;
  decision: Seq;
  body: string;
  status: number;
  redispatch: boolean;
  in_flight: boolean;
  transport: Seq;
  sends: number;
  event: string;
}

export interface GnoGovernance {
  governor: string;
  yielded: number;
  govdao_governs: boolean;
  seats: Seat[];
  hold: { held: boolean; since?: number; ends_at?: number };
  flags: GnoFlag[];
  epoch: number;
  epoch_client: string;
  last_seq: Seq;
  decisions: GnoDecision[];
  dispatches: GnoDispatch[];
  provenance: Provenance;
}

export interface EthDecision {
  epoch: number;
  seq: Seq;
  hash: string;
  kind: number;
  state: number;
  state_name: string;
  bad: boolean;
  recorded_time: number;
  recorded_block: number;
  /** recorded_time + the kind's delay; a kind 8 revocation is due at recorded_time itself. */
  mature_at?: number;
  /** A kind 8 with grant == false: due at once; suspended while a recovery is open. */
  revocation?: boolean;
  suspended?: boolean;
  params?: string;
  tombstoned: boolean;
}

export interface EthRecovery {
  delay: number;
  threshold: number;
  cancel_threshold: number;
  silence: number;
  hold_period: number;
  enabled: boolean;
  supported: boolean;
  disabled: boolean;
  status: "enabled" | "disabled" | "never_enabled";
  disabled_by?: { epoch: number; seq: Seq; block: number; tx: string };
  proposal: string;
  opened_at: number;
  executable_at?: number;
  cancel_votes: number;
  approved: string[];
}

export interface EthGovernance {
  epoch: number;
  last_seq: Seq;
  gov_client: string;
  gno_chain_id: string;
  last_record_time: number;
  setup_done: boolean;
  keys: string[];
  flags: EthFlag[];
  hold?: EthHold;
  holds: EthHold[];
  conflict_block: number;
  exec: string;
  exec_code_hash: string;
  decisions: EthDecision[];
  buffered: Seq[];
  recovery: EthRecovery;
  provenance: Provenance;
}

export interface Finding {
  subject: string;
  severity: "info" | "warning" | "critical" | "break";
  title: string;
  detail?: string;
  values?: Record<string, string>;
  provenance: Provenance[];
  alarm_key?: string;
  autoflag?: boolean;
}

export interface CheckResult {
  check: string;
  status: "ok" | "skipped" | "alarm" | "error";
  started_at: string;
  finished_at: string;
  reason?: string;
  findings: Finding[];
  provenance: Provenance[];
  consecutive_skips?: number;
}

export interface Alarm {
  key: string;
  check: string;
  severity: Finding["severity"];
  state: "raised" | "acknowledged" | "cleared";
  first_seen: string;
  last_seen: string;
  cleared_at?: string;
  title: string;
  detail?: string;
  evidence: Finding;
  autoflag: boolean;
  transitions: number;
}

export interface Info {
  version: string;
  public: boolean;
  started_at: string;
  gno_node: string;
  eth_node: string;
  eth_finality: string;
  /** The configuration: "off" | "dry-run" | "on". */
  autoflag_requested: string;
  /** What runs: "off" | "dry-run" | "on" | "unavailable" (sink not built). */
  autoflag_effective: string;
  worker: boolean;
}

export interface ChainState {
  ok: boolean;
  verified: boolean;
  /** False when a cross-chain dependency was missing: the route snapshots are not fresh; clients and governance are. */
  complete: boolean;
  incomplete?: string;
  error?: string;
  consecutive_failures?: number;
  consecutive_unverified?: number;
  last_success?: string;
  last_collection: number;
}

export interface Pins {
  collection: number;
  gno: { Height: number; BlockHash: string; Time: string };
  gno_verification: Provenance["verification"];
  gno_state: ChainState;
  eth: { Number: number; Hash: string; Timestamp: number; Finality: string };
  eth_verification: Provenance["verification"];
  eth_state: ChainState;
  collected_at: string;
  gno_error?: string;
  eth_error?: string;
  gno_cross_check?: string;
  eth_cross_check?: string;
}

export interface Overview {
  info: Info;
  pins: Pins | null;
  counts: Record<string, number>;
  alarms: Alarm[];
  checks: CheckResult[];
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path, { headers: { Accept: "application/json" } });
  if (!res.ok) throw new Error(`${path}: http ${res.status}`);
  return (await res.json()) as T;
}

export const api = {
  overview: () => get<Overview>("/api/v1/overview"),
  clients: () => get<{ gno: Client[]; ethereum: Client[] }>("/api/v1/clients"),
  routes: () => get<RouteView[]>("/api/v1/routes"),
  governance: () =>
    get<{ gno: GnoGovernance | null; ethereum: EthGovernance | null }>("/api/v1/governance"),
  checks: () => get<CheckResult[]>("/api/v1/checks"),
  alarms: (all = false) => get<Alarm[]>(`/api/v1/alarms${all ? "?all=true" : ""}`),
};
