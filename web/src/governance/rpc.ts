import { CORE, DAO, GNO_CHAIN, GNO_RPC, GOV, TRANSFER, type Role } from "./catalog";
import { goString, type RoleState } from "./messages";
import { decimal, validGnoAddress, type ValidationContext } from "./validation";
import type { GnoDecision, GnoDispatch } from "../api";

export function errorText(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (error && typeof error === "object" && "message" in error) return String(error.message);
  return String(error);
}
export function parseJson<T>(text: string): T {
  // Tokenise strings as well as numbers so digits INSIDE text are untouched.
  // RPC renders include unquoted uint64s: retain unsafe integers as strings.
  return JSON.parse(
    text.replace(/"(?:\\.|[^"\\])*"|-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/g, (token) => {
      if (
        /^-?[0-9]+$/.test(token) &&
        (BigInt(token) > BigInt(Number.MAX_SAFE_INTEGER) ||
          BigInt(token) < BigInt(Number.MIN_SAFE_INTEGER))
      )
        return JSON.stringify(token);
      return token;
    }),
  ) as T;
}
export async function rpc<T = Record<string, unknown>>(
  method: string,
  params: Record<string, unknown> = {},
): Promise<T> {
  const response = await fetch(GNO_RPC, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
    signal: AbortSignal.timeout(20_000),
  });
  if (!response.ok) throw new Error(`gno RPC: HTTP ${response.status}.`);
  const json = parseJson<{ error?: { message: string; data?: string }; result?: T }>(
    await response.text(),
  );
  if (json.error)
    throw new Error(`${json.error.message}${json.error.data ? `: ${json.error.data}` : ""}`);
  if (!json.result) throw new Error("Missing gno RPC result.");
  return json.result;
}
export function decodeQueryResponse(response: Record<string, unknown>): string {
  const base = (response.ResponseBase ?? response) as Record<string, unknown>;
  if (base.Error || base.error || (base.code && base.code !== "0"))
    throw new Error(
      `gno VM: ${String(base.Log ?? base.log ?? JSON.stringify(base.Error ?? base.error))}`,
    );
  const data = base.Data ?? base.data ?? response.Value ?? response.value;
  if (typeof data !== "string") throw new Error("Missing query response data.");
  return new TextDecoder().decode(Uint8Array.from(atob(data), (c) => c.charCodeAt(0)));
}
export async function query(path: string, data: string): Promise<string> {
  const encoded = btoa(
    Array.from(new TextEncoder().encode(data), (b) => String.fromCharCode(b)).join(""),
  );
  const result = await rpc<{ response: Record<string, unknown> }>("abci_query", {
    path,
    data: encoded,
  });
  return decodeQueryResponse(result.response);
}
export const evaluate = (expression: string) => query("vm/qeval", expression);
export const render = (realm: string, path = "") => query("vm/qrender", `${realm}:${path}`);
export function evalString(text: string): string {
  const match = /^\(("(?:\\.|[^"\\])*") (?:string|\.uverse.address)\)$/.exec(text.trim());
  if (!match) throw new Error("Unexpected qeval string / address response.");
  return JSON.parse(match[1]) as string;
}
export function evalInteger(text: string): string {
  const match = /^\((-?[0-9]+) (?:int|int64|uint32|uint64)\)$/.exec(text.trim());
  if (!match) throw new Error("Unexpected qeval integer response.");
  return BigInt(match[1]).toString();
}
export function evalBool(text: string): boolean {
  if (text.trim() === "(true bool)") return true;
  if (text.trim() === "(false bool)") return false;
  throw new Error("Unexpected qeval bool response.");
}
export function memberRows(markdown: string): { address: string; tier: string }[] {
  return markdown.split("\n").flatMap((line) => {
    const match = /^\|[^|]*\b(T[123])\s*\|\s*(g1[023456789acdefghjklmnpqrstuvwxyz]{38})\s*\|/.exec(
      line,
    );
    return match && validGnoAddress(match[2]) ? [{ address: match[2], tier: match[1] }] : [];
  });
}
export function nextPage(markdown: string, current: number): number | undefined {
  const pages = [...markdown.matchAll(/(?:[?&]|&amp;)page=([0-9]+)/g)]
    .map((m) => parseInt(m[1], 10))
    .filter((n) => n > current);
  return pages.length ? Math.min(...pages) : undefined;
}
export async function readMembers(): Promise<{ address: string; tier: string }[]> {
  const members: { address: string; tier: string }[] = [];
  let page: number | undefined = 1;
  for (let count = 0; page && count < 200; count++) {
    const text = await render(`${DAO}/memberstore/v0`, `members?page=${page}`);
    if (!text.includes("|")) throw new Error("GovDAO member table is unavailable.");
    members.push(...memberRows(text));
    page = nextPage(text, page);
  }
  if (page) throw new Error("GovDAO member pagination exceeded 200 pages.");
  return members;
}
export function detectRoles(
  address: string,
  governor: string,
  yielded: string,
  governs: boolean,
  seatResponse: string,
  members: { address: string; tier: string }[],
): RoleState {
  const seatString = evalInteger(seatResponse);
  if (!/^(?:-1|[0-5])$/.test(seatString)) throw new Error("Invalid seat response.");
  const seat = parseInt(seatString, 10);
  const roles: Role[] = ["anyone"];
  if (address === governor && yielded === "0" && !governs) roles.push("governor");
  const tier = members.find((member) => member.address === address)?.tier;
  if (tier) roles.push("govdao");
  if (seat >= 0 && seat <= 3) roles.push("guardian");
  if (seat >= 4) roles.push("watch");
  return { governor, yielded, govdaoGoverns: governs, roles, tier, seat };
}
export interface ChainHead {
  height: string;
  time: number;
}
export async function chainHead(): Promise<ChainHead> {
  const r = await rpc<{
    node_info: { network: string };
    sync_info: { latest_block_height: string; latest_block_time: string; catching_up: boolean };
  }>("status");
  if (r.node_info.network !== GNO_CHAIN || r.sync_info.catching_up)
    throw new Error("The RPC is on the wrong chain or is catching up.");
  const time = Date.parse(r.sync_info.latest_block_time) / 1000;
  if (!Number.isFinite(time)) throw new Error("Missing chain time.");
  return { height: decimal(r.sync_info.latest_block_height), time };
}
export interface LiveState extends ValidationContext {
  governor: string;
  yielded: string;
  govdaoGoverns: boolean;
  held: boolean;
  holdSince: number;
  decisions: GnoDecision[];
  dispatches: GnoDispatch[];
  chainTime: number;
  readAt: number;
}
export async function readDecisions(): Promise<GnoDecision[]> {
  const rows = parseJson<GnoDecision[]>(await render(GOV, "decisions"));
  if (!Array.isArray(rows)) throw new Error("Unexpected decisions ledger response.");
  return rows.map((d) => ({ ...d, id: decimal(String(d.id), (1n << 63n) - 1n, 1n) }));
}
export async function readLiveState(): Promise<LiveState> {
  const [governor, yielded, governs, epoch, client, held, since, root, routeText, decisions, head] =
    await Promise.all([
      evaluate(`${GOV}.Governor()`).then(evalString),
      evaluate(`${GOV}.Yielded()`).then(evalInteger),
      evaluate(`${GOV}.GovDAOGoverns()`).then(evalBool),
      evaluate(`${GOV}.Epoch()`).then(evalInteger),
      evaluate(`${GOV}.EpochClient()`).then(evalString),
      evaluate(`${GOV}.Held()`).then(evalBool),
      evaluate(`${GOV}.HoldSince()`).then(evalInteger),
      render(GOV),
      render(TRANSFER, "routes/ethereum-1"),
      readDecisions(),
      chainHead(),
    ]);
  const route = parseJson<ValidationContext["route"]>(routeText);
  if (!route || !Array.isArray(route.denoms) || !route.client)
    throw new Error("The live route controls are unavailable.");
  const [routerClient, lastSeq] = await Promise.all([
    evaluate(`${CORE}.CounterpartyClientID(${goString(client)})`).then(evalString),
    evaluate(`${GOV}.LastSeq(${decimal(epoch)})`).then(evalInteger),
  ]);
  const seats = Object.fromEntries(
    [
      ...root.matchAll(
        /^- ([0-5]) \((?:guardian|watch key)\):\s*(g1[023456789acdefghjklmnpqrstuvwxyz]{38})?/gm,
      ),
    ].map((m) => [m[1], m[2] ?? ""]),
  );
  if (Object.keys(seats).length !== 6)
    throw new Error("Cannot read the six live governance seats.");
  const dispatchNumbers = decisions.flatMap((d) => {
    const match = /dispatched as epoch ([0-9]+) seq ([0-9]+)/.exec(d.note);
    return match ? [{ epoch: match[1], seq: match[2] }] : [];
  });
  const dispatches = await Promise.all(
    dispatchNumbers.map(async ({ epoch: e, seq }) => {
      const d = parseJson<GnoDispatch>(await render(GOV, `dispatch/${e}/${seq}`));
      return {
        ...d,
        seq: String(d.seq),
        decision: String(d.decision),
        transport: String(d.transport),
      };
    }),
  );
  return {
    governor,
    yielded,
    govdaoGoverns: governs,
    epoch,
    routerClient,
    lastSeq,
    held,
    holdSince: parseInt(since, 10),
    seats,
    route,
    decisions,
    dispatches,
    height: head.height,
    chainTime: head.time,
    readAt: Date.now(),
    roles: ["anyone"],
  };
}
export async function readRoles(address: string, live: LiveState): Promise<RoleState> {
  if (!validGnoAddress(address)) throw new Error("Invalid connected address.");
  const [seat, members] = await Promise.all([
    evaluate(`${GOV}.SeatOf(${goString(address)})`),
    readMembers(),
  ]);
  return detectRoles(address, live.governor, live.yielded, live.govdaoGoverns, seat, members);
}
