import raw from "./catalog.json";

export type Role = "governor" | "govdao" | "guardian" | "watch" | "anyone";
export interface Param {
  name: string;
  type: string;
  validation: string;
  help: string;
  default?: string;
}
export interface Action {
  id: string;
  category: string;
  title: string;
  description: string;
  roles: Role[];
  kind: number | null;
  params: Param[];
  tx: {
    type: string;
    pkg_path?: string;
    func?: string;
    args?: string[];
    send?: string;
    source?: string;
    governor_source?: string;
    govdao_source?: string;
    expr?: string;
  };
  timing: {
    gno: string;
    execute_needed: boolean;
    delay_seconds_testnet?: number;
    ethereum?: string;
    notes?: string;
  };
  pitfalls: string[];
  ethereum_followup?: string;
  verification?: { ok: number; refused: number };
}

export const GNO_RPC = "https://rpc.2-29-108-12.sslip.io";
export const GNO_CHAIN = "test-ibc-1";
export const GNOWEB = "https://gnoweb.2-29-108-12.sslip.io";
export const GOV = "gno.land/r/aib/ibc/ibcgov";
export const CORE = "gno.land/r/aib/ibc/core";
export const TRANSFER = "gno.land/r/aib/ibc/apps/transfer";
export const DAO = "gno.land/r/gov/dao";
export const IBC_ADMIN = raw.meta.live_state.ethereum.ibc_admin;
export const CLOCKS = raw.meta.clocks_testnet;

// Three direct-proposal templates omit params in the catalog. Reuse the
// corresponding builder's schema; never infer a type from user input.
const supplemental: Record<string, string> = {
  "govdao.core_pause_app_direct": "core.pause_app",
  "govdao.core_reject_app_direct": "core.reject_app",
  "govdao.transfer_block_direct": "transfer.block",
};
export const actions: Action[] = (raw.actions as Action[]).map((a) => ({
  ...a,
  params: supplemental[a.id]
    ? raw.actions.find((b) => b.id === supplemental[a.id])!.params
    : a.params,
}));
export function actionById(id: string): Action {
  const action = actions.find((a) => a.id === id);
  if (!action) throw new Error("Unknown governance action.");
  return action;
}

export const categories = [
  "Route",
  "Denoms / limits",
  "Seats / flags",
  "Ethereum kinds",
  "Recovery",
  "GovDAO",
  "Maintenance / anyone",
] as const;
export function actionCategory(a: Action): (typeof categories)[number] {
  if (a.id.startsWith("govdao.")) return "GovDAO";
  if (a.roles.includes("anyone")) return "Maintenance / anyone";
  if (a.id === "denom.set" || a.id === "fee_collector.set") return "Denoms / limits";
  if (a.id.startsWith("flag.") || a.id === "seats.set" || a.id === "route.unpause")
    return "Seats / flags";
  if (["incident", "replacement"].includes(a.category) || /expect|tombstone/.test(a.id))
    return "Recovery";
  if (a.category === "ethereum") return "Ethereum kinds";
  return "Route";
}

export function browserLimitation(a: Action): string | undefined {
  if (a.id === "govern" || a.id === "govdao.propose")
    return "Choose a specific builder in the action list. Its typed form supplies the request; arbitrary Gno source cannot be entered here.";
  if (a.id === "transfer.recapitalize")
    return "Use a relayer or gnokey tool with the live unpaid authorization: coins require a direct m_call with send equal to the exact authorized amount and denom; m_run does not forward coins. GRC20 recapitalization needs a prior token approval. The catalog does not supply amount/denom parameters for this template.";
  if (a.params.some((p) => /struct|coins|source/i.test(p.type)))
    return "This action requires structured arguments or coin forwarding through external tooling.";
  return undefined;
}

export function actionTiming(a: Action): string {
  const timing = a.timing;
  return [
    timing.gno === "queued" ? "Queued · 3,600 s, then Execute" : timing.gno,
    timing.notes,
    timing.ethereum,
  ]
    .filter(Boolean)
    .join(". ");
}
