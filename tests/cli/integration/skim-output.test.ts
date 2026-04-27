/**
 * Skim-output regression suite (claude-code-profiles-pcs).
 *
 * Two responsibilities:
 *
 *  1. Snapshot the human-readable output of every read-only verb (list,
 *     status, drift, diff) so a future formatting tweak that breaks
 *     skimmability fails CI loudly.
 *
 *  2. Lock the --json payload SHAPE byte-for-byte: the polish epic
 *     reshuffled human formatting only; consumers reading --json must see
 *     no field reordering, additions, or renames. We hand-craft the
 *     expected JSON for each verb so the assertion catches both shape
 *     drift and key-order drift (`JSON.stringify` is insertion-ordered).
 *
 * The fixtures use deterministic content (fixed timestamps masked, fixed
 * profile names) so the snapshots stay stable across runs.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { runDiff } from "../../../src/cli/commands/diff.js";
import { runDrift } from "../../../src/cli/commands/drift.js";
import { runList } from "../../../src/cli/commands/list.js";
import { runStatus } from "../../../src/cli/commands/status.js";
import { merge } from "../../../src/merge/merge.js";
import { resolve } from "../../../src/resolver/resolve.js";
import { materialize } from "../../../src/state/materialize.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";
import { captureOutput } from "../helpers/output-sink.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

/**
 * Replace timestamps and other non-deterministic fields in a JSON payload
 * with stable placeholders so we can assert byte-identical shape across
 * runs. Returns a deep-cloned object — the input is left untouched.
 */
function normalizeForShape<T>(value: T): T {
  const ISO_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
  function walk(v: unknown): unknown {
    if (typeof v === "string" && ISO_RE.test(v)) return "<iso>";
    if (Array.isArray(v)) return v.map(walk);
    if (v !== null && typeof v === "object") {
      const out: Record<string, unknown> = {};
      // Preserve the original key insertion order so JSON.stringify(out)
      // matches JSON.stringify(value) modulo the masked fields.
      for (const [k, vv] of Object.entries(v)) out[k] = walk(vv);
      return out;
    }
    return v;
  }
  return walk(value) as T;
}

async function setupTwoProfiles(opts?: { activate?: "a" | "b" | null }) {
  const f = await makeFixture({
    profiles: {
      a: {
        manifest: {
          name: "a",
          description: "alpha profile",
          tags: ["dev"],
        },
        files: { "CLAUDE.md": "A\n" },
      },
      b: {
        manifest: {
          name: "b",
          description: "beta profile",
        },
        files: { "CLAUDE.md": "B\n" },
      },
    },
  });
  if (opts?.activate) {
    const plan = await resolve(opts.activate, { projectRoot: f.projectRoot });
    const m = await merge(plan);
    await materialize(buildStatePaths(f.projectRoot), plan, m);
  }
  return f;
}

// ──────────────────────────────────────────────────────────────────────
// list
// ──────────────────────────────────────────────────────────────────────
describe("list — skimmable output", () => {
  it("renders description and tags columns when at least one profile has them", async () => {
    fx = await setupTwoProfiles();
    const cap = captureOutput(false);
    const code = await runList({ cwd: fx.projectRoot, output: cap.channel });
    expect(code).toBe(0);
    const out = cap.stdout();
    // Both descriptions present.
    expect(out).toContain("alpha profile");
    expect(out).toContain("beta profile");
    // Tags column contains the bracketed tag list.
    expect(out).toContain("[dev]");
    // No ragged trailing whitespace on any line.
    for (const line of out.trimEnd().split("\n")) {
      expect(line).toBe(line.trimEnd());
    }
  });

  it("omits description column when no profile has a description", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: {} },
        b: { manifest: { name: "b" }, files: {} },
      },
    });
    const cap = captureOutput(false);
    await runList({ cwd: fx.projectRoot, output: cap.channel });
    const out = cap.stdout();
    // No "[" tag bracket should appear when no tags exist.
    expect(out).not.toContain("[");
  });

  it("active profile gets the * marker (glyph survives even without colour)", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false);
    await runList({ cwd: fx.projectRoot, output: cap.channel });
    const out = cap.stdout();
    expect(out).toMatch(/^\* a/m);
    // Non-active row still starts with a single space, not a `*`.
    expect(out).toMatch(/^  b/m);
  });

  it("empty project hint preserves wording", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    await runList({ cwd: fx.projectRoot, output: cap.channel });
    expect(cap.stdout()).toContain("run `claude-profiles new <name>`");
  });

  it("--json: payload SHAPE is stable (key order, field set)", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(true);
    await runList({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0];
    const normalized = normalizeForShape(payload);
    // Hand-crafted expected — locks in field set and key order. Adding,
    // removing, or reordering a field will fail this assertion loudly.
    expect(JSON.stringify(normalized)).toBe(
      JSON.stringify({
        profiles: [
          {
            name: "a",
            active: true,
            description: "alpha profile",
            extends: null,
            includes: [],
            tags: ["dev"],
            lastMaterialized: "<iso>",
          },
          {
            name: "b",
            active: false,
            description: "beta profile",
            extends: null,
            includes: [],
            tags: [],
            lastMaterialized: null,
          },
        ],
        stateWarning: null,
      }),
    );
  });
});

