import { useQuery } from "@tanstack/react-query";
import {
  api,
  type Denom,
  type EthRoute,
  type GnoRoute,
  type Meter,
  type RelationshipView,
  type RouteView,
} from "../api";
import { decimalsOf, formatAmount, percent } from "../amount";
import { Empty, Id, Loading, Pill, Provenance } from "../components";
import { iso, unix } from "../time";

function Amt({
  v,
  denom,
  decimals,
  saturated,
}: {
  v: string | undefined;
  denom?: string;
  decimals?: number;
  /** True when the source reports this field as saturated (design §4.1); never inferred from the value. */
  saturated?: boolean;
}) {
  if (v === undefined) return <span className="muted">—</span>;
  const d = decimals ?? (denom ? decimalsOf(denom) : 0);
  if (saturated) {
    return (
      <span
        className="amt"
        title="saturated: the realm's int64 view of a wider counter; a lower bound, not an exact amount, and unusable as an exact input"
      >
        ≥ {formatAmount(v, d)} <Pill kind="warn">saturated</Pill>
      </span>
    );
  }
  return (
    <span className="amt" title={`${v} base units`}>
      {formatAmount(v, d)}
    </span>
  );
}

function MeterBar({ level, cap }: { level?: string; cap?: string }) {
  if (!level || !cap) return null;
  const pct = percent(level, cap);
  if (pct === null) return null;
  return (
    <span className="bar" title={`${pct}% of the cap`}>
      <span className="bar-fill" style={{ width: `${pct}%` }} />
    </span>
  );
}

/** A gno.land meter: the stored level, and the live level only from an exact source. */
function GnoMeter({ m, denom, cap }: { m: Meter; denom: string; cap?: string }) {
  return (
    <>
      <div>
        stored <Amt v={m.level} denom={denom} />{" "}
        <span className="muted small">at {unix(m.at)}</span>
      </div>
      <div>
        live{" "}
        {m.live !== undefined ? (
          <>
            <Amt v={m.live} denom={denom} /> <span className="muted small">({m.live_source})</span>
          </>
        ) : (
          <span className="muted" title={m.live_reason}>
            unavailable
          </span>
        )}
      </div>
      {cap && (
        <div className="muted small">
          cap <Amt v={cap} denom={denom} />
        </div>
      )}
    </>
  );
}

function DenomRow({ d }: { d: Denom }) {
  const t = d.totals;
  return (
    <tr>
      <td>
        <strong>{d.denom}</strong>
        <div className="muted small">{d.kind}</div>
        <div className="small">
          token <Id v={d.token} />
        </div>
        {d.trace && <div className="muted small">{d.trace}</div>}
      </td>
      <td>
        <GnoMeter m={d.meter} denom={d.denom} />
        <div className="muted small">
          cap floor <Amt v={d.cap_floor} denom={d.denom} /> · epoch {d.meter.epoch ?? 0}
        </div>
      </td>
      <td>
        <GnoMeter m={d.refund_meter} denom={d.denom} cap={d.refund_cap} />
        <div className="muted small">
          {d.queued_refunds} queued ·{" "}
          <Amt
            v={t.queued_refunds}
            denom={d.denom}
            saturated={d.saturated?.includes("queued_refunds")}
          />{" "}
          · parked <Amt v={t.parked} denom={d.denom} saturated={d.saturated?.includes("parked")} />
        </div>
      </td>
      <td>
        <div>
          escrow accounting <Amt v={d.lock} denom={d.denom} />{" "}
          {!d.lock_exact && (
            <Pill kind="warn" title="float64 on chain: exact only below 2^53">
              inexact
            </Pill>
          )}
        </div>
        <div className="muted small">
          supply cap <Amt v={d.supply_cap} denom={d.denom} /> · outstanding{" "}
          <Amt v={t.outstanding} denom={d.denom} />
        </div>
      </td>
      <td>
        <div>
          owed <Amt v={t.owed} denom={d.denom} /> · withheld <Amt v={t.withheld} denom={d.denom} />
        </div>
        <div className="muted small">
          {d.waiting} waiting · swept <Amt v={t.swept} denom={d.denom} /> · paid{" "}
          <Amt v={t.paid} denom={d.denom} saturated={d.saturated?.includes("paid")} />
        </div>
      </td>
      <td>
        <div>
          in flight{" "}
          <Amt v={t.in_flight} denom={d.denom} saturated={d.saturated?.includes("in_flight")} />
        </div>
        <div className="muted small">
          sent <Amt v={t.sent} denom={d.denom} saturated={d.saturated?.includes("sent")} /> ·
          refunded{" "}
          <Amt v={t.refunded} denom={d.denom} saturated={d.saturated?.includes("refunded")} />
        </div>
      </td>
      <td>
        <div>
          lost <Amt v={t.lost} denom={d.denom} saturated={d.saturated?.includes("lost")} /> ·
          unrecovered{" "}
          <Amt v={t.unrecovered} denom={d.denom} saturated={d.saturated?.includes("unrecovered")} />
        </div>
        <div className="muted small">
          recap{" "}
          <Amt
            v={t.recapitalized}
            denom={d.denom}
            saturated={d.saturated?.includes("recapitalized")}
          />{" "}
          · reduced{" "}
          <Amt v={t.reduced} denom={d.denom} saturated={d.saturated?.includes("reduced")} /> ·
          accepted{" "}
          <Amt v={t.accepted} denom={d.denom} saturated={d.saturated?.includes("accepted")} />
        </div>
      </td>
      <td>
        <div className="muted small">
          fee <Amt v={d.fee} denom={d.denom} /> (floor <Amt v={d.fee_floor} denom={d.denom} />)
        </div>
        <div className="muted small">
          send <Amt v={d.send_min} denom={d.denom} />–<Amt v={d.send_max} denom={d.denom} />
        </div>
      </td>
    </tr>
  );
}

