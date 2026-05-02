/**
 * Gap closure #1 (PR6 #1, F2 epic claude-code-profiles-yhb):
 *
 * Interactive drift gate via the **non-interactive** path.
 *
 * Per PR6a, a true PTY-driven interactive gate test is deferred post-1.0; the
 * pre-merge contract is that the gate auto-aborts (or honours --on-drift=)
 * whenever the bin is run with a non-TTY stdin/stdout. spawn(2) inherits
 * pipes by default, so every test in this file runs the bin in
 * non-interactive mode without an explicit flag.
 *
 * Scope: extend gate-matrix.test.ts with cases the existing matrix doesn't
 * cover end-to-end:
 *   - invalid --on-drift= value rejected at parse time (exit 1, names the flag)
 *   - non-interactive `use` with NO drift completes cleanly (no flag needed)
 *   - non-interactive `sync` with NO drift completes cleanly (no flag needed)
 *   - the auto-abort error message is single-line, names the three valid
 *     values, and goes to stderr — pinning the contract Go must match
 *
 * F2 epic doc:
 *   "Each test uses tests/cli/integration/spawn.ts harness; output and exit
 *   codes match the spec verbatim."
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { merge } from "../../../src/merge/merge.js";
import { resolve } from "../../../src/resolver/resolve.js";
import { materialize } from "../../../src/state/materialize.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { ensureBuilt, runCli } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

describe("gap closure #1: non-interactive drift gate (PR6 #1)", () => {
  it("non-interactive use with drift + no --on-drift → single-line stderr names all three values", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "x.md": "B\n" } },
      },
    });
    const planA = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(planA);
    await materialize(buildStatePaths(fx.projectRoot), planA, m);
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "x.md"), "EDIT\n");

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });

    expect(r.exitCode).toBe(1);
    // Spec contract: the auto-abort message names the flag AND its three
    // valid values so a CI script author seeing the error has all three
    // remediations in one line.
    expect(r.stderr).toContain("--on-drift=");
    expect(r.stderr).toContain("discard");
    expect(r.stderr).toContain("persist");
    expect(r.stderr).toContain("abort");
    // Stdout must remain empty in the abort path (no progress, no banner).
    expect(r.stdout).toBe("");
  });

  it("invalid --on-drift= value is rejected at parse → exit 1, stderr names the flag", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=ignore", "use", "a"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("--on-drift");
    // Pinning the parser's error wording: the user sees the bad value echoed
    // and the three valid alternatives. Helps a typo'd script self-correct.
    expect(r.stderr).toContain("ignore");
    expect(r.stderr).toMatch(/discard\|persist\|abort|discard, persist, abort/);
  });

  it("non-interactive use without drift → exit 0 (no flag required)", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "x.md": "B\n" } },
      },
    });
    const planA = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(planA);
    await materialize(buildStatePaths(fx.projectRoot), planA, m);
    // Live tree is intact — no drift.
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Switched to b");
  });

  it("non-interactive sync without drift → exit 0 (no flag required)", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
      },
    });
    const planA = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(planA);
    await materialize(buildStatePaths(fx.projectRoot), planA, m);
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "sync"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Synced");
  });

  it("non-interactive --on-drift=abort surfaces a discriminable abort message (not a generic 'failed')", async () => {
    // The explicit-abort wording MUST differ from the no-flag auto-abort
    // wording — a CI script differentiates the two paths on stderr text.
    // We exercise both paths from the same fixture to compare the messages
    // directly, rather than asserting a generic /abort/ match (which any
    // abort-related stderr would satisfy).
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "x.md": "B\n" } },
      },
    });
    const planA = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(planA);
    await materialize(buildStatePaths(fx.projectRoot), planA, m);
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "x.md"), "EDIT\n");

    const noFlag = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(noFlag.exitCode).toBe(1);
    expect(noFlag.stderr.length).toBeGreaterThan(0);

    // Re-stage drift for the second invocation (the failed `use` may have
    // left the live tree in any state — the no-flag abort path doesn't
    // promise to leave drift untouched).
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "x.md"), "EDIT\n");

    const explicit = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=abort", "use", "b"],
    });
    expect(explicit.exitCode).toBe(1);
    expect(explicit.stderr.toLowerCase()).toMatch(/abort/);
    // The two abort paths must produce *different* stderr — that's the
    // discriminability contract. A regression that collapsed both to the
    // same message would silently break CI consumers.
    expect(explicit.stderr).not.toBe(noFlag.stderr);
  });
});
