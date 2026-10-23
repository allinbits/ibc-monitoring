// Exact amount handling: decimal strings in, bigint inside, strings out.
// No Number is ever involved (docs/design.md §1).

const DIGITS = /^-?\d+$/;

/** Parses a decimal string amount; throws on anything else. */
export function parseAmount(s: string): bigint {
  if (!DIGITS.test(s)) throw new Error(`not an integer amount: ${JSON.stringify(s)}`);
  return BigInt(s);
}

/** Formats base units with `decimals` fractional digits, grouping thousands. */
export function formatAmount(s: string | bigint, decimals = 6, maxFraction = decimals): string {
  const v = typeof s === "bigint" ? s : parseAmount(s);
  const neg = v < 0n;
  const abs = neg ? -v : v;
  const base = 10n ** BigInt(decimals);
  const whole = abs / base;
  let frac = (abs % base).toString().padStart(decimals, "0");
  frac = frac.slice(0, maxFraction).replace(/0+$/, "");
  const wholeStr = whole.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return `${neg ? "-" : ""}${wholeStr}${frac ? "." + frac : ""}`;
}

/** Percentage of part over total as an integer 0–100 (bigint-safe), or null. */
export function percent(part: string | bigint, total: string | bigint): number | null {
  const p = typeof part === "bigint" ? part : parseAmount(part);
  const t = typeof total === "bigint" ? total : parseAmount(total);
  if (t <= 0n) return null;
  const pct = (p * 100n) / t;
  // A percentage is a display ratio, never an amount: it fits a small integer.
  return pct < 0n ? 0 : pct > 100n ? 100 : parseInt(pct.toString(), 10);
}
