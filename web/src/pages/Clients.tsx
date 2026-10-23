import { Label } from "../Help";
import { useQuery } from "@tanstack/react-query";
import { api, type Client } from "../api";
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
        <strong>
          <Id v={c.id || c.address} />
        </strong>
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
      <td className="num">
        <div className="muted small">
          <Label
            term={
              c.kind === "ethereum"
                ? "latest height (gno.land ethereum client)"
                : "latest height (gnolightclient)"
            }
          >
            {c.kind === "ethereum" ? "Beacon slot" : "Block height"}
          </Label>
        </div>
        {c.latest_height.toLocaleString("en-US")}
        {c.latest_execution_block ? (
          <div className="muted small">
            <Label>Exec block</Label> {c.latest_execution_block.toLocaleString("en-US")}
          </div>
        ) : null}
        <div className="muted small">{iso(c.head_time)}</div>
      </td>
      <td>
        <strong>{countdown(c.expiry)}</strong>
        <div className="muted small">
          {iso(c.expiry)} · <Label>Trusting period</Label> {durationNs(c.trusting_period_ns)}
        </div>
      </td>
      <td>
        {c.verifier_version || "—"}
        {c.trust_generation ? (
          <div className="muted small">
            <Label>Trust generation</Label> {c.trust_generation}
          </div>
        ) : null}
      </td>
      <td>
        <Id v={c.counterparty_client_id} />
        {c.chain_id && (
          <div className="muted small">
            <Label>Chain id</Label> {c.chain_id}
          </div>
        )}
      </td>
      <td>
        <Provenance p={c.provenance} />
      </td>
    </tr>
  );
}

export function ClientsPage() {
  const q = useQuery({ queryKey: ["clients"], queryFn: api.clients });
  if (!q.data)
    return (
      <>
        <PageHeading title="Clients" />
        <Loading error={q.error} retry={() => void q.refetch()} />
      </>
    );
  const all = [...q.data.gno, ...q.data.ethereum];
  return (
    <>
      <PageHeading title="Clients" />
      <QueryNotice error={q.error} fetching={q.isFetching} retry={() => void q.refetch()} />
      <p className="page-copy">
        Expiry is derived. For an Ethereum client on gno.land, it is the start of slot latest + 8
        sync-committee periods + 1, using the slot clock. For GnoLightClient and Tendermint clients,
        it is head time + trusting period.
      </p>
      {all.length === 0 ? (
        <Empty what="client" />
      ) : (
        <section className="card">
          <h3>Light clients</h3>
          <Table label="Light clients">
            <thead>
              <tr>
                <th>
                  <Label>client</Label>
                </th>
                <th>
                  <Label>status</Label>
                </th>
                <th className="num">
                  <Label>head</Label>
                </th>
                <th>
                  <Label>expiry</Label>
                </th>
                <th>
                  <Label>verifier</Label>
                </th>
                <th>
                  <Label>counterparty</Label>
                </th>
                <th>
                  <Label>provenance</Label>
                </th>
              </tr>
            </thead>
            <tbody>
              {all.map((c) => (
                <ClientRow key={`${c.chain}/${c.id}/${c.address ?? ""}`} c={c} />
              ))}
            </tbody>
          </Table>
        </section>
      )}
    </>
  );
}
