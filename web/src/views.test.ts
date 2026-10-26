import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type {
  Claim,
  Send,
  Denom,
  EthRoute,
  EthGovernance,
  GnoGovernance,
  GnoRoute,
  RouteView,
  Totals,
} from "./api";
import { ClientsPage } from "./pages/Clients";
import { GovernancePage } from "./pages/Governance";
import { RoutesPage } from "./pages/Routes";

function render(Page: typeof RoutesPage, key: string, data: unknown) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData([key], data);
  const html = renderToStaticMarkup(
    createElement(QueryClientProvider, { client }, createElement(Page)),
  );
  client.clear();
  return html;
}

const totals = Object.fromEntries(
  [
    "credited",
    "owed",
    "withheld",
    "swept",
    "paid",
    "fees",
    "sweep_paid",
    "outstanding",
    "returning",
    "proven_owed",
    "in_flight",
    "sent",
    "send_fees",
    "refunded",
    "queued_refunds",
    "lost",
    "recapitalized",
    "reduced",
    "accepted",
    "unrecovered",
    "unresolved",
  ].map((key) => [key, "0"]),
) as unknown as Totals;
const voucher: Denom = {
  denom: `ibc/${"A".repeat(64)}`,
  kind: "mint",
  decimals: 18,
  token: "0xAB",
  cap_floor: "1000000000000000000000000",
  launch_cap_floor: "1000000000000000000000000",
  supply_cap: "1000000000000000000000000000",
  fee: "1000000000000000",
  fee_floor: "1",
  send_max: "0",
  send_min: "0",
  refund_cap: "1000000000000000000000000",
  remote: "",
  meter: { level: "1", at: 1792618727, live_source: "unavailable", live_reason: "No exact source" },
  refund_meter: { level: "0", at: 1792618727, live_source: "unavailable" },
  waiting: 0,
  queued_refunds: 0,
  totals,
  lock: "0",
};
const gno = {
  route: "ethereum-1",
  client: "ethereum-1",
  replaced: [],
  enrolled: "2026-10-21T00:00:00Z",
  activated: 1,
  closed: false,
  loss_closed: false,
  fee_collector: "g1test",
  unrecovered: "0",
  unresolved: "0",
  denoms: [voucher],
} as unknown as GnoRoute;
const eth = {
  route_key: "client-0",
  current_client: "client-0",
  migrations: [],
  escrow: "0xtest",
  route_flags: [],
  refunds_flags: [],
  guardian_keys: [],
  incidents: [],
  pending_incidents: 0,
  resolved_up_to: 0,
  client_recon: [],
  quarantine_seconds: 3600,
  quarantine_end: 0,
  limits: [
    {
      token: "0xab",
      set: true,
      max_per_message: voucher.cap_floor,
      day_cap: voucher.cap_floor,
      refund_cap: voucher.refund_cap,
      receive_level: "1",
      refund_level: "0",
      send_min: "0",
      send_max: "0",
      reserved: "0",
      losses: "0",
      accepted_deficit: "0",
    },
  ],
} as unknown as EthRoute;

