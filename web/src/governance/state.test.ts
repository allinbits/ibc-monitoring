import { afterEach, describe, expect, it, vi } from "vitest";
import raw from "./catalog.json";
import { DAO, GOV } from "./catalog";
import {
  decodeQueryResponse,
  detectRoles,
  evalBool,
  evalInteger,
  evalString,
  memberRows,
  nextPage,
  parseJson,
  readMembers,
} from "./rpc";
import {
  chainEvents,
  decisionDue,
  findCommittedTx,
  readProposals,
  eventAttr,
  outcomeEvents,
  proposalDeadline,
  proposalDetail,
  proposalExecutable,
  proposalList,
  secondsRemaining,
  timeRemaining,
  transactionHash,
  type Proposal,
} from "./followups";
import {
  connectAdena,
  executeCalldata,
  executeEthereum,
  ethereumTransaction,
  signAdena,
  type AdenaResponse,
} from "./wallets";

const addr = raw.meta.live_state.governor;
const member = raw.meta.live_state.govdao_members[0];
const membersMarkdown = `# Memberstore Govdao v0\n| [Tier](?sort-desc=Tier) | Address |\n| --- | --- |\n| ![T1 colored chip](data:image/svg+xml;base64,NOTHTML) T1 | ${member} |\n`;
const memberTable = memberRows(membersMarkdown);
const response = <T>(data: T): AdenaResponse<T> => ({
  code: 0,
  status: "success",
  type: "",
  message: "",
  data,
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("catalog response parsing and role detection", () => {
  it("parses qeval strings, addresses, booleans, seat / yield integers exactly", () => {
    expect(evalString(`("${addr}" .uverse.address)`)).toBe(addr);
    expect(evalString('( "x" string)'.replace("( ", "("))).toBe("x");
    expect(evalInteger("(18446744073709551615 uint64)")).toBe("18446744073709551615");
    expect(evalInteger("(-1 int)")).toBe("-1");
    expect(evalBool("(true bool)")).toBe(true);
    expect(evalBool("(false bool)")).toBe(false);
    for (const bad of ["true", "(true string)", "(0 int64)"]) expect(() => evalBool(bad)).toThrow();
    expect(() => evalInteger("(1.0 int)")).toThrow();
    expect(() => evalString("(client-0 string)")).toThrow();
  });
  it("detects all roles and never treats a yielded governor as governor", () => {
    expect(detectRoles(addr, addr, "0", false, "(-1 int)", memberTable).roles).toEqual([
      "anyone",
      "governor",
    ]);
    expect(detectRoles(addr, addr, "123", true, "(-1 int)", memberTable).roles).toEqual(["anyone"]);
    expect(detectRoles(member, addr, "0", false, "(0 int)", memberTable)).toMatchObject({
      tier: "T1",
      seat: 0,
      roles: ["anyone", "govdao", "guardian"],
    });
    expect(detectRoles(member, addr, "0", false, "(3 int)", []).roles).toContain("guardian");
    for (const seat of [4, 5])
      expect(detectRoles(addr, "", "0", true, `(${seat} int)`, []).roles).toEqual([
        "anyone",
        "watch",
      ]);
    expect(() => detectRoles(addr, addr, "0", false, "(6 int)", [])).toThrow();
    expect(memberRows(membersMarkdown.replace(member, `${member}x`))).toEqual([]);
    expect(memberRows(membersMarkdown.replace(member, `${addr.slice(0, -1)}q`))).toEqual([]);
    expect(memberRows(`arbitrary description ${member} T1`)).toEqual([]);
  });
  it("paginates the members table instead of missing members beyond page one", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        text: async () =>
          JSON.stringify({
            result: {
              response: { ResponseBase: { Data: btoa(`${membersMarkdown}[next](?page=2)`) } },
            },
          }),
      })
      .mockResolvedValueOnce({
        ok: true,
        text: async () =>
          JSON.stringify({
            result: {
              response: {
                ResponseBase: {
                  Data: btoa(membersMarkdown.replace(member, addr).replaceAll("T1", "T2")),
                },
              },
            },
          }),
      });
    vi.stubGlobal("fetch", fetcher);
    expect(await readMembers()).toEqual([
      { address: member, tier: "T1" },
      { address: addr, tier: "T2" },
    ]);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(nextPage("[1](?page=1) [2](?page=2) [4](?page=4)", 2)).toBe(4);
  });
  it("decodes both RPC envelopes, rejects VM failures, and preserves unsafe numbers without changing decision text", () => {
    const value = '[{"seq":18446744073709551615,"text":"seq 18446744073709551615","due":7200}]';
    expect(parseJson(value)).toEqual([
      { seq: "18446744073709551615", text: "seq 18446744073709551615", due: 7200 },
    ]);
    expect(decodeQueryResponse({ ResponseBase: { Data: btoa("(0 int64)"), Error: null } })).toBe(
      "(0 int64)",
    );
    expect(decodeQueryResponse({ data: btoa("hello"), code: 0 })).toBe("hello");
    expect(() =>
      decodeQueryResponse({
        ResponseBase: { Error: { type: "vm" }, Log: "refused", Data: btoa("ok") },
      }),
    ).toThrow(/refused/);
    expect(() => decodeQueryResponse({})).toThrow();
  });
});

