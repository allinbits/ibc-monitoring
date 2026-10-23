import { liveFixture, governorFixture, actionFixture } from "./test-fixtures";
import { describe, expect, it } from "vitest";
import raw from "./catalog.json";
import { actions, actionById, browserLimitation } from "./catalog";
import { defaultValues } from "./actions";
import { buildTransaction, canPerform, goString, renderSource, type RoleState } from "./messages";
import {
  decimal,
  MAX_UINT256,
  MAX_UINT64,
  validateParam,
  validateValues,
  validGnoAddress,
  type Values,
} from "./validation";

const addr = raw.meta.live_state.governor;
const eth = raw.meta.live_state.ethereum.test_token.toLowerCase();
const dao: RoleState = {
  ...governorFixture,
  yielded: "1000",
  govdaoGoverns: true,
  roles: ["anyone", "govdao"],
  tier: "T1",
};
function param(id: string, name: string, value: string, other: Values = {}) {
  const a = actionById(id);
  return validateParam(
    a,
    a.params.find((p) => p.name === name)!,
    value,
    { ...actionFixture(a), ...other },
    liveFixture,
  );
}

describe("catalog coverage and message generation", () => {
  it("accounts for all 78 actions, with 75 typed forms and three explained tooling / builder entries", () => {
    expect(actions).toHaveLength(78);
    expect(new Set(actions.map((a) => a.id)).size).toBe(78);
    expect(actions.filter(browserLimitation).map((a) => a.id)).toEqual([
      "govern",
      "transfer.recapitalize",
      "govdao.propose",
    ]);
    expect(actions.filter((a) => a.kind).map((a) => a.kind)).toEqual(
      Array.from({ length: 13 }, (_, i) => i + 1),
    );
  });
  for (const action of actions.filter((a) => !browserLimitation(a))) {
    it(`builds the catalog message for ${action.id} with no remaining placeholders`, () => {
      const values = actionFixture(action);
      const state: RoleState = { ...governorFixture, roles: liveFixture.roles };
      const tx = buildTransaction(action, values, addr, state, liveFixture);
      expect(tx.messages).toHaveLength(1);
      expect(tx.messages[0].type).toBe(action.tx.type);
      expect(tx.messages[0].value.caller).toBe(addr);
      expect(tx.messages[0].value.max_deposit).toBe("");
      expect(JSON.stringify(tx)).not.toContain("{{");
      if (action.tx.governor_source) {
        const proposed = buildTransaction(action, values, addr, dao, liveFixture);
        expect(JSON.stringify(proposed)).toContain("MustCreateProposal");
        expect(JSON.stringify(proposed)).toContain(".Proposal()");
        expect(JSON.stringify(proposed)).not.toContain("ibcgov.Govern(");
      }
    });
  }
  it("builds exact m_call and m_run Adena value shapes", () => {
    const tx = buildTransaction(
      actionById("flag.set"),
      { route: "ethereum-1", fn: "sends", keys: "" },
      addr,
      { ...governorFixture, roles: ["anyone", "guardian"] },
      liveFixture,
    );
    expect(tx).toEqual({
      messages: [
        {
          type: "/vm.m_call",
          value: {
            caller: addr,
            send: "",
            pkg_path: "gno.land/r/aib/ibc/ibcgov",
            func: "SetFlag",
            args: ["ethereum-1", "sends", ""],
            max_deposit: "",
          },
        },
      ],
      memo: "",
    });
    const run = buildTransaction(actionById("core.pause"), {}, addr, governorFixture, liveFixture);
    expect(run).toEqual({
      messages: [
        {
          type: "/vm.m_run",
          value: {
            caller: addr,
            send: "",
            package: {
              name: "main",
              path: "",
              files: [{ name: "main.gno", body: actionById("core.pause").tx.governor_source }],
            },
            max_deposit: "",
          },
        },
      ],
      memo: "",
    });
    expect(
      buildTransaction(actionById("yield"), {}, addr, governorFixture, liveFixture).messages[0],
    ).toMatchObject({ value: { args: [] } });
  });
  it("filters governor, GovDAO, guardian, watch and anyone correctly across yield", () => {
    const count = (state: RoleState) => actions.filter((a) => canPerform(a, state)).length;
    expect(count(governorFixture)).toBe(70);
    expect(count(dao)).toBe(74);
    expect(count({ ...dao, yielded: "0", govdaoGoverns: false })).toBe(17);
    expect(count({ ...governorFixture, roles: ["anyone", "guardian"] })).toBe(13);
    expect(count({ ...governorFixture, roles: ["anyone", "watch"] })).toBe(13);
    expect(count({ ...governorFixture, roles: ["anyone"] })).toBe(11);
    expect(() =>
      buildTransaction(
        actionById("core.pause"),
        {},
        addr,
        { ...governorFixture, yielded: "1", govdaoGoverns: true },
        liveFixture,
      ),
    ).toThrow();
  });
});

