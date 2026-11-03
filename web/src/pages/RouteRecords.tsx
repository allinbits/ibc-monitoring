import { evmText } from "../network";
import type { ReactNode } from "react";
import type { GnoRoute, RouteView } from "../api";
import { Label } from "../Help";
import { Amount, Empty, Id, Pill, Provenance, Table } from "../components";
import { denomUnit, tokenUnit } from "../format";
import { unix } from "../time";

function RecordsTable({
  label,
  term = label,
  records,
  children,
}: {
  label: string;
  term?: string;
  records?: unknown[];
  children: ReactNode;
}) {
  return (
    <section>
      <h4>
        <Label term={term}>{label}</Label>{" "}
        <span className="muted small">{records?.length ?? "unavailable"}</span>
      </h4>
      {records === undefined ? (
        <p className="muted">Records unavailable in this snapshot.</p>
      ) : records.length === 0 ? (
        <Empty what={label.toLowerCase()} />
      ) : (
        <Table label={label}>{children}</Table>
      )}
    </section>
  );
}

function RecordDenom({ r, denom }: { r: GnoRoute; denom: string }) {
  const d = r.denoms.find((d) => d.denom === denom);
  return (
    <>
      <Id v={denom} />
      <div className="small muted">{d ? denomUnit(d).unit : "base units"}</div>
    </>
  );
}

function RecordAmount({ r, denom, v }: { r: GnoRoute; denom: string; v?: string }) {
  const d = r.denoms.find((d) => d.denom === denom);
  return <Amount v={v} decimals={d ? denomUnit(d).decimals : 0} />;
}

