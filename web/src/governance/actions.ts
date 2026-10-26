import { CORE, GOV, TRANSFER, type Action } from "./catalog";
import { goString } from "./messages";
import { evalBool, evalString, evaluate, type LiveState } from "./rpc";
import { validateValues, type ValidationContext, type Values } from "./validation";
import { proposalExecutable, readProposals } from "./followups";
import { api, type EthRoute } from "../api";
import { actionAvailability, detectRequirement } from "./availability";

const controls: Record<string, string> = {
  capFloor: "cap_floor",
  supplyCap: "supply_cap",
  fee: "fee",
  feeFloor: "fee_floor",
  sendMax: "send_max",
  sendMin: "send_min",
  refundCap: "refund_cap",
  remoteToken: "remote",
  decimals: "decimals",
};
export function defaultValues(
  a: Action,
  live: LiveState,
  denom = "ugnot",
  ethereum?: EthRoute,
): Values {
  const values: Values = {};
  for (const p of a.params) {
    let value = p.default ?? "";
    if (["int", "uint32", "uint64", "uint256 decimal"].includes(p.type))
      value = /^[0-9]+(?= |$)/.exec(value)?.[0] ?? "";
    if (p.type === "hex" && value.includes(" ")) value = value.split(" ")[0];
    if (p.name === "route") value = live.route.route;
    if (p.type === "gno client id" && (p.default === "ethereum-1" || p.name === "clientID"))
      value = live.route.client;
    if (p.type === "router client id" && (p.default === "client-0" || a.id === "eth.settle"))
      value = live.routerClient;
    if (p.name === "epoch" && a.id !== "eth.apply_epoch_notice") value = live.epoch;
    if (p.type === "bool") value = "false";
    if (["object", "op"].includes(p.name)) value = "1";
    if (p.name === "fn") value = "sends";
    if (p.name === "fns") value = "sends,credits,payouts,refunds";
    if (p.name === "feeCollector") value = live.route.fee_collector;
    values[p.name] = value;
  }
  if (a.id === "denom.set") {
    const current = live.route.denoms.find((d) => d.denom === denom);
    values.denom = denom;
    if (current) {
      values.kind = current.kind;
      for (const [param, field] of Object.entries(controls))
        values[param] = String(current[field as keyof typeof current]);
    }
  }
  if (a.id === "eth.limits" && ethereum && !ethereum.provenance?.stale) {
    const limit = ethereum.limits.find((l) => l.token.toLowerCase() === values.token.toLowerCase());
    if (limit) {
      values.maxPerMessage = limit.max_per_message;
      values.dayCap = limit.day_cap;
      values.refundCap = limit.refund_cap;
      values.sendMax = limit.send_max;
      values.sendMin = limit.send_min;
    }
  }
  if (a.id === "eth.route_settings" && ethereum && !ethereum.provenance?.stale)
    values.quarantineSeconds = String(ethereum.quarantine_seconds);
  return values;
}

/** Branch-only pairing checks must be enforced even on the older deployed
 * realm. Query identifiers only after strict grammar validation.
 */