describe("typed input whitelists", () => {
  it("keeps uint256 and uint64 exact, normalises decimal literals, and rejects signs / exponents / overflow", () => {
    expect(decimal(MAX_UINT256.toString())).toBe(MAX_UINT256.toString());
    expect(decimal("000009")).toBe("9");
    expect(param("eth.role", "roleID", (MAX_UINT64 - 1n).toString())).toBe(
      (MAX_UINT64 - 1n).toString(),
    );
    for (const bad of [
      "-1",
      "+1",
      " 1",
      "1 ",
      "1e18",
      "0xff",
      "1.0",
      "１２",
      "١",
      (MAX_UINT256 + 1n).toString(),
    ])
      expect(() => decimal(bad)).toThrow();
    expect(() => param("eth.role", "roleID", MAX_UINT64.toString())).toThrow();
    expect(() => param("eth.role", "roleID", "0")).toThrow();
    expect(() => param("flag.prune", "n", "65")).toThrow();
    expect(param("transfer.claim", "n", "21")).toBe("21");
    expect(() => param("route.replace", "suspectFrom", "100001")).toThrow();
  });
  it("validates address checksums and canonicalises gno and Ethereum addresses", () => {
    expect(validGnoAddress(addr)).toBe(true);
    expect(validGnoAddress(`${addr.slice(0, -1)}q`)).toBe(false);
    expect(param("transfer.block", "addr", addr.toUpperCase())).toBe(addr);
    expect(param("eth.role", "account", raw.meta.live_state.ethereum.test_token)).toBe(eth);
    expect(() => param("eth.role", "account", "0x12")).toThrow();
    expect(() => param("denom.set", "remoteToken", `0x${"0".repeat(40)}`)).toThrow();
  });
  it("enforces the current router, custom-id and gno-id grammars, including conditional empty ids", () => {
    expect(param("eth.limits", "clientID", "client-0")).toBe("client-0");
    for (const wrong of [
      "ethereum-1",
      "client-1",
      "client-00",
      "channel-0",
      "client-18446744073709551616",
    ])
      expect(() => param("eth.limits", "clientID", wrong)).toThrow();
    expect(param("eth.unpause", "clientID", "", { object: "2" })).toBe("");
    expect(() => param("eth.unpause", "clientID", "client-0", { object: "2" })).toThrow();
    expect(param("eth.client", "clientID", "new-id_+#[<>]", { op: "1" })).toBe("new-id_+#[<>]");
    expect(() => param("eth.client", "clientID", "client-7", { op: "1" })).toThrow();
    expect(() => param("flag.set", "route", "client-0")).toThrow();
    expect(() => param("flag.set", "route", "ethereum-18446744073709551616")).toThrow();
    const action = actionById("eth.client");
    expect(() =>
      validateValues(action, { ...actionFixture(action), clientID: "wrong-custom" }, liveFixture),
    ).toThrow(/paired/);
  });
  it("uses exact denom / GRC20 wire alias and hex grammars", () => {
    for (const good of ["ugnot", "ibc/ABCD", "gno.land:r:aib:token.TEST_1"])
      expect(param("credit", "denom", good)).toBe(good);
    for (const good of ["ugnot", "gno.land:r:my-org:tokens:token.TEST_1"])
      expect(param("eth.voucher", "baseDenom", good)).toBe(good);
    for (const bad of [
      "/gno.land/r/demo:x",
      "gno.land:r:demo",
      "gno.land:r:my--org:token.x",
      "gno.land:r:org:token.",
      "UGNOT",
    ])
      expect(() => param("eth.voucher", "baseDenom", bad)).toThrow();
    expect(param("eth.upgrade", "call", "")).toBe("");
    expect(param("eth.upgrade", "call", "0xAbCd")).toBe("0xabcd");
    for (const bad of ["abc", "0xgg", "ab//", "ab\\cd"])
      expect(() => param("eth.upgrade", "call", bad)).toThrow();
    expect(() => param("eth.upgrade", "codeHash", "a".repeat(63))).toThrow();
    expect(() => param("core.migrate_client_fork_schedule", "paramsHex", "0x00")).toThrow();
  });
  it("checks seats, named-packet flags, incident labels, ranges, settlement bounds and enums", () => {
    expect(() => param("seats.set", "changes", `0:${addr},1:${addr}`)).toThrow(/two seats/);
    expect(() => param("seats.set", "changes", "0:,1:,2:")).toThrow(/two guardian/);
    expect(() => param("seats.set", "changes", `0:${liveFixture.seats["0"]}`)).toThrow(/already/);
    expect(param("seats.set", "changes", "5:")).toBe("5:");
    expect(() => param("void", "keys", "ethereum-1/7")).toThrow();
    const flag = actionById("flag.set");
    expect(() =>
      validateValues(
        flag,
        { route: "ethereum-1", fn: "payouts", keys: "ethereum-1/00000000000000000007" },
        { ...liveFixture, roles: ["anyone", "watch"] },
      ),
    ).toThrow(/guardian/);
    expect(() => param("void", "incident", "Free text")).toThrow();
    expect(() => param("eth.cancel", "ranges", "2-4")).toThrow();
    expect(() => param("eth.settle", "items", "1:4:0")).toThrow();
    expect(() => param("eth.settle", "items", "1:2:1")).toThrow();
    expect(() => param("eth.settle", "items", "1:3:0,1:4:2")).toThrow(/once/);
    expect(param("eth.settle", "items", "1:3:0,1:5:2")).toBe("1:3:0,1:5:2");
    expect(() => param("govdao.vote", "option", "yes")).toThrow();
    expect(() => param("void", "forged", "True")).toThrow();
  });
  it("enforces denom relationships and live launch floor", () => {
    const action = actionById("denom.set");
    const values = defaultValues(action, liveFixture);
    expect(values.capFloor).toBe("100000000000");
    expect(values.decimals).toBe("0");
    expect(() =>
      validateValues(action, { ...values, capFloor: "50000000000" }, liveFixture),
    ).toThrow(/launch/);
    expect(() => validateValues(action, { ...values, fee: "99999" }, liveFixture)).toThrow();
    expect(() => validateValues(action, { ...values, sendMin: "0" }, liveFixture)).toThrow();
    expect(() => validateValues(action, { ...values, decimals: "1" }, liveFixture)).toThrow();
    expect(() => validateValues(action, { ...values, supplyCap: "1" }, liveFixture)).toThrow();
  });
});

