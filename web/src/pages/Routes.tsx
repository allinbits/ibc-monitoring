import { Backing, RouteRecords } from "./RouteRecords";
import { Label } from "../Help";
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
import { parseAmount, percent } from "../amount";
import { denomUnit, tokenUnit } from "../format";
import {
  Amount as Amt,
  Empty,
  Id,
  Loading,
  PageHeading,
  Pill,
  Provenance,
  QueryNotice,
  Table,
} from "../components";
import { iso, unix } from "../time";

function Metric({
  label,
  v,
  decimals = 0,
  notApplicable = false,
}: {
  label: string;
  v?: string | number;
  decimals?: number;
  notApplicable?: boolean;
}) {
  return (
    <div className="metric">
      <span>
        <Label>{label}</Label>
      </span>
      {notApplicable ? (
        <span className="small muted">Not applicable</span>
      ) : typeof v === "number" ? (
        <span className="amt">{v.toLocaleString("en-US")}</span>
      ) : (
        <Amt v={v} decimals={decimals} />
      )}
    </div>
  );
}

function MeterBar({ level, cap }: { level?: string; cap?: string }) {
  if (!level || !cap) return null;
  const pct = percent(level, cap);
  if (pct === null) return null;
  return (
    <span
      className="bar"
      role="meter"
      aria-label="Ethereum bucket capacity used"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={pct}
    >
      <span className="bar-fill" style={{ width: `${pct}%` }} />
    </span>
  );
}

function GnoMeter({ m, decimals }: { m: Meter; decimals: number }) {
  return (
    <div className="meter">
      <Metric label="Stored level" v={m.level} decimals={decimals} />
      <span className="meter-time">
        <Label term="at">Stored at</Label> {unix(m.at)}
      </span>
      {m.live !== undefined ? (
        <Metric label="Live level" v={m.live} decimals={decimals} />
      ) : (
        <details className="small muted">
          <summary>
            <Label term="live level">Live level unavailable</Label>
          </summary>
          {m.live_reason || "No exact source supplied."}
        </details>
      )}
      {m.epoch !== undefined && <Metric label="Payout epoch" v={m.epoch} />}
    </div>
  );
}

function DenomCard({ d }: { d: Denom }) {
  const t = d.totals;
  const { decimals, unit } = denomUnit(d);
  const metrics = (entries: [string, string | undefined][]) =>
    entries.map(([label, v]) => (
      <Metric
        key={label}
        label={label}
        v={v}
        decimals={decimals}
        notApplicable={
          d.kind === "mint"
            ? ["Escrow accounting", "Send minimum", "Send maximum", "Send fees"].includes(label)
            : ["Voucher supply", "Supply cap", "Outstanding", "Returning"].includes(label)
        }
      />
    ));
  return (
    <section className="denom-card">
      <div className="denom-heading">
        <h4>
          <Id v={d.denom} />
        </h4>
        <Pill kind="neutral">{d.kind}</Pill>
        <span className="small muted">
          Amounts in {unit} ·{" "}
          <Label term="display precision vs decimals">{decimals} display decimals</Label> ·{" "}
          <Label term="decimals">{d.decimals} governed decimals</Label>
        </span>
      </div>
      <div className="small muted">
        <Label>Token</Label> <Id v={d.token} />
        {d.trace && (
          <div>
            <Label>Trace</Label> <Id v={d.trace} />
          </div>
        )}
      </div>
      <div className="denom-grid">
        <section>
          <h5>
            <Label>Bridge / payout meter</Label>
          </h5>
          <GnoMeter m={d.meter} decimals={decimals} />
          {metrics([
            ["Day cap", d.day_cap],
            ["Cap floor", d.cap_floor],
            ["Launch cap floor", d.launch_cap_floor],
          ])}
        </section>
        <section>
          <h5>
            <Label>Refund meter</Label>
          </h5>
          <GnoMeter m={d.refund_meter} decimals={decimals} />
          {metrics([
            ["Refund cap", d.refund_cap],
            ["Queued refund amount", t.queued_refunds],
          ])}
          <Metric label="Queued refunds" v={d.queued_refunds} />
        </section>
        <section>
          <h5>
            <Label>Lock / supply</Label>
          </h5>
          {metrics([
            ["Escrow accounting", d.lock],
            ["Voucher supply", d.supply],
            ["Supply cap", d.supply_cap],
            ["Outstanding", t.outstanding],
            ["Returning", t.returning],
            ["Proven owed", t.proven_owed],
          ])}
        </section>
        <section>
          <h5>
            <Label>Claims / sweeps</Label>
          </h5>
          <div className="small muted">
            <Label>Claim states</Label>
          </div>
          <Metric label="Waiting recipients" v={d.waiting} />
          {metrics([
            ["Credited", t.credited],
            ["Owed", t.owed],
            ["Withheld", t.withheld],
            ["Swept", t.swept],
            ["Paid", t.paid],
            ["Fees paid", t.fees],
            ["Sweep paid", t.sweep_paid],
          ])}
        </section>
        <section>
          <h5>
            <Label>Sends / outcomes</Label>
          </h5>
          {metrics([
            ["In flight", t.in_flight],
            ["Sent", t.sent],
            ["Send fees", t.send_fees],
            ["Refunded", t.refunded],
          ])}
        </section>
        <section>
          <h5>
            <Label>Losses</Label>
          </h5>
          {metrics([
            ["Lost", t.lost],
            ["Recapitalized", t.recapitalized],
            ["Reduced", t.reduced],
            ["Accepted losses", t.accepted],
            ["Unrecovered", t.unrecovered],
            ["Unresolved", t.unresolved],
          ])}
        </section>
        <section>
          <h5>
            <Label>Configuration</Label>
          </h5>
          {metrics([
            ["Fee", d.fee],
            ["Fee floor", d.fee_floor],
            ["Send minimum", d.send_min],
            ["Send maximum", d.send_max],
          ])}
        </section>
      </div>
    </section>
  );
}

