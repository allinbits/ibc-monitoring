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
}

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
  parked: string;
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
  /** The realm's escrow accounting (float64 on chain): exact only when lock_exact. */
  lock?: string;
  lock_exact: boolean;
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
  provenance: Provenance;
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

export interface EthRoute {
  route_key: string;
  current_client: string;
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

export interface RouteView {
  key: string;
  gno: GnoRoute;
  ethereum?: EthRoute;
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
  id: number;
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
  seq: number;
  kind: number;
  decision: number;
  body: string;
  status: number;
  redispatch: boolean;
  in_flight: boolean;
  transport: number;
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
  last_seq: number;
  decisions: GnoDecision[];
  dispatches: GnoDispatch[];
  provenance: Provenance;
}

export interface EthDecision {
  epoch: number;
  seq: number;
  hash: string;
  kind: number;
  state: number;
  state_name: string;
  bad: boolean;
  recorded_time: number;
  recorded_block: number;
  mature_at?: number;
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
  proposal: string;
  opened_at: number;
  executable_at?: number;
  cancel_votes: number;
  approved: string[];
}

export interface EthGovernance {
  epoch: number;
  last_seq: number;
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
  buffered: number[];
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
  autoflag: string;
  worker: boolean;
}

export interface ChainState {
  ok: boolean;
  error?: string;
  consecutive_failures?: number;
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
