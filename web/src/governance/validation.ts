import { browserLimitation, type Action, type Param, type Role } from "./catalog";

export const MAX_UINT256 = (1n << 256n) - 1n;
export const MAX_UINT64 = (1n << 64n) - 1n;
const MAX_INT = (1n << 63n) - 1n;
const CLIENT = /^[a-zA-Z0-9_]+(?:[a-zA-Z0-9_-]+[a-zA-Z0-9_])?-[0-9]{1,20}$/;
const CUSTOM = /^(?!client-|channel-)[a-zA-Z0-9._+#[\]<>-]{4,128}$/;
const ROUTER = /^(?:client-[0-9]+|(?!client-|channel-)[a-zA-Z0-9._+#[\]<>-]{4,128})$/;
const DENOM = /^[a-zA-Z0-9/:._-]{1,128}$/;
const WIRE =
  /^(?:[a-z][a-z0-9]{2,15}|gno\.land:r(?::[a-z][a-z0-9]*(?:[_-][a-z0-9]+)*){2,}(?:\.[A-Za-z0-9_-]{1,32})?)$/;
const ETH = /^0x[0-9a-f]{40}$/;
export interface LiveDenom {
  denom: string;
  kind: string;
  cap_floor: string;
  launch_cap_floor: string;
  supply_cap: string;
  fee: string;
  fee_floor: string;
  send_max: string;
  send_min: string;
  refund_cap: string;
  remote: string;
  decimals: number;
}
export interface ValidationContext {
  routerClient: string;
  epoch: string;
  lastSeq: string;
  height: string;
  roles: Role[];
  seats: Record<string, string>;
  route: {
    route: string;
    client: string;
    replaced: string[];
    fee_collector: string;
    denoms: LiveDenom[];
  };
  counterparties?: Record<string, string>;
  denomAliases?: Record<string, string>;
}
export type Values = Record<string, string>;

function requireValue(ok: boolean, message: string): asserts ok {
  if (!ok) throw new Error(message);
}
export function decimal(value: string, max = MAX_UINT256, min = 0n): string {
  requireValue(/^[0-9]{1,78}$/.test(value), "Use decimal digits only, without spaces or signs.");
  const n = BigInt(value);
  requireValue(n >= min && n <= max, `Value must be between ${min} and ${max}.`);
  // Normalise leading zeros, which Go otherwise treats as octal literals.
  return n.toString();
}

function gnoClient(value: string): boolean {
  if (!CLIENT.test(value) || value.startsWith("client-")) return false;
  return BigInt(value.slice(value.lastIndexOf("-") + 1)) <= MAX_UINT64;
}
function routerClient(value: string): boolean {
  if (!ROUTER.test(value)) return false;
  if (!value.startsWith("client-")) return true;
  const seq = value.slice(7);
  return /^(?:0|[1-9][0-9]{0,19})$/.test(seq) && BigInt(seq) <= MAX_UINT64;
}

// A 20-byte bech32 address (32 data words plus 6 checksum words), HRP g.
// Keep checksum validation local; no wallet or chain library is required.
export function validGnoAddress(value: string): boolean {
  if (!/^g1[023456789acdefghjklmnpqrstuvwxyz]{38}$/.test(value)) return false;
  const alphabet = "qpzry9x8gf2tvdw0s3jn54khce6mua7l";
  const words = [3, 0, 7, ...Array.from(value.slice(2), (c) => alphabet.indexOf(c))];
  let check = 1;
  const generator = [0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3];
  for (const word of words) {
    const top = check >>> 25;
    check = ((check & 0x1ffffff) << 5) ^ word;
    for (let i = 0; i < 5; i++) if ((top >>> i) & 1) check ^= generator[i];
  }
  return check >>> 0 === 1;
}
function ethAddress(value: string, nonzero = false): string {
  const s = value.toLowerCase();
  requireValue(ETH.test(s), "Use an Ethereum address: 0x and 40 hex digits.");
  requireValue(!nonzero || BigInt(s) !== 0n, "The Ethereum address must be non-zero.");
  return s;
}
function list(value: string, max: number, allowEmpty = false): string[] {
  if (value === "" && allowEmpty) return [];
  const parts = value.split(",");
  requireValue(
    parts.length >= 1 && parts.length <= max && parts.every(Boolean),
    `Name 1 to ${max} items, separated by commas without spaces.`,
  );
  return parts;
}
function packet(value: string): string {
  const parts = value.split("/");
  requireValue(
    parts.length === 2 && gnoClient(parts[0]) && /^[0-9]{20}$/.test(parts[1]),
    "Use a gno client id / 20-digit sequence packet key.",
  );
  decimal(parts[1], MAX_UINT64);
  return value;
}
const enums: Record<string, string[]> = {
  fn: ["sends", "credits", "payouts", "refunds"],
  kind: ["unescrow", "mint"],
  option: ["YES", "NO", "ABSTAIN"],
  chain: ["gno", "ethereum"],
  cause: ["forged-delivery", "collision"],
};
export function paramOptions(p: Param): string[] | undefined {
  if (p.type === "bool") return ["true", "false"];
  if (p.name === "object" || p.name === "op") return ["1", "2"];
  return enums[p.name];
}

export function validateParam(
  a: Action,
  p: Param,
  value: string,
  all: Values,
  ctx: ValidationContext,
): string {
  requireValue(typeof value === "string" && value.length <= 16384, "Value is missing or too long.");
  // No unicode folding, trimming, or invisible characters: validation is ASCII-only.
  requireValue(!/[^\x20-\x7e]/.test(value), "Use plain ASCII characters without newlines.");
  const name = p.name;
  const choices = paramOptions(p);
  if (choices) {
    requireValue(choices.includes(value), `Choose ${choices.join(", ")}.`);
    return value;
  }
  if (["int", "uint32", "uint64", "uint256 decimal"].includes(p.type)) {
    const max =
      p.type === "uint32"
        ? (1n << 32n) - 1n
        : p.type === "uint64"
          ? MAX_UINT64
          : p.type === "int"
            ? MAX_INT
            : MAX_UINT256;
    const positive =
      name === "amount" ||
      name === "n" ||
      name === "seq" ||
      name === "id" ||
      name === "capFloor" ||
      name === "feeFloor" ||
      (name === "refundCap" && a.id === "denom.set");
    const s = decimal(value, max, positive ? 1n : 0n);
    const n = BigInt(s);
    if (name === "n" && /1 to 64/.test(p.validation))
      requireValue(n <= 64n, "Batch must be 1 to 64.");
    if (name === "decimals") requireValue(n <= 18n, "Decimals must be 0 to 18.");
    if (name === "roleID")
      requireValue(n !== 0n && n !== MAX_UINT64, "ADMIN_ROLE and PUBLIC_ROLE cannot be named.");
    if (name === "suspectFrom" && a.id === "route.replace")
      requireValue(n <= BigInt(ctx.height), "Suspect height cannot be in the future.");
    if (name === "epoch") {
      const e = BigInt(ctx.epoch);
      requireValue(
        a.id === "eth.apply_epoch_notice" ? n > e && n <= e + 64n : n === e,
        "Use the current epoch (or current + 1..64 for a proven notice).",
      );
    }
    if (name === "seq")
      requireValue(n <= BigInt(ctx.lastSeq), "Sequence exceeds the live last sequence.");
    return s;
  }
  if (p.type === "address") {
    const s = value.toLowerCase();
    requireValue(validGnoAddress(s), "Use a valid g1 bech32 address, including its checksum.");
    return s;
  }
  if (p.type === "router client id") {
    if (a.id === "eth.unpause" && all.object === "2") {
      requireValue(value === "", "IBCAdmin (object 2) requires an empty client id.");
      return "";
    }
    requireValue(
      routerClient(value),
      "Use client-N or a custom router id (4–128 ASCII identifier characters).",
    );
    requireValue(
      value !== ctx.route.client && !/^ethereum-[0-9]+$/.test(value),
      "Use the Ethereum router client, never the gno id ethereum-1.",
    );
    const custom =
      (name === "newClientID" && !/remove_expectation|purge_tombstone/.test(a.id)) ||
      (a.id === "eth.client" && all.op === "1" && name === "clientID");
    if (custom)
      requireValue(
        CUSTOM.test(value),
        "A new router client must be custom, without client- or channel- prefix.",
      );
    const current =
      [
        "eth.unpause",
        "eth.limits",
        "eth.voucher",
        "eth.route_settings",
        "eth.reopen",
        "eth.disableRecovery",
      ].includes(a.id) ||
      (a.id === "eth.client" && all.op === "2") ||
      (name === "oldClientID" && /route_replacement|expect_recovery/.test(a.id));
    if (current)
      requireValue(
        !!ctx.routerClient && value === ctx.routerClient,
        `Use the route's live router client: ${ctx.routerClient}.`,
      );
    return value;
  }
  if (p.type === "gno client id") {
    if (a.id === "eth.client" && all.op === "2" && name === "counterpartyClientID") {
      requireValue(
        value === "" || routerClient(value),
        "Use an empty value or a router client id for migration.",
      );
      return value;
    }
    requireValue(gnoClient(value), "Use a gno core client id, e.g. ethereum-1, never client-0.");
    decimal(value.slice(value.lastIndexOf("-") + 1), MAX_UINT64);
    return value;
  }
  if (p.type === "denom") {
    requireValue(DENOM.test(value), "Denom must be 1–128 ASCII letters, digits or /:._-.");
    if (name === "baseDenom")
      requireValue(
        WIRE.test(value),
        "Use a native wire denom or gno.land:r:segment:segment[.slug] alias.",
      );
    if (a.id === "denom.set" && all.kind === "mint") {
      if (/^ibc\/[0-9a-fA-F]{64}$/.test(value)) return `ibc/${value.slice(4).toUpperCase()}`;
      if (/^0x[0-9a-fA-F]{40}$/.test(value)) return value.toLowerCase();
    }
    return value;
  }
  if (p.type === "hex") {
    if (name === "addrs")
      return list(value, 64)
        .map((s) => ethAddress(s))
        .join(",");
    if (name === "remoteToken" && all.kind === "mint") {
      requireValue(value === "", "A mint denom has an empty remoteToken.");
      return "";
    }
    if (
      [
        "token",
        "voucher",
        "proxy",
        "implementation",
        "exec",
        "account",
        "lightClient",
        "remoteToken",
      ].includes(name)
    )
      return ethAddress(value, name === "remoteToken");
    const s = value.toLowerCase();
    if (name === "merklePrefix") {
      requireValue(
        list(s, 64).every((v) => /^(?:[0-9a-f]{2})+$/.test(v)),
        "Merkle prefix items must be even-length hex, without 0x.",
      );
    } else {
      const hex = name === "paramsHex" ? s : s.replace(/^0x/, "");
      requireValue(/^(?:[0-9a-f]{2})*$/.test(hex), "Use even-length hex bytes only.");
      if (name === "codeHash")
        requireValue(hex.length === 64, "Code hash must be exactly 32 bytes.");
      if (name !== "call") requireValue(hex.length > 0, "Hex bytes are required.");
    }
    return s;
  }
  if (p.type === "string") {
    if (name === "incident")
      requireValue(
        /^[a-z0-9-]{1,32}$/.test(value),
        "Incident must be 1–32 lowercase letters, digits or '-'.",
      );
    else if (name === "portID")
      requireValue(/^[a-zA-Z0-9._+-]{1,128}$/.test(value), "Use an ASCII port identifier.");
    else if (name === "pkgPath")
      requireValue(
        /^gno\.land\/r\/[a-zA-Z0-9_]+(?:\/[a-zA-Z0-9_]+)+$/.test(value),
        "Use a realm package path.",
      );
    else if (name === "toVersion")
      requireValue(
        /^ethereum\/v[0-9]+$/.test(value),
        "Use an Ethereum verifier version, e.g. ethereum/v1.",
      );
    else if (name === "path" && a.id !== "eth.apply_epoch_notice") {
      const prefix =
        a.id === "core.update_impl"
          ? "gno.land/r/aib/ibc/core/impl/v"
          : "gno.land/r/aib/ibc/apps/transfer/impl/v";
      requireValue(
        value.startsWith(prefix) && /^[0-9]+$/.test(value.slice(prefix.length)),
        `Use ${prefix}<N>.`,
      );
    } else if (name === "key") packet(value);
    else if (name === "keys") {
      const parts = list(value, 64, a.id === "flag.set");
      parts.forEach(packet);
      if (a.id === "flag.set" && parts.length)
        requireValue(
          all.fn === "payouts" && ctx.roles.includes("guardian"),
          "Only a guardian payouts flag may name packets.",
        );
    } else if (name === "fns")
      requireValue(
        list(value, 4).every((s) => enums.fn.includes(s)) &&
          new Set(value.split(",")).size === value.split(",").length,
        "Choose distinct sends,credits,payouts,refunds.",
      );
    else if (name === "bridge") {
      const parts = value.split(" ");
      requireValue(
        parts.length === 3,
        "Name router, ICS20 and IBCAdmin, separated by single spaces.",
      );
      const addrs = parts.map((s) => ethAddress(s, true));
      requireValue(new Set(addrs).size === 3, "Bridge contract addresses must be distinct.");
      return addrs.join(" ");
    } else if (name === "changes") {
      const parts = list(value, 6);
      const seen = new Set<string>();
      const next = a.id === "seats.set" ? { ...ctx.seats } : ({} as Record<string, string>);
      let guardians = 0;
      for (const part of parts) {
        const [seat, addr, extra] = part.split(":");
        requireValue(
          /^[0-5]$/.test(seat) && extra === undefined && addr !== undefined && !seen.has(seat),
          "Each seat (0–5) may appear once as seat:address.",
        );
        seen.add(seat);
        if (parseInt(seat, 10) < 4) guardians++;
        if (a.id === "seats.set") {
          requireValue(
            addr === "" || validGnoAddress(addr.toLowerCase()),
            "A seat needs a valid g1 address, or an empty address to vacate.",
          );
          requireValue(
            addr === "" || ctx.seats[seat] !== addr.toLowerCase(),
            "This address already holds that seat.",
          );
          next[seat] = addr.toLowerCase();
        } else next[seat] = ethAddress(addr);
      }
      requireValue(guardians <= 2, "A decision changes at most two guardian seats.");
      if (a.id === "seats.set") {
        const occupied = Object.values(next).filter(Boolean);
        requireValue(
          new Set(occupied).size === occupied.length,
          "One address cannot hold two seats.",
        );
      }
      return parts.map((s) => s.toLowerCase()).join(",");
    } else if (name === "items") {
      const seen = new Set<string>();
      for (const item of list(value, a.id === "eth.settle" ? 8 : 64)) {
        const parts = item.split(":");
        requireValue(
          parts.length === 3 && /^[1-5]$/.test(parts[1]),
          "Settlement items are seq:action:amount, action 1–5.",
        );
        const seq = decimal(parts[0], MAX_UINT64, 1n);
        const amount = BigInt(decimal(parts[2]));
        const action = parts[1];
        const key = `${action === "5" ? "in" : "out"}:${seq}`;
        requireValue(!seen.has(key), "Each sequence may appear only once per direction.");
        seen.add(key);
        requireValue(
          !["2", "3"].includes(action) || amount === 0n,
          "Rejected refunds and deliveries must name amount 0.",
        );
        requireValue(
          !["4", "5"].includes(action) || amount > 0n,
          "Forged refund / receive must name a positive amount.",
        );
      }
    } else if (name === "ranges") {
      for (const item of list(value, 14)) {
        requireValue(/^[0-9]+(?:-[0-9]+)?$/.test(item), "Use n or from-to ranges.");
        const [from, to = from] = item.split("-");
        const low = BigInt(decimal(from, MAX_UINT64, 1n));
        const high = BigInt(decimal(to, MAX_UINT64, low));
        requireValue(high <= BigInt(ctx.lastSeq), "Range exceeds the live last sequence.");
      }
    } else if (name === "path" && a.id === "eth.apply_epoch_notice") {
      const steps = list(value, 64);
      requireValue(
        BigInt(steps.length) === BigInt(all.epoch) - BigInt(ctx.epoch),
        "Path length must equal epoch minus current epoch.",
      );
      for (const step of steps) {
        const parts = step.split("/");
        requireValue(
          parts.length === 2 && routerClient(parts[0]) && gnoClient(parts[1]),
          "Path steps are routerClient/gnoClient.",
        );
      }
      requireValue(
        steps.at(-1)?.split("/")[1] === all.client,
        "Path must end at the proven notice's gno client.",
      );
    } else throw new Error(`No safe input grammar for ${name}; use external tooling.`);
    return value;
  }
  throw new Error(`Unsupported parameter type: ${p.type}.`);
}

export function validateValues(a: Action, values: Values, ctx: ValidationContext): Values {
  const limitation = browserLimitation(a);
  if (limitation) throw new Error(limitation);
  const result: Values = {};
  for (const p of a.params) result[p.name] = validateParam(a, p, values[p.name] ?? "", values, ctx);
  if (a.id === "denom.set") {
    const n = (name: string) => BigInt(result[name]);
    requireValue(
      n("capFloor") >= 4n * n("fee") && n("fee") >= n("feeFloor"),
      "Require capFloor ≥ 4 × fee and fee ≥ feeFloor.",
    );
    const canonicalDenom = ctx.denomAliases?.[result.denom] ?? result.denom;
    const old = ctx.route.denoms.find((d) => d.denom === canonicalDenom);
    if (old) {
      requireValue(old.kind === result.kind, "An existing denom's accounting kind cannot change.");
      requireValue(
        n("capFloor") >= BigInt(old.launch_cap_floor),
        "Cap floor cannot fall below the live launch cap floor.",
      );
      if (old.kind === "mint")
        requireValue(
          result.decimals === String(old.decimals),
          "An existing voucher's decimals cannot change.",
        );
    }
    if (result.kind === "unescrow") {
      requireValue(
        WIRE.test(result.denom) && !result.denom.startsWith("ibc/"),
        "Unescrow requires ugnot / a native wire denom or GRC20 alias.",
      );
      requireValue(
        n("supplyCap") === 0n &&
          n("decimals") === 0n &&
          n("sendMin") > 0n &&
          n("sendMax") >= n("sendMin"),
        "Unescrow: supplyCap = decimals = 0; 0 < sendMin ≤ sendMax.",
      );
    } else {
      requireValue(
        /^ibc\/[A-Fa-f0-9]{64}$/.test(result.denom) || ETH.test(result.denom.toLowerCase()),
        "Mint requires ibc/<64 hex hash> or an Ethereum token address.",
      );
      requireValue(
        n("supplyCap") > 0n && n("sendMin") === 0n && n("sendMax") === 0n,
        "Mint: supplyCap > 0; sendMin = sendMax = 0.",
      );
    }
  }
  if (
    a.id === "recapitalize" &&
    !result.denom.startsWith("gno.land:r:") &&
    !result.denom.startsWith("ibc/")
  )
    requireValue(
      BigInt(result.amount) <= MAX_INT,
      "A coin recapitalization amount must fit int64.",
    );
  if (result.newClientID && result.oldClientID)
    requireValue(result.newClientID !== result.oldClientID, "New and old clients must differ.");
  if (a.id === "eth.client" && result.op === "1" && ctx.counterparties)
    requireValue(
      ctx.counterparties[result.counterpartyClientID] === result.clientID,
      "The new gno client must be paired with this custom router id.",
    );
  if (
    result.localClientID &&
    result.newClientID &&
    ctx.counterparties &&
    !/remove_expectation|purge_tombstone/.test(a.id)
  )
    requireValue(
      ctx.counterparties[result.localClientID] === result.newClientID,
      "The carrier's counterparty must equal the new router client.",
    );
  return result;
}
