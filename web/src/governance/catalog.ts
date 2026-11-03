import { evmText, network } from "../network";
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
  requires?: string;
  verified?: string;
}

export { GNO_RPC, GNO_CHAIN, GNOWEB, GOV, CORE, TRANSFER, IBC_ADMIN } from "../network";
export const DAO = "gno.land/r/gov/dao";
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
  get title() {
    return evmText(a.title);
  },
  get description() {
    return evmText(a.description);
  },
  get pitfalls() {
    return a.pitfalls
      .filter((p) => network?.gno.client_type !== "arc" || !p.startsWith("Live mint denom "))
      .map(evmText);
  },
  get timing() {
    return {
      ...a.timing,
      notes: a.timing.notes && evmText(a.timing.notes),
      ethereum: a.timing.ethereum && evmText(a.timing.ethereum),
    };
  },
  get ethereum_followup() {
    return (
      a.ethereum_followup &&
      evmText(
        a.ethereum_followup
          .split(raw.meta.live_state.ethereum.ibc_admin)
          .join(network?.evm.admin_address ?? "IBCAdmin"),
      )
    );
  },
  get verified() {
    return a.verified && evmText(a.verified);
  },
  params: supplemental[a.id]
    ? raw.actions.find((b) => b.id === supplemental[a.id])!.params
    : a.params.map((p) => ({
        ...p,
        get help() {
          return evmText(p.help);
        },
        get validation() {
          return evmText(p.validation);
        },
      })),
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
