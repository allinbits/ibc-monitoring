import type { EthRecovery } from "../api";
import type { Action } from "./catalog";
import { errorText, query } from "./rpc";

export interface RequirementCheck {
  status: "available" | "unavailable" | "unknown";
  func?: string;
  warning?: string;
}
export interface ActionAvailability {
  disabled?: string;
  warning?: string;
}

/** Only a successfully decoded function list can establish that a function is absent. */
export function parseFunctions(text: string): string[] {
  const functions: unknown = JSON.parse(text);
  if (
    !Array.isArray(functions) ||
    !functions.every(
      (f: unknown) =>
        f &&
        typeof f === "object" &&
        "FuncName" in f &&
        typeof f.FuncName === "string" &&
        /^[A-Za-z_]\w*$/.test(f.FuncName),
    )
  )
    throw new Error("Unexpected vm/qfuncs function list.");
  return functions.map((f: { FuncName: string }) => f.FuncName);
}

export function resolveRequirement(action: Action): { realm: string; func: string } {
  const match = /^(.*)\.([A-Za-z_]\w*)$/.exec(action.requires ?? "");
  if (!match) throw new Error("Invalid catalog requirement.");
  const [, qualifier, func] = match;
  if (qualifier.includes("/")) return { realm: qualifier, func };
  if (action.tx.pkg_path?.split("/").at(-1) === qualifier)
    return { realm: action.tx.pkg_path, func };
  const sources = [action.tx.source, action.tx.governor_source, action.tx.govdao_source].join("\n");
  for (const entry of sources.matchAll(
    /(?:\b([A-Za-z_]\w*)[ \t]+)?"(gno\.land\/[pr]\/[^"\n]+)"/g,
  )) {
    const realm = entry[2];
    const alias = entry[1] && entry[1] !== "import" ? entry[1] : realm.split("/").at(-1);
    if (alias === qualifier) return { realm, func };
  }
  throw new Error(`Cannot resolve required realm ${qualifier}.`);
}

export function createRequirementDetector(read: typeof query) {
  // Keep in-flight requests and results (including unknown) for this browser session.
  // Multiple actions in one realm share its function-list query.
  const cache = new Map<string, Promise<{ functions?: string[]; error?: string }>>();
  return async (action: Action): Promise<RequirementCheck> => {
    if (!action.requires) return { status: "available" };
    try {
      const { realm, func } = resolveRequirement(action);
      let pending = cache.get(realm);
      if (!pending) {
        pending = Promise.resolve()
          .then(() => read("vm/qfuncs", realm))
          .then((text) => ({ functions: parseFunctions(text) }))
          .catch((error: unknown) => ({ error: errorText(error) }));
        cache.set(realm, pending);
      }
      const result = await pending;
      if (!result.functions) throw new Error(result.error);
      return { status: result.functions.includes(func) ? "available" : "unavailable", func };
    } catch (error) {
      return {
        status: "unknown",
        warning: `Could not verify deployment support for ${action.requires}: ${errorText(error)} You may continue, but this action may be unavailable on this deployment.`,
      };
    }
  };
}

export const detectRequirement = createRequirementDetector(query);

export function actionAvailability(
  action: Action,
  requirement?: RequirementCheck,
  recovery?: EthRecovery,
): ActionAvailability {
  if (action.id === "eth.disableRecovery") {
    if (recovery?.disabled || recovery?.status === "disabled")
      return {
        disabled:
          "Emergency recovery is already permanently disabled. There is nothing to disable; it can never be re-enabled.",
      };
    if (recovery?.status === "never_enabled")
      return {
        disabled:
          "Emergency recovery was never enabled on this deployment. There is nothing to disable.",
      };
  }
  if (action.requires) {
    if (!requirement) return { disabled: "Checking deployment support…" };
    if (requirement.status === "unavailable")
      return {
        disabled: `Not available on this deployment (the realm predates ${requirement.func})`,
      };
  }
  if (action.id === "eth.disableRecovery" && recovery?.supported === false)
    return {
      disabled:
        "Not available on this deployment (IBCAdmin predates the permanent recovery disable).",
    };
  if (action.id === "eth.disableRecovery" && !recovery)
    return { disabled: "Recovery state unavailable. Refresh before continuing." };
  return { warning: requirement?.warning };
}