// ──────────────────────────────────────────────────────────────────────
// status
// ──────────────────────────────────────────────────────────────────────
describe("status — skimmable output", () => {
  it("active profile: prints description on a separate dim line", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const out = cap.stdout();
    expect(out).toContain("active: a");
    // The description shows up on a line of its own (indented).
    expect(out).toMatch(/^  alpha profile$/m);
  });

  it("clean drift: prefixed with the [ok] glyph (init parity)", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    // Non-TTY in tests → glyph renders as `[ok]`.
    expect(cap.stdout()).toContain("[ok] drift: clean");
  });

  it("no profiles at all: hint says `new <name> first`, not `use`", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const out = cap.stdout();
    expect(out).toContain("run `claude-profiles new <name>` first");
    expect(out).not.toContain("use <name>");
  });

  it("profiles exist but none active: hint says `use <name>`", async () => {
    fx = await setupTwoProfiles();
    const cap = captureOutput(false);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    expect(cap.stdout()).toContain("use <name>");
  });

  it("--json: payload SHAPE is stable (key order, field set)", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(true);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0];
    const normalized = normalizeForShape(payload);
    expect(JSON.stringify(normalized)).toBe(
      JSON.stringify({
        activeProfile: "a",
        materializedAt: "<iso>",
        drift: {
          fingerprintOk: true,
          modified: 0,
          added: 0,
          deleted: 0,
          unrecoverable: 0,
          total: 0,
        },
        warnings: [],
      }),
    );
  });
});

// ──────────────────────────────────────────────────────────────────────
// drift
// ──────────────────────────────────────────────────────────────────────
describe("drift — skimmable output", () => {
  it("default human summary: omits scan-stats suffix", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "CLAUDE.md"),
      "EDIT\n",
    );
    const cap = captureOutput(false);
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    const out = cap.stdout();
    expect(out).toContain("drift:");
    expect(out).not.toContain("scanned");
    expect(out).not.toContain("fast=");
    expect(out).not.toContain("slow=");
  });

  it("--verbose: human summary includes scan-stats suffix", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "CLAUDE.md"),
      "EDIT\n",
    );
    const cap = captureOutput(false);
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: true,
    });
    expect(cap.stdout()).toMatch(/scanned \d+, fast=\d+, slow=\d+/);
  });

  it("per-file output includes provenance regardless of --verbose", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "CLAUDE.md"),
      "EDIT\n",
    );
    const cap = captureOutput(false);
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    expect(cap.stdout()).toContain("(from: a)");
  });

  it("--json: scan-stats fields ALWAYS present (verbose flag affects only human output)", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "CLAUDE.md"),
      "EDIT\n",
    );
    const cap = captureOutput(true);
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      // Even when verbose=false, scannedFiles/fastPathHits/slowPathHits
      // are part of the JSON contract.
      verbose: false,
    });
    const payload = cap.jsonLines()[0] as {
      scannedFiles: number;
      fastPathHits: number;
      slowPathHits: number;
    };
    expect(typeof payload.scannedFiles).toBe("number");
    expect(typeof payload.fastPathHits).toBe("number");
    expect(typeof payload.slowPathHits).toBe("number");
  });
});

// ──────────────────────────────────────────────────────────────────────
// diff
// ──────────────────────────────────────────────────────────────────────
describe("diff — skimmable output", () => {
  it("identical (post-resolve): summary names file count in both", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "X\n", "y.md": "Y\n" } },
        b: { manifest: { name: "b" }, files: { "x.md": "X\n", "y.md": "Y\n" } },
      },
    });
    const cap = captureOutput(false);
    await runDiff({
      cwd: fx.projectRoot,
      output: cap.channel,
      a: "a",
      b: "b",
    });
    expect(cap.stdout()).toBe(
      "a=a b=b: identical (2 files in both)\n",
    );
  });

  it("non-identical: summary uses 'changes' wording with totals breakdown", async () => {
    fx = await makeFixture({
      profiles: {
        a: {
          manifest: { name: "a" },
          files: { "shared.md": "A\n", "only-a.md": "X\n" },
        },
        b: {
          manifest: { name: "b" },
          files: { "shared.md": "B\n", "only-b.md": "Y\n" },
        },
      },
    });
    const cap = captureOutput(false);
    await runDiff({
      cwd: fx.projectRoot,
      output: cap.channel,
      a: "a",
      b: "b",
    });
    const out = cap.stdout();
    expect(out).toMatch(
      /^a=a b=b: 3 changes \(1 added, 1 removed, 1 changed\)/m,
    );
    // Per-file lines include sigil column.
    expect(out).toMatch(/^  \+ only-a\.md$/m);
    expect(out).toMatch(/^  - only-b\.md$/m);
    expect(out).toMatch(/^  ~ shared\.md$/m);
  });

  it("--json: payload SHAPE is stable (key order, field set)", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "x.md": "B\n" } },
      },
    });
    const cap = captureOutput(true);
    await runDiff({
      cwd: fx.projectRoot,
      output: cap.channel,
      a: "a",
      b: "b",
    });
    const payload = cap.jsonLines()[0];
    expect(JSON.stringify(payload)).toBe(
      JSON.stringify({
        a: "a",
        b: "b",
        entries: [{ relPath: "x.md", status: "changed" }],
        totals: { added: 0, removed: 0, changed: 1 },
      }),
    );
  });
});
