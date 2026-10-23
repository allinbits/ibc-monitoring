import { CLOCKS, DAO, GOV } from "./catalog";
import { chainHead, nextPage, render, rpc } from "./rpc";
import { vmFailure } from "./wallets";

export function decisionDue(state: string, due: number, now: number): boolean {
  return state === "pending" && Number.isFinite(due) && due > 0 && now >= due;
}
export function proposalDeadline(created: number): number {
  return created + CLOCKS.proposal_deadline_s;
}
export function secondsRemaining(deadline: number, now: number): number {
  return Math.max(0, Math.ceil(deadline - now));
}
export function timeRemaining(deadline: number, now: number): string {
  const remaining = secondsRemaining(deadline, now);
  return `${Math.floor(remaining / 3600)}h ${Math.floor((remaining % 3600) / 60)}m ${remaining % 60}s`;
}
export interface ChainEvent {
  type: string;
  pkg_path?: string;
  attrs: { key: string; value: string }[];
}
export function chainEvents(value: unknown): ChainEvent[] {
  if (!value || typeof value !== "object") return [];
  const obj = value as Record<string, unknown>;
  const base = (obj.ResponseBase ?? obj) as Record<string, unknown>;
  const events = base.Events ?? base.events;
  if (!Array.isArray(events)) return [];
  return events.flatMap((e: unknown) => {
    if (!e || typeof e !== "object") return [];
    const event = e as ChainEvent;
    return typeof event.type === "string" && Array.isArray(event.attrs) ? [event] : [];
  });
}
export function eventAttr(event: ChainEvent, key: string): string | undefined {
  return event.attrs.find((a) => a.key === key)?.value;
}
export function outcomeEvents(events: ChainEvent[]) {
  return {
    decisions: events
      .filter((e) => e.type === "Decision" && e.pkg_path === GOV)
      .map((e) => ({
        id: eventAttr(e, "id"),
        state: eventAttr(e, "state"),
        due: eventAttr(e, "due"),
      })),
    proposals: events
      .filter((e) => e.type === "ProposalCreated" && e.pkg_path === DAO)
      .map((e) => eventAttr(e, "id"))
      .filter((id): id is string => !!id && /^[0-9]+$/.test(id)),
    dispatches: events
      .filter((e) => e.type === "GovPacketSent" && e.pkg_path === GOV)
      .map((e) => ({
        epoch: eventAttr(e, "epoch"),
        seq: eventAttr(e, "seq"),
        kind: eventAttr(e, "kind"),
      })),
  };
}
interface Block {
  header: { height: string; time: string; total_txs: string };
  data: { txs: string[] | null };
}
const blocks = new Map<string, Block>();
async function blockAt(height: bigint): Promise<Block> {
  const key = height.toString();
  const cached = blocks.get(key);
  if (cached) return cached;
  const result = await rpc<{ block: Block }>("block", { height: key });
  if (!result.block?.header) throw new Error("Block data unavailable.");
  if (blocks.size > 4096) blocks.clear();
  blocks.set(key, result.block);
  return result.block;
}
async function blockResults(height: string): Promise<unknown[]> {
  const result = await rpc<{ results: { deliver_tx: unknown[] | null } }>("block_results", {
    height,
  });
  return result.results.deliver_tx ?? [];
}
/** The public sentry has tx_index=off and no tx_search. Its cumulative
 * total_txs header lets us skip empty block ranges by binary subdivision.
 */
async function transactionBlocks(first: bigint, last: bigint): Promise<Block[]> {
  const found: Block[] = [];
  let visits = 0;
  const before = first > 1n ? BigInt((await blockAt(first - 1n)).header.total_txs) : 0n;
  const end = BigInt((await blockAt(last)).header.total_txs);
  async function walk(lo: bigint, hi: bigint, left: bigint, right: bigint): Promise<void> {
    if (right === left) return;
    if (++visits > 512)
      throw new Error(
        "Too much transaction history for this browser lookup. Check the proposal on gnoweb.",
      );
    if (lo === hi) {
      found.push(await blockAt(lo));
      return;
    }
    const mid = (lo + hi) / 2n;
    const total = BigInt((await blockAt(mid)).header.total_txs);
    await walk(lo, mid, left, total);
    await walk(mid + 1n, hi, total, right);
  }
  if (first <= last) await walk(first, last, before, end);
  return found;
}
export async function transactionHash(encoded: string): Promise<string> {
  const bytes = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0")).join("");
}
export interface CommittedTx {
  hash: string;
  height: string;
  time: number;
  events: ChainEvent[];
  failure?: string;
}
export async function findCommittedTx(
  hash: string,
  fromHeight: string,
  signal?: AbortSignal,
): Promise<CommittedTx | undefined> {
  let cursor = BigInt(fromHeight);
  const target = hash.replace(/^0x/, "").toLowerCase();
  for (let poll = 0; poll < 45; poll++) {
    if (signal?.aborted) return;
    const head = await chainHead();
    const latest = BigInt(head.height);
    if (cursor <= latest) {
      const candidates = await transactionBlocks(cursor, latest);
      for (const block of candidates) {
        const hashes = await Promise.all((block.data.txs ?? []).map(transactionHash));
        const index = hashes.indexOf(target);
        if (index < 0) continue;
        const tx = (await blockResults(block.header.height))[index];
        const committed = {
          hash,
          height: block.header.height,
          time: Date.parse(block.header.time) / 1000,
          events: chainEvents(tx),
          failure: vmFailure(tx),
        };
        for (const id of outcomeEvents(committed.events).proposals)
          proposalTimes.set(id, committed.time);
        return committed;
      }
      cursor = latest + 1n;
    }
    await new Promise<void>((resolve) => {
      const timer = setTimeout(resolve, 2000);
      signal?.addEventListener(
        "abort",
        () => {
          clearTimeout(timer);
          resolve();
        },
        { once: true },
      );
    });
  }
}

