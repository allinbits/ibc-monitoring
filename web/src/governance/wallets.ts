import { evmChainId, evmText, network } from "../network";
import { GNO_CHAIN, GNO_RPC, IBC_ADMIN } from "./catalog";
import type { Transaction } from "./messages";
import { decimal, MAX_UINT64, validGnoAddress } from "./validation";

export interface AdenaResponse<T = Record<string, unknown>> {
  code: number;
  status: string;
  type: string;
  message: string;
  data: T;
}
export interface AdenaAccount {
  address: string;
  chainId: string;
}
export interface Broadcast {
  hash: string;
  height?: string;
  checkTx?: unknown;
  deliverTx?: unknown;
}
export interface Adena {
  AddEstablish(name: string): Promise<AdenaResponse>;
  GetAccount(): Promise<AdenaResponse<AdenaAccount>>;
  AddNetwork(network: {
    chainId: string;
    rpcUrl: string;
    chainName: string;
  }): Promise<AdenaResponse>;
  SwitchNetwork(chainId: string): Promise<AdenaResponse>;
  DoContract(tx: Transaction): Promise<AdenaResponse<Broadcast>>;
}
export interface EvmProvider {
  request(args: { method: string; params?: unknown[] }): Promise<unknown>;
  isMetaMask?: boolean;
  providers?: EvmProvider[];
}
declare global {
  interface Window {
    adena?: Adena;
    ethereum?: EvmProvider;
  }
}
function result<T>(response: AdenaResponse<T>, allowed: string[] = []): T {
  if (response.status !== "success" && !allowed.includes(response.type))
    throw new Error(response.message || response.type || "Adena request failed.");
  return response.data;
}
async function envelope<T>(
  operation: () => Promise<AdenaResponse<T>>,
  allowed: string[],
): Promise<AdenaResponse<T>> {
  try {
    return await operation();
  } catch (error) {
    if (
      error &&
      typeof error === "object" &&
      "type" in error &&
      allowed.includes(String(error.type))
    )
      return error as AdenaResponse<T>;
    throw error;
  }
}
export async function readAdenaAccount(): Promise<AdenaAccount> {
  if (!window.adena) throw new Error("Install the Adena extension, then reload this page.");
  const account = result(await window.adena.GetAccount());
  if (!validGnoAddress(account.address)) throw new Error("Adena returned an invalid gno address.");
  if (account.chainId !== GNO_CHAIN)
    throw new Error(`Use Connect Adena to switch to ${GNO_CHAIN}.`);
  return account;
}
export async function connectAdena(): Promise<AdenaAccount> {
  const adena = window.adena;
  if (!adena) throw new Error("Install the Adena browser extension, then reload this page.");
  result(await envelope(() => adena.AddEstablish("gno.land IBC Monitor"), ["ALREADY_CONNECTED"]), [
    "ALREADY_CONNECTED",
  ]);
  const switched = await envelope(
    () => adena.SwitchNetwork(GNO_CHAIN),
    ["UNADDED_NETWORK", "REDUNDANT_CHANGE_REQUEST"],
  );
  if (switched.type === "UNADDED_NETWORK") {
    result(
      await envelope(
        () =>
          adena.AddNetwork({
            chainId: GNO_CHAIN,
            chainName: `gno.land · ${GNO_CHAIN}`,
            rpcUrl: GNO_RPC,
          }),
        ["NETWORK_ALREADY_EXISTS"],
      ),
      ["NETWORK_ALREADY_EXISTS"],
    );
    result(await envelope(() => adena.SwitchNetwork(GNO_CHAIN), ["REDUNDANT_CHANGE_REQUEST"]), [
      "REDUNDANT_CHANGE_REQUEST",
    ]);
  } else result(switched, ["REDUNDANT_CHANGE_REQUEST"]);
  return readAdenaAccount();
}
export function vmFailure(value: unknown): string | undefined {
  if (!value || typeof value !== "object") return;
  const obj = value as Record<string, unknown>;
  const base = (obj.ResponseBase ?? obj) as Record<string, unknown>;
  if (base.Error || base.error || (base.code !== undefined && String(base.code) !== "0"))
    return String(base.Log ?? base.log ?? JSON.stringify(base.Error ?? base.error));
}
export async function signAdena(expected: string, tx: Transaction): Promise<Broadcast> {
  const account = await readAdenaAccount();
  if (account.address !== expected || tx.messages[0].value.caller !== expected)
    throw new Error("Your Adena account changed. Reconnect and review the action again.");
  const data = result(await window.adena!.DoContract(tx));
  if (!/^(?:0x)?[a-fA-F0-9]{64}$/.test(data.hash ?? ""))
    throw new Error("Adena did not return a valid transaction hash.");
  // Preserve the hash even if broadcast already contains an execution failure;
  // the outcome component reports that failure alongside the transaction link.
  return data;
}
export function metaMask(): EvmProvider | undefined {
  if (typeof window === "undefined") return;
  return (window.ethereum?.providers ?? (window.ethereum ? [window.ethereum] : [])).find(
    (p) => p.isMetaMask,
  );
}
/** keccak256('execute(uint64)')[0:4] = b590be77, followed by one ABI word. */
export function executeCalldata(seq: string): string {
  return `0xb590be77${BigInt(decimal(seq, MAX_UINT64, 1n))
    .toString(16)
    .padStart(64, "0")}`;
}
export function ethereumTransaction(from: string, seq: string) {
  if (!/^0x[0-9a-fA-F]{40}$/.test(from)) throw new Error("MetaMask returned an invalid account.");
  return { from, to: IBC_ADMIN, value: "0x0", data: executeCalldata(seq) };
}
export async function executeEthereum(
  seq: string,
  provider: EvmProvider,
  expectedEpoch?: string,
): Promise<string> {
  executeCalldata(seq);
  if (!/^0x[0-9a-fA-F]{40}$/.test(IBC_ADMIN) || BigInt(IBC_ADMIN) === 0n)
    throw new Error("IBCAdmin is not configured for this route.");
  if ((await provider.request({ method: "eth_chainId" })) !== evmChainId()) {
    try {
      await provider.request({
        method: "wallet_switchEthereumChain",
        params: [{ chainId: evmChainId() }],
      });
    } catch (error) {
      if ((error as { code?: number }).code !== 4902) throw error;
      await provider.request({
        method: "wallet_addEthereumChain",
        params: [
          {
            chainId: evmChainId(),
            chainName: network.evm.network_name,
            nativeCurrency: network.evm.native_currency,
            rpcUrls: [network.evm.rpc_url],
            blockExplorerUrls: [network.evm.explorer_url],
          },
        ],
      });
      await provider.request({
        method: "wallet_switchEthereumChain",
        params: [{ chainId: evmChainId() }],
      });
    }
  }
  const accounts = (await provider.request({ method: "eth_requestAccounts" })) as string[];
  if (!accounts?.[0]) throw new Error(evmText("MetaMask did not share an Ethereum account."));
  if ((await provider.request({ method: "eth_chainId" })) !== evmChainId())
    throw new Error(evmText("Switch MetaMask to Sepolia before continuing."));
  if (expectedEpoch !== undefined) {
    const epoch = await provider.request({
      method: "eth_call",
      params: [{ to: IBC_ADMIN, data: "0x900cf0cf" }, "latest"],
    });
    if (
      typeof epoch !== "string" ||
      !/^0x[0-9a-fA-F]+$/.test(epoch) ||
      BigInt(epoch) !== BigInt(decimal(expectedEpoch, (1n << 32n) - 1n))
    )
      throw new Error("IBCAdmin changed epoch. Refresh governance before executing this sequence.");
  }
  const tx = ethereumTransaction(accounts[0], seq);
  // Dry-run the exact call against the wallet's live RPC before signing:
  // this rejects a still-delayed, held, spent, or already executed decision.
  await provider.request({ method: "eth_call", params: [tx, "latest"] });
  const hash = await provider.request({ method: "eth_sendTransaction", params: [tx] });
  if (typeof hash !== "string" || !/^0x[0-9a-fA-F]{64}$/.test(hash))
    throw new Error("MetaMask returned no transaction hash.");
  return hash;
}
