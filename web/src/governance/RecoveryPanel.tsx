import type { EthRecovery } from "../api";
import { Id, Pill } from "../components";
import { Help, Label } from "../Help";
import { glossaryText } from "../glossary";
import { unix } from "../time";
import { recoveryDuration, recoveryStatus, recoveryThreshold } from "./recovery";

export function RecoveryPanel({ recovery: r }: { recovery: EthRecovery }) {
  const status = recoveryStatus(r);
  const disabled = r.disabled || r.status === "disabled";
  return (
    <section aria-labelledby="recovery-heading">
      <h4 id="recovery-heading">
        <Label>Emergency recovery</Label>
      </h4>
      <dl>
        <dt>
          <Label>Recovery status</Label>
        </dt>
        <dd>
          <Pill kind={status.kind}>{status.label}</Pill> <Help term={status.term} />
          {r.supported === false && (
            <p className="muted small">
              Permanent disable is unavailable on this deployment. Recovery is currently{" "}
              {r.enabled ? "enabled" : "never enabled"}.
            </p>
          )}
        </dd>
        <dt>
          <Label>Delay</Label>
        </dt>
        <dd>{recoveryDuration(r.delay)}</dd>
        <dt>
          <Label term="Threshold (approvals)">Threshold</Label>
        </dt>
        <dd>{recoveryThreshold(r.threshold)}</dd>
        <dt>
          <Label>Cancel threshold</Label>
        </dt>
        <dd>{recoveryThreshold(r.cancel_threshold)}</dd>
        <dt>
          <Label>Silence</Label>
        </dt>
        <dd>{recoveryDuration(r.silence)}</dd>
        <dt>
          <Label>Hold period</Label>
        </dt>
        <dd>{recoveryDuration(r.hold_period)}</dd>
        <dt>
          <Label>Proposal</Label>
        </dt>
        <dd>
          {r.opened_at > 0 ? (
            <>
              <Pill kind="warn">proposal open</Pill> <Id v={r.proposal} />
              <div>
                <Label>Opened at</Label>: {unix(r.opened_at)}
              </div>
              <div>
                <Label>Executable at</Label>: {unix(r.executable_at)}
              </div>
              <div>
                <Label>Cancel votes</Label>: {recoveryThreshold(r.cancel_votes)}
              </div>
            </>
          ) : (
            <span className="muted">no open proposal</span>
          )}
        </dd>
        <dt>
          <Label term="Approvals by seat">Approvals</Label>
        </dt>
        <dd>
          {r.opened_at > 0 && (
            <div>
              {recoveryThreshold(
                r.approved.filter((a) => a.toLowerCase() === r.proposal.toLowerCase()).length,
              )}{" "}
              guardian seats approved this proposal
            </div>
          )}
          {r.approved.map((approval, seat) => (
            <div key={seat}>
              seat {seat}: {/^0x0+$/.test(approval) ? "none" : <Id v={approval} />}
            </div>
          ))}
        </dd>
        {disabled && (
          <>
            <dt>
              <Label>Disabled by</Label>
            </dt>
            <dd>
              {r.disabled_by ? (
                <>
                  <Label term="Kind 14 (disable recovery)">Kind 14</Label> · epoch{" "}
                  {r.disabled_by.epoch} · seq {r.disabled_by.seq} · block {r.disabled_by.block}
                  <div>
                    <Id v={r.disabled_by.tx} />{" "}
                    <a
                      href={`https://sepolia.etherscan.io/tx/${r.disabled_by.tx}`}
                      target="_blank"
                      rel="noreferrer"
                    >
                      View on Sepolia Etherscan ↗
                    </a>
                  </div>
                </>
              ) : (
                "Disable event details unavailable in this snapshot"
              )}
            </dd>
          </>
        )}
      </dl>
      {disabled && (
        <p className="notice">
          <strong>
            <Label>After the disable (residual risk)</Label>
          </strong>{" "}
          {glossaryText("After the disable (residual risk)")}
        </p>
      )}
    </section>
  );
}
