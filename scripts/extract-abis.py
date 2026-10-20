#!/usr/bin/env python3
"""Extracts the ABIs ibcmon embeds from a Foundry build of gno-eth-ibc.

usage: extract-abis.py <gno-eth-ibc>/contracts/out <this repo>/abi

The transfer proxy's ABI is the union of the fork and its three modules, which
the proxy reaches by delegatecall through its fallback (docs/design.md §4.2).
"""
import json
import os
import sys


def load(out, path):
    with open(os.path.join(out, path)) as f:
        return json.load(f)["abi"]


def key(entry):
    if entry["type"] in ("function", "event", "error"):
        return (entry["type"], entry["name"], tuple(i["type"] for i in entry.get("inputs", [])))
    return (entry["type"],)


def merge(*abis):
    seen, result = set(), []
    for abi in abis:
        for entry in abi:
            k = key(entry)
            if k in seen:
                continue
            seen.add(k)
            result.append(entry)
    return result


def main(out, dest):
    files = {
        "GnoICS20Transfer": merge(
            load(out, "GnoICS20Transfer.sol/GnoICS20Transfer.json"),
            load(out, "IGnoICS20Controls.sol/IGnoICS20Controls.json"),
            load(out, "GnoICS20Evidence.sol/GnoICS20Evidence.json"),
            load(out, "GnoICS20Reconcile.sol/GnoICS20Reconcile.json"),
        ),
        "IBCAdmin": load(out, "IBCAdmin.sol/IBCAdmin.json"),
        "IBCAdminExec": load(out, "IBCAdminExec.sol/IBCAdminExec.json"),
        "GnoLightClient": load(out, "GnoLightClient.sol/GnoLightClient.json"),
        "GnoVoucher": load(out, "GnoVoucher.sol/GnoVoucher.json"),
        "ICS26Router": load(out, "ICS26Router.sol/ICS26Router.json"),
    }
    os.makedirs(dest, exist_ok=True)
    for name, abi in files.items():
        with open(os.path.join(dest, name + ".json"), "w") as f:
            json.dump(abi, f, indent=1)
            f.write("\n")
        print(name, len(abi), "entries")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    main(sys.argv[1], sys.argv[2])
