import { evmText, evmTxURL, evmChainId } from "../network";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type EthDecision, type EthRoute, type GnoDecision } from "../api";
import { Help, Label } from "../Help";
import { Empty, Pill } from "../components";
import { unix } from "../time";
import {
  actions,
  actionById,
  actionCategory,
  actionTiming,
  browserLimitation,
  categories,
  CLOCKS,
  DAO,
  GNO_RPC,
  GNOWEB,
  IBC_ADMIN,
  type Action,
} from "./catalog";
import { defaultValues, preflightAction } from "./actions";
import { buildTransaction, canPerform, type RoleState, type Transaction } from "./messages";
import { errorText, readLiveState, readRoles, type LiveState } from "./rpc";
import {
  decisionDue,
  findCommittedTx,
  outcomeEvents,
  proposalDeadline,
  proposalExecutable,
  readProposals,
  timeRemaining,
  type CommittedTx,
  type Proposal,
} from "./followups";
import {
  connectAdena,
  executeCalldata,
  executeEthereum,
  metaMask,
  readAdenaAccount,
  signAdena,
  vmFailure,
  type Broadcast,
} from "./wallets";
import { paramOptions, validateParam, type Values } from "./validation";
import { actionAvailability, detectRequirement, type ActionAvailability } from "./availability";

