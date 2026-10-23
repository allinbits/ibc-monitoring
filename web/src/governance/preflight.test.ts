import { afterEach, describe, expect, it, vi } from "vitest";
import { actionById } from "./catalog";
import raw from "./catalog.json";
import { preflightAction } from "./actions";
import { validateValues } from "./validation";
import { liveFixture, actionFixture } from "./test-fixtures";

afterEach(() => vi.unstubAllGlobals());

describe("live preflight security checks", () => {
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
