import { describe, expect, it, vi } from "vitest";
import type { EthRecovery } from "../api";
import { actionById } from "./catalog";
import {
  actionAvailability,
  createRequirementDetector,
  parseFunctions,
  resolveRequirement,
} from "./availability";

const action = actionById("eth.disableRecovery");
const enabled = {
  enabled: true,
  supported: true,
  disabled: false,
  status: "enabled",
} as EthRecovery;

describe("catalog requires detection", () => {
  it("resolves package paths, realm import names and explicit import aliases generically", () => {
    expect(resolveRequirement(action)).toEqual({
      realm: "gno.land/r/aib/ibc/ibcgov",
      func: "NewEthDisableRecoveryRequest",
    });
    expect(resolveRequirement({ ...action, requires: "gno.land/p/demo/v1.Build" })).toEqual({
      realm: "gno.land/p/demo/v1",
      func: "Build",
    });
    expect(
      resolveRequirement({
        ...action,
        requires: "other.Build",
        tx: { type: "/vm.m_run", source: 'import other "gno.land/r/demo/builders"' },
      }),
    ).toEqual({ realm: "gno.land/r/demo/builders", func: "Build" });
  });
  it("recognizes the actual qfuncs JSON shape using exact function names", async () => {
    const read = vi
      .fn()
      .mockResolvedValue(
        JSON.stringify([{ FuncName: "NewEthDisableRecoveryRequest", Params: [], Results: [] }]),
      );
    const result = await createRequirementDetector(read)(action);
    expect(result.status).toBe("available");
    expect(read).toHaveBeenCalledWith("vm/qfuncs", "gno.land/r/aib/ibc/ibcgov");
    expect(actionAvailability(action, result, enabled)).toEqual({ warning: undefined });
  });
  it("disables an absent function instead of offering its form", async () => {
    const read = vi
      .fn()
      .mockResolvedValue(
        JSON.stringify([
          { FuncName: "NewEthReopenRequest" },
          { FuncName: "NewEthDisableRecoveryRequestLater" },
        ]),
      );
    const result = await createRequirementDetector(read)(action);
    expect(result.status).toBe("unavailable");
    expect(actionAvailability(action, result, enabled).disabled).toBe(
      "Not available on this deployment (the realm predates NewEthDisableRecoveryRequest)",
    );
    expect(parseFunctions("[]")).toEqual([]);
  });
  it.each([
    "not JSON",
    "null",
    "{}",
    '[{"Name":"NewEthDisableRecoveryRequest"}]',
    '[{"FuncName":42}]',
  ])("treats malformed detection (%s) as unknown and allows with a warning", async (text) => {
    const result = await createRequirementDetector(vi.fn().mockResolvedValue(text))(action);
    expect(result.status).toBe("unknown");
    const availability = actionAvailability(action, result, enabled);
    expect(availability.disabled).toBeUndefined();
    expect(availability.warning).toContain("Could not verify deployment support");
  });
  it("allows with a warning on query failure and caches the unknown result", async () => {
    const read = vi.fn().mockRejectedValue(new Error("RPC unavailable"));
    const detect = createRequirementDetector(read);
    const result = await detect(action);
    expect(result.status).toBe("unknown");
    expect(result.warning).toContain("RPC unavailable");
    expect(actionAvailability(action, result, enabled).disabled).toBeUndefined();
    await detect(action);
    expect(read).toHaveBeenCalledTimes(1);
  });
  it("shares an in-flight function list between actions in the same realm for the session", async () => {
    const read = vi.fn().mockResolvedValue('[{"FuncName":"NewEthReopenRequest"}]');
    const detect = createRequirementDetector(read);
    const other = { ...action, requires: "ibcgov.NewEthReopenRequest" };
    const results = await Promise.all([detect(action), detect(other), detect(action)]);
    expect(results.map((r) => r.status)).toEqual(["unavailable", "available", "unavailable"]);
    await detect(other);
    expect(read).toHaveBeenCalledTimes(1);
    await detect(actionById("eth.reopen"));
    expect(read).toHaveBeenCalledTimes(1);
  });
});

describe("kind 14 recovery state gating", () => {
  it.each(["disabled", "never_enabled"] as const)(
    "disables kind 14 when recovery is %s",
    (status) => {
      const recovery = { ...enabled, enabled: false, status };
      const availability = actionAvailability(action, { status: "available" }, recovery);
      expect(availability.disabled).toContain("nothing to disable");
      expect(
        actionAvailability(action, { status: "unknown", warning: "RPC unavailable" }, recovery)
          .disabled,
      ).toContain("nothing to disable");
      expect(
        actionAvailability(actionById("eth.reopen"), undefined, recovery).disabled,
      ).toBeUndefined();
    },
  );
  it("honors the latch, unsupported contracts and the pending deployment check", () => {
    expect(
      actionAvailability(action, { status: "available" }, { ...enabled, disabled: true }).disabled,
    ).toContain("already permanently disabled");
    expect(
      actionAvailability(action, { status: "available" }, { ...enabled, supported: false })
        .disabled,
    ).toContain("IBCAdmin predates");
    expect(actionAvailability(action, undefined, enabled).disabled).toBe(
      "Checking deployment support…",
    );
  });
});
