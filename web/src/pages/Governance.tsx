import { GovernanceActions, EthereumDecisionAction } from "../governance/GovernanceActions";
import { Label } from "../Help";
import { useQuery } from "@tanstack/react-query";
import { api, type EthGovernance, type GnoGovernance } from "../api";
import {
  Empty,
  PageHeading,
  QueryNotice,
  Table,
  Id,
  Loading,
  Pill,
  Provenance,
  statusKind,
} from "../components";
import { countdown, unix } from "../time";

const KINDS = [
  "",
  "cancel",
  "unpause",
  "seats",
  "limits",
  "voucher",
  "client",
  "upgrade",
  "role",
  "hand-over",
  "route replacement",
  "route settings",
  "settle",
  "reopen",
];

const ACK = ["", "recorded", "buffered", "duplicate", "conflict", "spent"];

function GnoSide({ g }: { g: GnoGovernance }) {
  const pending = g.decisions.filter((d) => d.state === "pending");
  const settled = g.decisions
    .filter((d) => d.state !== "pending")
    .slice(-100)
    .reverse();
  return (
    <div className="card">
      <div className="card-heading">
        <h3>gno.land · ibcgov</h3>
        <Provenance p={g.provenance} />
      </div>
      <dl>
        <dt>
          <Label>governor</Label>
        </dt>
        <dd>
          <Id v={g.governor} />{" "}
          {g.govdao_governs ? (
            <Pill kind="ok">yielded to GovDAO at height {g.yielded}</Pill>
          ) : (
            <Pill kind="warn">governor phase (not yielded)</Pill>
          )}
        </dd>
        <dt>
          <Label term="channel (gno)">channel</Label>
        </dt>
        <dd>
          <Label term="epoch (gno.land)">Epoch</Label> {g.epoch} over <Id v={g.epoch_client} /> ·{" "}
          <Label term="last seq (gno.land)">last seq</Label> {g.last_seq}
        </dd>
        <dt>
          <Label>hold</Label>
        </dt>
        <dd>
          {g.hold.held ? (
            <>
              <Pill kind="bad">held</Pill> since {unix(g.hold.since)} — queued long-delay decisions
              are deferred; unpauses are spent
            </>
          ) : (
            <Pill kind="ok">no hold</Pill>
          )}
        </dd>
        <dt>
          <Label>seats</Label>
        </dt>
        <dd>
          {g.seats.map((s) => (
            <div key={s.index}>
              seat {s.index} (<Label term={s.role}>{s.role}</Label>):{" "}
              {s.address ? <Id v={s.address} /> : <em>vacant</em>}
            </div>
          ))}
        </dd>
        <dt>
          <Label>flags</Label>
        </dt>
        <dd>
          {g.flags.length === 0 && <span className="muted">none standing</span>}
          {g.flags.map((f, i) => (
            <div key={i}>
              <Pill kind="bad">{f.function}</Pill> {f.route} · seat {f.seat} at {unix(f.at)}
              {f.naming && f.naming.length > 0 && (
                <span className="muted small"> naming {f.naming.join(", ")}</span>
              )}
            </div>
          ))}
        </dd>
      </dl>
      <h4>Pending decisions ({pending.length})</h4>
      {pending.length === 0 ? (
        <Empty what="pending decision" />
      ) : (
        <Table label="Pending gno.land decisions">
          <thead>
            <tr>
              <th className="num">
                <Label>id</Label>
              </th>
              <th>
                <Label term="decision">kind</Label>
              </th>
              <th>
                <Label>text</Label>
              </th>
              <th>
                <Label>due</Label>
              </th>
              <th>
                <Label>note</Label>
              </th>
            </tr>
          </thead>
          <tbody>
            {pending.map((d) => (
              <tr key={d.id}>
                <td className="num">{d.id}</td>
                <td>{d.kind}</td>
                <td className="text">{d.text}</td>
                <td>
                  {unix(d.due)}
                  <div className="muted small">{countdown(new Date(d.due * 1000))}</div>
                </td>
                <td className="text">{d.note}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <h4>Dispatches to Ethereum (epoch {g.epoch})</h4>
      {g.dispatches.length === 0 ? (
        <Empty what="dispatch" />
      ) : (
        <Table label="Governance dispatches">
          <thead>
            <tr>
              <th className="num">
                <Label>seq</Label>
              </th>
              <th>
                <Label term="kind (dispatch)">kind</Label>
              </th>
              <th className="num">
                <Label term="decision (dispatch)">decision</Label>
              </th>
              <th>
                <Label>answer</Label>
              </th>
              <th>
                <Label>transport</Label>
              </th>
              <th>
                <Label>envelope</Label>
              </th>
            </tr>
          </thead>
          <tbody>
            {g.dispatches.map((d) => (
              <tr key={d.seq}>
                <td className="num">{d.seq}</td>
                <td>
                  {d.kind} {KINDS[d.kind] ?? ""}
                </td>
                <td className="num">{d.decision}</td>
                <td>
                  <Pill
                    kind={d.status === 1 || d.status === 3 ? "ok" : d.status === 0 ? "warn" : "bad"}
                  >
                    {d.status === 0 ? "no answer" : (ACK[d.status] ?? `unknown (${d.status})`)}
                  </Pill>
                  {d.in_flight && (
                    <Pill kind="warn">
                      <Label term="in flight (dispatch)">in flight</Label>
                    </Pill>
                  )}
                  {d.redispatch && (
                    <Pill kind="warn">
                      <Label>redispatch</Label>
                    </Pill>
                  )}
                  {d.event && <span className="muted small"> {d.event}</span>}
                </td>
                <td>
                  core seq {d.transport} · {d.sends} send{d.sends === 1 ? "" : "s"}
                </td>
                <td>
                  <Id v={d.body} n={10} />
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <h4>
        Settled decisions (newest {settled.length} of {g.decisions.length - pending.length})
      </h4>
      {settled.length === 0 ? (
        <Empty what="settled decision" />
      ) : (
        <Table label="Settled gno.land decisions">
          <thead>
            <tr>
              <th className="num">
                <Label>id</Label>
              </th>
              <th>
                <Label>kind</Label>
              </th>
              <th>
                <Label term="gno decision state">state</Label>
              </th>
              <th>
                <Label>text</Label>
              </th>
              <th>
                <Label>when</Label>
              </th>
            </tr>
          </thead>
          <tbody>
            {settled.map((d) => (
              <tr key={d.id}>
                <td className="num">{d.id}</td>
                <td>{d.kind}</td>
                <td>
                  <Pill kind={statusKind(d.state)}>{d.state}</Pill>
                </td>
                <td className="text">
                  {d.text}
                  {d.note && <p className="muted small">{d.note}</p>}
                </td>
                <td>{unix(d.applied || d.cancelled || d.recorded)}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}

function EthSide({ e }: { e: EthGovernance }) {
  const r = e.recovery;
  return (
    <div className="card">
      <div className="card-heading">
        <h3>Ethereum · IBCAdmin</h3>
        <Provenance p={e.provenance} />
      </div>
      <dl>
        <dt>
          <Label term="channel (ethereum)">channel</Label>
        </dt>
        <dd>
          <Label term="epoch (ethereum)">Epoch</Label> {e.epoch} over <Id v={e.gov_client} /> ·{" "}
          <Label term="last seq (ethereum)">last seq</Label> {e.last_seq} · gno chain{" "}
          {e.gno_chain_id}{" "}
          {e.setup_done ? <Pill kind="ok">setup done</Pill> : <Pill kind="bad">setup pending</Pill>}
        </dd>
        <dt>
          <Label>last record</Label>
        </dt>
        <dd>{unix(e.last_record_time)}</dd>
        <dt>
          <Label>executor</Label>
        </dt>
        <dd>
          <Id v={e.exec} /> code hash <Id v={e.exec_code_hash} />
        </dd>
        <dt>
          <Label>flags on IBCAdmin</Label>
        </dt>
        <dd>
          {e.flags.length === 0 && !e.conflict_block && (
            <span className="muted">none standing</span>
          )}
          {e.flags.map((f) => (
            <div key={f.seat}>
              seat {f.seat} <Id v={f.key} /> at {unix(f.time)} (block {f.block_number})
            </div>
          ))}
          {e.conflict_block > 0 && (
            <div>
              <Pill kind="bad">conflict flag</Pill> block {e.conflict_block}
            </div>
          )}
          {e.hold && (
            <div>
              <Pill kind="bad">hold</Pill> since {unix(e.hold.start)} — long-delay decision kinds
              deferred
            </div>
          )}
        </dd>
        <dt>
          <Label>hold history</Label>
        </dt>
        <dd>
          {e.holds.length === 0 && <span className="muted">none</span>}
          {e.holds.map((h, i) => (
            <div key={i}>
              {unix(h.start)} – {h.end ? unix(h.end) : "open"}{" "}
              {h.merged && <Pill kind="neutral">merged (never an anchor)</Pill>}
            </div>
          ))}
        </dd>
        <dt>
          <Label>keys</Label>
        </dt>
        <dd>
          {e.keys.map((k, i) => (
            <div key={i}>
              seat {i} (
              <Label term={i < 4 ? "guardian" : "watch"}>{i < 4 ? "guardian" : "watch"}</Label>):{" "}
              <Id v={k} />
            </div>
          ))}
        </dd>
        <dt>
          <Label>recovery</Label>
        </dt>
        <dd>
          {!r.enabled ? (
            <span>
              disabled at deployment; emergency recovery is unavailable if the channel is lost
            </span>
          ) : (
            <>
              enabled: <Label>delay</Label> {r.delay.toLocaleString("en-US")} s ·{" "}
              <Label>approvals</Label> {r.threshold}/4 · <Label>cancellations</Label>{" "}
              {r.cancel_threshold}/4 · <Label>silence</Label> {r.silence.toLocaleString("en-US")} s
              · <Label>hold period</Label> {r.hold_period.toLocaleString("en-US")} s
              {r.opened_at > 0 ? (
                <div>
                  <Pill kind="bad">proposal open</Pill> <Id v={r.proposal} /> since{" "}
                  {unix(r.opened_at)} · <Label>executable</Label> {unix(r.executable_at)} ·{" "}
                  <Label>cancel votes</Label> {r.cancel_votes}
                </div>
              ) : (
                <div className="muted small">no open proposal</div>
              )}
              <div className="muted small">
                <Label>Approvals by seat</Label>:{" "}
                {r.approved.map((a, i) => (
                  <span key={i} className="kv">
                    {i}: {a.replace(/^0x0+$/, "none") === "none" ? "none" : <Id v={a} n={6} />}
                  </span>
                ))}
              </div>
            </>
          )}
        </dd>
      </dl>
      <h4>Decisions (epoch {e.epoch})</h4>
      {e.decisions.length === 0 ? (
        <Empty what="recorded decision" />
      ) : (
        <Table label="Ethereum decisions">
          <thead>
            <tr>
              <th className="num">
                <Label>seq</Label>
              </th>
              <th>
                <Label term="kind (dispatch)">kind</Label>
              </th>
              <th>
                <Label term="ethereum decision state">state</Label>
              </th>
              <th>
                <Label>recorded</Label>
              </th>
              <th>
                <Label>matures</Label>
              </th>
              <th>
                <Label term="decision hash">envelope hash</Label>
              </th>
            </tr>
          </thead>
          <tbody>
            {e.decisions.map((d) => (
              <tr key={d.seq}>
                <td className="num">{d.seq}</td>
                <td>
                  {d.kind} {KINDS[d.kind] ?? ""}
                </td>
                <td>
                  <Pill kind={statusKind(d.state_name)}>{d.state_name}</Pill>
                  {d.tombstoned && (
                    <Pill kind="bad">
                      <Label>tombstoned</Label>
                    </Pill>
                  )}
                  {d.bad && (
                    <Pill kind="bad">
                      <Label>bad</Label>
                    </Pill>
                  )}
                </td>
                <td>
                  {unix(d.recorded_time)}
                  <div className="muted small">block {d.recorded_block}</div>
                </td>
                <td>
                  {d.mature_at ? (
                    <>
                      {unix(d.mature_at)}
                      <div className="muted small">{countdown(new Date(d.mature_at * 1000))}</div>
                      {d.suspended && (
                        <Pill kind="warn">
                          suspended by the open recovery; due at once after it closes
                        </Pill>
                      )}
                      {d.revocation && !d.suspended && <Pill kind="bad">revocation: due now</Pill>}
                    </>
                  ) : (
                    "—"
                  )}
                </td>
                <td>
                  <Id v={d.hash} />
                  <EthereumDecisionAction
                    decision={d}
                    currentEpoch={e.epoch}
                    blocked={!!e.hold || !!e.recovery.opened_at}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {e.buffered.length > 0 && (
        <p>
          <Pill kind="warn">buffered</Pill> sequences {e.buffered.join(", ")} wait behind last seq{" "}
          {e.last_seq}: a gap, or a halted decision — <code>drain()</code> records them when the
          executor answers.
        </p>
      )}
    </div>
  );
}

export function GovernancePage() {
  const q = useQuery({ queryKey: ["governance"], queryFn: api.governance });
  if (!q.data)
    return (
      <>
        <PageHeading title="Governance" />
        <GovernanceActions />
        <Loading error={q.error} retry={() => void q.refetch()} />
      </>
    );
  return (
    <>
      <PageHeading title="Governance" />
      <GovernanceActions />
      <QueryNotice error={q.error} fetching={q.isFetching} retry={() => void q.refetch()} />
      <p className="page-copy">
        Decision text and envelopes are the original records. Amounts embedded in decision text are
        base units.
      </p>
      {q.data.gno ? <GnoSide g={q.data.gno} /> : <Empty what="gno.land governance snapshot" />}
      {q.data.ethereum ? (
        <EthSide e={q.data.ethereum} />
      ) : (
        <Empty what="Ethereum governance snapshot" />
      )}
    </>
  );
}
