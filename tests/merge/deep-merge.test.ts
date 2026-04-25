import { describe, expect, it } from "vitest";

import { InvalidSettingsJsonError } from "../../src/errors/index.js";
import { deepMergeStrategy } from "../../src/merge/deep-merge.js";
import type { ContributorBytes } from "../../src/merge/types.js";

function inputs(...entries: Array<[string, unknown]>): ContributorBytes[] {
  return entries.map(([id, value]) => ({
    id,
    bytes: Buffer.from(typeof value === "string" ? value : JSON.stringify(value), "utf8"),
  }));
}

function parse(result: { bytes: Buffer }) {
  return JSON.parse(result.bytes.toString("utf8"));
}

describe("deepMergeStrategy (R8)", () => {
  it("merges objects field-by-field, later scalars win", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(["a", { x: 1, y: 2 }], ["b", { y: 99, z: 3 }]),
    );
    expect(parse(r)).toEqual({ x: 1, y: 99, z: 3 });
    expect(r.contributors).toEqual(["a", "b"]);
  });

  it("recurses through nested objects", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { ui: { theme: "dark", font: "mono" } }],
        ["b", { ui: { theme: "light" } }],
      ),
    );
    expect(parse(r)).toEqual({ ui: { theme: "light", font: "mono" } });
  });

  it("REPLACES arrays at the same path (R8 default)", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(["a", { tools: ["x", "y"] }], ["b", { tools: ["z"] }]),
    );
    expect(parse(r)).toEqual({ tools: ["z"] });
  });

  it("REPLACES nested arrays as well", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { permissions: { allow: ["a", "b"] } }],
        ["b", { permissions: { allow: ["c"] } }],
      ),
    );
    expect(parse(r)).toEqual({ permissions: { allow: ["c"] } });
  });

  it("later-wins on type mismatch (object vs scalar, array vs object)", () => {
    const r1 = deepMergeStrategy(
      "settings.json",
      inputs(["a", { x: { nested: true } }], ["b", { x: "string-now" }]),
    );
    expect(parse(r1)).toEqual({ x: "string-now" });

    const r2 = deepMergeStrategy(
      "settings.json",
      inputs(["a", { x: ["array"] }], ["b", { x: { obj: true } }]),
    );
    expect(parse(r2)).toEqual({ x: { obj: true } });
  });

  it("preserves keys not touched by later contributor", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { keep: "me", ui: { font: "mono" } }],
        ["b", { ui: { theme: "dark" } }],
      ),
    );
    expect(parse(r)).toEqual({
      keep: "me",
      ui: { font: "mono", theme: "dark" },
    });
  });

  it("treats empty/whitespace bytes as {}", () => {
    const r = deepMergeStrategy(
      "settings.json",
      [
        { id: "a", bytes: Buffer.from("", "utf8") },
        { id: "b", bytes: Buffer.from("   \n", "utf8") },
        { id: "c", bytes: Buffer.from(JSON.stringify({ ok: true }), "utf8") },
      ],
    );
    expect(parse(r)).toEqual({ ok: true });
  });

  it("emits trailing newline", () => {
    const r = deepMergeStrategy("settings.json", inputs(["a", { x: 1 }]));
    expect(r.bytes.toString("utf8").endsWith("\n")).toBe(true);
  });

  it("throws InvalidSettingsJsonError when a contributor's settings.json is unparseable", () => {
    expect(() =>
      deepMergeStrategy(
        "settings.json",
        [
          { id: "good", bytes: Buffer.from(JSON.stringify({ ok: true })) },
          { id: "broken", bytes: Buffer.from("{not json}", "utf8") },
        ],
      ),
    ).toThrowError(InvalidSettingsJsonError);
  });

  it("InvalidSettingsJsonError names the offending contributor and path", () => {
    try {
      deepMergeStrategy(
        "settings.json",
        [{ id: "broken", bytes: Buffer.from("{not json}", "utf8") }],
      );
      expect.fail("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(InvalidSettingsJsonError);
      const e = err as InvalidSettingsJsonError;
      expect(e.contributor).toBe("broken");
      expect(e.relPath).toBe("settings.json");
      expect(e.message).toContain("broken");
      expect(e.message).toContain("settings.json");
    }
  });

  it("does not mutate inputs", () => {
    const a = { x: { nested: 1 } };
    const b = { x: { other: 2 } };
    const aJson = JSON.stringify(a);
    const bJson = JSON.stringify(b);
    deepMergeStrategy(
      "settings.json",
      inputs(["a", a], ["b", b]),
    );
    expect(JSON.stringify(a)).toBe(aJson);
    expect(JSON.stringify(b)).toBe(bJson);
  });

  it("rejects valid JSON whose top-level value is not an object (array)", () => {
    expect(() =>
      deepMergeStrategy(
        "settings.json",
        [{ id: "arr", bytes: Buffer.from(JSON.stringify([{ a: 1 }]), "utf8") }],
      ),
    ).toThrowError(InvalidSettingsJsonError);
  });

  it("rejects valid JSON whose top-level value is null or a scalar", () => {
    expect(() =>
      deepMergeStrategy("settings.json", [
        { id: "n", bytes: Buffer.from("null", "utf8") },
      ]),
    ).toThrowError(InvalidSettingsJsonError);
    expect(() =>
      deepMergeStrategy("settings.json", [
        { id: "num", bytes: Buffer.from("42", "utf8") },
      ]),
    ).toThrowError(InvalidSettingsJsonError);
  });

  it("preserves __proto__ as a literal own key (no silent data loss via prototype setter)", () => {
    // JSON.parse produces __proto__ as an own enumerable property (per ES2017
    // [[DefineOwnProperty]] semantics). A naive `out["__proto__"] = v` would
    // trigger the prototype setter and elide the key from JSON.stringify(out)
    // — verify it survives both the deep merge and the round-trip intact.
    const r = deepMergeStrategy("settings.json", [
      { id: "a", bytes: Buffer.from('{"keep":"yes"}', "utf8") },
      { id: "b", bytes: Buffer.from('{"__proto__":"surprise"}', "utf8") },
    ]);
    const text = r.bytes.toString("utf8");
    expect(text).toContain('"__proto__"');
    expect(text).toContain('"surprise"');
    expect(text).toContain('"keep"');
    // And global Object.prototype is untouched (no prototype pollution).
    expect((Object.prototype as Record<string, unknown>)["surprise"]).toBeUndefined();
  });
});

