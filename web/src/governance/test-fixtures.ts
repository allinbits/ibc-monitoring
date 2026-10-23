import raw from "./catalog.json";
import type { Action } from "./catalog";
import { defaultValues } from "./actions";
import type { RoleState } from "./messages";
import type { LiveState } from "./rpc";
import type { Values } from "./validation";
const addr = raw.meta.live_state.governor;
const eth = raw.meta.live_state.ethereum.test_token.toLowerCase();
export const liveFixture: LiveState = {
  governor: addr,
  yielded: "0",
  govdaoGoverns: false,
  epoch: "1",
  lastSeq: "3",
  height: "100000",
  routerClient: "client-0",
  seats: raw.meta.live_state.seats,
  roles: ["anyone", "governor", "govdao", "guardian"],
  held: false,
  holdSince: 0,
  decisions: [],
  dispatches: [],
  chainTime: 10000,
  readAt: 0,
  route: {
    route: "ethereum-1",
    client: "ethereum-1",
    replaced: [],
    fee_collector: addr,
    denoms: [
      {
        denom: "ugnot",
        kind: "unescrow",
        cap_floor: "100000000000",
        launch_cap_floor: "100000000000",
        supply_cap: "0",
        fee: "1000000",
        fee_floor: "100000",
        send_min: "2000000",
        send_max: "25000000000",
        refund_cap: "100000000000",
        remote: eth,
        decimals: 0,
      },
    ],
  },
  counterparties: { "ethereum-1": "client-0", "ethereum-2": "gno-test-2" },
};
export const governorFixture: RoleState = {
  governor: addr,
  yielded: "0",
  govdaoGoverns: false,
  roles: ["anyone", "governor"],
  seat: -1,
};
export function actionFixture(a: Action): Values {
  const values = defaultValues(a, liveFixture);
  const examples: Values = {
    id: "1",
    pid: "0",
    n: "1",
    seq: "1",
    epoch: "1",
    addr,
    to: addr,
    feeCollector: addr,
    path:
      a.id === "core.update_impl"
        ? "gno.land/r/aib/ibc/core/impl/v1"
        : "gno.land/r/aib/ibc/apps/transfer/impl/v1",
    portID: "transfer",
    pkgPath: "gno.land/r/demo/foo",
    clientID: "ethereum-1",
    subjectClientID: "ethereum-1",
    substituteClientID: "ethereum-2",
    client: "ethereum-1",
    newClientID:
      a.params.find((p) => p.name === "newClientID")?.type === "router client id"
        ? "gno-test-2"
        : "ethereum-2",
    localClientID: "ethereum-2",
    oldClientID: "client-0",
    counterpartyClientID: "ethereum-2",
    paramsHex: "00",
    toVersion: "ethereum/v1",
    changes: a.id === "eth.seats" ? `5:${eth}` : "5:",
    keys: "ethereum-1/00000000000000000007",
    key: "ethereum-1/00000000000000000007",
    bridge: Object.values(raw.meta.live_state.ethereum).slice(0, 3).join(" "),
    addrs: eth,
    forged: "false",
    amount: "1",
    chain: "gno",
    cause: "collision",
    items: "1:3:0",
    suspectFrom: "0",
    ranges: "1-3",
    lightClient: eth,
    codeHash: "ab".repeat(32),
    proxy: eth,
    implementation: eth,
    grant: "true",
    roleID: "1",
    account: eth,
    exec: eth,
    option: "YES",
  };
  for (const p of a.params)
    if (!values[p.name] || ["keys", "changes", "counterpartyClientID"].includes(p.name))
      values[p.name] = examples[p.name] ?? "";
  if (a.id === "flag.set") values.fn = "payouts";
  if (a.id === "eth.client") values.clientID = "gno-test-2";
  if (a.id === "eth.apply_epoch_notice") {
    values.epoch = "2";
    values.path = "client-0/ethereum-1";
  }
  return values;
}
