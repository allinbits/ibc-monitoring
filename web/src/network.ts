/** Public settings supplied by /api/v1/info before any route or wallet is used. */
export interface Network {
  gno: {
    chain_id: string;
    rpc_url: string;
    web_url: string;
    route_client_id: string;
    client_type: "ethereum" | "arc";
    core: string;
    transfer: string;
    ibcgov: string;
  };
  evm: {
    chain_id: string;
    chain_name: string;
    network_name: string;
    rpc_url: string;
    explorer_url: string;
    admin_address: string;
    native_currency: { name: string; symbol: string; decimals: number };
  };
  poll_interval_ms: number;
}
export let network: Network;
export let GNO_RPC = "";
export let GNO_CHAIN = "";
export let GNOWEB = "";
export let GOV = "";
export let CORE = "";
export let TRANSFER = "";
export let IBC_ADMIN = "";
export function configureNetwork(value: Network) {
  if (!value?.gno?.chain_id || !value?.evm?.chain_id)
    throw new Error("The monitor did not supply its network configuration.");
  network = value;
  GNO_RPC = value.gno.rpc_url;
  GNO_CHAIN = value.gno.chain_id;
  GNOWEB = value.gno.web_url.replace(/\/$/, "");
  GOV = value.gno.ibcgov;
  CORE = value.gno.core;
  TRANSFER = value.gno.transfer;
  IBC_ADMIN = value.evm.admin_address;
}
export function evmChainId(): string {
  return `0x${BigInt(network.evm.chain_id).toString(16)}`;
}
export function evmTxURL(hash: string): string {
  return `${network.evm.explorer_url.replace(/\/$/, "")}/tx/${encodeURIComponent(hash)}`;
}
/** Translate presentation copy only; realm function names and wire keys stay intact. */
export function evmText(text: string): string {
  if (!network) return text;
  return text
    .replace(
      /Sepolia Etherscan/g,
      network.evm.chain_name === "Ethereum"
        ? "Sepolia Etherscan"
        : `${network.evm.chain_name} explorer`,
    )
    .replace(/Ethereum/g, network.evm.chain_name)
    .replace(/Sepolia/g, network.evm.network_name)
    .replace(/test-ibc-1/g, network.gno.chain_id)
    .replace(/ethereum-1/g, network.gno.route_client_id)
    .replace(/ethereum\/v(?=[0-9])/g, `${network.gno.client_type}/v`);
}