describe("Gno source security boundary", () => {
  const injections = [
    '1);panic("owned");//',
    '"',
    "\\",
    "\n",
    ")",
    "//",
    "\r\n",
    "\u2028",
    "\u200b",
    "\u202e",
    "ｅthereum-1",
    "ethereum-１",
  ];
  for (const action of actions.filter((a) => !browserLimitation(a))) {
    it(`rejects or safely quotes injection attempts in every ${action.id} input`, () => {
      const values = actionFixture(action);
      for (const p of action.params)
        for (const injection of injections) {
          let built: ReturnType<typeof buildTransaction> | undefined;
          try {
            built = buildTransaction(
              action,
              { ...values, [p.name]: injection },
              addr,
              { ...governorFixture, roles: liveFixture.roles },
              liveFixture,
            );
            // Some punctuation is legal *data* in a denom (e.g. //). It must
            // stay inside a full quoted Go literal, or a call argument.
          } catch (error) {
            expect(error).toBeInstanceOf(Error);
          }
          if (built) {
            const message = built.messages[0];
            if (message.type === "/vm.m_run")
              expect(message.value.package.files[0].body).toContain(goString(injection));
            else expect(message.value.args).toContain(injection);
          }
        }
    });
  }
  it("escapes Go string literals, rejects raw string holes and undeclared placeholders, and prevents octal numbers", () => {
    expect(goString('"\\\n\r\t\u2028')).toBe('"\\"\\\\\\n\\r\\t\\u2028"');
    expect(goString("🧨")).toBe('"\\U0001f9e8"');
    expect(() => goString("\ud800")).toThrow(/surrogate/);
    const a = actionById("core.replace_verifier");
    expect(() => renderSource(a, "{{toVersion}}", actionFixture(a), liveFixture)).toThrow(
      /Raw source/,
    );
    expect(() => renderSource(a, '"prefix{{toVersion}}"', actionFixture(a), liveFixture)).toThrow();
    expect(() => renderSource(a, '"{{missing}}"', actionFixture(a), liveFixture)).toThrow(
      /undeclared/,
    );
    const source = buildTransaction(
      actionById("cancel"),
      { id: "000009" },
      addr,
      governorFixture,
      liveFixture,
    );
    expect(JSON.stringify(source)).toContain("NewCancelRequest(cross(cur), 9)");
    for (const id of ["govern", "govdao.propose", "transfer.recapitalize"])
      expect(() =>
        validateValues(actionById(id), { EXPR: 'panic("owned")' }, liveFixture),
      ).toThrow();
  });
});
