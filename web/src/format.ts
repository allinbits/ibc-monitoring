import type { Denom } from "./api";

/** Keep both ends of an identifier so similarly prefixed values remain distinct. */
export function middleEllipsis(value: string, start = 12, end = 6): string {
  return value.length > start + end + 1
    ? `${value.slice(0, start)}…${end ? value.slice(-end) : ""}`
    : value;
}

export function denomUnit(d: Denom): { decimals: number; unit: string } {
  const decimals = d.display_decimals ?? (d.kind === "mint" ? d.decimals : 0);
  const unit =
    d.denom === "ugnot" && decimals === 6
      ? "GNOT"
      : decimals === 0
        ? "base units"
        : d.kind === "mint"
          ? "voucher tokens"
          : "tokens";
  return { decimals, unit };
}

/** Prefer the token's own precision; older snapshots can use the matched denom. */
export function tokenUnit(token: string, denoms: Denom[] = [], tokenDecimals?: number) {
  const matches = denoms.filter((d) => d.token?.toLowerCase() === token.toLowerCase());
  if (tokenDecimals !== undefined) {
    const unit =
      matches.some((d) => d.denom === "ugnot") && tokenDecimals === 6 ? "GNOT" : "tokens";
    return { decimals: tokenDecimals, unit };
  }
  if (matches.length === 0) return { decimals: 0, unit: "base units" };
  const first = denomUnit(matches[0]);
  return matches.every((d) => denomUnit(d).decimals === first.decimals)
    ? { ...first, unit: first.unit === "voucher tokens" ? "tokens" : first.unit }
    : { decimals: 0, unit: "base units" };
}
