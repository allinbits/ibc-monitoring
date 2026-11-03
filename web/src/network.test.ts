import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureNetwork, evmChainId, evmText, evmTxURL, network } from "./network";
import { arcNetwork, sepoliaNetwork } from "./test-network";
import { actionById } from "./governance/catalog";
import { executeEthereum, type EvmProvider } from "./governance/wallets";
import { defaultValues } from "./governance/actions";
import { validateParam } from "./governance/validation";
import { liveFixture } from "./governance/test-fixtures";
import { chainHead, readLiveState } from "./governance/rpc";
import { glossaryText } from "./glossary";
import { ClientsPage } from "./pages/Clients";
import { RecoveryPanel } from "./governance/RecoveryPanel";
import { api, type Client, type EthRecovery } from "./api";

beforeEach(() => configureNetwork(arcNetwork));
afterEach(() => {
  configureNetwork(sepoliaNetwork);
  vi.unstubAllGlobals();
});
const arcLive = {
  ...liveFixture,
  route: { ...liveFixture.route, route: "arc-1", client: "arc-1" },
};

describe("API-selected Arc route", () => {
  it("loads display and wallet settings from the API", async () => {
    configureNetwork(sepoliaNetwork);
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({ ok: true, json: async () => ({ network: arcNetwork }) })),
    );
    await api.info();
    expect(network.gno.chain_id).toBe("test-arc-1");
    expect(evmChainId()).toBe("0x4cef52");
    expect(evmTxURL("0xabc")).toBe("https://testnet.arcscan.app/tx/0xabc");
  });
  it("labels catalog actions with Arc while retaining realm builder names", () => {
    const a = actionById("eth.route_settings");
    expect(actionById("core.replace_verifier").description).toContain("Arc testnet");
    expect(a.tx.governor_source).toContain("NewEthRouteSettingsRequest");
    expect(a.ethereum_followup).toContain(arcNetwork.evm.admin_address);
    expect(a.ethereum_followup).not.toContain(sepoliaNetwork.evm.admin_address);
    expect(evmText("Sepolia · Ethereum route test-ibc-1 ethereum-1")).toBe(
      "Arc testnet · Arc testnet route test-arc-1 arc-1",
    );
    expect(glossaryText("Trusting period")).toContain("last consensus timestamp");
    expect(glossaryText("eth head age (~13 min)")).not.toContain("13 minutes");
  });
  it("accepts Arc verifier versions and keeps gno ids out of router fields", () => {
    const versionAction = actionById("core.replace_verifier");
    const param = versionAction.params.find((p) => p.name === "toVersion")!;
    expect(validateParam(versionAction, param, "arc/v1", {}, arcLive)).toBe("arc/v1");
    expect(() => validateParam(versionAction, param, "ethereum/v1", {}, arcLive)).toThrow();
    const a = actionById("eth.limits");
    const client = a.params.find((p) => p.type === "router client id")!;
    expect(validateParam(a, client, "client-0", {}, arcLive)).toBe("client-0");
    expect(() => validateParam(a, client, "arc-2", {}, arcLive)).toThrow();
    const defaults = defaultValues(actionById("eth.route_settings"), arcLive);
    expect(defaults.clientID).toBe("client-0");
    const target = defaultValues(actionById("eth.upgrade"), arcLive);
    expect(Object.values(target).join(" ")).not.toMatch(/0x[0-9a-fA-F]{40}/);
  });
  it("switches to Arc, adds native USDC and signs only the configured IBCAdmin", async () => {
    const hash = `0x${"ab".repeat(32)}`;
    let chain = "0xaa36a7";
    const request = vi.fn(async ({ method, params }: { method: string; params?: unknown[] }) => {
      if (method === "eth_chainId") return chain;
      if (method === "wallet_switchEthereumChain") {
        if (chain !== evmChainId()) {
          chain = evmChainId();
          throw { code: 4902 };
        }
        return null;
      }
      if (method === "eth_requestAccounts") return ["0x2222222222222222222222222222222222222222"];
      if (method === "eth_call") {
        expect((params![0] as { to: string }).to).toBe(arcNetwork.evm.admin_address);
        return "0x1";
      }
      if (method === "eth_sendTransaction") return hash;
      return null;
    });
    expect(await executeEthereum("1", { request } as EvmProvider, "1")).toBe(hash);
    expect(request).toHaveBeenCalledWith({
      method: "wallet_addEthereumChain",
      params: [
        {
          chainId: "0x4cef52",
          chainName: "Arc testnet",
          nativeCurrency: { name: "USDC", symbol: "USDC", decimals: 18 },
          rpcUrls: [arcNetwork.evm.rpc_url],
          blockExplorerUrls: [arcNetwork.evm.explorer_url],
        },
      ],
    });
    const sent = request.mock.calls.find(([p]) => p.method === "eth_sendTransaction")![0];
    expect(sent.params![0]).toMatchObject({ to: arcNetwork.evm.admin_address, value: "0x0" });
  });
  it("reads the configured gno RPC, chain and arc-1 route", async () => {
    const queries: string[] = [];
    const fetcher = vi.fn(async (url: string, options: { body: string }) => {
      expect(url).toBe(arcNetwork.gno.rpc_url);
      const { method, params } = JSON.parse(options.body);
      if (method === "status")
        return {
          ok: true,
          text: async () =>
            JSON.stringify({
              result: {
                node_info: { network: "test-arc-1" },
                sync_info: {
                  latest_block_height: "1",
                  latest_block_time: "2026-11-03T00:00:00Z",
                  catching_up: false,
                },
              },
            }),
        };
      const expr = atob(params.data);
      queries.push(expr);
      let value: string;
      if (expr.endsWith(":routes/arc-1")) value = JSON.stringify(arcLive.route);
      else if (expr.endsWith(":decisions")) value = "[]";
      else if (expr.endsWith(":"))
        value = [0, 1, 2, 3, 4, 5].map((n) => `- ${n} (guardian):`).join("\n");
      else if (expr.endsWith(".Governor()")) value = `("${arcLive.governor}" string)`;
      else if (expr.endsWith(".EpochClient()")) value = '("arc-1" string)';
      else if (expr.includes(".CounterpartyClientID(")) value = '("client-0" string)';
      else if (/GovDAOGoverns|Held/.test(expr)) value = "(false bool)";
      else value = "(1 uint64)";
      return {
        ok: true,
        text: async () => JSON.stringify({ result: { response: { Data: btoa(value) } } }),
      };
    });
    vi.stubGlobal("fetch", fetcher);
    const live = await readLiveState();
    expect(live.route.client).toBe("arc-1");
    expect(live.routerClient).toBe("client-0");
    expect(queries.some((q) => q.endsWith(":routes/arc-1"))).toBe(true);
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        text: async () =>
          JSON.stringify({
            result: {
              node_info: { network: "test-ibc-1" },
              sync_info: { catching_up: false },
            },
          }),
      })),
    );
    await expect(chainHead()).rejects.toThrow("wrong chain");
  });
  it("renders Arc consensus details and configured recovery explorer links", () => {
    const c = {
      chain: "gno",
      id: "arc-1",
      kind: "arc",
      status: "Active",
      latest_height: 123,
      head_time: "2026-11-03T00:00:00Z",
      expiry: "2026-11-17T00:00:00Z",
      trusting_period_ns: 1209600000000000,
      arc: {
        next_validators: [{ address: "AAAA", power: "9007199254740993" }],
        next_total_power: "9007199254740993",
        registry_storage_root: "FhcY",
        max_clock_drift: "90",
        trust_level_numerator: "1",
        trust_level_denominator: "3",
      },
      provenance: {
        chain: "gno",
        height: 1,
        verification: "cross-checked",
        state_verification: "none",
      },
    } as Client;
    const client = new QueryClient();
    client.setQueryData(["clients"], { gno: [c], ethereum: [] });
    const html = renderToStaticMarkup(
      createElement(QueryClientProvider, { client }, createElement(ClientsPage)),
    );
    expect(html).toContain("Arc testnet light client");
    expect(html).toContain("1 validators · power 9007199254740993");
    expect(html).toContain("FhcY");
    expect(html).not.toMatch(/Beacon slot|Sepolia|Ethereum/);
    client.clear();
    const recovery = {
      disabled: true,
      status: "disabled",
      supported: true,
      approved: [],
      disabled_by: { epoch: 1, seq: "1", block: 1, tx: "0xabc" },
    } as unknown as EthRecovery;
    const panel = renderToStaticMarkup(createElement(RecoveryPanel, { recovery }));
    expect(panel).toContain("https://testnet.arcscan.app/tx/0xabc");
    expect(panel).not.toContain("Sepolia");
  });
});