function GnoSide({ r }: { r: GnoRoute }) {
  return (
    <div className="card">
      <div className="card-heading">
        <h3>gno.land</h3>
        <Provenance p={r.provenance} />
      </div>
      <dl>
        <dt>
          <Label>Client</Label>
        </dt>
        <dd>
          <Id v={r.client} />
          {r.replaced.length > 0 && (
            <div className="small muted">
              Replaced{" "}
              {r.replaced.map((v) => (
                <Id key={v} v={v} />
              ))}
            </div>
          )}
        </dd>
        <dt>
          <Label term="route state (gno)">State</Label>
        </dt>
        <dd>
          <Pill kind={r.closed ? "bad" : "ok"}>{r.closed ? "closed" : "open"}</Pill>
          {r.loss_closed && <Pill kind="bad">loss-closed</Pill>}
        </dd>
        <dt>
          <Label>Enrolled</Label>
        </dt>
        <dd>
          {iso(r.enrolled)} · activated at height {r.activated.toLocaleString("en-US")}
        </dd>
        <dt>
          <Label>Fee collector</Label>
        </dt>
        <dd>
          <Id v={r.fee_collector} />
        </dd>
        <dt>
          <Label>Route loss totals</Label>
        </dt>
        <dd>
          <Label>Unrecovered</Label>{" "}
          <Pill kind={parseAmount(r.unrecovered) > 0n ? "warn" : "ok"}>
            {parseAmount(r.unrecovered) > 0n ? "Losses open: see denoms" : "None"}
          </Pill>{" "}
          · <Label>Unresolved</Label>{" "}
          <Pill kind={parseAmount(r.unresolved) > 0n ? "bad" : "ok"}>
            {parseAmount(r.unresolved) > 0n ? "Losses open: see denoms" : "None"}
          </Pill>
          <div className="small muted">
            Mixed-unit loss signals: zero means none. See each denom for token amounts.
          </div>
        </dd>
      </dl>
      {r.denoms.length ? (
        r.denoms.map((d) => <DenomCard key={d.denom} d={d} />)
      ) : (
        <Empty what="denom configuration" />
      )}
      <RouteRecords r={r} />
    </div>
  );
}

