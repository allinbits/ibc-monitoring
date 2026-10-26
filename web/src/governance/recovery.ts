import type { EthRecovery } from "../api";

export function recoveryStatus(recovery: EthRecovery) {
  if (recovery.supported === false)
    return {
      label: "Not supported by this deployment",
      kind: "neutral",
      term: "Unsupported by this deployment",
    };
  if (recovery.disabled || recovery.status === "disabled")
    return { label: "Permanently disabled", kind: "neutral", term: "Permanently disabled" };
  if (recovery.status === "never_enabled")
    return { label: "Never enabled", kind: "neutral", term: "Recovery status" };
  return { label: "Enabled", kind: "ok", term: "Recovery enabled" };
}

export function recoveryDuration(seconds: number): string {
  const parts: string[] = [];
  let remaining = seconds;
  for (const [unit, size] of [
    ["d", 86400],
    ["h", 3600],
    ["m", 60],
    ["s", 1],
  ] as const) {
    const value = Math.floor(remaining / size);
    if (value) parts.push(`${value}${unit}`);
    remaining %= size;
  }
  return parts.join(" ") || "0m";
}

export function recoveryThreshold(votes: number): string {
  return `${votes} of 4`;
}
