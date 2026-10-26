import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { EthRecovery } from "../api";
import { glossaryText } from "../glossary";
import { RecoveryPanel } from "./RecoveryPanel";
import { recoveryDuration, recoveryStatus, recoveryThreshold } from "./recovery";

const zero = `0x${"0".repeat(64)}`;
const proposal = `0x${"1".repeat(64)}`;
const recovery: EthRecovery = {
  delay: 2592000,
  threshold: 3,
  cancel_threshold: 3,
  silence: 2592000,
  hold_period: 1209600,
  enabled: true,
  supported: true,
  disabled: false,
  status: "enabled",
  proposal: zero,
  opened_at: 0,
  cancel_votes: 0,
  approved: [zero, zero, zero, zero],
};
const render = (changes: Partial<EthRecovery> = {}) =>
  renderToStaticMarkup(createElement(RecoveryPanel, { recovery: { ...recovery, ...changes } }));

describe("emergency recovery panel", () => {
  it.each([
    [{}, "Enabled"],
    [{ enabled: false, disabled: true, status: "disabled" }, "Permanently disabled"],
    [{ enabled: false, delay: 0, status: "never_enabled" }, "Never enabled"],
    [{ supported: false }, "Not supported by this deployment"],
  ] satisfies [Partial<EthRecovery>, string][])("maps %j to %s", (changes, label) => {
    const value = { ...recovery, ...changes };
    expect(recoveryStatus(value).label).toBe(label);
    const html = render(changes);
    expect(html).toContain(label);
    expect(html).toContain("30d");
    expect(html).toContain("14d");
    expect(html).toContain("3 of 4");
    expect(html).toContain('aria-label="About Emergency recovery"');
  });
  it("keeps proposal times, cancellation votes and seat approvals on unsupported deployments", () => {
    const html = render({
      supported: false,
      proposal,
      opened_at: 1792618727,
      executable_at: 1792625927,
      cancel_votes: 2,
      approved: [proposal, zero, proposal, proposal],
    });
    expect(html).toContain("Permanent disable is unavailable on this deployment");
    expect(html).toContain("proposal open");
    expect(html).toContain('title="' + proposal + '"');
    expect(html).toContain("2026-10-21 21:38:47Z");
    expect(html).toContain("2026-10-21 23:38:47Z");
    expect(html).toContain("2 of 4");
    expect(html).toContain("3 of 4 guardian seats approved this proposal");
    expect(html).toContain("seat 1: none");
  });
  it("shows the disabling event, exact sequence, Sepolia transaction and residual risk", () => {
    const html = render({
      enabled: false,
      disabled: true,
      status: "disabled",
      disabled_by: { epoch: 2, seq: "18446744073709551615", block: 11998922, tx: proposal },
    });
    expect(html).toContain("Kind 14");
    expect(html).toContain("epoch 2 · seq 18446744073709551615 · block 11998922");
    expect(html).toContain(`href="https://sepolia.etherscan.io/tx/${proposal}"`);
    expect(html).toContain("After the disable (residual risk)");
    expect(html.replaceAll("&#x27;", "'")).toContain(
      glossaryText("After the disable (residual risk)"),
    );
    expect(render({ enabled: false, delay: 0, status: "never_enabled" })).not.toContain(
      "After the disable (residual risk)",
    );
  });
  it.each([
    [0, "0m"],
    [60, "1m"],
    [3598, "59m 58s"],
    [7200, "2h"],
    [86400, "1d"],
    [93780, "1d 2h 3m"],
    [2592000, "30d"],
  ])("formats %i seconds as %s without rounding away configured time", (seconds, text) => {
    expect(recoveryDuration(seconds)).toBe(text);
  });
  it("formats guardian thresholds as seat counts", () => {
    expect(recoveryThreshold(3)).toBe("3 of 4");
    expect(recoveryThreshold(4)).toBe("4 of 4");
  });
});