function EthSide({ e, denoms }: { e: EthRoute; denoms?: Denom[] }) {
  return (
    <div className="card">
      <div className="card-heading">
        <h3>Ethereum</h3>
        <Provenance p={e.provenance} />
      </div>
      <dl>
        <dt>
          <Label>Route key / client</Label>
        </dt>
        <dd>
          <Id v={e.route_key} /> / <Id v={e.current_client} />
          {e.incomplete?.map((what) => (
            <div key={what} className="notice error">
              Incomplete: {what}
            </div>
          ))}
          {e.migrations.map((m, i) => (
            <div key={i} className="small muted">
              <Id v={m.old} /> → <Id v={m.new} /> (block {m.block.toLocaleString("en-US")})
            </div>
          ))}
        </dd>
        <dt>
          <Label>Escrow contract</Label>
        </dt>
        <dd>
          <Id v={e.escrow} />
        </dd>
        <dt>
          <Label term="route state (ethereum)">State</Label>
        </dt>
        <dd>
          <Pill kind={e.closed ? "bad" : "ok"}>{e.closed ? "closed" : "open"}</Pill>
          {e.reopening && <Pill kind="warn">reopening</Pill>}
          {e.paused && <Pill kind="bad">paused (receives and escrowed sends)</Pill>}
          {e.refunds_held && <Pill kind="bad">refunds held</Pill>}
          {e.recovery_blocked && <Pill kind="bad">recovery block</Pill>}
        </dd>
        <dt>
          <Label term="route flag (pause)">Flags</Label>
        </dt>
        <dd>
          {e.route_flags.length === 0 && e.refunds_flags.length === 0 && (
            <span className="muted">None standing</span>
          )}
          {[
            ...e.route_flags.map((f) => ({ ...f, scope: "pause" })),
            ...e.refunds_flags.map((f) => ({ ...f, scope: "refunds" })),
          ].map((f) => (
            <div key={`${f.scope}/${f.seat}`}>
              {f.scope} · seat {f.seat} <Id v={f.key} /> at {unix(f.time)} (block {f.block_number})
            </div>
          ))}
        </dd>
        <dt>
          <Label>Holds</Label>
        </dt>
        <dd>
          {!e.route_hold && !e.refunds_hold ? (
            "None"
          ) : (
            <>
              {e.route_hold && (
                <div>
                  <Pill kind="bad">Route hold</Pill> since {unix(e.route_hold.start)}
                </div>
              )}
              {e.refunds_hold && (
                <div>
                  <Pill kind="bad">Refunds hold</Pill> since {unix(e.refunds_hold.start)}
                </div>
              )}
              <p className="small muted">
                The end depends on flag and seat history. A guardian replacement can end a hold; a
                watch-key replacement cannot. After flags dip below two, the hold can persist for 24
                hours. The dip time is not exposed on chain.
              </p>
            </>
          )}
        </dd>
        <dt>
          <Label>Quarantine</Label>
        </dt>
        <dd>
          {e.quarantine_seconds.toLocaleString("en-US")} seconds · deadline{" "}
          {e.quarantine_end ? unix(e.quarantine_end) : "none"}
        </dd>
        <dt>
          <Label>Guardian / watch keys</Label>
        </dt>
        <dd>
          {e.guardian_keys.length
            ? e.guardian_keys.map((key, i) => (
                <div key={i}>
                  Seat {i} · {i < 4 ? "guardian" : "watch"} <Id v={key} />
                </div>
              ))
            : "None"}
        </dd>
        <dt>
          <Label term="incident">Incidents</Label>
        </dt>
        <dd>
          {e.incidents.length === 0 && <span className="muted">None</span>}
          {e.incidents.map((incident, i) => (
            <div key={i}>
              #{i} {incident.kind === 1 ? "replacement" : "late loss"} ·{" "}
              {["none", "open", "reopening", "resolved"][incident.status] ??
                `status ${incident.status}`}{" "}
              · suspect from block {incident.suspect_from} · client <Id v={incident.client_hash} />
            </div>
          ))}
          <div className="small muted">
            <Label term="pending incidents">Pending incidents</Label> {e.pending_incidents} ·{" "}
            <Label>Resolved up to</Label> {e.resolved_up_to}
          </div>
        </dd>
      </dl>
      <h4>
        <Label>Token limits</Label>
      </h4>
      {e.limits.length === 0 ? (
        <Empty what="token limits" />
      ) : (
        <Table label="Ethereum token limits">
          <thead>
            <tr>
              <th>
                <Label>Token / units</Label>
              </th>
              <th className="num">
                <Label>Max / message</Label>
              </th>
              <th className="num">
                <Label>Day cap / used level</Label>
              </th>
              <th className="num">
                <Label>Refund cap / used level</Label>
              </th>
              <th className="num">
                <Label>Send min / max</Label>
              </th>
              <th className="num">
                <Label>Reserved</Label>
              </th>
              <th className="num">
                <Label>Losses / accepted deficit</Label>
              </th>
            </tr>
          </thead>
          <tbody>
            {e.limits.map((l) => {
              const { decimals, unit } = tokenUnit(l.token, denoms, l.decimals);
              return (
                <tr key={l.token}>
                  <td>
                    <Id v={l.token} />
                    <div className="small muted">
                      {unit} · {decimals} decimals
                    </div>
                    {!l.set && <Pill kind="warn">no limits configured</Pill>}
                  </td>
                  <td className="num">
                    <Amt v={l.max_per_message} decimals={decimals} />
                  </td>
                  <td className="num">
                    <Amt v={l.day_cap} decimals={decimals} />
                    <div className="muted">
                      <Amt v={l.receive_level} decimals={decimals} /> used
                    </div>
                    <div className="muted">
                      <Label term="receive / refund available">Available</Label>{" "}
                      <Amt v={l.receive_available} decimals={decimals} />
                    </div>
                    <MeterBar level={l.receive_level} cap={l.day_cap} />
                  </td>
                  <td className="num">
                    <Amt v={l.refund_cap} decimals={decimals} />
                    <div className="muted">
                      <Amt v={l.refund_level} decimals={decimals} /> used
                    </div>
                    <div className="muted">
                      <Label term="receive / refund available">Available</Label>{" "}
                      <Amt v={l.refund_available} decimals={decimals} />
                    </div>
                    <MeterBar level={l.refund_level} cap={l.refund_cap} />
                  </td>
                  <td className="num">
                    {l.send_min === "0" ? "No minimum" : <Amt v={l.send_min} decimals={decimals} />}{" "}
                    /{" "}
                    {l.send_max === "0" ? "No maximum" : <Amt v={l.send_max} decimals={decimals} />}
                  </td>
                  <td className="num">
                    <Amt v={l.reserved} decimals={decimals} />
                  </td>
                  <td className="num">
                    <Amt v={l.losses} decimals={decimals} /> /{" "}
                    <Amt v={l.accepted_deficit} decimals={decimals} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}
      <h4>
        <Label>Reconciliation per client</Label>
      </h4>
      {e.client_recon.length === 0 ? (
        <Empty what="client reconciliation records" />
      ) : (
        <Table label="Client reconciliation">
          <thead>
            <tr>
              <th>
                <Label>Client</Label>
              </th>
              <th>
                <Label>Retired</Label>
              </th>
              <th className="num">
                <Label>Open records</Label>
              </th>
              <th className="num">
                <Label>Suspect start / open</Label>
              </th>
              <th className="num">
                <Label>Processed</Label>
              </th>
              <th className="num">
                <Label>Incident</Label>
              </th>
            </tr>
          </thead>
          <tbody>
            {e.client_recon.map((c) => (
              <tr key={c.client_id}>
                <td>
                  <Id v={c.client_id} />
                </td>
                <td>{c.retired ? <Pill kind="warn">retired</Pill> : "no"}</td>
                <td className="num">{c.open}</td>
                <td className="num">
                  {c.suspect_start} / {c.suspect_open}
                </td>
                <td className="num">{c.processed}</td>
                <td className="num">{c.incident}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}

function Relationship({ v }: { v: RelationshipView }) {
  return (
    <div className="card">
      <h3>gno.land ↔ Ethereum relationship</h3>
      {v.deferred ? (
        <p className="muted">
          Deferred: one of the two bundles was not fresh at the last evaluation.
        </p>
      ) : (
        <p>
          <Pill kind={v.mismatch ? "bad" : "ok"} title={v.detail}>
            {v.mismatch ? "gno.land disagrees" : "agree"}
          </Pill>{" "}
          {v.title}
        </p>
      )}
      <div className="small muted">
        Evaluated {iso(v.evaluated_at)}
        {v.provenance.map((p, i) => (
          <Provenance key={i} p={p} />
        ))}
      </div>
    </div>
  );
}

function RouteCard({ r }: { r: RouteView }) {
  return (
    <section className="route">
      <h2>
        Route <Id v={r.key} />
      </h2>
      {r.gno ? <GnoSide r={r.gno} /> : <Empty what="gno.land route naming this Ethereum route" />}
      {r.ethereum ? (
        <EthSide e={r.ethereum} denoms={r.gno?.denoms} />
      ) : (
        <Empty what="Ethereum snapshot" />
      )}
      <Backing r={r} />
      {r.relationship && <Relationship v={r.relationship} />}
    </section>
  );
}

export function RoutesPage() {
  const q = useQuery({ queryKey: ["routes"], queryFn: api.routes });
  return (
    <>
      <PageHeading title="Routes" />
      <QueryNotice
        error={q.data && q.error}
        fetching={q.isFetching}
        retry={() => void q.refetch()}
      />
      <p className="page-copy">
        Stored meters are recorded at the last refill. Live gno.land levels appear only when
        supplied by an exact source. Ethereum live levels come from limits(). Amounts use the API’s
        display decimals and each Ethereum token’s own decimals. Unknown precision is shown in base
        units.
      </p>
      {!q.data ? (
        <Loading error={q.error} retry={() => void q.refetch()} />
      ) : q.data.length === 0 ? (
        <Empty what="route" />
      ) : (
        q.data.map((r) => <RouteCard key={r.key} r={r} />)
      )}
    </>
  );
}
