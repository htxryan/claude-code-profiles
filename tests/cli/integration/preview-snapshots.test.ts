/**
 * Snapshot-style integration tests for `drift --preview` and `diff --preview`
 * (azp). The in-process unit tests in tests/cli/commands/{drift,diff}.test.ts
 * already exercise the rendering primitives; this file pins the *human-
 * readable spawn-boundary output* so a future refactor that subtly degrades
 * the unified-diff body (wrong sigil, missing indent, swapped +/- order)
 * fails loudly instead of silently shipping.
 *
 * Determinism: we redact run-dependent fields (timestamps, byte counts,
 * absolute paths) before comparison so the assertions remain stable across
 * filesystems and clock skew. The rendered diff body itself is content-
 * deterministic (small fixed inputs) — no redaction needed.
 *
 * Why pin output here AND in commands/{drift,diff}.test.ts: the in-process
 * tests run with a captured OutputChannel that does not pass through the CLI
 * dispatcher, parser, or stdout pipe — a regression in argv parsing, in
 * --preview flag plumbing, or in process-exit timing could ship a working
 * function but a broken command. The spawn boundary catches that.
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

/**
 * Strip ANSI escapes so the assertions don't depend on the (Node)
 * subprocess's auto-detected TTY status. Spawn never inherits a TTY (we
 * pipe stdio), so style.* should already produce ASCII output — but we
 * strip defensively so a future refactor that flips the spawned child
 * onto a TTY-detection heuristic doesn't cascade into snapshot churn.
 */
function stripAnsi(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/\[[0-9;]*m/g, "");
}

describe("drift --preview spawn snapshots (azp)", () => {
  it("modified entry: unified-diff body with - resolved / + live", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: {
          manifest: { name: "a" },
          files: {
            "CLAUDE.md": "alpha\nbeta\ngamma\n",
          },
        },
      },
    });
    const plan = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(plan);
    await materialize(buildStatePaths(fx.projectRoot), plan, m);

    // Edit the live file: change the middle line. The preview body should
    // emit prefix/suffix as context, the divergent line as -/+.
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "CLAUDE.md"),
      "alpha\nBETA\ngamma\n",
    );

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "drift", "--preview"],
    });
    expect(r.exitCode).toBe(0);

    const stdout = stripAnsi(r.stdout);
    // Header line shape and provenance.
    expect(stdout).toContain("active: a");
    // Per-file row: status word + relPath + (from: a) tail. Indentation
    // and column padding are part of the polished UX — pin the literal
    // shape so a future column-width tweak surfaces here.
    expect(stdout).toMatch(/modified\s+CLAUDE\.md/);
    expect(stdout).toContain("(from: a)");
    // Preview body lines: 6-space indent followed by the unified-diff op.
    expect(stdout).toContain("       alpha");
    expect(stdout).toContain("      -beta");
    expect(stdout).toContain("      +BETA");
    expect(stdout).toContain("       gamma");
  });

  it("added entry: head preview body lists live file lines", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: {
          manifest: { name: "a" },
          files: { "CLAUDE.md": "alpha\n" },
        },
      },
    });
    const plan = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(plan);
    await materialize(buildStatePaths(fx.projectRoot), plan, m);

    // New file the user added to .claude/ that the profile doesn't own.
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "extra.md"),
      "one\ntwo\nthree\n",
    );

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "drift", "--preview"],
    });
    expect(r.exitCode).toBe(0);

    const stdout = stripAnsi(r.stdout);
    expect(stdout).toMatch(/added\s+extra\.md/);
    // Head preview: each live line printed verbatim, indented.
    expect(stdout).toContain("      one");
    expect(stdout).toContain("      two");
    expect(stdout).toContain("      three");
    // Crucially: NO `+`/`-` sigils on a head preview (those are diff-only).
    expect(stdout).not.toMatch(/^      \+one$/m);
    expect(stdout).not.toMatch(/^      -one$/m);
  });

  it("binary modified entry: substitutes binary placeholder", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: {
          manifest: { name: "a" },
          files: { "CLAUDE.md": "text\n" },
        },
      },
    });
    const plan = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(plan);
    await materialize(buildStatePaths(fx.projectRoot), plan, m);

    // Inject a NUL into the live file to flip isBinary().
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "CLAUDE.md"),
      Buffer.concat([Buffer.from("hdr"), Buffer.from([0]), Buffer.from("end")]),
    );

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "drift", "--preview"],
    });
    expect(r.exitCode).toBe(0);
    expect(stripAnsi(r.stdout)).toMatch(/\(binary file — \d+ bytes\)/);
  });
});

describe("diff --preview spawn snapshots (azp)", () => {
  it("changed entry: unified-diff body uses (b, a) order so + is the focus", async () => {
    // The diff command's preview convention: `c3p diff dev ci`
    // shows the dev side as `+` (focus) and the ci side as `-` (baseline).
    // Pin this convention — a refactor that swapped argument order would
    // silently invert the user's mental model.
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        dev: {
          manifest: { name: "dev" },
          files: { "CLAUDE.md": "alpha\nDEV\ngamma\n" },
        },
        ci: {
          manifest: { name: "ci" },
          files: { "CLAUDE.md": "alpha\nCI\ngamma\n" },
        },
      },
    });

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "diff", "dev", "ci", "--preview"],
    });
    expect(r.exitCode).toBe(0);

    const stdout = stripAnsi(r.stdout);
    // Header line: `a=dev b=ci: 1 changes …`
    expect(stdout).toMatch(/a=dev b=ci: \d+ changes/);
    // Per-row sigil column: `~ CLAUDE.md` for changed entries.
    expect(stdout).toMatch(/~ CLAUDE\.md/);
    // Preview body: 6-space indent, prefix/suffix as context, divergent
    // line as -ci → +dev (per the (b, a) order documented in diff.ts:204).
    expect(stdout).toContain("       alpha");
    expect(stdout).toContain("      -CI");
    expect(stdout).toContain("      +DEV");
    expect(stdout).toContain("       gamma");
  });

  it("identical profiles: prints 'identical' and no preview body", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: {
          manifest: { name: "a" },
          files: { "CLAUDE.md": "same\n" },
        },
      },
    });

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "diff", "a", "a", "--preview"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("identical");
    // No `+`/`-` lines: the trivial-equal path doesn't render previews.
    expect(stripAnsi(r.stdout)).not.toMatch(/^[+-]/m);
  });

  it("--preview is silent for added/removed entries (no opposing buffer)", async () => {
    // The diff command renders `--preview` bodies only for `changed`
    // entries (added/removed have no opposing buffer to diff against — the
    // byte counts in the entry summary are the signal). Pin this.
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: {
          manifest: { name: "a" },
          files: { "CLAUDE.md": "common\n", "only-a.md": "A\n" },
        },
        b: {
          manifest: { name: "b" },
          files: { "CLAUDE.md": "common\n", "only-b.md": "B\n" },
        },
      },
    });

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "diff", "a", "b", "--preview"],
    });
    expect(r.exitCode).toBe(0);
    const stdout = stripAnsi(r.stdout);
    expect(stdout).toMatch(/\+ only-a\.md/);
    expect(stdout).toMatch(/- only-b\.md/);
    // No preview body for added or removed entries — only `~ <changed>`
    // entries get a unified-diff body. Common file is byte-equal so no
    // entry at all.
    expect(stdout).not.toMatch(/^      \+A$/m);
    expect(stdout).not.toMatch(/^      -B$/m);
  });
});
