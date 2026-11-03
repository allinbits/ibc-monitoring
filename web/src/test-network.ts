import type { Network } from "./network";
import raw from "./governance/catalog.json";

export const sepoliaNetwork: Network = {
  gno: {
    chain_id: "test-ibc-1",
    rpc_url: "https://rpc.2-29-108-12.sslip.io",
    web_url: "https://gnoweb.2-29-108-12.sslip.io",
    route_client_id: "ethereum-1",
    client_type: "ethereum",
    core: "gno.land/r/aib/ibc/core",
    transfer: "gno.land/r/aib/ibc/apps/transfer",
    ibcgov: "gno.land/r/aib/ibc/ibcgov",
  },
  evm: {
    chain_id: "11155111",
    chain_name: "Ethereum",
    network_name: "Sepolia",
    rpc_url: "https://rpc.sepolia.ethpandaops.io",
    explorer_url: "https://sepolia.etherscan.io",
    admin_address: raw.meta.live_state.ethereum.ibc_admin,
    native_currency: { name: "Sepolia Ether", symbol: "ETH", decimals: 18 },
  },
  poll_interval_ms: 10000,
};
export const arcNetwork: Network = {
  ...sepoliaNetwork,
  gno: {
    ...sepoliaNetwork.gno,
    chain_id: "test-arc-1",
    rpc_url: "https://rpc-2.2-29-108-12.sslip.io",
    web_url: "https://gnoweb-2.2-29-108-12.sslip.io",
    route_client_id: "arc-1",
    client_type: "arc",
  },
  evm: {
    chain_id: "5042002",
    chain_name: "Arc testnet",
    network_name: "Arc testnet",
    rpc_url: "https://rpc.testnet.arc.io",
    explorer_url: "https://testnet.arcscan.app",
    admin_address: "0x1111111111111111111111111111111111111111",
    native_currency: { name: "USDC", symbol: "USDC", decimals: 18 },
  },
  poll_interval_ms: 2000,
};
