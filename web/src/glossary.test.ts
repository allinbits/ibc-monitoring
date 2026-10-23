import { describe, expect, it } from "vitest";
import { glossaryText } from "./glossary";

describe("glossary coverage", () => {
  it("explains the required operational terms and active checks", () => {
    const terms = [
      "Bridge / payout meter",
      "Refund meter",
      "Day cap",
      "Cap floor",
      "Supply cap",
      "Send minimum",
      "Send maximum",
      "Refund cap",
      "Fee",
      "Fee floor",
      "Claims",
      "Claim states",
      "Sweeps",
      "Sends / outcomes",
      "Outcome",
      "Payment",
      "Lost",
      "Unrecovered",
      "Unresolved",
      "Accepted deficit",
      "Reserved",
      "Escrow accounting",
      "Quarantine",
      "Holds",
      "Flags",
      "Guardian",
      "Watch",
      "Answer",
      "Epoch",
      "Expiry",
      "Trusting period",
      "Provenance",
      "Freshness",
      "Commitment ok",
      "Settlement",
      "Receive / refund available",
      "Backing by token",
      "Voucher supply",
      "Day cap / used level",
      "client_expiry/gno",
      "client_expiry/ethereum",
      "route_relationship",
    ];
    for (const term of terms) expect(glossaryText(term), term).toBeTruthy();
  });
});
