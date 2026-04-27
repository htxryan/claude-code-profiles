/**
 * Integration coverage for R45 strict-abort pre-flight on project-root
 * CLAUDE.md (cw6/T4).
 *
 * The unit tests in tests/state/materialize-section-splice.test.ts pin the
 * preflightRootSplice contract at the materialize-engine level. This file
 * pins the same contract through the spawned CLI:
 *
 *   - Exit code 1 (user error class — see src/cli/exit.ts:exitCodeFor;
 *     MaterializeError → EXIT_USER_ERROR with the "run init" remediation).
 *   - The error message names the file path AND references
 *     `claude-profiles init` so the user has actionable wording.
 *   - Both destinations are byte-identical to their pre-state on abort:
 *     project-root CLAUDE.md AND `.claude/` (R45
 *     atomic-across-destinations).
 *   - No staging artifacts (`.pending/`, `.prior/`, projectRoot `*.tmp`)
 *     remain after the abort.
 *   - The lock is released so a follow-up `init` recovers cleanly.
 *
 * Reverting any of: the preflightRootSplice ordering (must run BEFORE
 * step a), the in-memory snapshot of before/after slices (TOCTOU
 * mitigation), the tmp-cleanup on splice failure, or the lock-release on
 * throw must surface as a failure here.
 *
 * Coverage gap closed: previously the only coverage for the spawn-boundary
 * R45 path was an indirect mention in scenarios.test.ts S11 (which
 * exercises `validate`, not `use`). A future refactor of materialize that
 * wrote bytes to either destination before pre-flight could land green.
 */

import { promises as fs, type Dirent } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { merge } from "../../../src/merge/merge.js";
import { resolve } from "../../../src/resolver/resolve.js";
import { materialize } from "../../../src/state/materialize.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { isRootClaudeMdTmpName } from "../../../src/state/paths.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { ensureBuilt, runCli } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

/**
 * Build a fixture with a profile `b` that contributes a projectRoot
 * CLAUDE.md (so a `use b` would hit the splice path) plus a `.claude/`
 * file (so step a/b/c would be exercised if pre-flight didn't abort).
 *
 * `start` is the active profile pre-flight. We materialize it directly
 * via the in-process engine so we don't introduce a chicken-and-egg
 * issue with project-root CLAUDE.md handling on the very first run.
 */
async function setupRootContributor(start: "a" = "a"): Promise<Fixture> {
  const f = await makeFixture({
    profiles: {
      a: {
        manifest: { name: "a" },
        files: { "CLAUDE.md": "A\n", "settings.json": '{"v":"a"}' },
      },
      b: {
        manifest: { name: "b" },
        files: { "CLAUDE.md": "B\n", "settings.json": '{"v":"b"}' },
        rootFiles: { "CLAUDE.md": "PROFILE-B-MANAGED-BODY\n" },
      },
    },
  });
  // Materialize `start` (no projectRoot contributor for `a`). The live
  // `.claude/` is now populated; root CLAUDE.md is whatever the test
  // arranges next.
  const plan = await resolve(start, { projectRoot: f.projectRoot });
  const m = await merge(plan);
  await materialize(buildStatePaths(f.projectRoot), plan, m);
  return f;
}

