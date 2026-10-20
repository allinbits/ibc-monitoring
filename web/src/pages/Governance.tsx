import { useQuery } from "@tanstack/react-query";
import { api, type EthGovernance, type GnoGovernance } from "../api";
import { Empty, Id, Loading, Pill, Provenance, statusKind } from "../components";
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
      <h3>
        gno.land · ibcgov <Provenance p={g.provenance} />
      </h3>
      <dl>
        <dt>governor</dt>
        <dd>
          <Id v={g.governor} />{" "}
          {g.govdao_governs ? (
            <Pill kind="ok">yielded to GovDAO at height {g.yielded}</Pill>
          ) : (
            <Pill kind="warn">governor phase (not yielded)</Pill>
          )}
        </dd>
        <dt>channel</dt>
        <dd>
          epoch {g.epoch} over {g.epoch_client} · last seq {g.last_seq}
        </dd>
        <dt>hold</dt>
        <dd>
          {g.hold.held ? (
            <>
              <Pill kind="bad">held</Pill> since {unix(g.hold.since)} — every queued 7-day decision
              is deferred; unpauses are spent (ADR §7.4)
            </>
          ) : (
            <Pill kind="ok">no hold</Pill>
          )}
        </dd>
        <dt>seats</dt>
        <dd>
          {g.seats.map((s) => (
            <div key={s.index}>
              seat {s.index} ({s.role}): {s.address ? <Id v={s.address} /> : <em>vacant</em>}
            </div>
          ))}
        </dd>
        <dt>flags</dt>
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
        <table>
          <thead>
            <tr>
              <th>id</th>
              <th>kind</th>
              <th>text</th>
              <th>due</th>
              <th>note</th>
            </tr>
          </thead>
          <tbody>
            {pending.map((d) => (
              <tr key={d.id}>
                <td>{d.id}</td>
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
        </table>
      )}
      <h4>Dispatches to Ethereum (epoch {g.epoch})</h4>
      {g.dispatches.length === 0 ? (
        <Empty what="dispatch" />
      ) : (
        <table>
          <thead>
            <tr>
              <th>seq</th>
              <th>kind</th>
              <th>decision</th>
              <th>answer</th>
              <th>transport</th>
              <th>envelope</th>
            </tr>
          </thead>
          <tbody>
            {g.dispatches.map((d) => (
              <tr key={d.seq}>
                <td>{d.seq}</td>
                <td>
                  {d.kind} {KINDS[d.kind] ?? ""}
                </td>
                <td>{d.decision}</td>
                <td>
                  <Pill
                    kind={d.status === 1 || d.status === 3 ? "ok" : d.status === 0 ? "warn" : "bad"}
                  >
                    {d.status === 0 ? "no answer" : ACK[d.status]}
                  </Pill>
                  {d.in_flight && <Pill kind="warn">in flight</Pill>}
                  {d.redispatch && <Pill kind="warn">redispatch</Pill>}
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
        </table>
      )}
      <h4>Settled decisions (newest 100 of {g.decisions.length})</h4>
      <table>
        <thead>
          <tr>
            <th>id</th>
            <th>kind</th>
            <th>state</th>
            <th>text</th>
            <th>when</th>
          </tr>
        </thead>
        <tbody>
          {settled.map((d) => (
            <tr key={d.id}>
              <td>{d.id}</td>
              <td>{d.kind}</td>
              <td>
                <Pill kind={statusKind(d.state)}>{d.state}</Pill>
              </td>
              <td className="text">{d.text}</td>
              <td>{unix(d.applied || d.cancelled || d.recorded)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EthSide({ e }: { e: EthGovernance }) {
  const r = e.recovery;
  return (
    <div className="card">
      <h3>
        Ethereum · IBCAdmin <Provenance p={e.provenance} />
      </h3>
      <dl>
        <dt>channel</dt>
        <dd>
          epoch {e.epoch} over {e.gov_client} · last seq {e.last_seq} · gno chain {e.gno_chain_id}{" "}
          {e.setup_done ? <Pill kind="ok">setup done</Pill> : <Pill kind="bad">setup pending</Pill>}
        </dd>
        <dt>last record</dt>
        <dd>{unix(e.last_record_time)}</dd>
        <dt>executor</dt>
        <dd>
          <Id v={e.exec} /> code hash <Id v={e.exec_code_hash} />
        </dd>
        <dt>flags on IBCAdmin</dt>
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
              <Pill kind="bad">hold</Pill> since {unix(e.hold.start)} — 7-day kinds deferred (ADR
              §7.4)
            </div>
          )}
        </dd>
        <dt>hold history</dt>
        <dd>
          {e.holds.length === 0 && <span className="muted">none</span>}
          {e.holds.map((h, i) => (
            <div key={i}>
              {unix(h.start)} – {h.end ? unix(h.end) : "open"}{" "}
              {h.merged && <Pill kind="neutral">merged (never an anchor)</Pill>}
            </div>
          ))}
        </dd>
        <dt>keys</dt>
        <dd>
          {e.keys.map((k, i) => (
            <div key={i}>
              seat {i} ({i < 4 ? "guardian" : "watch"}): <Id v={k} />
            </div>
          ))}
        </dd>
        <dt>recovery</dt>
        <dd>
          {!r.enabled ? (
            <span>disabled at deployment (a lost channel strands the backing, ADR §7.12 R10)</span>
          ) : (
            <>
              enabled: delay {r.delay / 86400} d · approvals {r.threshold}/4 · cancellations{" "}
              {r.cancel_threshold}/4 · silence {r.silence / 86400} d · hold period{" "}
              {r.hold_period / 86400} d
              {r.opened_at > 0 ? (
                <div>
                  <Pill kind="bad">proposal open</Pill> <Id v={r.proposal} /> since{" "}
                  {unix(r.opened_at)} · executable {unix(r.executable_at)} · cancel votes{" "}
                  {r.cancel_votes}
                </div>
              ) : (
                <div className="muted small">no open proposal</div>
              )}
              <div className="muted small">
                approvals by seat:{" "}
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
        <table>
          <thead>
            <tr>
              <th>seq</th>
              <th>kind</th>
              <th>state</th>
              <th>recorded</th>
              <th>matures</th>
              <th>hash</th>
            </tr>
          </thead>
          <tbody>
            {e.decisions.map((d) => (
              <tr key={d.seq}>
                <td>{d.seq}</td>
                <td>
                  {d.kind} {KINDS[d.kind] ?? ""}
                </td>
                <td>
                  <Pill kind={statusKind(d.state_name)}>{d.state_name}</Pill>
                  {d.tombstoned && <Pill kind="bad">tombstoned</Pill>}
                  {d.bad && <Pill kind="bad">bad</Pill>}
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
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {e.buffered.length > 0 && (
        <p>
          <Pill kind="warn">buffered</Pill> sequences {e.buffered.join(", ")} wait behind last seq{" "}
          {e.last_seq}: a gap, or a halted decision — <code>drain()</code> records them when the
          executor answers (ADR §5.4).
        </p>
      )}
    </div>
  );
}

export function GovernancePage() {
  const q = useQuery({ queryKey: ["governance"], queryFn: api.governance });
  if (!q.data) return <Loading error={q.error} />;
  return (
    <>
      <h2>Governance</h2>
      {q.data.gno ? <GnoSide g={q.data.gno} /> : <Empty what="gno.land governance snapshot" />}
      {q.data.ethereum ? (
        <EthSide e={q.data.ethereum} />
      ) : (
        <Empty what="Ethereum governance snapshot" />
      )}
    </>
  );
}
