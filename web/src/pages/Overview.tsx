import { useQuery } from "@tanstack/react-query";
import { api, type Alarm, type CheckResult, type Pins } from "../api";
import { Empty, Loading, Pill, Provenance, statusKind } from "../components";
import { iso, unix } from "../time";

function PinsCard({ pins }: { pins: Pins | null }) {
  if (!pins) return <Empty what="collection" />;
  return (
    <div className="grid2">
      <div className="card">
        <h3>gno.land</h3>
        {pins.gno_error ? (
          <p className="error">{pins.gno_error}</p>
        ) : (
          <dl>
            <dt>height</dt>
            <dd>{pins.gno.Height}</dd>
            <dt>block</dt>
            <dd>
              <code className="id">{pins.gno.BlockHash}</code>
            </dd>
            <dt>time</dt>
            <dd>{iso(pins.gno.Time)}</dd>
            <dt>verification</dt>
            <dd>
              <Pill kind={pins.gno_verification === "unverified" ? "bad" : "ok"}>
                {pins.gno_verification}
              </Pill>{" "}
              <span className="muted small">{pins.gno_cross_check}</span>
            </dd>
          </dl>
        )}
      </div>
      <div className="card">
        <h3>Ethereum</h3>
        {pins.eth_error ? (
          <p className="error">{pins.eth_error}</p>
        ) : (
          <dl>
            <dt>block</dt>
            <dd>
              {pins.eth.Number} <span className="muted small">({pins.eth.Finality})</span>
            </dd>
            <dt>hash</dt>
            <dd>
              <code className="id">{pins.eth.Hash}</code>
            </dd>
            <dt>time</dt>
            <dd>{unix(pins.eth.Timestamp)}</dd>
            <dt>verification</dt>
            <dd>
              <Pill kind={pins.eth_verification === "unverified" ? "bad" : "ok"}>
                {pins.eth_verification}
              </Pill>{" "}
              <span className="muted small">{pins.eth_cross_check}</span>
            </dd>
          </dl>
        )}
      </div>
    </div>
  );
}

function Alarms({ alarms }: { alarms: Alarm[] }) {
  if (alarms.length === 0) return <p className="ok">No standing alarm.</p>;
  return (
    <table>
      <thead>
        <tr>
          <th>severity</th>
          <th>alarm</th>
          <th>since</th>
          <th>evidence</th>
        </tr>
      </thead>
      <tbody>
        {alarms.map((a) => (
          <tr key={a.key}>
            <td>
              <Pill kind={statusKind(a.severity)}>{a.severity}</Pill>
              {a.autoflag && <Pill kind="bad">auto-flag eligible</Pill>}
            </td>
            <td>
              <strong>{a.title}</strong>
              {a.detail && <div className="muted small">{a.detail}</div>}
              <div className="muted small">
                {a.check} · <code className="id">{a.key}</code>
              </div>
            </td>
            <td>{iso(a.first_seen)}</td>
            <td>
              {a.evidence.provenance?.map((p, i) => <Provenance key={i} p={p} />)}
              {a.evidence.values && (
                <div className="small">
                  {Object.entries(a.evidence.values).map(([k, v]) => (
                    <span key={k} className="kv">
                      {k}={v}
                    </span>
                  ))}
                </div>
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function Checks({ checks }: { checks: CheckResult[] }) {
  if (checks.length === 0) return <Empty what="check result" />;
  return (
    <table>
      <thead>
        <tr>
          <th>check</th>
          <th>status</th>
          <th>findings</th>
          <th>last run</th>
        </tr>
      </thead>
      <tbody>
        {checks.map((c) => (
          <tr key={c.check}>
            <td>
              <code>{c.check}</code>
            </td>
            <td>
              <Pill kind={statusKind(c.status)}>{c.status}</Pill>
              {c.reason && <div className="muted small">{c.reason}</div>}
              {c.consecutive_skips ? (
                <div className="muted small">skipped {c.consecutive_skips}× in a row</div>
              ) : null}
            </td>
            <td>
              {c.findings.length}
              {c.findings.filter((f) => f.alarm_key).length > 0 &&
                ` (${c.findings.filter((f) => f.alarm_key).length} alarming)`}
            </td>
            <td>{iso(c.finished_at)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function OverviewPage() {
  const q = useQuery({ queryKey: ["overview"], queryFn: api.overview });
  if (!q.data) return <Loading error={q.error} />;
  const o = q.data;
  return (
    <>
      <h2>Overview</h2>
      <p className="muted small">
        {o.counts.gno_clients} gno.land clients · {o.counts.eth_clients} Ethereum verifiers ·{" "}
        {o.counts.routes} routes · nodes {o.info.gno_node} / {o.info.eth_node} (finality{" "}
        {o.info.eth_finality})
      </p>
      <PinsCard pins={o.pins} />
      <h3>Alarms</h3>
      <Alarms alarms={o.alarms} />
      <h3>Checks</h3>
      <Checks checks={o.checks} />
    </>
  );
}