export async function preflightAction(
  a: Action,
  values: Values,
  live: LiveState,
  roles: ValidationContext["roles"],
): Promise<ValidationContext> {
  const ctx: ValidationContext = { ...live, roles };
  const safe = validateValues(a, values, ctx);
  const requirement = await detectRequirement(a);
  if (requirement.status === "unavailable")
    throw new Error(actionAvailability(a, requirement).disabled);
  const recovery =
    a.id === "eth.disableRecovery" ? (await api.governance()).ethereum?.recovery : undefined;
  const availability = actionAvailability(a, requirement, recovery);
  if (availability.disabled) throw new Error(availability.disabled);
  if (a.id === "denom.set" && safe.kind === "mint" && /^0x[0-9a-f]{40}$/.test(safe.denom)) {
    const path = `transfer/${live.route.route}/${safe.denom}`;
    const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(path));
    const hash = Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0"))
      .join("")
      .toUpperCase();
    ctx.denomAliases = { [safe.denom]: `ibc/${hash}` };
    validateValues(a, safe, ctx);
  }
  const pairs: Record<string, string> = {};
  const pairClient =
    a.id === "eth.client" && safe.op === "1" ? safe.counterpartyClientID : safe.localClientID;
  if (pairClient && !/remove_expectation|purge_tombstone/.test(a.id)) {
    pairs[pairClient] = evalString(
      await evaluate(`${CORE}.CounterpartyClientID(${goString(pairClient)})`),
    );
    ctx.counterparties = pairs;
    validateValues(a, safe, ctx);
  }
  const clients = a.params
    .filter((p) => p.type === "gno client id" && !(a.id === "eth.client" && safe.op === "2"))
    .map((p) => safe[p.name])
    .filter(Boolean);
  await Promise.all(
    [...new Set(clients)].map(async (client) => {
      if (!evalString(await evaluate(`${CORE}.ClientType(${goString(client)})`)))
        throw new Error(`Unknown gno core client: ${client}.`);
    }),
  );
  if (safe.route && !a.id.startsWith("flag.")) {
    const routeKey = [live.route.route, live.route.client, ...live.route.replaced].includes(
      safe.route,
    )
      ? live.route.route
      : safe.route;
    if (!evalBool(await evaluate(`${GOV}.IsRoute(${goString(routeKey)})`)))
      throw new Error("This gno route is not enrolled.");
  }
  if (a.id === "execute" || a.id === "cancel") {
    const decision = live.decisions.find((d) => d.id === safe.id);
    if (!decision || decision.state !== "pending")
      throw new Error("Choose an existing pending decision.");
    if (a.id === "execute" && live.chainTime < decision.due)
      throw new Error("This decision is not due yet.");
  }
  if (a.id === "eth.redispatch" || a.id === "eth.retransmit") {
    const dispatch = live.dispatches.find(
      (d) => String(d.epoch) === safe.epoch && d.seq === safe.seq,
    );
    if (!dispatch || dispatch.in_flight)
      throw new Error("No matching dispatch, or its packet is still in flight.");
    if (a.id === "eth.retransmit" && (dispatch.status !== 4 || dispatch.redispatch))
      throw new Error("Retransmit requires a conflict without redispatch permission.");
  }
  if (a.id === "core.update_impl" || a.id === "transfer.update_impl") {
    const realm = a.id === "core.update_impl" ? CORE : "gno.land/r/aib/ibc/apps/transfer";
    const registered = await evaluate(`${realm}.RegisteredImplPaths()`);
    if (!registered.includes(goString(safe.path)))
      throw new Error("This implementation path is not registered on chain.");
  }
  if (a.id === "eth.settle") {
    const clients = [...new Set([live.route.client, ...live.route.replaced])];
    const routers = await Promise.all(
      clients.map((client) =>
        evaluate(`${CORE}.CounterpartyClientID(${goString(client)})`).then(evalString),
      ),
    );
    if (!routers.includes(safe.clientID) && safe.clientID !== live.routerClient)
      throw new Error(
        "This router client is not paired with a current or retired gno route client.",
      );
  }
  if (["core.pause_app", "core.unpause_app", "govdao.core_pause_app_direct"].includes(a.id)) {
    if (!evalString(await evaluate(`${CORE}.AppPkgPath(${goString(safe.portID)})`)))
      throw new Error("No registered app uses this port.");
  }
  if (["core.remove_relayer", "core.remove_member", "transfer.remove_member"].includes(a.id)) {
    const realm = a.id.startsWith("core.") ? CORE : TRANSFER;
    const getter = a.id === "core.remove_relayer" ? "Relayers" : "AuthorityMembers";
    if (!(await evaluate(`${realm}.${getter}()`)).includes(goString(safe.addr)))
      throw new Error("The address is not in the live relayer / authority list.");
  }
  if (a.id === "govdao.execute") {
    const proposal = (await readProposals()).proposals.find((p) => p.id === safe.pid);
    if (!proposal || !proposalExecutable(proposal, live.chainTime, live.govdaoGoverns))
      throw new Error(
        "This proposal is not executable: check its YES power, deadline and governance phase.",
      );
  }
  return ctx;
}
