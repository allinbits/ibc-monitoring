// Time helpers. Heights and timestamps are JSON numbers: they are not
// amounts and fit a double on every real chain.

export function unix(ts: number | undefined): string {
  if (!ts) return "—";
  return new Date(ts * 1000)
    .toISOString()
    .replace("T", " ")
    .replace(/\.\d+Z$/, "Z");
}

export function iso(s: string | undefined): string {
  if (!s) return "—";
  const d = new Date(s);
  if (Number.isNaN(d.getTime()) || d.getTime() <= 0) return "—";
  return d
    .toISOString()
    .replace("T", " ")
    .replace(/\.\d+Z$/, "Z");
}

/** Humanised duration from now to `to` (a Date or ISO string), signed. */
export function countdown(to: string | Date | undefined, now = new Date()): string {
  if (!to) return "—";
  const d = typeof to === "string" ? new Date(to) : to;
  if (Number.isNaN(d.getTime()) || d.getTime() <= 0) return "—";
  let secs = Math.round((d.getTime() - now.getTime()) / 1000);
  const past = secs < 0;
  secs = Math.abs(secs);
  const days = Math.floor(secs / 86400);
  const hours = Math.floor((secs % 86400) / 3600);
  const mins = Math.floor((secs % 3600) / 60);
  const parts = days > 0 ? `${days}d ${hours}h` : hours > 0 ? `${hours}h ${mins}m` : `${mins}m`;
  return past ? `${parts} ago` : `in ${parts}`;
}

export function durationNs(ns: number): string {
  const secs = Math.floor(ns / 1e9);
  const days = Math.floor(secs / 86400);
  const hours = Math.floor((secs % 86400) / 3600);
  return days > 0 ? `${days}d ${hours}h` : `${hours}h`;
}