function useNow(live?: LiveState): number {
  const [tick, setTick] = useState(Date.now());
  useEffect(() => {
    const timer = setInterval(() => setTick(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  return live ? live.chainTime + (tick - live.readAt) / 1000 : tick / 1000;
}
interface Selection {
  action: Action;
  values?: Values;
  key: number;
}
interface Result {
  broadcast: Broadcast;
  committed?: CommittedTx;
  message?: string;
  checking?: boolean;
}
export function EthereumExecute({
  seq,
  epoch,
  ready = true,
}: {
  seq: string;
  epoch: string;
  ready?: boolean;
}) {
  const [provider, setProvider] = useState(metaMask);
  const [pending, setPending] = useState(false);
  const [hash, setHash] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    const timer = setInterval(() => setProvider(() => metaMask()), 2000);
    return () => clearInterval(timer);
  }, []);
  async function execute() {
    if (!provider) return;
    setPending(true);
    setError("");
    try {
      setHash(await executeEthereum(seq, provider, epoch));
    } catch (e) {
      setError(errorText(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <div className="eth-followup">
      <div className="small">
        {evmText("Sepolia · IBCAdmin ")}
        <code className="break-word">{IBC_ADMIN}</code> · epoch {epoch}, seq {seq}
      </div>
      <p className="small">
        After the relayer delivers the packet and IBCAdmin’s delay, anyone can call{" "}
        <code>execute({seq})</code>
        {evmText(". Ethereum may apply a cancellation or pure tightening at recording.")}
      </p>
      {provider && (
        <>
          <details className="tx-preview">
            <summary>{evmText("Preview Ethereum call")}</summary>
            <pre>
              {JSON.stringify(
                { chainId: evmChainId(), to: IBC_ADMIN, value: "0x0", data: executeCalldata(seq) },
                null,
                2,
              )}
            </pre>
          </details>
          <button
            type="button"
            className="button"
            disabled={!ready || pending}
            onClick={() => void execute()}
          >
            {pending ? "Check MetaMask…" : evmText("Execute on Ethereum")}
          </button>
        </>
      )}
      {!provider && (
        <p className="muted small">
          {evmText("Connect MetaMask to this browser to execute on Ethereum.")}
        </p>
      )}
      {hash && (
        <p role="status">
          <a href={evmTxURL(hash)} target="_blank" rel="noreferrer">
            {evmText("Ethereum transaction submitted ↗")}
          </a>{" "}
          · check its receipt for the outcome.
        </p>
      )}
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
    </div>
  );
}
export function EthereumDecisionAction({
  decision,
  currentEpoch,
  blocked,
}: {
  decision: EthDecision;
  currentEpoch: number;
  blocked: boolean;
}) {
  const now = useNow();
  if (
    decision.epoch !== currentEpoch ||
    !/pending|recorded|queued/i.test(decision.state_name) ||
    decision.tombstoned ||
    decision.bad
  )
    return null;
  return (
    <EthereumExecute
      seq={String(decision.seq)}
      epoch={String(decision.epoch)}
      ready={!blocked && !decision.suspended && !!decision.mature_at && now >= decision.mature_at}
    />
  );
}
function ActionDetails({ action }: { action: Action }) {
  return (
    <>
      <p>{action.description}</p>
      {action.verification && (
        <p className="muted small">
          Catalog simulations: {action.verification.ok} accepted · {action.verification.refused}{" "}
          expected refusals.
        </p>
      )}
      {action.verified && <p className="muted small">{action.verified}</p>}
      <p className="notice small">
        <Label term="governance timing">Timing</Label>: {actionTiming(action)}
      </p>
      {!!action.pitfalls.length && (
        <details className="action-pitfalls">
          <summary>Pitfalls ({action.pitfalls.length})</summary>
          <ul>
            {action.pitfalls.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </details>
      )}
    </>
  );
}
function TransactionOutcome({ result, ledger }: { result: Result; ledger: GnoDecision[] }) {
  const events = outcomeEvents(result.committed?.events ?? []);
  const failure =
    result.committed?.failure ??
    vmFailure(result.broadcast.checkTx) ??
    vmFailure(result.broadcast.deliverTx);
  return (
    <div className={`notice ${failure ? "error" : ""}`} role="status" aria-live="polite">
      <strong>
        {failure
          ? "Transaction failed"
          : result.committed
            ? "Transaction committed"
            : "Transaction submitted"}
      </strong>
      <p>
        <a
          className="break-word"
          href={`${GNOWEB}/tx/${result.broadcast.hash.replace(/^0x/, "")}`}
          target="_blank"
          rel="noreferrer"
        >
          {result.broadcast.hash} ↗
        </a>
      </p>
      {result.checking && <p>Waiting for the committed result…</p>}
      {result.message && <p>{result.message}</p>}
      {failure && <p className="break-word">{failure}</p>}
      {events.decisions.map((d, i) => (
        <p key={i}>
          Decision #{d.id} · <strong>{d.state}</strong>
          {d.due ? ` · due ${unix(parseInt(d.due, 10))}` : ""}
          {d.state === "spent"
            ? " · final refusal; inspect the ledger note before creating another request."
            : ""}
          {ledger.find((decision) => decision.id === d.id)?.note && (
            <span className="break-word">
              {" "}
              · {ledger.find((decision) => decision.id === d.id)?.note}
            </span>
          )}
        </p>
      ))}
      {events.proposals.map((id) => (
        <p key={id}>
          GovDAO proposal{" "}
          <a href={`${GNOWEB}/r/gov/dao:${id}`} target="_blank" rel="noreferrer">
            #{id} ↗
          </a>
          {result.committed
            ? ` · ${CLOCKS.proposal_deadline_s} s deadline for ibcgov requests: ${unix(proposalDeadline(result.committed.time))}`
            : ""}
        </p>
      ))}
      {events.dispatches.map((d) =>
        d.seq && d.epoch ? (
          <EthereumExecute key={`${d.epoch}/${d.seq}`} epoch={d.epoch} seq={d.seq} />
        ) : null,
      )}
    </div>
  );
}
function ActionForm({
  selection,
  address,
  live,
  roles,
  ethereum,
  availability,
  busy,
  onBusy,
  onResult,
  refresh,
}: {
  selection: Selection;
  address: string;
  live: LiveState;
  roles: RoleState;
  ethereum?: EthRoute;
  availability: ActionAvailability;
  busy: boolean;
  onBusy: (busy: boolean) => void;
  onResult: (result: Result) => void;
  refresh: () => Promise<unknown>;
}) {
  const action = selection.action;
  const [values, setValues] = useState<Values>(() => ({
    ...defaultValues(action, live, "ugnot", ethereum),
    ...selection.values,
  }));
  const [touched, setTouched] = useState(false);
  const [error, setError] = useState("");
  const [preview, setPreview] = useState<Transaction>();
  const [preparing, setPreparing] = useState(false);
  const [confirmYield, setConfirmYield] = useState(false);
  const cancel = useRef<AbortController>();
  useEffect(() => () => cancel.current?.abort(), []);
  const limitation = availability.disabled ?? browserLimitation(action);
  const ctx = { ...live, roles: roles.roles };
  function change(name: string, value: string) {
    setPreview(undefined);
    setError("");
    setValues((current) => {
      if (
        action.id === "denom.set" &&
        name === "denom" &&
        live.route.denoms.some((d) => d.denom === value)
      )
        return { ...current, ...defaultValues(action, live, value, ethereum) };
      const next = { ...current, [name]: value };
      if (action.id === "eth.unpause" && name === "object")
        next.clientID = value === "2" ? "" : live.routerClient;
      if (action.id === "eth.client" && name === "op") {
        next.clientID = value === "2" ? live.routerClient : "";
        next.counterpartyClientID = "";
      }
      if (action.id === "denom.set" && name === "kind" && value === "mint") {
        next.remoteToken = "";
        next.sendMin = "0";
        next.sendMax = "0";
      }
      return next;
    });
  }
  async function prepare(event: FormEvent) {
    event.preventDefault();
    setTouched(true);
    setError("");
    setPreparing(true);
    setPreview(undefined);
    try {
      const fresh = await readLiveState();
      const detected = await readRoles(address, fresh);
      const context = await preflightAction(action, values, fresh, detected.roles);
      setPreview(buildTransaction(action, values, address, detected, context));
    } catch (e) {
      setError(errorText(e));
    } finally {
      setPreparing(false);
    }
  }
  async function sign() {
    if (!preview) return;
    onBusy(true);
    setError("");
    try {
      const fresh = await readLiveState();
      const detected = await readRoles(address, fresh);
      const context = await preflightAction(action, values, fresh, detected.roles);
      const rebuilt = buildTransaction(action, values, address, detected, context);
      if (JSON.stringify(rebuilt) !== JSON.stringify(preview)) {
        setPreview(undefined);
        throw new Error("Governance phase or parameters changed. Preview the action again.");
      }
      const broadcast = await signAdena(address, preview);
      setPreview(undefined);
      onResult({ broadcast, checking: true });
      cancel.current = new AbortController();
      try {
        const committed = await findCommittedTx(
          broadcast.hash,
          fresh.height,
          cancel.current.signal,
        );
        onResult({
          broadcast,
          committed,
          message: committed
            ? undefined
            : "Commit could not be confirmed. Refresh the ledger / proposals and inspect the hash before retrying; a submitted transaction may still commit.",
        });
      } catch (e) {
        onResult({
          broadcast,
          message: `Commit lookup unavailable: ${errorText(e)}. Refresh the ledger and proposals before retrying.`,
        });
      }
      await refresh();
    } catch (e) {
      setError(errorText(e));
    } finally {
      onBusy(false);
    }
  }
  return (
    <section className="action-form" aria-labelledby="selected-action-title">
      <div className="card-heading">
        <h4 id="selected-action-title" tabIndex={-1}>
          {action.title}
        </h4>
        <Help term={`governance action ${action.id}`} />
      </div>
      <ActionDetails action={action} />
      {availability.warning && (
        <p className="notice" role="status">
          {availability.warning}
        </p>
      )}
      {limitation ? (
        <div className="notice">{limitation}</div>
      ) : (
        <form onSubmit={(event) => void prepare(event)} noValidate>
          <fieldset disabled={busy || preparing}>
            <legend className="sr-only">Parameters for {action.title}</legend>
            <div className="action-fields">
              {action.params.map((p) => {
                const id = `gov-${p.name}`;
                const options = paramOptions(p);
                let problem: string | undefined;
                if (touched) {
                  try {
                    validateParam(action, p, values[p.name] ?? "", values, ctx);
                  } catch (e) {
                    problem = errorText(e);
                  }
                }
                return (
                  <div className="action-field" key={p.name}>
                    <div className="field-heading">
                      <label htmlFor={id}>
                        {p.name} <span className="muted small">{p.type}</span>
                      </label>
                      <Help term={`governance param ${action.id}.${p.name}`} />
                    </div>
                    {options ? (
                      <select
                        id={id}
                        value={values[p.name] ?? ""}
                        onChange={(e) => change(p.name, e.target.value)}
                        aria-describedby={`${id}-help${problem ? ` ${id}-error` : ""}`}
                        aria-invalid={!!problem}
                      >
                        <option value="" disabled>
                          Choose…
                        </option>
                        {options.map((o) => (
                          <option value={o} key={o}>
                            {o}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <input
                        id={id}
                        type="text"
                        inputMode={/int|decimal/.test(p.type) ? "numeric" : "text"}
                        spellCheck={false}
                        autoComplete="off"
                        value={values[p.name] ?? ""}
                        onChange={(e) => change(p.name, e.target.value)}
                        aria-invalid={!!problem}
                        aria-describedby={`${id}-help${problem ? ` ${id}-error` : ""}`}
                        list={p.name === "denom" ? "gov-live-denoms" : undefined}
                      />
                    )}
                    <p id={`${id}-help`} className="muted small">
                      {p.help}{" "}
                      {p.name === "clientID" && p.type === "router client id"
                        ? `Current router client: ${live.routerClient}.`
                        : ""}
                    </p>
                    {problem && (
                      <p className="error small" id={`${id}-error`}>
                        {problem}
                      </p>
                    )}
                  </div>
                );
              })}
            </div>
            <datalist id="gov-live-denoms">
              {live.route.denoms.map((d) => (
                <option key={d.denom} value={d.denom} />
              ))}
            </datalist>
            {!action.params.length && <p className="muted">This action takes no parameters.</p>}
            <p className="muted small">
              Amounts are exact decimal strings in base units. Route controls and client defaults
              come from live RPC state; other prefilled examples come from the catalog. Review every
              field.
            </p>
            {action.id === "yield" && (
              <label className="confirm-yield">
                <input
                  type="checkbox"
                  checked={confirmYield}
                  onChange={(e) => {
                    setConfirmYield(e.target.checked);
                    setPreview(undefined);
                  }}
                />
                I understand that yielding permanently hands governance to GovDAO.
              </label>
            )}
            <button
              type="submit"
              className="button button-primary"
              disabled={action.id === "yield" && !confirmYield}
            >
              {preparing ? "Reading live state…" : "Preview transaction"}
            </button>
          </fieldset>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          {preview && (
            <div className="signing-review">
              <p>
                <strong>Review before signing</strong> ·{" "}
                {preview.messages[0].type === "/vm.m_run" && "GovDAO proposal / governor script"}
              </p>
              <details className="tx-preview" open>
                <summary>Exact Adena message</summary>
                <pre>{JSON.stringify(preview, null, 2)}</pre>
              </details>
              {preview.messages[0].type === "/vm.m_run" && (
                <details className="tx-preview" open>
                  <summary>Full Gno script</summary>
                  <pre>{preview.messages[0].value.package.files[0].body}</pre>
                </details>
              )}
              <button
                className="button button-primary"
                type="button"
                onClick={() => void sign()}
                disabled={busy || (action.id === "yield" && !confirmYield)}
              >
                {busy ? "Waiting for Adena / chain…" : "Sign with Adena"}
              </button>
            </div>
          )}
        </form>
      )}
    </section>
  );
}
function Proposals({
  proposals,
  roles,
  live,
  now,
  select,
  disabled,
}: {
  proposals: Proposal[];
  roles?: RoleState;
  live?: LiveState;
  now: number;
  select: (id: string, values: Values) => void;
  disabled: boolean;
}) {
  return (
    <section className="gov-followups" aria-labelledby="govdao-heading">
      <h4 id="govdao-heading">
        <Label term="govdao proposal">Open GovDAO proposals</Label>
      </h4>
      <p className="muted small">
        ibcgov proposals need 66.66% YES power and execution within{" "}
        {CLOCKS.proposal_deadline_s.toLocaleString("en-US")} s (2 h) of creation on this testnet.
        Direct core / transfer proposals have no ibcgov deadline.
      </p>
      {!proposals.length && <Empty what="open GovDAO proposal" />}
      {proposals.map((p) => {
        const expired = p.expired || (p.created !== undefined && now > proposalDeadline(p.created));
        const canVote =
          roles?.roles.includes("govdao") && !!roles.tier && p.eligible.includes(roles.tier);
        return (
          <article className="followup-item" key={p.id}>
            <h5>
              <a href={`${GNOWEB}/r/gov/dao:${p.id}`} target="_blank" rel="noreferrer">
                #{p.id} · {p.title} ↗
              </a>
            </h5>
            <Pill kind="neutral">{p.status}</Pill>
            <span>
              YES: {p.yesPercent ?? "unknown"}% · eligible tiers:{" "}
              {p.eligible.join(", ") || "unknown"}
            </span>
            {p.deadlineRequired && (
              <p>
                <Label term="proposal execution deadline">Execution deadline</Label>:{" "}
                {expired ? (
                  <strong className="error">expired</strong>
                ) : p.created ? (
                  <>
                    <time dateTime={new Date(proposalDeadline(p.created) * 1000).toISOString()}>
                      {unix(proposalDeadline(p.created))}
                    </time>{" "}
                    · {timeRemaining(proposalDeadline(p.created), now)} left
                  </>
                ) : (
                  "creation time unavailable; execution is disabled"
                )}
              </p>
            )}
            {p.deadlineRequired && live && !live.govdaoGoverns && (
              <p className="muted small">ibcgov execution waits for the governor’s yield.</p>
            )}
            <div className="gov-buttons">
              {canVote &&
                !expired &&
                ["YES", "NO", "ABSTAIN"].map((option) => (
                  <button
                    type="button"
                    className="button"
                    key={option}
                    disabled={disabled}
                    onClick={() => select("govdao.vote", { pid: p.id, option })}
                  >
                    Vote {option}
                  </button>
                ))}
              <button
                type="button"
                className="button"
                disabled={
                  disabled || !roles || !live || !proposalExecutable(p, now, live.govdaoGoverns)
                }
                onClick={() => select("govdao.execute", { pid: p.id })}
              >
                Execute proposal
              </button>
            </div>
            <details>
              <summary>Proposal details</summary>
              <pre className="proposal-copy">{p.detail}</pre>
            </details>
          </article>
        );
      })}
      <a className="small" href={`${GNOWEB}/r/gov/dao`} target="_blank" rel="noreferrer">
        View GovDAO on gnoweb ↗
      </a>
    </section>
  );
}
export function GovernanceActions() {
  const client = useQueryClient();
  const [address, setAddress] = useState("");
  const [connecting, setConnecting] = useState(false);
  const [walletError, setWalletError] = useState("");
  const [selection, setSelection] = useState<Selection>();
  const [search, setSearch] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<Result>();
  const formAnchor = useRef<HTMLDivElement>(null);
  const liveQ = useQuery({
    queryKey: ["governance-live"],
    queryFn: readLiveState,
    refetchInterval: 15_000,
    retry: 1,
  });
  const live = liveQ.data;
  const governanceQ = useQuery({ queryKey: ["governance"], queryFn: api.governance });
  const recovery = governanceQ.data?.ethereum?.recovery;
  const requirementsQ = useQuery({
    queryKey: ["governance-requirements"],
    queryFn: async () =>
      Object.fromEntries(
        await Promise.all(
          actions
            .filter((a) => a.requires)
            .map(async (a) => [a.id, await detectRequirement(a)] as const),
        ),
      ),
    staleTime: Infinity,
    gcTime: Infinity,
    refetchInterval: false,
  });
  const rolesQ = useQuery({
    queryKey: ["governance-roles", address, live?.yielded, live?.govdaoGoverns],
    queryFn: () => readRoles(address, live!),
    enabled: !!address && !!live,
    refetchInterval: 15_000,
    retry: 1,
  });
  const roles = rolesQ.data;
  const proposalQ = useQuery({
    queryKey: ["governance-proposals"],
    queryFn: readProposals,
    refetchInterval: 15_000,
    retry: 1,
  });
  const routesQ = useQuery({ queryKey: ["routes"], queryFn: api.routes });
  const ethereum = routesQ.data?.find((r) => r.key === live?.route.route)?.ethereum;
  const now = useNow(live);
  useEffect(() => {
    if (!address) return;
    const check = async () => {
      try {
        const account = await readAdenaAccount();
        if (account.address !== address)
          throw new Error("Adena account changed. Reconnect to detect its roles.");
      } catch (e) {
        setWalletError(errorText(e));
        setAddress("");
        setSelection(undefined);
      }
    };
    const timer = setInterval(() => void check(), 3000);
    return () => clearInterval(timer);
  }, [address]);
  async function connect() {
    setConnecting(true);
    setWalletError("");
    setSelection(undefined);
    try {
      setAddress((await connectAdena()).address);
    } catch (e) {
      setWalletError(errorText(e));
    } finally {
      setConnecting(false);
    }
  }
  async function refresh() {
    await Promise.all([
      liveQ.refetch(),
      proposalQ.refetch(),
      rolesQ.refetch(),
      client.invalidateQueries({ queryKey: ["governance"] }),
    ]);
  }
  function select(id: string, values?: Values) {
    setSelection({ action: actionById(id), values, key: Date.now() });
    setTimeout(() => {
      formAnchor.current?.scrollIntoView({ behavior: "smooth", block: "start" });
      document.getElementById("selected-action-title")?.focus({ preventScroll: true });
    }, 0);
  }
  const enabled = !!address && !!roles && !!live && !rolesQ.error && !liveQ.error;
  const available = roles && enabled ? actions.filter((a) => canPerform(a, roles)) : [];
  const filtered = available.filter((a) =>
    `${a.title} ${a.description} ${a.id}`.toLowerCase().includes(search.toLowerCase()),
  );
  const pending = live?.decisions.filter((d) => d.state === "pending") ?? [];
  return (
    <section className="card governance-actions" aria-labelledby="governance-actions-title">
      <div className="gov-wallet-heading">
        <div>
          <p className="eyebrow">TESTNET · TEST-IBC-1</p>
          <h3 id="governance-actions-title">
            Governance actions <Help term="governance roles" />
          </h3>
        </div>
        <div className="gov-buttons">
          <button
            className="button"
            type="button"
            onClick={() => void refresh()}
            disabled={busy || liveQ.isFetching}
          >
            Refresh state
          </button>
          <button
            className="button button-primary"
            type="button"
            onClick={() => void connect()}
            disabled={connecting || busy}
          >
            {connecting ? "Connecting…" : address ? "Reconnect Adena" : "Connect Adena"}
          </button>
        </div>
      </div>
      <p className="muted small">
        Live wallet and action state from{" "}
        <a href={GNO_RPC} target="_blank" rel="noreferrer">
          {evmText("the test-ibc-1 RPC ↗")}
        </a>
        . Adena simulates and sets the gas fee when you approve.
      </p>
      {walletError && (
        <p className="error" role="alert">
          {walletError}
        </p>
      )}
      {liveQ.error && (
        <p className="error" role="alert">
          Live governance state unavailable: {errorText(liveQ.error)}. Refresh to enable actions.
        </p>
      )}
      {!live && !liveQ.error && <p role="status">Reading live governance state…</p>}
      {live && (
        <div className="gov-wallet-state">
          <p>
            <Label>governor</Label>: <code className="break-word">{live.governor}</code>{" "}
            <Pill kind={live.govdaoGoverns ? "ok" : "warn"}>
              {live.yielded !== "0"
                ? `yielded at height ${live.yielded}`
                : live.govdaoGoverns
                  ? "GovDAO governs"
                  : "not yielded"}
            </Pill>
          </p>
          <p className="small">
            <Label term="router client id">Router client</Label>: <code>{live.routerClient}</code> ·
            gno client: <code>{live.route.client}</code> · live height {live.height}
          </p>
          {live.held && (
            <p className="notice">
              <Label>hold</Label> since {unix(live.holdSince)}. Queued decisions are deferred;
              unpauses are spent. Seat changes may record their hold anchor on the first due
              Execute.
            </p>
          )}
          {address ? (
            <>
              <p>
                Connected: <code className="break-word">{address}</code>
              </p>
              <p aria-live="polite">
                <Label term="governance roles">Detected roles</Label>:{" "}
                {rolesQ.isPending
                  ? "detecting…"
                  : roles?.roles.map((role) => (
                      <Pill key={role} kind="neutral">
                        {role}
                        {role === "govdao"
                          ? ` · ${roles.tier}`
                          : role === "guardian" || role === "watch"
                            ? ` · seat ${roles.seat}`
                            : ""}
                      </Pill>
                    ))}
              </p>
              {rolesQ.error && (
                <p className="error" role="alert">
                  Role detection failed: {errorText(rolesQ.error)}
                </p>
              )}
            </>
          ) : (
            <p className="muted">
              Connect Adena to detect your roles and show the actions you may perform. Any funded
              account can execute due decisions and maintenance calls.
            </p>
          )}
        </div>
      )}
      {result && <TransactionOutcome result={result} ledger={live?.decisions ?? []} />}
      {enabled && (
        <>
          <p className="small">
            {available.length} of {actions.length} catalog actions available to your roles in this
            phase.
          </p>
          <div className="action-layout">
            <nav className="action-catalog" aria-label="Available governance actions">
              <label htmlFor="gov-action-search" className="sr-only">
                Search available actions
              </label>
              <input
                id="gov-action-search"
                type="search"
                placeholder="Search your actions…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              {categories.map((category) => {
                const group = filtered.filter((a) => actionCategory(a) === category);
                return (
                  group.length > 0 && (
                    <details
                      key={category}
                      open={
                        !!search ||
                        category ===
                          (roles.roles.includes("governor") || roles.roles.includes("govdao")
                            ? "Route"
                            : "Maintenance / anyone")
                      }
                    >
                      <summary>
                        {evmText(category)} <span className="muted">{group.length}</span>
                      </summary>
                      {group.map((a) => {
                        const availability = actionAvailability(
                          a,
                          requirementsQ.data?.[a.id],
                          recovery,
                        );
                        return (
                          <div
                            className={`catalog-action ${selection?.action.id === a.id ? "selected" : ""}`}
                            key={a.id}
                          >
                            <button
                              type="button"
                              disabled={busy || !!availability.disabled}
                              aria-pressed={selection?.action.id === a.id}
                              onClick={() => select(a.id)}
                            >
                              {a.title}
                            </button>
                            <Help term={`governance action ${a.id}`} />
                            <p className="small muted">{a.description}</p>
                            {availability.disabled && (
                              <p className="small muted">{availability.disabled}</p>
                            )}
                            {availability.warning && (
                              <p className="notice small">{availability.warning}</p>
                            )}
                            <span className="small muted">
                              {a.timing.gno === "queued" ? "3,600 s + Execute" : a.timing.gno}
                              {browserLimitation(a) ? " · tooling / builder selection" : ""}
                            </span>
                          </div>
                        );
                      })}
                    </details>
                  )
                );
              })}
              {!filtered.length && <p className="muted">No matching actions.</p>}
            </nav>
            <div ref={formAnchor} className="action-form-anchor">
              {selection && canPerform(selection.action, roles) ? (
                <ActionForm
                  key={`${address}/${selection.key}`}
                  selection={selection}
                  address={address}
                  live={live}
                  roles={roles}
                  ethereum={ethereum}
                  availability={actionAvailability(
                    selection.action,
                    requirementsQ.data?.[selection.action.id],
                    recovery,
                  )}
                  busy={busy}
                  onBusy={setBusy}
                  onResult={setResult}
                  refresh={refresh}
                />
              ) : (
                <div className="empty-state">
                  Choose an action to review its timing, pitfalls and parameters.
                </div>
              )}
            </div>
          </div>
        </>
      )}
      <section className="gov-followups" aria-labelledby="live-decisions-heading">
        <h4 id="live-decisions-heading">
          <Label term="decision">Decisions</Label> · pending ({pending.length})
        </h4>
        {!pending.length && live && <Empty what="pending decision" />}
        {pending.map((d) => (
          <article className="followup-item" key={d.id}>
            <h5>
              #{d.id} · {d.kind}
            </h5>
            <p>{d.text}</p>
            <p>
              Due {unix(d.due)} ·{" "}
              {decisionDue(d.state, d.due, now) ? "due now" : timeRemaining(d.due, now)}
            </p>
            {d.note && <p className="small muted">{d.note}</p>}
            <button
              className="button"
              type="button"
              disabled={!enabled || busy || !decisionDue(d.state, d.due, now)}
              onClick={() => select("execute", { id: d.id })}
            >
              Execute decision #{d.id}
            </button>
          </article>
        ))}
      </section>
      {proposalQ.error && (
        <p className="error" role="alert">
          GovDAO proposals unavailable: {errorText(proposalQ.error)}
        </p>
      )}
      {proposalQ.data?.warning && <p className="notice">{proposalQ.data.warning}</p>}
      <Proposals
        proposals={proposalQ.data?.proposals ?? []}
        roles={enabled ? roles : undefined}
        live={live}
        now={now}
        select={select}
        disabled={!enabled || busy}
      />
      <section className="gov-followups">
        <h4>
          {evmText("Ethereum follow-ups ")}
          <Help term="ethereum governance follow-up" />
        </h4>
        <p className="small muted">
          {evmText(
            "Kinds 1–14 dispatch when recorded on gno.land. Their Ethereum delay starts after delivery. The Ethereum snapshot below shows maturity and execution controls.",
          )}
        </p>
        {live?.dispatches
          .slice()
          .reverse()
          .map((d) => (
            <details key={`${d.epoch}/${d.seq}`}>
              <summary>
                Epoch {d.epoch} · seq {d.seq} · kind {d.kind} · decision #{d.decision}
              </summary>
              <p className="small">{actions.find((a) => a.kind === d.kind)?.ethereum_followup}</p>
              <EthereumExecute
                epoch={String(d.epoch)}
                seq={d.seq}
                ready={String(d.epoch) === live.epoch}
              />
            </details>
          ))}
      </section>
      <details className="gov-followups">
        <summary>External tooling and recovery steps</summary>
        <p>
          Creating a replacement gno client, registering its counterparty and proving outcomes
          require relayer tooling. Struct arguments and coin forwarding through m_run are not
          browser forms. Use the specific catalog actions for the governance steps around them.
        </p>
        <ol>
          <li>Create and pair the new gno / custom router client with relayer tooling.</li>
          <li>Kind 6 op 1, then kind 10; Execute its queued expectation after 3,600 s.</li>
          <li>
            Replace on gno.land, PrepareReplacement, prove outcomes with relayer tooling, then
            settle.
          </li>
          <li>
            {evmText(
              "Reopen on both sides. Ethereum guardian flags and emergency recovery run on Ethereum.",
            )}
          </li>
        </ol>
        <a href={`${GNOWEB}/r/${DAO.slice("gno.land/r/".length)}`} target="_blank" rel="noreferrer">
          Inspect GovDAO ↗
        </a>
      </details>
    </section>
  );
}
