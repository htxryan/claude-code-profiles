import { describe, expect, it } from "vitest";

import { lastWinsStrategy } from "../../src/merge/last-wins.js";

describe("lastWinsStrategy (R10)", () => {
  it("returns the last contributor's bytes verbatim", () => {
    const r = lastWinsStrategy("commands/x.sh", [
      { id: "a", bytes: Buffer.from("from-a\n") },
      { id: "b", bytes: Buffer.from("from-b\n") },
    ]);
    expect(r.bytes.toString("utf8")).toBe("from-b\n");
    expect(r.contributors).toEqual(["b"]);
  });

  it("single-contributor case is the trivial pass-through", () => {
    const buf = Buffer.from("solo");
    const r = lastWinsStrategy("agents/foo.json", [{ id: "only", bytes: buf }]);
    expect(Buffer.compare(r.bytes, buf)).toBe(0);
    expect(r.contributors).toEqual(["only"]);
  });

  it("returns a fresh Buffer (no aliasing of the input)", () => {
    // Future callers (E3 dry-run, E5 validate) must be able to mutate the
    // output without corrupting input bytes the orchestrator may still hold.
    const buf = Buffer.from("payload");
    const r = lastWinsStrategy("agents/foo.json", [{ id: "only", bytes: buf }]);
    expect(r.bytes).not.toBe(buf);
    r.bytes.fill(0);
    expect(buf.toString("utf8")).toBe("payload");
  });

  it("ignores all but the last contributor in provenance", () => {
    const r = lastWinsStrategy("plugin.json", [
      { id: "x1", bytes: Buffer.from("1") },
      { id: "x2", bytes: Buffer.from("2") },
      { id: "x3", bytes: Buffer.from("3") },
    ]);
    expect(r.bytes.toString()).toBe("3");
    expect(r.contributors).toEqual(["x3"]);
  });

  it("throws if invoked with no inputs", () => {
    expect(() => lastWinsStrategy("foo.txt", [])).toThrow();
  });
});
