# Embedded ABIs

Extracted with `scripts/extract-abis.py` from a Foundry build (`make build`) of
[allinbits/gno-eth-ibc](https://github.com/allinbits/gno-eth-ibc) at branch
`feat/ibc-controls`, revision `9cd0154` (solc 0.8.28, via_ir, cancun).

| File | Source | Note |
|---|---|---|
| `GnoICS20Transfer.json` | `GnoICS20Transfer` + `IGnoICS20Controls` + `GnoICS20Evidence` + `GnoICS20Reconcile` | the transfer **proxy's** ABI: modules are reached by delegatecall through the fallback |
| `IBCAdmin.json` | `admin/IBCAdmin.sol` | a plain contract, not a proxy |
| `IBCAdminExec.json` | `admin/IBCAdminExec.sol` | `bindings()` and the delegate-graph immutables |
| `GnoLightClient.json` | `GnoLightClient.sol` | also used for kind 6 and recovery candidates |
| `GnoVoucher.json` | `GnoVoucher.sol` | ERC20 plus `ics20()`, `fullDenomPath()` |
| `ICS26Router.json` | eureka v3.0.2 `ICS26Router` | events `SendPacket`, `WriteAcknowledgement`, `AckPacket`, `TimeoutPacket`; `getCommitment` |

Refresh with `make abis GNO_ETH_IBC=../gno-eth-ibc` after building there, and
update the revision above.