describe("decisions, GovDAO and event outcomes", () => {
  const list = `# GovDAO\n## Proposals\n### [Prop #0 - ${GOV}: core pause](/r/gov/dao:0)\nAuthor: ${member}\n\nStatus: ACTIVE\n\nTiers eligible to vote: T1, T2\n\n---\n\n### [Prop #1 - a direct proposal](/r/gov/dao:1)\nAuthor: ${member}\n\nStatus: ACCEPTED\n\nTiers eligible to vote: T1\n`;
  const detail = `## Prop #0 - ${GOV}: core pause\n\n### Stats\n- **Proposal is open for votes**\n- Tiers eligible to vote: T1, T2\n- YES PERCENT: 66.66666666666667%\n- NO PERCENT: 0%\n- ABSTAIN PERCENT: 0%\n`;
  it("parses the deployed GovDAO markdown list and stats", () => {
    const parsed = proposalList(list);
    expect(parsed).toHaveLength(2);
    expect(parsed[0]).toMatchObject({
      id: "0",
      status: "ACTIVE",
      eligible: ["T1", "T2"],
      deadlineRequired: true,
    });
    expect(parsed[1].deadlineRequired).toBe(false);
    const p = proposalDetail(parsed[0], detail);
    expect(p.yesPercent).toBeCloseTo(66.6666);
    expect(p.status).toBe("ACTIVE");
    expect(proposalList("# GovDAO\nNo proposals yet.")).toEqual([]);
    expect(proposalDetail(parsed[0], "unrecognized data").status).toBe("UNKNOWN");
  });
  it("uses exact due and deadline boundaries, including the testnet's 7200 seconds", () => {
    expect(decisionDue("pending", 100, 99)).toBe(false);
    expect(decisionDue("pending", 100, 100)).toBe(true);
    expect(decisionDue("spent", 100, 101)).toBe(false);
    expect(decisionDue("pending", 0, 100)).toBe(false);
    expect(proposalDeadline(100)).toBe(7300);
    expect(secondsRemaining(7300, 100)).toBe(7200);
    expect(secondsRemaining(7300, 7301)).toBe(0);
    expect(timeRemaining(7300, 100)).toBe("2h 0m 0s");
    const p: Proposal = { ...proposalList(list)[0], created: 100, yesPercent: 66.6667 };
    expect(proposalExecutable(p, 7300, true)).toBe(true);
    expect(proposalExecutable(p, 7301, true)).toBe(false);
    expect(proposalExecutable(p, 7300, false)).toBe(false);
    expect(proposalExecutable({ ...p, created: undefined }, 100, true)).toBe(false);
    expect(proposalExecutable({ ...p, yesPercent: 33.33 }, 100, true)).toBe(false);
    expect(
      proposalExecutable({ ...p, deadlineRequired: false, created: undefined }, 10000, false),
    ).toBe(true);
  });
  it("extracts only authentic realm events, including a SPENT decision in a successful tx", () => {
    const result = {
      ResponseBase: {
        Events: [
          {
            type: "Decision",
            pkg_path: GOV,
            attrs: [
              { key: "id", value: "11" },
              { key: "state", value: "spent" },
            ],
          },
          { type: "ProposalCreated", pkg_path: DAO, attrs: [{ key: "id", value: "0" }] },
          {
            type: "GovPacketSent",
            pkg_path: GOV,
            attrs: [
              { key: "epoch", value: "1" },
              { key: "seq", value: "18446744073709551615" },
            ],
          },
          {
            type: "Decision",
            pkg_path: "gno.land/r/untrusted/fake",
            attrs: [{ key: "id", value: "999" }],
          },
          { "@type": "/tm.StorageDepositEvent", bytes_delta: "123" },
        ],
      },
    };
    const events = chainEvents(result);
    expect(events).toHaveLength(4);
    expect(eventAttr(events[0], "id")).toBe("11");
    expect(outcomeEvents(events)).toMatchObject({
      decisions: [{ id: "11", state: "spent" }],
      proposals: ["0"],
      dispatches: [{ epoch: "1", seq: "18446744073709551615" }],
    });
    expect(outcomeEvents(chainEvents({})).decisions).toEqual([]);
  });
  it("computes the committed transaction hash from block bytes", async () => {
    expect(await transactionHash(btoa("abc"))).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
  });
});

