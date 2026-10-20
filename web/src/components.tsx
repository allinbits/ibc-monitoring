import type { Provenance as Prov } from "./api";
import { iso } from "./time";

/** The provenance badge every number carries (docs/design.md §5). */
export function Provenance({ p }: { p: Prov | undefined }) {
  if (!p) return <span className="prov prov-missing">no provenance</span>;
  const unverified = p.verification === "unverified";
  const chain = p.chain === "gno" ? "gno.land" : "Ethereum";
  const at = p.chain === "gno" ? `height ${p.height}` : `block ${p.height}`;
  return (
    <span
      className={`prov ${unverified ? "prov-unverified" : ""}`}
      title={`${chain} ${at}${p.block_hash ? ` ${p.block_hash}` : ""}\nread ${iso(p.read_at)} via ${p.node}\n${p.verification}${p.finality ? `, ${p.finality}` : ""}`}
    >
      {chain} @ {p.height} · {p.verification}
      {unverified ? " · unverified" : ""}
    </span>
  );
}

export function Pill({
  kind,
  title,
  children,
}: {
  kind: string;
  title?: string;
  children: React.ReactNode;
}) {
  return (
    <span className={`pill pill-${kind}`} title={title}>
      {children}
    </span>
  );
}

export function statusKind(status: string): string {
  switch (status) {
    case "Active":
    case "ok":
    case "passed":
    case "applied":
    case "executed":
      return "ok";
    case "Expired":
    case "Frozen":
    case "critical":
    case "break":
    case "failed":
    case "error":
      return "bad";
    case "warning":
    case "pending":
    case "buffered":
    case "skipped":
      return "warn";
    default:
      return "neutral";
  }
}

export function Loading({ error }: { error?: unknown }) {
  if (error) return <p className="error">Could not load: {String(error)}</p>;
  return <p className="muted">Loading…</p>;
}

/** Monospace, truncated identifier with the full value on hover. */
export function Id({ v, n = 12 }: { v: string | undefined; n?: number }) {
  if (!v) return <span className="muted">—</span>;
  const short = v.length > n * 2 + 1 ? `${v.slice(0, n)}…${v.slice(-6)}` : v;
  return (
    <code title={v} className="id">
      {short}
    </code>
  );
}

export function Empty({ what }: { what: string }) {
  return <p className="muted">No {what} yet.</p>;
}