export interface Proposal {
  id: string;
  title: string;
  status: "ACTIVE" | "ACCEPTED" | "REJECTED" | "UNKNOWN";
  eligible: string[];
  yesPercent?: number;
  created?: number;
  expired?: boolean;
  deadlineRequired: boolean;
  detail?: string;
}
export function proposalList(markdown: string): Proposal[] {
  // Split on the known list-item heading: titles remain plain text in React.
  const headings = [
    ...markdown.matchAll(/^### \[Prop #([0-9]+) - (.*)\]\(\/r\/gov\/dao:[0-9]+\)/gm),
  ];
  return headings.map((m, i) => {
    const section = markdown.slice(
      m.index! + m[0].length,
      headings[i + 1]?.index ?? markdown.length,
    );
    const statusText = /^Status: (ACTIVE|ACCEPTED|REJECTED)\s*$/m.exec(section)?.[1];
    return {
      id: BigInt(m[1]).toString(),
      title: m[2].replace(/\\([\\`*{}[\]()#+\-.!_>])/g, "$1"),
      status: (statusText ?? "UNKNOWN") as Proposal["status"],
      eligible: /^Tiers eligible to vote: (.*)$/m.exec(section)?.[1].match(/T[123]/g) ?? [],
      deadlineRequired: m[2].startsWith(`${GOV}:`),
    };
  });
}
export function proposalDetail(proposal: Proposal, detail: string): Proposal {
  const stats = detail.slice(detail.lastIndexOf("### Stats\n"));
  const yes = /^- YES PERCENT: ([0-9]+(?:\.[0-9]+)?)%\s*$/m.exec(stats);
  const status = stats.includes("**PROPOSAL HAS BEEN ACCEPTED**")
    ? "ACCEPTED"
    : stats.includes("**PROPOSAL HAS BEEN DENIED**")
      ? "REJECTED"
      : stats.includes("**Proposal is open for votes**")
        ? "ACTIVE"
        : "UNKNOWN";
  return { ...proposal, status, yesPercent: yes ? parseFloat(yes[1]) : undefined, detail };
}
const proposalTimes = new Map<string, number>();
const oldProposals = new Set<string>();
async function recentProposalTimes(ids: string[]): Promise<void> {
  if (ids.every((id) => proposalTimes.has(id) || oldProposals.has(id))) return;
  const head = await chainHead();
  const cutoff = head.time - 7200;
  let lo = 1n,
    hi = BigInt(head.height);
  while (lo < hi) {
    const mid = (lo + hi) / 2n;
    if (Date.parse((await blockAt(mid)).header.time) / 1000 < cutoff) lo = mid + 1n;
    else hi = mid;
  }
  const recent = await transactionBlocks(lo, BigInt(head.height));
  for (const block of recent) {
    for (const tx of await blockResults(block.header.height)) {
      if (vmFailure(tx)) continue;
      for (const id of outcomeEvents(chainEvents(tx)).proposals)
        proposalTimes.set(id, Date.parse(block.header.time) / 1000);
    }
  }
  // Only mark old after a COMPLETE scan; RPC errors never invent a deadline.
  for (const id of ids) if (!proposalTimes.has(id)) oldProposals.add(id);
}
export async function readProposals(): Promise<{ proposals: Proposal[]; warning?: string }> {
  const proposals: Proposal[] = [];
  let page: number | undefined = 1;
  for (let count = 0; page && count < 200; count++) {
    const text = await render(DAO, `?page=${page}`);
    const rows = proposalList(text);
    if (!rows.length && !text.includes("No proposals yet."))
      throw new Error("Unrecognized GovDAO proposal list. Open GovDAO on gnoweb to inspect it.");
    proposals.push(...rows);
    page = nextPage(text, page);
  }
  if (page) throw new Error("Proposal pagination exceeded 200 pages.");
  const open = await Promise.all(
    proposals
      .filter((p) => p.status === "ACTIVE")
      .map(async (p) => proposalDetail(p, await render(DAO, p.id))),
  );
  let warning: string | undefined;
  try {
    await recentProposalTimes(open.filter((p) => p.deadlineRequired).map((p) => p.id));
  } catch (error) {
    warning = `Proposal creation times unavailable: ${error instanceof Error ? error.message : String(error)}`;
  }
  return {
    proposals: open.map((p) => ({
      ...p,
      created: proposalTimes.get(p.id),
      expired: oldProposals.has(p.id),
    })),
    warning,
  };
}
export function proposalExecutable(proposal: Proposal, now: number, daoGoverns: boolean): boolean {
  if (
    proposal.status !== "ACTIVE" ||
    proposal.yesPercent === undefined ||
    proposal.yesPercent < 66.66
  )
    return false;
  if (!proposal.deadlineRequired) return true;
  return (
    daoGoverns &&
    !proposal.expired &&
    proposal.created !== undefined &&
    now <= proposalDeadline(proposal.created)
  );
}
