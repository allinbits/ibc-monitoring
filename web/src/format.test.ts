import { describe, expect, it } from "vitest";
import type { Denom } from "./api";
import { denomUnit, middleEllipsis, tokenUnit } from "./format";

const native = {
  denom: "ugnot",
  kind: "unescrow",
  decimals: 0,
  display_decimals: 6,
  token: "0xAB",
} as Denom;
const voucher = { denom: "ibc/hash", kind: "mint", decimals: 18, token: "0xCD" } as Denom;

describe("display formatting", () => {
  it("preserves short identifiers and both ends of long identifiers", () => {
    expect(middleEllipsis("client-0")).toBe("client-0");
    expect(middleEllipsis("0x123456789abcdef", 4, 4)).toBe("0x12…cdef");
    expect(middleEllipsis("123456789", 4, 0)).toBe("1234…");
    expect(middleEllipsis("")).toBe("");
  });
  it("uses API display decimals and governed voucher fallback", () => {
    expect(denomUnit(native)).toEqual({ decimals: 6, unit: "GNOT" });
    expect(denomUnit(voucher)).toEqual({ decimals: 18, unit: "voucher tokens" });
    expect(denomUnit({ ...native, denom: "custom", display_decimals: 8 })).toEqual({
      decimals: 8,
      unit: "tokens",
    });
    expect(denomUnit({ ...native, display_decimals: undefined })).toEqual({
      decimals: 0,
      unit: "base units",
    });
    expect(tokenUnit("unknown", [], 9)).toEqual({ decimals: 9, unit: "tokens" });
    expect(tokenUnit("0xcd", [voucher], 8)).toEqual({ decimals: 8, unit: "tokens" });
    expect(tokenUnit("0Xcd", [native, voucher])).toEqual({ decimals: 18, unit: "tokens" });
    expect(tokenUnit("unknown", [native, voucher])).toEqual({ decimals: 0, unit: "base units" });
    expect(tokenUnit("0xab", [native, { ...voucher, token: native.token }])).toEqual({
      decimals: 0,
      unit: "base units",
    });
  });
});