describe("console data views", () => {
  it("renders exact voucher units on both chains and contains the token table", () => {
    const html = render(RoutesPage, "routes", [
      { key: "ethereum-1", gno, ethereum: eth },
    ] satisfies RouteView[]);
    expect(html).toContain("1,000,000");
    expect(html).toContain("0.000000000000000001");
    expect(html).toContain("voucher tokens");
    expect(html).toContain('class="table-wrap"');
    expect(html).toContain("scroll horizontally for more columns");
    expect(html).toContain(`title="${voucher.denom}"`);
    expect(html).not.toContain("1,000,000,000,000,000,000,000,000<");
    expect(html).toContain("3,600 seconds");
  });
  it("labels unmatched Ethereum amounts as base units", () => {
    const html = render(RoutesPage, "routes", [{ key: "orphan", ethereum: eth }]);
    expect(html).toContain("base units");
    expect(html).toContain("1,000,000,000,000,000,000,000,000");
  });
  it("renders claim and send records, physical backing and loss signals", () => {
    const denom = {
      ...voucher,
      display_decimals: 18,
      day_cap: voucher.cap_floor,
      supply: "60000000000000000000",
    };
    const claim = {
      key: "route/claim",
      denom: denom.denom,
      to: "g1recipient",
      amount: "10000000000000000000",
      paid: "10000000000000000000",
      reduced: "0",
      fee: "1",
      state: "done",
      by: "packet",
      at: 1792618727,
      height: 10,
      seq: 1,
    } as Claim;
    const send = {
      key: "route/send",
      sequence: "18446744073709551615",
      denom: denom.denom,
      amount: claim.amount,
      fee: "0",
      outcome: "error",
      payment: "paid",
      refunded: claim.amount,
      settlement: 0,
      commitment_ok: false,
      sender: "g1sender",
      receiver: "0xrecipient",
      sent_at: 1792618727,
      sent_height: 10,
      processed_at: 1792618728,
      processed_height: 11,
      timeout_timestamp: 1792620000,
      reservation: "0",
      proven: 0,
    } as Send;
    const html = render(RoutesPage, "routes", [
      {
        key: "route",
        gno: {
          ...gno,
          denoms: [denom],
          claims: [claim],
          sends: [send],
          sweeps: [],
          losses: [],
          unrecovered: "12345678901234567890",
        },
        ethereum: {
          ...eth,
          limits: eth.limits.map((l) => ({
            ...l,
            decimals: 18,
            origin: "ethereum",
            escrow_balance: denom.supply,
            receive_available: l.day_cap,
            refund_available: l.refund_cap,
          })),
        },
      },
    ]);
    expect(html).toContain("18446744073709551615");
    expect(html).toContain("done");
    expect(html).toContain("mismatch");
    expect(html).toContain("Losses open: see denoms");
    expect(html).not.toContain("12,345,678,901,234,567,890");
    expect(html).toContain('title="60 · 60000000000000000000 base units"');
    expect(html).toContain("No sweeps in this snapshot");
    expect(html).toContain("No loss ledger in this snapshot");
    expect(html).toContain("No minimum");
    expect(html).toContain("No maximum");
  });
  it("renders empty states for all data pages", () => {
    expect(render(RoutesPage, "routes", [])).toContain("No route in this snapshot");
    expect(render(ClientsPage, "clients", { gno: [], ethereum: [] })).toContain(
      "No client in this snapshot",
    );
    const html = render(GovernancePage, "governance", { gno: null, ethereum: null });
    expect(html).toContain("No gno.land governance snapshot");
    expect(html).toContain("No Ethereum governance snapshot");
  });
  it("preserves uint64 governance sequences and handles no settled decisions", () => {
    const g = {
      governor: "g1test",
      govdao_governs: false,
      epoch: 1,
      epoch_client: "ethereum-1",
      last_seq: "18446744073709551615",
      hold: { held: false },
      seats: [],
      flags: [],
      decisions: [],
      dispatches: [],
    } as unknown as GnoGovernance;
    const html = render(GovernancePage, "governance", { gno: g, ethereum: null });
    expect(html).toContain("18446744073709551615");
    expect(html).toContain("No settled decision");
    expect(html).toContain("No dispatch");
  });
  it("shows kind 14 suspension even without a maturity timestamp", () => {
    const ethereum = {
      epoch: 1,
      last_seq: "4",
      gov_client: "client-0",
      keys: [],
      flags: [],
      holds: [],
      buffered: [],
      recovery: {
        enabled: true,
        supported: true,
        disabled: false,
        status: "enabled",
        delay: 7200,
        threshold: 3,
        cancel_threshold: 3,
        silence: 3600,
        hold_period: 3600,
        approved: [],
        opened_at: 1,
      },
      decisions: [
        { epoch: 1, seq: "4", kind: 14, state_name: "pending", hash: "0xabc", suspended: true },
      ],
    } as unknown as EthGovernance;
    const html = render(GovernancePage, "governance", { gno: null, ethereum });
    expect(html).toContain("14 disable-recovery");
    expect(html).toContain("suspended by the open recovery");
    expect(html).toContain('aria-label="About Suspended by the open recovery"');
  });
});
