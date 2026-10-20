import { useQuery } from "@tanstack/react-query";
import { api, type Client } from "../api";
import { Empty, Id, Loading, Pill, Provenance, statusKind } from "../components";
import { countdown, durationNs, iso } from "../time";

function kindLabel(c: Client): string {
  switch (c.kind) {
    case "ethereum":
      return "Ethereum light client (gno.land)";
    case "tendermint":
      return "Tendermint client (gno.land)";
    case "gno-light-client":
      return "GnoLightClient (Ethereum)";
  }
}

function ClientRow({ c }: { c: Client }) {
  const retired = [c.retired_for_transfer && "transfer", c.retired_for_gov && "gov"].filter(
    Boolean,
  );
  return (
    <tr>
      <td>
        <strong>{c.id || <Id v={c.address} />}</strong>
        <div className="muted small">{kindLabel(c)}</div>
        {c.address && c.id && (
          <div className="small">
            <Id v={c.address} />
          </div>
        )}
        {c.role && <Pill kind="neutral">{c.role}</Pill>}
      </td>
      <td>
        <Pill kind={statusKind(c.status)}>{c.status || "unknown"}</Pill>
        {retired.length > 0 && <div className="muted small">retired for {retired.join(", ")}</div>}
      </td>
      <td>
        {c.latest_height}
        {c.latest_execution_block ? (
          <div className="muted small">exec block {c.latest_execution_block}</div>
        ) : null}
        <div className="muted small">{iso(c.head_time)}</div>
      </td>
      <td>
        <strong>{countdown(c.expiry)}</strong>
        <div className="muted small">
          {iso(c.expiry)} · trusting {durationNs(c.trusting_period_ns)}
        </div>
      </td>
      <td>
        {c.verifier_version || "—"}
        {c.trust_generation ? (
          <div className="muted small">generation {c.trust_generation}</div>
        ) : null}
      </td>
      <td>
        {c.counterparty_client_id || "—"}
        {c.chain_id && <div className="muted small">chain {c.chain_id}</div>}
      </td>
      <td>
        <Provenance p={c.provenance} />
      </td>
    </tr>
  );
}

export function ClientsPage() {
  const q = useQuery({ queryKey: ["clients"], queryFn: api.clients });
  if (!q.data) return <Loading error={q.error} />;
  const all = [...q.data.gno, ...q.data.ethereum];
  return (
    <>
      <h2>Clients</h2>
      <p className="muted small">
        Expiry is derived: gno.land Ethereum clients expire 8 sync-committee periods after their
        latest slot; GnoLightClient and Tendermint clients at head time + trusting period (design
        §4.1).
      </p>
      {all.length === 0 ? (
        <Empty what="client" />
      ) : (
        <table>
          <thead>
            <tr>
              <th>client</th>
              <th>status</th>
              <th>head</th>
              <th>expiry</th>
              <th>verifier</th>
              <th>counterparty</th>
              <th>provenance</th>
            </tr>
          </thead>
          <tbody>
            {all.map((c) => (
              <ClientRow key={`${c.chain}/${c.id}/${c.address ?? ""}`} c={c} />
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