export function RouteRecords({ r }: { r: GnoRoute }) {
  const amount = (denom: string, v?: string) => <RecordAmount r={r} denom={denom} v={v} />;
  return (
    <div className="route-records">
      <RecordsTable label="Claims" records={r.claims}>
        <thead>
          <tr>
            <th>
              <Label term="claims">Key</Label>
            </th>
            <th>
              <Label>Denom</Label>
            </th>
            <th>
              <Label term="claim states">State</Label>
            </th>
            <th>
              <Label term="claim by packet / vote">By</Label>
            </th>
            <th className="num">
              <Label term="claim amount / paid / reduced / fee">Amount</Label>
            </th>
            <th className="num">
              <Label term="claim amount / paid / reduced / fee">Paid, incl. fee</Label>
            </th>
            <th className="num">
              <Label term="claim amount / paid / reduced / fee">Reduced</Label>
            </th>
            <th className="num">
              <Label term="claim amount / paid / reduced / fee">Fee</Label>
            </th>
            <th>
              <Label term="claims">Recipient</Label>
            </th>
            <th>
              <Label term="claim at / height / seq">Credited at / height / queue seq</Label>
            </th>
          </tr>
        </thead>
        <tbody>
          {r.claims?.map((c) => (
            <tr key={c.key}>
              <td>
                <Id v={c.key} />
              </td>
              <td>
                <RecordDenom r={r} denom={c.denom} />
              </td>
              <td>
                <Pill kind={c.state === "done" ? "ok" : c.state === "forged" ? "bad" : "warn"}>
                  {c.state}
                </Pill>
              </td>
              <td>{c.by}</td>
              <td className="num">{amount(c.denom, c.amount)}</td>
              <td className="num">{amount(c.denom, c.paid)}</td>
              <td className="num">{amount(c.denom, c.reduced)}</td>
              <td className="num">{amount(c.denom, c.fee)}</td>
              <td>
                <Id v={c.to} />
              </td>
              <td>
                {unix(c.at)}
                <div className="small muted">
                  Height {c.height.toLocaleString("en-US")} · seq {c.seq}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </RecordsTable>
      <RecordsTable label="Sweeps" records={r.sweeps}>
        <thead>
          <tr>
            <th>
              <Label term="sweeps">Key</Label>
            </th>
            <th>
              <Label>Denom</Label>
            </th>
            <th className="num">
              <Label term="sweeps">Amount</Label>
            </th>
            <th className="num">
              <Label>Reduced</Label>
            </th>
            <th>
              <Label term="sweep reason">Reason</Label>
            </th>
            <th>
              <Label term="sweep voided / paid / paid to">State / paid to</Label>
            </th>
            <th>
              <Label term="sweeps">Original recipient</Label>
            </th>
            <th>
              <Label term="from claim / claim paid">From claim / previously paid</Label>
            </th>
          </tr>
        </thead>
        <tbody>
          {r.sweeps?.map((s) => (
            <tr key={s.key}>
              <td>
                <Id v={s.key} />
                <div className="small muted">Height {s.height.toLocaleString("en-US")}</div>
              </td>
              <td>
                <RecordDenom r={r} denom={s.denom} />
              </td>
              <td className="num">{amount(s.denom, s.amount)}</td>
              <td className="num">{amount(s.denom, s.reduced)}</td>
              <td className="text">{s.sweep_reason || "Not supplied"}</td>
              <td>
                {s.voided && <Pill kind="bad">voided</Pill>}
                {s.paid && <Pill kind="ok">paid</Pill>}
                {!s.voided && !s.paid && <Pill kind="warn">owed</Pill>}
                <div>
                  <Id v={s.paid_to} />
                </div>
              </td>
              <td>
                <Id v={s.to} />
              </td>
              <td>{s.from_claim ? <>Yes · {amount(s.denom, s.claim_paid)}</> : "No"}</td>
            </tr>
          ))}
        </tbody>
      </RecordsTable>
      <RecordsTable label="Sends" term="send record" records={r.sends}>
        <thead>
          <tr>
            <th className="num">
              <Label term="sequence / timeout">Sequence</Label>
            </th>
            <th>
              <Label>Denom</Label>
            </th>
            <th className="num">
              <Label term="send record">Amount</Label>
            </th>
            <th className="num">
              <Label>Fee</Label>
            </th>
            <th>
              <Label>Outcome</Label>
            </th>
            <th>
              <Label>Payment</Label>
            </th>
            <th className="num">
              <Label>Refunded</Label>
            </th>
            <th>
              <Label>Settlement</Label>
            </th>
            <th>
              <Label>Commitment ok</Label>
            </th>
            <th>
              <Label term="send record">Sender / receiver</Label>
            </th>
            <th>
              <Label term="sent at / processed at">Sent / processed</Label>
            </th>
            <th>
              <Label term="sequence / timeout">Timeout</Label>
            </th>
            <th className="num">
              <Label>Reservation</Label>
            </th>
            <th>
              <Label>Proven</Label>
            </th>
          </tr>
        </thead>
        <tbody>
          {r.sends?.map((s) => (
            <tr key={s.key}>
              <td className="num">
                {s.sequence}
                <div>
                  <Id v={s.key} n={8} />
                </div>
                <details className="small muted">
                  <summary>Clients</summary>
                  <Id v={s.source_client} /> → <Id v={s.destination_client} />
                </details>
              </td>
              <td>
                <RecordDenom r={r} denom={s.denom} />
              </td>
              <td className="num">{amount(s.denom, s.amount)}</td>
              <td className="num">{amount(s.denom, s.fee)}</td>
              <td>
                <Pill kind={s.outcome === "success" ? "ok" : "warn"}>{s.outcome || "pending"}</Pill>
              </td>
              <td>
                <Pill
                  kind={
                    ["paid", "claimed", "reconciled"].includes(s.payment)
                      ? "ok"
                      : s.payment
                        ? "warn"
                        : "neutral"
                  }
                >
                  {s.payment || "nothing owed"}
                </Pill>
              </td>
              <td className="num">{amount(s.denom, s.refunded)}</td>
              <td>
                {s.settlement} ·{" "}
                {[
                  "unsettled",
                  "genuine refund",
                  "refund rejected",
                  "delivered",
                  "forged refund",
                  "forged receive",
                ][s.settlement] ?? "unknown"}
                {s.incident && (
                  <div>
                    <Id v={s.incident} />
                  </div>
                )}
              </td>
              <td>
                <Pill kind={s.commitment_ok ? "ok" : "bad"}>
                  {s.commitment_ok ? "matches" : "mismatch"}
                </Pill>
              </td>
              <td>
                <Id v={s.sender} />
                <div>
                  <Id v={s.receiver} />
                </div>
              </td>
              <td>
                {unix(s.sent_at)}
                <div className="small muted">Height {s.sent_height.toLocaleString("en-US")}</div>
                {unix(s.processed_at)}
                <div className="small muted">
                  Height {s.processed_height.toLocaleString("en-US")}
                </div>
              </td>
              <td>{unix(s.timeout_timestamp)}</td>
              <td className="num">{amount(s.denom, s.reservation)}</td>
              <td>
                {s.proven} ·{" "}
                {["none", "delivered", "refundable", "collision"][s.proven] ?? "unknown"}
                {s.proven > 0 && (
                  <details className="small muted">
                    <summary>
                      <Label term="proven at / by / gen">Proof source</Label>
                    </summary>
                    Height {s.proven_at} · generation {s.proven_gen}
                    <div>
                      <Id v={s.proven_by} />
                    </div>
                  </details>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </RecordsTable>
      <RecordsTable label="Loss ledger" records={r.losses}>
        <thead>
          <tr>
            <th>
              <Label term="loss ledger">Key / direction</Label>
            </th>
            <th>
              <Label>Denom</Label>
            </th>
            <th className="num">
              <Label term="loss ledger">Amount</Label>
            </th>
            <th>
              <Label term="loss chain">Chain</Label>
            </th>
            <th>
              <Label>Incident</Label>
            </th>
            <th>
              <Label term="loss ledger">Cause</Label>
            </th>
          </tr>
        </thead>
        <tbody>
          {r.losses?.map((l, i) => (
            <tr key={`${l.direction}/${l.key}/${i}`}>
              <td>
                <Id v={l.key} />
                <div className="small muted">{l.direction}</div>
              </td>
              <td>
                <RecordDenom r={r} denom={l.denom} />
              </td>
              <td className="num">{amount(l.denom, l.amount)}</td>
              <td>{l.chain}</td>
              <td>
                <Id v={l.incident} />
              </td>
              <td className="text">{l.cause}</td>
            </tr>
          ))}
        </tbody>
      </RecordsTable>
    </div>
  );
}

export function Backing({ r }: { r: RouteView }) {
  if (!r.gno?.denoms.length) return null;
  return (
    <section className="card">
      <h3>
        <Label>Backing by token</Label>
      </h3>
      <p className="small muted">
        Each chain is read at its own pinned block. These balances show the backing pair; equal
        values alone do not verify the bridge’s full accounting.
      </p>
      <Table label="Token backing pairs">
        <thead>
          <tr>
            <th>
              <Label>Denom</Label>
            </th>
            <th className="num">
              <Label term="gno backing">gno.land</Label>
            </th>
            <th className="num">
              <Label term="ethereum backing">{evmText("Ethereum")}</Label>
            </th>
          </tr>
        </thead>
        <tbody>
          {r.gno.denoms.map((d) => {
            const limits = r.ethereum?.limits.find(
              (l) => l.token.toLowerCase() === d.token.toLowerCase(),
            );
            const local = denomUnit(d);
            const remote = limits
              ? tokenUnit(limits.token, r.gno?.denoms, limits.decimals)
              : undefined;
            return (
              <tr key={d.denom}>
                <td>
                  <Id v={d.denom} />
                  <div className="small muted">{local.unit}</div>
                </td>
                <td className="num">
                  <div className="small muted">
                    <Label
                      term={d.kind === "mint" ? "supply (gno.land)" : "lock / escrow accounting"}
                    >
                      {d.kind === "mint" ? "Voucher supply" : "Escrow accounting"}
                    </Label>
                  </div>
                  <Amount v={d.kind === "mint" ? d.supply : d.lock} decimals={local.decimals} />
                </td>
                <td className="num">
                  <div className="small muted">
                    <Label
                      term={
                        d.kind === "mint" ? "ethereum escrow balance" : "voucher supply (ethereum)"
                      }
                    >
                      {d.kind === "mint" ? "Escrow balance" : "Voucher supply"}
                    </Label>
                  </div>
                  <Amount
                    v={d.kind === "mint" ? limits?.escrow_balance : limits?.voucher_supply}
                    decimals={remote?.decimals ?? 0}
                  />
                  {remote && <div className="small muted">{remote.unit}</div>}
                </td>
              </tr>
            );
          })}
        </tbody>
      </Table>
      <Provenance p={r.gno.provenance} />
      <Provenance p={r.ethereum?.provenance} />
    </section>
  );
}
