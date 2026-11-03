import { evmText } from "../network";
import { Label } from "../Help";
import { useQuery } from "@tanstack/react-query";
import { api, type Alarm, type ChainState, type CheckResult, type Pins } from "../api";
import {
  Empty,
  Id,
  PageHeading,
  QueryNotice,
  Table,
  Loading,
  Pill,
  Provenance,
  statusKind,
} from "../components";
import { iso, unix } from "../time";

function ChainError({ state, error }: { state?: ChainState; error?: string }) {
  if (state?.ok && !state.verified) {
    return (
      <p className="error">
        header not agreed by the second provider
        {state.consecutive_unverified ? ` ${state.consecutive_unverified}× in a row` : ""}: nothing
        from this chain is evaluated until it agrees (design §3.3)
      </p>
    );
  }
  if (state?.ok && state.verified && !state.complete) {
    return (
      <p className="error">
        collection incomplete{state.incomplete ? `: ${evmText(state.incomplete)}` : ""}; the route
        snapshots are not fresh (clients and governance are)
      </p>
    );
  }
  return (
    <p className="error">
      collection failed
      {state?.consecutive_failures ? ` ${state.consecutive_failures}× in a row` : ""}
      {error ? `: ${evmText(error)}` : ""}
      {state?.last_success && (
        <span className="muted small">
          {" "}
          · last success {iso(state.last_success)}; the snapshots below are stale
        </span>
      )}
    </p>
  );
}

function PinsCard({ pins }: { pins: Pins | null }) {
  if (!pins) return <Empty what="collection" />;
  return (
    <div className="grid2">
      <div className="card">
        <h3>gno.land</h3>
        {!pins.gno_state?.ok || !pins.gno_state?.verified || !pins.gno_state?.complete ? (
          <ChainError state={pins.gno_state} error={pins.gno_error} />
        ) : (
          <dl>
            <dt>
              <Label term="height (gno.land pin)">height</Label>
            </dt>
            <dd className="num">{pins.gno.Height.toLocaleString("en-US")}</dd>
            <dt>
              <Label term="block hash / hash">block hash</Label>
            </dt>
            <dd>
              <Id v={pins.gno.BlockHash} />
            </dd>
            <dt>
              <Label>time</Label>
            </dt>
            <dd>{iso(pins.gno.Time)}</dd>
            <dt>
              <Label>header agreement</Label>
            </dt>
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
        <h3>{evmText("Ethereum")}</h3>
        {!pins.eth_state?.ok || !pins.eth_state?.verified || !pins.eth_state?.complete ? (
          <ChainError state={pins.eth_state} error={pins.eth_error} />
        ) : (
          <dl>
            <dt>
              <Label term="block (ethereum pin)">block</Label>
            </dt>
            <dd>
              {pins.eth.Number.toLocaleString("en-US")}{" "}
              <span className="muted small">({pins.eth.Finality})</span>
            </dd>
            <dt>
              <Label>hash</Label>
            </dt>
            <dd>
              <Id v={pins.eth.Hash} />
            </dd>
            <dt>
              <Label>time</Label>
            </dt>
            <dd>{unix(pins.eth.Timestamp)}</dd>
            <dt>
              <Label>header agreement</Label>
            </dt>
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
  if (alarms.length === 0)
    return (
      <div className="empty-state ok" role="status">
        ✓ No standing alarms in this snapshot.
      </div>
    );
  return (
    <Table label="Standing alarms">
      <thead>
        <tr>
          <th>
            <Label>severity</Label>
          </th>
          <th>
            <Label>alarm</Label>
          </th>
          <th>
            <Label>since</Label>
          </th>
          <th>
            <Label>evidence</Label> (raw values)
          </th>
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
              <strong>{evmText(a.title)}</strong>
              {a.detail && <div className="muted small">{evmText(a.detail)}</div>}
              <div className="muted small">
                {a.check} · <Id v={a.key} />
              </div>
            </td>
            <td>{iso(a.first_seen)}</td>
            <td>
              {a.evidence.provenance?.map((p, i) => <Provenance key={i} p={p} />)}
              {a.evidence.values && (
                <div className="small">
                  {Object.entries(a.evidence.values).map(([k, v]) => (
                    <span key={k} className="kv">
                      <Label term={k}>{k}</Label>={v}
                    </span>
                  ))}
                </div>
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

function Checks({ checks }: { checks: CheckResult[] }) {
  if (checks.length === 0) return <Empty what="check result" />;
  return (
    <Table label="Monitor checks">
      <thead>
        <tr>
          <th>
            <Label>check</Label>
          </th>
          <th>
            <Label term="check status">status</Label>
          </th>
          <th className="num">
            <Label>findings</Label>
          </th>
          <th>
            <Label>last run</Label>
          </th>
        </tr>
      </thead>
      <tbody>
        {checks.map((c) => (
          <tr key={c.check}>
            <td>
              <Label term={c.check}>
                <code>{c.check}</code>
              </Label>
            </td>
            <td>
              <Pill kind={statusKind(c.status)}>{c.status}</Pill>
              {c.reason && <div className="muted small">{evmText(c.reason)}</div>}
              {c.consecutive_skips ? (
                <div className="muted small">skipped {c.consecutive_skips}× in a row</div>
              ) : null}
            </td>
            <td className="num">
              {c.findings.length}
              {c.findings.filter((f) => f.alarm_key).length > 0 &&
                ` (${c.findings.filter((f) => f.alarm_key).length} alarming)`}
            </td>
            <td>{iso(c.finished_at)}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

export function OverviewPage() {
  const q = useQuery({ queryKey: ["overview"], queryFn: api.overview });
  if (!q.data)
    return (
      <>
        <PageHeading title="Overview" />
        <Loading error={q.error} retry={() => void q.refetch()} />
      </>
    );
  const o = q.data;
  return (
    <>
      <PageHeading title="Overview">
        {o.pins && (
          <>
            <Label>Collection</Label> {o.pins.collection.toLocaleString("en-US")} ·{" "}
            {iso(o.pins.collected_at)}
          </>
        )}
      </PageHeading>
      <QueryNotice error={q.error} fetching={q.isFetching} retry={() => void q.refetch()} />
      <div className="stats">
        <dl className="stat">
          <dt>
            <Label>gno.land clients</Label>
          </dt>
          <dd>{o.counts.gno_clients ?? 0}</dd>
        </dl>
        <dl className="stat">
          <dt>
            <Label term="ethereum verifiers">{evmText("Ethereum verifiers")}</Label>
          </dt>
          <dd>{o.counts.eth_clients ?? 0}</dd>
        </dl>
        <dl className="stat">
          <dt>
            <Label>Routes</Label>
          </dt>
          <dd>{o.counts.routes ?? 0}</dd>
        </dl>
      </div>
      <p className="page-copy small">
        Nodes {o.info.gno_node} / {o.info.eth_node} · finality {o.info.eth_finality}
      </p>
      <PinsCard pins={o.pins} />
      <section className="card">
        <h3>Alarms</h3>
        <Alarms alarms={o.alarms} />
      </section>
      <section className="card">
        <h3>Checks</h3>
        <Checks checks={o.checks} />
      </section>
    </>
  );
}