describe("confirmation on an RPC without a transaction index", () => {
  it("finds the exact transaction in blocks and reads its committed failure / events", async () => {
    const fetcher = vi.fn(async (_url: string, options: { body: string }) => {
      const { method, params } = JSON.parse(options.body);
      let result: unknown;
      if (method === "status")
        result = {
          node_info: { network: "test-ibc-1" },
          sync_info: {
            latest_block_height: "3",
            latest_block_time: "2026-10-23T00:00:00Z",
            catching_up: false,
          },
        };
      else if (method === "block")
        result = {
          block: {
            header: {
              height: params.height,
              time: "2026-10-23T00:00:00Z",
              total_txs: params.height === "3" ? "2" : "1",
            },
            data: {
              txs:
                params.height === "1"
                  ? [btoa("abc")]
                  : params.height === "3"
                    ? [btoa("def")]
                    : null,
            },
          },
        };
      else if (method === "block_results")
        result = {
          results: {
            deliver_tx: [
              { ResponseBase: { Error: { type: "vm" }, Log: "execution refused", Events: [] } },
            ],
          },
        };
      else throw new Error(`Unexpected RPC ${method}`);
      return { ok: true, text: async () => JSON.stringify({ result }) };
    });
    vi.stubGlobal("fetch", fetcher);
    const committed = await findCommittedTx(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
      "1",
    );
    expect(committed).toMatchObject({ height: "1", failure: "execution refused", events: [] });
    expect(
      fetcher.mock.calls.every(
        ([, options]) => !["tx", "tx_search"].includes(JSON.parse(options.body).method),
      ),
    ).toBe(true);
  });
  it("recovers proposal creation block time from ProposalCreated events for the countdown", async () => {
    // Use disjoint heights so the read-only block cache from the previous test
    // has no overlap with this independent synthetic chain range.
    const list = `### [Prop #17 - ${GOV}: core pause](/r/gov/dao:17)\nStatus: ACTIVE\nTiers eligible to vote: T1\n`;
    const detail = "### Stats\n- **Proposal is open for votes**\n- YES PERCENT: 66.67%\n";
    const fetcher = vi.fn(async (_url: string, options: { body: string }) => {
      const { method, params } = JSON.parse(options.body);
      let result: unknown;
      if (method === "status")
        result = {
          node_info: { network: "test-ibc-1" },
          sync_info: {
            latest_block_height: "103",
            latest_block_time: new Date(14000 * 1000).toISOString(),
            catching_up: false,
          },
        };
      else if (method === "abci_query") {
        const expression = atob(params.data);
        result = {
          response: { ResponseBase: { Data: btoa(expression.endsWith(":17") ? detail : list) } },
        };
      } else if (method === "block") {
        const height = BigInt(params.height);
        const time = height < 100n ? 6000 : height === 100n ? 8000 : 14000;
        result = {
          block: {
            header: {
              height: params.height,
              time: new Date(time * 1000).toISOString(),
              total_txs: height < 100n ? "0" : "1",
            },
            data: { txs: height === 100n ? [btoa("proposal")] : null },
          },
        };
      } else if (method === "block_results")
        result = {
          results: {
            deliver_tx: [
              {
                ResponseBase: {
                  Error: null,
                  Events: [
                    { type: "ProposalCreated", pkg_path: DAO, attrs: [{ key: "id", value: "17" }] },
                  ],
                },
              },
            ],
          },
        };
      else throw new Error(`Unexpected RPC ${method}`);
      return { ok: true, text: async () => JSON.stringify({ result }) };
    });
    vi.stubGlobal("fetch", fetcher);
    const result = await readProposals();
    expect(result.warning).toBeUndefined();
    expect(result.proposals[0]).toMatchObject({ id: "17", created: 8000, yesPercent: 66.67 });
    expect(timeRemaining(proposalDeadline(result.proposals[0].created!), 14000)).toBe("0h 20m 0s");
  });
});