describe("R45 pre-flight — strict abort with malformed root CLAUDE.md (cw6/T4)", () => {
  it("file absent: `use b` aborts; .claude/ byte-identical; exit 1; no leftovers", async () => {
    await ensureBuilt();
    fx = await setupRootContributor();
    const claudeDir = path.join(fx.projectRoot, ".claude");
    const rootClaudeMd = path.join(fx.projectRoot, "CLAUDE.md");

    // Pre-state: capture both destinations byte-by-byte.
    const preClaude = await readTree(claudeDir);
    // No root CLAUDE.md exists.
    await expect(fs.access(rootClaudeMd)).rejects.toMatchObject({ code: "ENOENT" });

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });

    // Exit code: MaterializeError → EXIT_USER_ERROR (1) per exit.ts.
    expect(r.exitCode).toBe(1);
    // Actionable wording.
    expect(r.stderr).toContain("claude-profiles init");
    expect(r.stderr).toContain("CLAUDE.md");

    // Both destinations untouched (R45 atomic-across-destinations).
    const postClaude = await readTree(claudeDir);
    expect(postClaude).toEqual(preClaude);
    await expect(fs.access(rootClaudeMd)).rejects.toMatchObject({ code: "ENOENT" });

    // No leftover staging.
    const metaDir = path.join(fx.projectRoot, ".claude-profiles", ".meta");
    await expect(
      fs.access(path.join(metaDir, "pending")),
    ).rejects.toMatchObject({ code: "ENOENT" });
    await expect(
      fs.access(path.join(metaDir, "prior")),
    ).rejects.toMatchObject({ code: "ENOENT" });
    await expect(
      fs.access(path.join(metaDir, "lock")),
    ).rejects.toMatchObject({ code: "ENOENT" });
    // No leftover splice tmp at projectRoot.
    const rootEntries = await fs.readdir(fx.projectRoot);
    expect(rootEntries.filter(isRootClaudeMdTmpName)).toHaveLength(0);
  });

  it("file present, markers missing: `use b` aborts; both files byte-identical", async () => {
    await ensureBuilt();
    fx = await setupRootContributor();
    const claudeDir = path.join(fx.projectRoot, ".claude");
    const rootClaudeMd = path.join(fx.projectRoot, "CLAUDE.md");

    // Plain CLAUDE.md without markers — user has not run `init`.
    const original = "# Project README\n\nNo claude-profiles markers here yet.\n";
    await fs.writeFile(rootClaudeMd, original);

    const preClaude = await readTree(claudeDir);

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("claude-profiles init");

    // R45: BOTH destinations byte-identical.
    expect(await readTree(claudeDir)).toEqual(preClaude);
    expect(await fs.readFile(rootClaudeMd, "utf8")).toBe(original);
  });

  it("file present, markers malformed (lone :begin): aborts; file unchanged", async () => {
    await ensureBuilt();
    fx = await setupRootContributor();
    const rootClaudeMd = path.join(fx.projectRoot, "CLAUDE.md");
    const claudeDir = path.join(fx.projectRoot, ".claude");

    // Lone :begin with no :end. parseMarkers reports "not found" → R45 abort.
    const broken = "intro\n<!-- claude-profiles:v1:begin -->\nstuff but no end\n";
    await fs.writeFile(rootClaudeMd, broken);

    const preClaude = await readTree(claudeDir);

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r.exitCode).toBe(1);
    expect(await fs.readFile(rootClaudeMd, "utf8")).toBe(broken);
    expect(await readTree(claudeDir)).toEqual(preClaude);
  });

  it("after manual marker recovery, subsequent `use b` succeeds (no leftover state)", async () => {
    // Round-trip: confirm the abort doesn't leave the project in a state
    // that requires anything beyond fixing the markers. Once a well-formed
    // marker pair is in place, the next `use b` lands cleanly — no manual
    // .pending/.prior/.tmp cleanup needed.
    //
    // Note: in this fixture .claude-profiles/ is already populated, so
    // `claude-profiles init` would refuse ("already initialised"). The
    // user's real remediation is to add the marker pair themselves (the
    // error message documents the exact bytes). Here we do that to prove
    // the abort left no other obstacle to recovery.
    await ensureBuilt();
    fx = await setupRootContributor();
    const rootClaudeMd = path.join(fx.projectRoot, "CLAUDE.md");
    const claudeDir = path.join(fx.projectRoot, ".claude");

    // Plain markers-less CLAUDE.md.
    await fs.writeFile(rootClaudeMd, "# Pre-existing notes.\n");

    // First attempt: must abort.
    const r1 = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r1.exitCode).toBe(1);

    // Manually add a valid marker pair (preserving prior content above) —
    // the recovery the error message asks the user to perform.
    await fs.writeFile(
      rootClaudeMd,
      "# Pre-existing notes.\n\n<!-- claude-profiles:v1:begin -->\n<!-- Managed block. -->\n\n<!-- claude-profiles:v1:end -->\n",
    );

    // Now `use b` lands the splice cleanly.
    const r2 = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r2.exitCode).toBe(0);
    expect(r2.stdout).toContain("Switched to b");
    // The user's prose above the markers is preserved byte-for-byte.
    const final = await fs.readFile(rootClaudeMd, "utf8");
    expect(final.startsWith("# Pre-existing notes.")).toBe(true);
    expect(final).toContain("PROFILE-B-MANAGED-BODY");
    // .claude/ landed b's content.
    expect(
      await fs.readFile(path.join(claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
  });

  it("multiple managed blocks (currently rejected): aborts; file unchanged", async () => {
    // Per docs/specs §12.3 multiple managed blocks are reserved for future
    // versions and v1 implementations may treat as malformed. Exercise that
    // path so a future relaxation needs an explicit test update.
    await ensureBuilt();
    fx = await setupRootContributor();
    const rootClaudeMd = path.join(fx.projectRoot, "CLAUDE.md");
    const claudeDir = path.join(fx.projectRoot, ".claude");

    const dual =
      "intro\n" +
      "<!-- claude-profiles:v1:begin -->\nblock 1\n<!-- claude-profiles:v1:end -->\n" +
      "between\n" +
      "<!-- claude-profiles:v1:begin -->\nblock 2\n<!-- claude-profiles:v1:end -->\n" +
      "tail\n";
    await fs.writeFile(rootClaudeMd, dual);

    const preClaude = await readTree(claudeDir);

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r.exitCode).toBe(1);
    expect(await fs.readFile(rootClaudeMd, "utf8")).toBe(dual);
    expect(await readTree(claudeDir)).toEqual(preClaude);
  });
});

/**
 * Recursively read every regular file under `root` keyed by relative posix
 * path → utf8 content. Returns {} for a missing root so pre/post comparisons
 * stay uniform.
 */
async function readTree(root: string): Promise<Record<string, string>> {
  const out: Record<string, string> = {};
  async function walk(rel: string): Promise<void> {
    const here = rel === "" ? root : path.join(root, rel);
    let entries: Dirent[];
    try {
      entries = (await fs.readdir(here, { withFileTypes: true })) as Dirent[];
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code === "ENOENT") return;
      throw err;
    }
    for (const e of entries) {
      const name = String(e.name);
      const childRel = rel === "" ? name : `${rel}/${name}`;
      const childAbs = path.join(here, name);
      if (e.isDirectory()) {
        await walk(childRel);
      } else if (e.isFile()) {
        out[childRel] = await fs.readFile(childAbs, "utf8");
      }
    }
  }
  await walk("");
  return out;
}
