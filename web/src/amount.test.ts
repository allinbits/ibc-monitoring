import { describe, expect, it } from "vitest";
import { formatAmount, parseAmount, percent } from "./amount";

describe("amount", () => {
  it("parses exactly above 2^53 and int64", () => {
    const big = "340282366920938463463374607431768211455";
    expect(parseAmount(big).toString()).toBe(big);
    expect(() => parseAmount("1.5")).toThrow();
    expect(() => parseAmount("1e3")).toThrow();
    expect(() => parseAmount("")).toThrow();
  });
  it("formats with decimals and grouping", () => {
    expect(formatAmount("200000000000", 6)).toBe("200,000");
    expect(formatAmount("20000000", 6)).toBe("20");
    expect(formatAmount("1234567", 6)).toBe("1.234567");
    expect(formatAmount("1234567", 6, 2)).toBe("1.23");
    expect(formatAmount("-15", 0)).toBe("-15");
    expect(formatAmount("340282366920938463463374607431768211455", 0)).toBe(
      "340,282,366,920,938,463,463,374,607,431,768,211,455",
    );
  });
  it("computes percentages without Number on amounts", () => {
    expect(percent("4000000000", "20000000000")).toBe(20);
    expect(percent("30", "20")).toBe(100);
    expect(percent("1", "0")).toBeNull();
  });
  it("preserves all 18 voucher decimals, including a single base unit", () => {
    expect(formatAmount("1000000000000000000000000", 18)).toBe("1,000,000");
    expect(formatAmount("1", 18)).toBe("0.000000000000000001");
    expect(formatAmount("1234567890123456789", 18)).toBe("1.234567890123456789");
    expect(formatAmount((2n ** 256n - 1n).toString(), 18)).toBe(
      "115,792,089,237,316,195,423,570,985,008,687,907,853,269,984,665,640,564,039,457.584007913129639935",
    );
  });
});
