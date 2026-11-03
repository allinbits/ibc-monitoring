import { afterEach, describe, expect, it, vi } from "vitest";
import { readDecisions } from "./rpc";

afterEach(() => vi.unstubAllGlobals());

const decision = (id: number) => ({
  id, kind: "set seats", text: "t", state: "applied", made: 1, recorded: 1, due: 2, applied: 2, cancelled: 0, note: "",
});

// Answers vm/qrender of ibcgov with the given renders, keyed by render path.
function stubRenders(renders: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init: { body: string }) => {
      const { params } = JSON.parse(init.body) as { params: { data: string } };
      const data = atob(params.data);
      const path = data.slice(data.indexOf(":") + 1);
      if (!(path in renders)) throw new Error(`unexpected render ${path}`);
      const body = btoa(JSON.stringify(renders[path]));
      return { ok: true, text: async () => JSON.stringify({ result: { response: { ResponseBase: { Data: body } } } }) };
    }),
  );
}

describe("readDecisions", () => {
  it("reads the bare ledger of the first ibcgov versions", async () => {
    stubRenders({ decisions: [decision(1), decision(2)] });
    expect((await readDecisions()).map((d) => d.id)).toEqual(["1", "2"]);
  });
  it("follows the pages of later versions", async () => {
    stubRenders({
      decisions: { entries: [decision(1), decision(2)], next: 3 },
      "decisions?from=3": { entries: [decision(3)], next: 0 },
    });
    expect((await readDecisions()).map((d) => d.id)).toEqual(["1", "2", "3"]);
  });
  it("refuses a page without entries", async () => {
    stubRenders({ decisions: { next: 0 } });
    await expect(readDecisions()).rejects.toThrow("Unexpected decisions ledger response.");
  });
});