function GnoSide({ r }: { r: GnoRoute }) {
  return (
    <div className="card">
      <h4>
        gno.land <Provenance p={r.provenance} />
      </h4>
      <dl>
        <dt>client</dt>
        <dd>
          {r.client}
          {r.replaced.length > 0 && (
            <span className="muted small"> · replaced {r.replaced.join(", ")}</span>
          )}
        </dd>
        <dt>state</dt>
        <dd>
          <Pill kind={r.closed ? "bad" : "ok"}>{r.closed ? "closed" : "open"}</Pill>
          {r.loss_closed && <Pill kind="bad">loss-closed</Pill>}
        </dd>
        <dt>enrolled</dt>
        <dd>
          {iso(r.enrolled)} · activated at height {r.activated}
        </dd>
        <dt>fee collector</dt>
        <dd>
          <Id v={r.fee_collector} />
        </dd>
        <dt>unrecovered / unresolved</dt>
        <dd>
          <Amt v={r.unrecovered} saturated={r.saturated?.includes("unrecovered")} /> /{" "}
          <Amt v={r.unresolved} saturated={r.saturated?.includes("unresolved")} />
        </dd>
      </dl>
      <table>
        <thead>
          <tr>
            <th>denom</th>
            <th>bridge meter</th>
            <th>refund meter</th>
            <th>lock / supply</th>
            <th>claims</th>
            <th>sends</th>
            <th>losses</th>
            <th>config</th>
          </tr>
        </thead>
        <tbody>
          {r.denoms.map((d) => (
            <DenomRow key={d.denom} d={d} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EthSide({ e }: { e: EthRoute }) {
  return (
    <div className="card">
      <h4>
        Ethereum <Provenance p={e.provenance} />
      </h4>
      <dl>
        <dt>route key / client</dt>
        <dd>
          {e.route_key} / {e.current_client}
          {e.incomplete?.map((what) => (
            <Pill key={what} kind="warn" title={what}>
              incomplete
            </Pill>
          ))}
          {e.migrations.length > 0 && (
            <div className="muted small">
              {e.migrations.map((m) => `${m.old} → ${m.new} (block ${m.block})`).join("; ")}
            </div>
          )}
        </dd>
        <dt>escrow</dt>
        <dd>
          <Id v={e.escrow} />
        </dd>
        <dt>state</dt>
        <dd>
          <Pill kind={e.closed ? "bad" : "ok"}>{e.closed ? "closed" : "open"}</Pill>
          {e.reopening && <Pill kind="warn">reopening</Pill>}
          {e.paused && <Pill kind="bad">paused (receives and escrowed sends)</Pill>}
          {e.refunds_held && <Pill kind="bad">refunds held</Pill>}
          {e.recovery_blocked && <Pill kind="bad">recovery block</Pill>}
        </dd>
        <dt>flags</dt>
        <dd>
          {e.route_flags.length === 0 && e.refunds_flags.length === 0 && (
            <span className="muted">none standing</span>
          )}
          {e.route_flags.map((f) => (
            <div key={`r${f.seat}`}>
              pause · seat {f.seat} <Id v={f.key} /> at {unix(f.time)} (block {f.block_number})
            </div>
          ))}
          {e.refunds_flags.map((f) => (
            <div key={`f${f.seat}`}>
              refunds · seat {f.seat} <Id v={f.key} /> at {unix(f.time)} (block {f.block_number})
            </div>
          ))}
          {e.route_hold && (
            <div>
              <Pill kind="bad">route hold</Pill> since {unix(e.route_hold.start)}; end unknown until
              the flag and seat history establishes it: 24 h after the guardian flags dip below two
              (the dip time is not exposed on chain), or at once when a guardian flag standing at
              the dip loses its seat — a watch key's or an unrelated seat's replacement ends nothing
            </div>
          )}
          {e.refunds_hold && (
            <div>
              <Pill kind="bad">refunds hold</Pill> since {unix(e.refunds_hold.start)}; end unknown
              until the flag and seat history establishes it: 24 h after the guardian flags dip
              below two (the dip time is not exposed on chain), or at once when a guardian flag
              standing at the dip loses its seat — a watch key's or an unrelated seat's replacement
              ends nothing
            </div>
          )}
        </dd>
        <dt>quarantine</dt>
        <dd>
          {e.quarantine_seconds / 86400} days · deadline{" "}
          {e.quarantine_end ? unix(e.quarantine_end) : "none"}
        </dd>
        <dt>incidents</dt>
        <dd>
          {e.incidents.length === 0 && <span className="muted">none</span>}
          {e.incidents.map((i, n) => (
            <div key={n}>
              #{n} {i.kind === 1 ? "replacement" : "late loss"} ·{" "}
              {["", "open", "reopening", "resolved"][i.status]} · suspect from block{" "}
              {i.suspect_from} · client <Id v={i.client_hash} />
            </div>
          ))}
          <div className="muted small">
            {e.pending_incidents} pending · resolved up to {e.resolved_up_to}
          </div>
        </dd>
      </dl>
      <table>
        <thead>
          <tr>
            <th>token</th>
            <th>max / message</th>
            <th>day cap · live level</th>
            <th>refund cap · live level</th>
            <th>reserved</th>
            <th>losses / deficit</th>
          </tr>
        </thead>
        <tbody>
          {e.limits.map((l) => (
            <tr key={l.token}>
              <td>
                <Id v={l.token} /> {!l.set && <Pill kind="warn">no limits</Pill>}
              </td>
              <td>
                <Amt v={l.max_per_message} />
              </td>
              <td>
                <Amt v={l.day_cap} /> · <Amt v={l.receive_level} />{" "}
                <MeterBar level={l.receive_level} cap={l.day_cap} />
              </td>
              <td>
                <Amt v={l.refund_cap} /> · <Amt v={l.refund_level} />{" "}
                <MeterBar level={l.refund_level} cap={l.refund_cap} />
              </td>
              <td>
                <Amt v={l.reserved} />
              </td>
              <td>
                <Amt v={l.losses} /> / <Amt v={l.accepted_deficit} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <h5>Reconciliation per client</h5>
      <table>
        <thead>
          <tr>
            <th>client</th>
            <th>retired</th>
            <th>open records</th>
            <th>suspect start / open</th>
            <th>processed</th>
            <th>incident</th>
          </tr>
        </thead>
        <tbody>
          {e.client_recon.map((c) => (
            <tr key={c.client_id}>
              <td>{c.client_id}</td>
              <td>{c.retired ? <Pill kind="warn">retired</Pill> : "no"}</td>
              <td>{c.open}</td>
              <td>
                {c.suspect_start} / {c.suspect_open}
              </td>
              <td>{c.processed}</td>
              <td>{c.incident}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** The cross-chain comparison as the check last judged it, with both inputs' provenance. */
function Relationship({ v }: { v: RelationshipView }) {
  return (
    <div className="card">
      <h4>gno.land ↔ Ethereum relationship</h4>
      {v.deferred ? (
        <p className="muted">
          deferred: one of the two bundles was not fresh at the last evaluation (
          {iso(v.evaluated_at)})
        </p>
      ) : (
        <p>
          {v.mismatch ? (
            <Pill kind="bad" title={v.detail}>
              gno.land disagrees
            </Pill>
          ) : (
            <Pill kind="ok">agree</Pill>
          )}{" "}
          <span className="muted small">{v.title}</span>
          <div className="muted small">
            evaluated {iso(v.evaluated_at)} ·{" "}
            {v.provenance.map((p, i) => (
              <Provenance key={i} p={p} />
            ))}
          </div>
        </p>
      )}
    </div>
  );
}

function RouteCard({ r }: { r: RouteView }) {
  return (
    <section className="route">
      <h3>Route {r.key}</h3>
      {r.gno ? <GnoSide r={r.gno} /> : <Empty what="gno.land route naming this Ethereum route" />}
      {r.ethereum ? <EthSide e={r.ethereum} /> : <Empty what="Ethereum snapshot" />}
      {r.relationship && <Relationship v={r.relationship} />}
    </section>
  );
}

export function RoutesPage() {
  const q = useQuery({ queryKey: ["routes"], queryFn: api.routes });
  if (!q.data) return <Loading error={q.error} />;
  return (
    <>
      <h2>Routes</h2>
      <p className="muted small">
        Stored meter levels are what the realm keeps at its last refill; live levels are refilled to
        the snapshot time (by the realm&apos;s reader when available, else here from the refill
        rule, labelled). Ethereum levels come from <code>limits()</code>.
      </p>
      {q.data.length === 0 ? (
        <Empty what="route" />
      ) : (
        q.data.map((r) => <RouteCard key={r.key} r={r} />)
      )}
    </>
  );
}
