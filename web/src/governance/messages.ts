import { type Action, type Role } from "./catalog";
import { validateValues, validGnoAddress, type ValidationContext, type Values } from "./validation";

export interface RoleState {
  governor: string;
  yielded: string;
  govdaoGoverns: boolean;
  roles: Role[];
  seat: number;
  tier?: string;
}
export type CallMessage = {
  type: "/vm.m_call";
  value: {
    caller: string;
    send: string;
    pkg_path: string;
    func: string;
    args: string[];
    max_deposit: "";
  };
};
export type RunMessage = {
  type: "/vm.m_run";
  value: {
    caller: string;
    send: "";
    package: { name: "main"; path: ""; files: [{ name: "main.gno"; body: string }] };
    max_deposit: "";
  };
};
export interface Transaction {
  messages: [CallMessage | RunMessage];
  memo: string;
}

export function canPerform(a: Action, state: RoleState): boolean {
  if (a.tx.governor_source) {
    return state.govdaoGoverns
      ? state.roles.includes("govdao")
      : state.yielded === "0" && state.roles.includes("governor");
  }
  return a.roles.some((role) => state.roles.includes(role));
}

/** JSON's ASCII escapes are also Go interpreted-string escapes. Escape every
 * non-ASCII code point explicitly; this helper always returns a full literal.
 */
export function goString(value: string): string {
  for (const c of value) {
    const point = c.codePointAt(0)!;
    if (point >= 0xd800 && point <= 0xdfff)
      throw new Error("Unpaired Unicode surrogate is not a Go string character.");
  }
  return JSON.stringify(value).replace(/[\u007f-\u{10ffff}]/gu, (c) => {
    const point = c.codePointAt(0)!;
    return point > 0xffff
      ? `\\U${point.toString(16).padStart(8, "0")}`
      : `\\u${point.toString(16).padStart(4, "0")}`;
  });
}

/** Only catalog templates enter here. A quoted hole is replaced as a WHOLE
 * Go literal. Unquoted holes accept decimal numbers / bools exclusively.
 * Unknown, embedded, leftover, and raw-expression holes fail closed.
 */
export function renderSource(
  a: Action,
  source: string,
  values: Values,
  ctx: ValidationContext,
): string {
  const safe = validateValues(a, values, ctx);
  const params = new Map(a.params.map((p) => [p.name, p]));
  const rendered = source.replace(
    /"\{\{([A-Za-z0-9_]+)\}\}"|\{\{([A-Za-z0-9_]+)\}\}/g,
    (_hole, quoted: string | undefined, bare: string | undefined) => {
      const name = quoted ?? bare!;
      const p = params.get(name);
      if (!p || safe[name] === undefined)
        throw new Error(`Template has an undeclared parameter: ${name}.`);
      const numeric = ["int", "uint32", "uint64", "uint256 decimal", "bool"].includes(p.type);
      if (quoted) {
        if (numeric) throw new Error(`Numeric parameter ${name} must not be quoted in source.`);
        return goString(safe[name]);
      }
      if (!numeric || !/^(?:[0-9]+|true|false)$/.test(safe[name]))
        throw new Error(`Raw source substitution refused for ${name}.`);
      return safe[name];
    },
  );
  if (/\{\{|\}\}/.test(rendered)) throw new Error("Unresolved source template parameter.");
  return rendered;
}

function renderArgument(template: string, safe: Values): string {
  const rendered = template.replace(/\{\{([A-Za-z0-9_]+)\}\}/g, (_hole, name: string) => {
    if (safe[name] === undefined) throw new Error(`Undeclared call parameter: ${name}.`);
    return safe[name];
  });
  if (/\{\{|\}\}/.test(rendered)) throw new Error("Unresolved call template parameter.");
  return rendered;
}
export function buildTransaction(
  a: Action,
  values: Values,
  caller: string,
  state: RoleState,
  ctx: ValidationContext,
): Transaction {
  if (!validGnoAddress(caller)) throw new Error("Connect a valid Adena account.");
  if (!canPerform(a, state))
    throw new Error("This account cannot perform this action in the current governance phase.");
  const safe = validateValues(a, values, ctx);
  if (a.tx.type === "/vm.m_call") {
    if (!a.tx.pkg_path || !a.tx.func || !a.tx.args)
      throw new Error("Incomplete catalog call template.");
    return {
      messages: [
        {
          type: "/vm.m_call",
          value: {
            caller,
            send: renderArgument(a.tx.send ?? "", safe),
            pkg_path: a.tx.pkg_path,
            func: a.tx.func,
            args: a.tx.args.map((arg) => renderArgument(arg, safe)),
            max_deposit: "",
          },
        },
      ],
      memo: "",
    };
  }
  if (a.tx.type !== "/vm.m_run") throw new Error("Unsupported catalog transaction type.");
  const source = a.tx.governor_source
    ? state.govdaoGoverns
      ? a.tx.govdao_source
      : a.tx.governor_source
    : a.tx.source;
  if (!source) throw new Error("No source template for this governance phase.");
  const body = renderSource(a, source, safe, ctx);
  return {
    messages: [
      {
        type: "/vm.m_run",
        value: {
          caller,
          send: "",
          package: { name: "main", path: "", files: [{ name: "main.gno", body }] },
          max_deposit: "",
        },
      },
    ],
    memo: "",
  };
}