describe("deepMergeStrategy + R12 hooks-by-event", () => {
  it("CONCATENATES action arrays at hooks.<EventName> instead of replacing", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { hooks: { PreToolUse: [{ run: "from-a" }] } }],
        ["b", { hooks: { PreToolUse: [{ run: "from-b" }] } }],
      ),
    );
    expect(parse(r)).toEqual({
      hooks: { PreToolUse: [{ run: "from-a" }, { run: "from-b" }] },
    });
  });

  it("R12 wins over R8 even when array-replace would otherwise apply", () => {
    // Explicitly verifies the precedence note in the epic key invariant.
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { hooks: { PreToolUse: ["a1", "a2"] } }],
        ["b", { hooks: { PreToolUse: ["b1"] } }],
      ),
    );
    expect(parse(r)).toEqual({
      hooks: { PreToolUse: ["a1", "a2", "b1"] },
    });
  });

  it("merges different events from different contributors without clobbering", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { hooks: { PreToolUse: ["pre-a"] } }],
        ["b", { hooks: { PostToolUse: ["post-b"] } }],
      ),
    );
    expect(parse(r)).toEqual({
      hooks: { PreToolUse: ["pre-a"], PostToolUse: ["post-b"] },
    });
  });

  it("accumulates the same event across many contributors in order", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["base", { hooks: { Stop: ["s1"] } }],
        ["extended", { hooks: { Stop: ["s2"] } }],
        ["compA", { hooks: { Stop: ["s3"] } }],
        ["leaf", { hooks: { Stop: ["s4"] } }],
      ),
    );
    expect(parse(r)).toEqual({ hooks: { Stop: ["s1", "s2", "s3", "s4"] } });
  });

  it("R12 only fires at depth 2 — top-level 'hooks' or deeper paths use R8", () => {
    // Top-level array path called "hooks" — depth 1, NOT R12.
    const r1 = deepMergeStrategy(
      "settings.json",
      inputs(["a", { hooks: ["a"] }], ["b", { hooks: ["b"] }]),
    );
    expect(parse(r1)).toEqual({ hooks: ["b"] });

    // Deeper array under hooks.X.actions — depth 3, NOT R12 (still array-replace).
    const r2 = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { hooks: { PreToolUse: { actions: ["x"] } } }],
        ["b", { hooks: { PreToolUse: { actions: ["y"] } } }],
      ),
    );
    expect(parse(r2)).toEqual({
      hooks: { PreToolUse: { actions: ["y"] } },
    });
  });

  it("falls back to R8 last-wins when one side at hooks.<E> is not an array", () => {
    // Contributor 'b' sets it to an object → array-vs-object → later wins.
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { hooks: { PreToolUse: ["a1"] } }],
        ["b", { hooks: { PreToolUse: { not: "an array" } } }],
      ),
    );
    expect(parse(r)).toEqual({ hooks: { PreToolUse: { not: "an array" } } });
  });

  // Locked-in behavior for a footgun the spec is silent on: a non-array
  // contributor in the middle "resets" the slot, so a later array does NOT
  // resurrect previously-accumulated entries — it concatenates only with
  // whatever survived after the type-mismatch (the non-array value).
  // Document and test so a future spec edit is a deliberate decision.
  it("array → non-array → array resets the accumulator (R8 wins on mismatch, then R12 resumes)", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { hooks: { PreToolUse: ["a1", "a2"] } }],
        ["b", { hooks: { PreToolUse: "string-instead" } }],
        ["c", { hooks: { PreToolUse: ["c1"] } }],
      ),
    );
    // After step 1: ["a1","a2"]. After step 2: "string-instead" (R8).
    // After step 3: array-vs-string is also a mismatch → later wins → ["c1"].
    expect(parse(r)).toEqual({ hooks: { PreToolUse: ["c1"] } });
  });

  it("does NOT touch arrays under unrelated top-level keys", () => {
    const r = deepMergeStrategy(
      "settings.json",
      inputs(
        ["a", { other: { PreToolUse: ["a"] } }],
        ["b", { other: { PreToolUse: ["b"] } }],
      ),
    );
    expect(parse(r)).toEqual({ other: { PreToolUse: ["b"] } });
  });
});
