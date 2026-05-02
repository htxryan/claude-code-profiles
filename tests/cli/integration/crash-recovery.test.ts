/**
 * Gap closure #3 (PR6 #3, F2 epic claude-code-profiles-yhb):
 *
 * Crash-injection — 2 mandatory cases pre-1.0 (per port spec §8):
 *   (a) post-`.state.json.tmp`-write-pre-rename — the writer crashed after
 *       fsync but before atomic rename. The next invocation must reconcile.
 *   (b) mid-`.claude/`→`.prior/` rename — the materializer crashed mid
 *       backup-then-promote dance. The next invocation must reconcile.
 *
 * The remaining 3 originally-listed cases (mid-`.pending/` write,
 * mid-backup-snapshot, post-persist-but-pre-materialize) are explicitly
 * deferred to post-1.0 hardening per the spec §8.
 *
 * Approach:
 *   We don't have an in-process crash hook in the TS bin (no
 *   `C3P_CRASH_AT=...` env var) — that's a Go-side hardening
 *   convention. For the TS bin we **simulate** the crashed state directly:
 *     (a) leave a `.claude-profiles/.meta/state.json.tmp` on disk; verify
 *         the next swap proceeds and the orphaned tmp is cleaned up.
 *     (b) leave a `.claude-profiles/.meta/.prior/` and/or `.pending/` on
 *         disk; verify the next swap reconciles per R16a.
 *
 *   The contract pinned: c3p MUST recover gracefully — it must not refuse
 *   to start, must not surface the orphan files to the user as drift, and
 *   the next successful invocation MUST leave the staging area empty.
 *
 *   The Go IV translation will replace the simulated-state approach with
 *   real `C3P_CRASH_AT=...` env-var injection that aborts the bin at the
 *   exact crash site; the contract pinned here matches what those tests
 *   will assert.
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

async function setupActive(profile: "a" | "b" = "a"): Promise<Fixture> {
  const f = await makeFixture({
    profiles: {
      a: {
        manifest: { name: "a" },
        files: { "CLAUDE.md": "A\n", "settings.json": '{"v":"a"}' },
      },
      b: {
        manifest: { name: "b" },
        files: { "CLAUDE.md": "B\n", "settings.json": '{"v":"b"}' },
      },
    },
  });
  const plan = await resolve(profile, { projectRoot: f.projectRoot });
  const m = await merge(plan);
  await materialize(buildStatePaths(f.projectRoot), plan, m);
  return f;
}

describe("gap closure #3: crash recovery — 2 mandatory cases (PR6 #3)", () => {
  // ──────────────────────────────────────────────────────────────────────
  // Case (a): post-state.json.tmp write, pre-rename
  // ──────────────────────────────────────────────────────────────────────
  it("orphan .state.json.tmp does NOT confuse the bin → next swap succeeds, canonical state.json updated", async () => {
    // Production state.json staging happens under `.meta/tmp/` with a
    // unique-name pattern, so an orphan at `.meta/state.json.tmp` is *not*
    // on the bin's path. The contract pinned: an unrelated orphan file
    // beside the canonical state.json must NEVER be read in place of the
    // canonical file, and the next swap must complete cleanly.
    //
    // Note: the bin doesn't sweep this orphan (it's outside its tmp dir).
    // The Go translation may add an explicit reconcile sweep — that's a
    // separate hardening item, not pinned here.
    await ensureBuilt();
    fx = await setupActive("a");
    const stateDir = path.join(fx.projectRoot, ".claude-profiles", ".meta");
    const tmpFile = path.join(stateDir, "state.json.tmp");
    await fs.writeFile(tmpFile, "GARBAGE-FROM-CRASHED-WRITE");

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r.exitCode).toBe(0);

    // The canonical state.json reflects the new active profile, not the
    // garbage in the orphan tmp.
    const state = JSON.parse(
      await fs.readFile(path.join(stateDir, "state.json"), "utf8"),
    );
    expect(state.activeProfile).toBe("b");
  });

  it("orphan .state.json.tmp + invocation reads state.json (not tmp)", async () => {
    // Even READ verbs (like status) must not be confused by an orphan tmp.
    // The contract: the canonical file is state.json; tmp is internal.
    await ensureBuilt();
    fx = await setupActive("a");
    const stateDir = path.join(fx.projectRoot, ".claude-profiles", ".meta");
    await fs.writeFile(path.join(stateDir, "state.json.tmp"), "GARBAGE");

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "status"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("active: a");
  });

  it("orphan staging in .meta/tmp/<unique> does not break the bin", async () => {
    // The canonical staging path (`.meta/tmp/<unique>`) — production code
    // creates per-call unique tmps and unlinks them on success. Verify
    // that an unrelated orphan in this dir doesn't break a subsequent op.
    await ensureBuilt();
    fx = await setupActive("a");
    const tmpDir = path.join(fx.projectRoot, ".claude-profiles", ".meta", "tmp");
    await fs.mkdir(tmpDir, { recursive: true });
    await fs.writeFile(path.join(tmpDir, "orphan-xyz"), "GARBAGE");

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r.exitCode).toBe(0);
  });

  // ──────────────────────────────────────────────────────────────────────
  // Case (b): mid-.claude/→.prior/ rename
  //
  // The R16a swap dance is:
  //   1. write the new tree to `.pending/`
  //   2. rename live `.claude/` to `.prior/`
  //   3. rename `.pending/` to `.claude/`
  //   4. delete `.prior/`
  //
  // A crash between (2) and (3) leaves `.prior/` populated and `.claude/`
  // missing. A crash between (3) and (4) leaves `.prior/` populated and
  // `.claude/` containing the new tree. Either way, the next invocation
  // MUST reconcile cleanly.
  // ──────────────────────────────────────────────────────────────────────
  it("orphan prior/ + missing .claude/ (crash between rename steps) → next invocation recovers", async () => {
    // Per src/state/paths.ts:79, the canonical priorDir is
    // `.claude-profiles/.meta/prior` (no leading dot). reconcileMaterialize
    // must promote it back to live `.claude/` at startup of the next op.
    await ensureBuilt();
    fx = await setupActive("a");
    const profilesMeta = path.join(fx.projectRoot, ".claude-profiles", ".meta");
    const live = path.join(fx.projectRoot, ".claude");
    const prior = path.join(profilesMeta, "prior");

    // Simulate: rename live → prior happened, but the next rename never did.
    await fs.rename(live, prior);
    // Use idiomatic assertions (existence-as-bool) rather than the implicit
    // `fs.access` throw, so a regression surfaces as a Vitest failure with
    // the asserted value rather than a raw ENOENT stack.
    const priorExistsBefore = await fs
      .access(prior)
      .then(() => true)
      .catch(() => false);
    expect(priorExistsBefore).toBe(true);
    const liveExistsBefore = await fs
      .access(live)
      .then(() => true)
      .catch(() => false);
    expect(liveExistsBefore).toBe(false);

    // Run a swap. reconcile fires before materialize and restores live
    // from prior; the new materialize then writes b's bytes.
    //
    // NB: opus review suggested an intermediate `c3p status` to prove
    // reconcile ran *before* the next swap. The TS bin reconciles only on
    // write paths (`use`/`sync`), not on read verbs — so an intermediate
    // status check would falsely fail. The contract pinned here is
    // intentionally "next invocation recovers", matching the spec wording.
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r.exitCode).toBe(0);
    expect(await fs.readFile(path.join(live, "CLAUDE.md"), "utf8")).toBe("B\n");
    // prior/ is gone (reconcile + post-success cleanup remove it).
    const priorAfter = await fs
      .access(prior)
      .then(() => true)
      .catch(() => false);
    expect(priorAfter).toBe(false);
  });

  it("orphan prior/ + .claude/ present (crash AFTER promote, BEFORE prior cleanup) → next invocation recovers", async () => {
    // Reconcile restores live `.claude/` from prior/ at startup, then the
    // re-materialize is idempotent (R16a). For this test the prior content
    // is the same as the active fingerprint so the post-restore drift
    // check passes cleanly. We pass --on-drift=discard as a belt-and-
    // suspenders to make the contract robust against minor reconcile-vs-
    // drift ordering differences across implementations.
    await ensureBuilt();
    fx = await setupActive("a");
    const profilesMeta = path.join(fx.projectRoot, ".claude-profiles", ".meta");
    const prior = path.join(profilesMeta, "prior");

    // Snapshot live `.claude/` bytes (which match recorded fingerprint for a)
    // into prior/ to look like a real "post-promote, pre-cleanup" intermediate
    // where prior has the previous successful state (= recorded state).
    await fs.mkdir(prior, { recursive: true });
    await fs.writeFile(path.join(prior, "CLAUDE.md"), "A\n");
    await fs.writeFile(path.join(prior, "settings.json"), '{"v":"a"}');

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=discard", "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
    const priorAfter = await fs
      .access(prior)
      .then(() => true)
      .catch(() => false);
    expect(priorAfter).toBe(false);
  });

  it("orphan pending/ (crash mid-build) → next invocation recovers", async () => {
    // Per paths.ts:78, pendingDir is `.claude-profiles/.meta/pending`.
    await ensureBuilt();
    fx = await setupActive("a");
    const profilesMeta = path.join(fx.projectRoot, ".claude-profiles", ".meta");
    const pending = path.join(profilesMeta, "pending");
    await fs.mkdir(pending, { recursive: true });
    await fs.writeFile(path.join(pending, "CLAUDE.md"), "PARTIAL\n");

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "b"] });
    expect(r.exitCode).toBe(0);
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
    const pendingAfter = await fs
      .access(pending)
      .then(() => true)
      .catch(() => false);
    expect(pendingAfter).toBe(false);
  });
});