describe("Adena connection and MetaMask calldata", () => {
  it("adds and switches test-ibc-1, handling extension rejection envelopes", async () => {
    const adena = {
      AddEstablish: vi.fn().mockRejectedValue({ type: "ALREADY_CONNECTED" }),
      SwitchNetwork: vi
        .fn()
        .mockRejectedValueOnce({ type: "UNADDED_NETWORK" })
        .mockResolvedValueOnce(response({})),
      AddNetwork: vi.fn().mockResolvedValue(response({})),
      GetAccount: vi.fn().mockResolvedValue(response({ address: addr, chainId: "test-ibc-1" })),
    };
    vi.stubGlobal("window", { adena });
    expect((await connectAdena()).address).toBe(addr);
    expect(adena.AddNetwork).toHaveBeenCalledWith({
      chainId: "test-ibc-1",
      chainName: "gno.land · test-ibc-1",
      rpcUrl: "https://rpc.2-29-108-12.sslip.io",
    });
    expect(adena.SwitchNetwork).toHaveBeenCalledTimes(2);
  });
  it("rejects signing when the wallet switches account, before invoking DoContract", async () => {
    const adena = {
      GetAccount: vi.fn().mockResolvedValue(response({ address: member, chainId: "test-ibc-1" })),
      DoContract: vi.fn(),
    };
    vi.stubGlobal("window", { adena });
    await expect(
      signAdena(addr, {
        messages: [
          {
            type: "/vm.m_call",
            value: {
              caller: addr,
              send: "",
              pkg_path: GOV,
              func: "Yield",
              args: [],
              max_deposit: "",
            },
          },
        ],
        memo: "",
      }),
    ).rejects.toThrow(/changed/);
    expect(adena.DoContract).not.toHaveBeenCalled();
  });
  it("hand-encodes execute(uint64) exactly, with fixed selector and uint64 bounds", () => {
    expect(executeCalldata("1")).toBe(`0xb590be77${"0".repeat(63)}1`);
    expect(executeCalldata("18446744073709551615")).toBe(
      `0xb590be77${"0".repeat(48)}ffffffffffffffff`,
    );
    for (const bad of ["0", "-1", "1e1", "18446744073709551616", "1);", "１"])
      expect(() => executeCalldata(bad)).toThrow();
    expect(ethereumTransaction(`0x${"1".repeat(40)}`, "3")).toEqual({
      from: `0x${"1".repeat(40)}`,
      to: "0x3D8B06752b98A3D5ee5368870174B278Cc7a1442",
      value: "0x0",
      data: executeCalldata("3"),
    });
  });
  it("switches Sepolia, verifies epoch and simulates before eth_sendTransaction", async () => {
    let chain = "0x1";
    const request = vi.fn(async ({ method, params }: { method: string; params?: unknown[] }) => {
      if (method === "eth_chainId") return chain;
      if (method === "wallet_switchEthereumChain") {
        chain = "0xaa36a7";
        return null;
      }
      if (method === "eth_requestAccounts") return [`0x${"1".repeat(40)}`];
      if (method === "eth_call")
        return (params?.[0] as { data?: string }).data === "0x900cf0cf" ? "0x1" : "0x";
      if (method === "eth_sendTransaction") return `0x${"a".repeat(64)}`;
      throw new Error("unexpected RPC");
    });
    expect(await executeEthereum("1", { request }, "1")).toBe(`0x${"a".repeat(64)}`);
    expect(request.mock.calls.map(([args]) => args.method)).toEqual([
      "eth_chainId",
      "wallet_switchEthereumChain",
      "eth_requestAccounts",
      "eth_chainId",
      "eth_call",
      "eth_call",
      "eth_sendTransaction",
    ]);
    request.mockClear();
    await expect(executeEthereum("1", { request }, "2")).rejects.toThrow(/epoch/);
    expect(request.mock.calls.some(([args]) => args.method === "eth_sendTransaction")).toBe(false);
  });
});
