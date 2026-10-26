import { afterEach, describe, expect, it, vi } from "vitest";
import { actionById } from "./catalog";
import raw from "./catalog.json";
import { preflightAction } from "./actions";
import { validateValues } from "./validation";
import { liveFixture, actionFixture } from "./test-fixtures";

afterEach(() => vi.unstubAllGlobals());

function deploymentFetch(functions: string, recovery: Record<string, unknown>) {
  const fetcher = vi.fn().mockImplementation(async (url: string) =>
    url === "/api/v1/governance"
      ? { ok: true, json: async () => ({ ethereum: { recovery } }) }
      : {
          ok: true,
          text: async () =>
            JSON.stringify({
              result: { response: { ResponseBase: { Data: btoa(functions) } } },
            }),
        },
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("live preflight security checks", () => {
  it("requires the live current router client for eth.disableRecovery", async () => {
    const action = actionById("eth.disableRecovery");
    expect(validateValues(action, { clientID: "client-0" }, liveFixture)).toEqual({
      clientID: "client-0",
    });
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    for (const clientID of ["ethereum-1", "client-1", "wrong-custom", ""]) {
      await expect(
        preflightAction(action, { clientID }, liveFixture, liveFixture.roles),
      ).rejects.toThrow();
    }
    expect(fetcher).not.toHaveBeenCalled();
    expect(
      validateValues(
        action,
        { clientID: "new-router" },
        { ...liveFixture, routerClient: "new-router" },
      ),
    ).toEqual({ clientID: "new-router" });
  });
  it("refuses an unavailable required builder before reading recovery or building a transaction", async () => {
    vi.resetModules();
    const { preflightAction: check } = await import("./actions");
    const fetcher = deploymentFetch('[{"FuncName":"NewEthReopenRequest"}]', {});
    await expect(
      check(
        actionById("eth.disableRecovery"),
        { clientID: "client-0" },
        liveFixture,
        liveFixture.roles,
      ),
    ).rejects.toThrow(
      "Not available on this deployment (the realm predates NewEthDisableRecoveryRequest)",
    );
    expect(fetcher).toHaveBeenCalledTimes(1);
    const request = JSON.parse(fetcher.mock.calls[0][1].body);
    expect(request.params.path).toBe("vm/qfuncs");
    expect(atob(request.params.data)).toBe("gno.land/r/aib/ibc/ibcgov");
  });
  it.each([
    [{ status: "disabled", disabled: true, supported: true }, /already permanently disabled/],
    [{ status: "never_enabled", disabled: false, supported: true }, /never enabled/],
    [{ status: "enabled", disabled: false, supported: false }, /IBCAdmin predates/],
  ])("refuses kind 14 against the freshly read recovery state %j", async (recovery, error) => {
    vi.resetModules();
    const { preflightAction: check } = await import("./actions");
    deploymentFetch('[{"FuncName":"NewEthDisableRecoveryRequest"}]', recovery);
    await expect(
      check(
        actionById("eth.disableRecovery"),
        { clientID: "client-0" },
        liveFixture,
        liveFixture.roles,
      ),
    ).rejects.toThrow(error);
  });
  it("rechecks recovery before signing even when deployment detection is cached", async () => {
    vi.resetModules();
    const { preflightAction: check } = await import("./actions");
    const recovery = { status: "enabled", disabled: false, supported: true };
    const fetcher = deploymentFetch('[{"FuncName":"NewEthDisableRecoveryRequest"}]', recovery);
    const action = actionById("eth.disableRecovery");
    await expect(
      check(action, { clientID: "client-0" }, liveFixture, liveFixture.roles),
    ).resolves.toMatchObject({ routerClient: "client-0" });
    recovery.status = "disabled";
    recovery.disabled = true;
    await expect(
      check(action, { clientID: "client-0" }, liveFixture, liveFixture.roles),
    ).rejects.toThrow(/already permanently disabled/);
    expect(fetcher.mock.calls.filter(([url]) => url === "/api/v1/governance")).toHaveLength(2);
    expect(fetcher).toHaveBeenCalledTimes(3);
  });
  it("allows a detection error with a warning when recovery is enabled", async () => {
    vi.resetModules();
    const { preflightAction: check } = await import("./actions");
    const { detectRequirement } = await import("./availability");
    deploymentFetch("unexpected response", { status: "enabled", disabled: false, supported: true });
    const action = actionById("eth.disableRecovery");
    await expect(
      check(action, { clientID: "client-0" }, liveFixture, liveFixture.roles),
    ).resolves.toMatchObject({ routerClient: "client-0" });
    expect((await detectRequirement(action)).warning).toContain(
      "Could not verify deployment support",
    );
  });
  it("rejects invalid source input before it can enter a query or wallet request", async () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    const action = actionById("core.add_member");
    await expect(
      preflightAction(action, { addr: '");panic("owned");//' }, liveFixture, [
        "anyone",
        "governor",
      ]),
    ).rejects.toThrow();
    expect(fetcher).not.toHaveBeenCalled();
  });
  it("checks the live custom-id pairing even when the older deployed realm would accept it", async () => {
    const action = actionById("eth.client");
    const values = { ...actionFixture(action), counterpartyClientID: "ethereum-1" };
    const fetcher = vi.fn().mockResolvedValue({
      ok: true,
      text: async () =>
        JSON.stringify({
          result: { response: { ResponseBase: { Data: btoa('("client-0" string)') } } },
        }),
    });
    vi.stubGlobal("fetch", fetcher);
    await expect(
      preflightAction(
        action,
        values,
        { ...liveFixture, counterparties: undefined },
        liveFixture.roles,
      ),
    ).rejects.toThrow(/paired/);
    expect(fetcher).toHaveBeenCalledTimes(1);
    const request = JSON.parse(fetcher.mock.calls[0][1].body);
    expect(atob(request.params.data)).toBe(
      'gno.land/r/aib/ibc/core.CounterpartyClientID("ethereum-1")',
    );
  });
  it("cannot bypass live voucher controls by using a token-address alias instead of ibc/hash", async () => {
    const action = actionById("denom.set");
    const denom = Object.keys(raw.meta.live_state.denoms).find((s) => s.startsWith("ibc/"))!;
    const current = {
      ...liveFixture.route.denoms[0],
      denom,
      kind: "mint",
      decimals: 18,
      supply_cap: "1000000000000000000000000",
      send_min: "0",
      send_max: "0",
      remote: "",
    };
    const live = { ...liveFixture, route: { ...liveFixture.route, denoms: [current] } };
    const values = {
      route: "ethereum-1",
      denom: raw.meta.live_state.ethereum.test_token,
      kind: "mint",
      capFloor: "50000000000",
      supplyCap: current.supply_cap,
      fee: "1000000",
      feeFloor: "100000",
      sendMax: "0",
      sendMin: "0",
      refundCap: "100000000000",
      remoteToken: "",
      decimals: "18",
    };
    await expect(preflightAction(action, values, live, liveFixture.roles)).rejects.toThrow(
      /launch/,
    );
  });
  it("rejects coin recapitalization amounts that do not fit int64", () => {
    const action = actionById("recapitalize");
    expect(() =>
      validateValues(
        action,
        { ...actionFixture(action), amount: (1n << 63n).toString() },
        liveFixture,
      ),
    ).toThrow(/int64/);
  });
});
