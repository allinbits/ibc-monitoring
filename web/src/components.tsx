import { evmText } from "./network";
import { Label } from "./Help";
import { useState, type ReactNode } from "react";
import type { Provenance as Prov } from "./api";
import { middleEllipsis } from "./format";
import { formatAmount } from "./amount";
import { iso } from "./time";

export function PageHeading({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="page-heading">
      <div>
        <p className="eyebrow">GNO.LAND ↔ {evmText("Ethereum").toUpperCase()}</p>
        <h1>{title}</h1>
      </div>
      {children && <div className="page-meta">{children}</div>}
    </div>
  );
}

/** Source details remain accessible on touch and keyboard as well as hover. */
export function Provenance({ p }: { p: Prov | undefined }) {
  if (!p) return <span className="prov prov-missing">No provenance</span>;
  const chain = p.chain === "gno" ? "gno.land" : evmText("Ethereum");
  return (
    <details className="prov-details">
      <summary
        className={`prov ${p.verification === "unverified" || p.stale ? "prov-unverified" : ""}`}
      >
        {chain} @ {p.height.toLocaleString("en-US")} · header {p.verification} · value{" "}
        {p.state_verification || "none"}
        {p.stale ? " · stale" : ""}
      </summary>
      <dl>
        <dt>
          <Label>Block hash</Label>
        </dt>
        <dd>
          <Id v={p.block_hash} />
        </dd>
        <dt>
          <Label>Read at</Label>
        </dt>
        <dd>{iso(p.read_at)}</dd>
        <dt>
          <Label>Node</Label>
        </dt>
        <dd>{p.node || "Not supplied"}</dd>
        <dt>
          <Label>Header verification</Label>
        </dt>
        <dd>
          {p.verification}
          {p.finality && ` · ${p.finality}`}
        </dd>
        <dt>
          <Label>Value verification</Label>
        </dt>
        <dd>{p.state_verification || "none"}</dd>
        <dt>
          <Label>Freshness</Label>
        </dt>
        <dd>
          {p.stale
            ? "Stale: not from the latest header-agreed collection"
            : "Latest header-agreed collection"}
        </dd>
      </dl>
    </details>
  );
}

export function Pill({
  kind,
  title,
  children,
}: {
  kind: string;
  title?: string;
  children: ReactNode;
}) {
  return (
    <span className={`pill pill-${kind}`} title={title}>
      {children}
    </span>
  );
}

export function statusKind(status: string): string {
  switch (status.toLowerCase()) {
    case "active":
    case "ok":
    case "passed":
    case "applied":
    case "executed":
      return "ok";
    case "expired":
    case "frozen":
    case "critical":
    case "break":
    case "failed":
    case "error":
    case "alarm":
    case "conflict":
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

export function QueryNotice({
  error,
  retry,
  fetching,
}: {
  error?: unknown;
  retry: () => void;
  fetching?: boolean;
}) {
  if (error)
    return (
      <div className="notice error" role="alert">
        Refresh failed. Showing the last successful snapshot. {String(error)}
        <button className="button" onClick={retry}>
          Retry
        </button>
      </div>
    );
  return (
    <span className="sr-only" role="status">
      {fetching ? "Refreshing snapshot" : "Snapshot up to date"}
    </span>
  );
}

export function Loading({ error, retry }: { error?: unknown; retry?: () => void }) {
  return (
    <div className={`empty-state ${error ? "error" : ""}`} role={error ? "alert" : "status"}>
      {error ? `Could not load snapshot. ${String(error)}` : "Loading the latest snapshot…"}
      {Boolean(error) && retry && (
        <button className="button" onClick={retry}>
          Retry
        </button>
      )}
    </div>
  );
}

/** Only the display is shortened; hover and copy preserve the complete identifier. */
export function Id({ v, n = 12 }: { v: string | undefined; n?: number }) {
  const [copyState, setCopyState] = useState("");
  if (!v) return <span className="muted">—</span>;
  async function copy() {
    try {
      await navigator.clipboard.writeText(v!);
      setCopyState("Copied");
    } catch {
      setCopyState("Copy unavailable");
    }
  }
  return (
    <span className="identifier">
      <code title={v} className="id">
        {middleEllipsis(v, n)}
      </code>
      <button
        type="button"
        className="copy-button"
        title={`Copy ${v}`}
        aria-label={`Copy ${v}`}
        onClick={() => void copy()}
        onBlur={() => setCopyState("")}
      >
        {copyState || "Copy"}
      </button>
      <span className="sr-only" role="status">
        {copyState}
      </span>
    </span>
  );
}

export function Empty({ what }: { what: string }) {
  return <div className="empty-state">No {what} in this snapshot.</div>;
}

export function Table({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div
      className="table-wrap"
      role="region"
      aria-label={`${label} (scroll horizontally for more columns)`}
      tabIndex={0}
    >
      <table>
        <caption className="sr-only">{label}</caption>
        {children}
      </table>
    </div>
  );
}

export function Amount({ v, decimals = 0 }: { v?: string; decimals?: number }) {
  if (v === undefined) return <span className="muted">Unavailable</span>;
  const formatted = formatAmount(v, decimals);
  return (
    <span
      className="amt"
      title={`${formatted} · ${v} base units`}
      tabIndex={formatted.length > 28 ? 0 : undefined}
    >
      {formatted}
    </span>
  );
}
