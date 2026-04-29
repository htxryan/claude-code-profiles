/**
 * E7 fitness function: scenario coverage S1-S18 (spec §5) end-to-end through
 * the spawned CLI binary. This is the cross-epic acceptance gate — each
 * scenario exercises ResolvedPlan (E1) → MergedFile (E2) → StateFile/Lock (E3)
 * → DriftReport (E4) → CLI dispatch (E5) → Init/Hook (E6) without in-process
 * mocks.
 *
 * Scenarios already covered exhaustively at the unit level in their owning
 * epic's tests are smoke-checked here; the value of this file is asserting
 * that the contract holds across the spawn/argv/stderr/exit-code boundary.
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

async function setupActive(profile: "a" | "b" = "a") {
  const f = await makeFixture({
    profiles: {
      a: {
        manifest: { name: "a", description: "alpha" },
        files: { "CLAUDE.md": "A\n", "settings.json": '{"v":"a"}' },
      },
      b: {
        manifest: { name: "b", description: "beta" },
        files: { "CLAUDE.md": "B\n", "settings.json": '{"v":"b"}' },
      },
    },
  });
  const plan = await resolve(profile, { projectRoot: f.projectRoot });
  const m = await merge(plan);
  await materialize(buildStatePaths(f.projectRoot), plan, m);
  return f;
}

describe("E7 scenarios S1-S18 (cross-epic CLI gate)", () => {
  // ──────────────────────────────────────────────────────────────────────
  // S1: First-time init in project with existing .claude/
  // ──────────────────────────────────────────────────────────────────────
  it("S1: init in project with existing .claude/ seeds starter, writes gitignore", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    await fs.mkdir(path.join(fx.projectRoot, ".claude"), { recursive: true });
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "# rules\n");
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "init", "--no-hook"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain('Seeded starter profile "default"');

    // gitignore (R28) was written
    const gi = await fs.readFile(path.join(fx.projectRoot, ".gitignore"), "utf8");
    expect(gi).toContain(".claude/");
    expect(gi).toContain(".claude-profiles/.meta/");
    // Starter profile (R27) seeded with content
    const seeded = await fs.readFile(
      path.join(fx.projectRoot, ".claude-profiles", "default", ".claude", "CLAUDE.md"),
      "utf8",
    );
    expect(seeded).toBe("# rules\n");
  });

  // ──────────────────────────────────────────────────────────────────────
  // S2: Clean swap (no drift)
  // ──────────────────────────────────────────────────────────────────────
  it("S2: clean swap — .claude/ replaced; .state.json updated", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Switched to b");
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
    const state = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", ".meta", "state.json"),
        "utf8",
      ),
    );
    expect(state.activeProfile).toBe("b");
  });

  // ──────────────────────────────────────────────────────────────────────
  // S3: Drift gate — discard
  // ──────────────────────────────────────────────────────────────────────
  it("S3: drift discard — edits lost; new profile materialised", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "EDIT\n");
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=discard", "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
    expect(r.stdout).toContain("Switched to b");
  });

  // ──────────────────────────────────────────────────────────────────────
  // S4: Drift gate — persist (whole-tree write-back to active profile)
  // ──────────────────────────────────────────────────────────────────────
  it("S4: drift persist — live tree copied into active profile, then swap", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "EDIT\n");
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=persist", "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    // a's profile dir now holds the edited content
    expect(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", "a", ".claude", "CLAUDE.md"),
        "utf8",
      ),
    ).toBe("EDIT\n");
    // live .claude/ now reflects b
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
  });

  // ──────────────────────────────────────────────────────────────────────
  // S5: Drift persist with component-sourced edit — file lands in active
  //     profile (overrides component); component dir untouched.
  // ──────────────────────────────────────────────────────────────────────
  it("S5: drift persist with component drift — saved to active, component untouched", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a", includes: ["compA"] }, files: {} },
        b: { manifest: { name: "b" }, files: { "CLAUDE.md": "B\n" } },
      },
      components: {
        compA: { files: { "CLAUDE.md": "FROM-COMP\n" } },
      },
    });
    const planA = await resolve("a", { projectRoot: fx.projectRoot });
    const m = await merge(planA);
    await materialize(buildStatePaths(fx.projectRoot), planA, m);
    // Edit a component-sourced file in the live tree.
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "EDIT\n");

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=persist", "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    // The override file is now inside the active profile, overriding the component.
    expect(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", "a", ".claude", "CLAUDE.md"),
        "utf8",
      ),
    ).toBe("EDIT\n");
    // The component itself is left untouched.
    expect(
      await fs.readFile(
        path.join(
          fx.projectRoot,
          ".claude-profiles",
          "_components",
          "compA",
          ".claude",
          "CLAUDE.md",
        ),
        "utf8",
      ),
    ).toBe("FROM-COMP\n");
  });

  // ──────────────────────────────────────────────────────────────────────
  // S6: Drift gate — abort (no change to disk)
  // ──────────────────────────────────────────────────────────────────────
  it("S6: drift abort — no change to .claude/ or .state.json", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "EDIT\n");
    const stateBefore = await fs.readFile(
      path.join(fx.projectRoot, ".claude-profiles", ".meta", "state.json"),
      "utf8",
    );
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=abort", "use", "b"],
    });
    expect(r.exitCode).toBe(1); // user error class
    // Content unchanged
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("EDIT\n");
    // State unchanged
    const stateAfter = await fs.readFile(
      path.join(fx.projectRoot, ".claude-profiles", ".meta", "state.json"),
      "utf8",
    );
    expect(stateAfter).toBe(stateBefore);
  });

  // ──────────────────────────────────────────────────────────────────────
  // S7: Include conflict (R11) — error names contributors and path
  // ──────────────────────────────────────────────────────────────────────
  it("S7: include conflict — exit 3, stderr names contributors and path", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        broken: {
          manifest: { name: "broken", includes: ["compA", "compB"] },
          files: {},
        },
      },
      components: {
        compA: { files: { "agents/x.json": "A" } },
        compB: { files: { "agents/x.json": "B" } },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "broken"],
    });
    expect(r.exitCode).toBe(3);
    // Resolver names the conflicting path + contributors.
    expect(r.stderr).toContain("agents/x.json");
  });

  // ──────────────────────────────────────────────────────────────────────
  // S8: Missing external component (R7)
  // ──────────────────────────────────────────────────────────────────────
  it("S8: missing external include — exit 3, stderr names the missing path", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        broken: {
          manifest: {
            name: "broken",
            includes: ["/this/path/does/not/exist/anywhere"],
          },
          files: {},
        },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "broken"],
    });
    expect(r.exitCode).toBe(3);
    expect(r.stderr).toContain("/this/path/does/not/exist/anywhere");
  });

  // ──────────────────────────────────────────────────────────────────────
  // S9: Cycle in extends — error names cycle members
  // ──────────────────────────────────────────────────────────────────────
  it("S9: cycle in extends — exit 3, stderr names cycle members", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a", extends: "b" }, files: {} },
        b: { manifest: { name: "b", extends: "a" }, files: {} },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "a"],
    });
    expect(r.exitCode).toBe(3);
    expect(r.stderr.toLowerCase()).toContain("cycle");
    // CycleError formats members in cycle order joined with " → " (see
    // src/errors/index.ts:CycleError). A loose `toContain("a")` would also
    // match incidental letters in words like "Cycle" or "extends"; assert
    // the full ordered chain instead so this gate fails on the right thing.
    expect(r.stderr).toMatch(/a\s*→\s*b\s*→\s*a/);
  });

  // ──────────────────────────────────────────────────────────────────────
  // S10: Pre-commit warning — drift present, hook exits 0 + prints warning
  // ──────────────────────────────────────────────────────────────────────
  it("S10: drift --pre-commit-warn — drift present, exit 0, warning printed", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "EDIT\n");
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "drift", "--pre-commit-warn"],
    });
    // The hook contract is: never block the commit. R25.
    expect(r.exitCode).toBe(0);
    // Hook output goes to stderr per R25, with the canonical phrasing from
    // src/drift/pre-commit.ts: "claude-profiles: <N> drifted file(s)
    // in .claude/ vs active profile '<name>'". Asserting against just
    // "drift" would also pass on the no-drift "drift: clean" output and
    // would mask a regression that flipped the hook to read-only mode.
    expect(r.stderr).toMatch(/claude-profiles: \d+ drifted file\(s\)/);
  });

  // ──────────────────────────────────────────────────────────────────────
  // S11: Validate all profiles — pass/fail report; non-zero on failures
  // ──────────────────────────────────────────────────────────────────────
  it("S11: validate — pass:true on healthy fixture; non-zero on broken fixture", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    // cw6 / R44: when an active profile is set, validate verifies project-
    // root CLAUDE.md has markers. setupActive activates a profile but does
    // not run init, so we seed the markers manually here. (S1 covers the
    // init-managed case explicitly.)
    await fs.writeFile(
      path.join(fx.projectRoot, "CLAUDE.md"),
      "<!-- claude-profiles:v1:begin -->\n<!-- Managed block. -->\n\n<!-- claude-profiles:v1:end -->\n",
    );
    let r = await runCli({
      args: ["--cwd", fx.projectRoot, "validate"],
    });
    expect(r.exitCode).toBe(0);

    // Replace with a broken fixture and re-run. Resolution failures map to
    // exit 3 (CONFLICT class — see src/cli/exit.ts:exitCodeFor).
    await fx.cleanup();
    fx = await makeFixture({
      profiles: {
        broken: { manifest: { name: "broken", extends: "missing" }, files: {} },
      },
    });
    r = await runCli({
      args: ["--cwd", fx.projectRoot, "validate"],
    });
    expect(r.exitCode).toBe(3);
  });

  // ──────────────────────────────────────────────────────────────────────
  // S12: Sync after editing the active profile directly
  // ──────────────────────────────────────────────────────────────────────
  it("S12: sync — re-materialises after profile-source edit; drift gate runs first", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    // Edit the *profile source* (not the live tree). After sync, the live
    // tree should reflect the edit.
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude-profiles", "a", ".claude", "CLAUDE.md"),
      "A-V2\n",
    );
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "sync"],
    });
    expect(r.exitCode).toBe(0);
    expect(
      await fs.readFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "utf8"),
    ).toBe("A-V2\n");
  });

  // ──────────────────────────────────────────────────────────────────────
  // S13: Diff two profiles
  // ──────────────────────────────────────────────────────────────────────
  it("S13: diff a b — file-level diff of resolved file lists", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "diff", "a", "b"],
    });
    expect(r.exitCode).toBe(0);
    const payload = JSON.parse(r.stdout);
    expect(Array.isArray(payload.entries)).toBe(true);
    expect(payload.entries.length).toBeGreaterThan(0);
    // CLAUDE.md content differs between a and b. The diff entry shape is
    // `{ relPath, status }` (see DiffEntry in src/cli/commands/diff.ts).
    const claudeEntry = payload.entries.find(
      (e: { relPath: string }) => e.relPath === "CLAUDE.md",
    );
    expect(claudeEntry).toBeDefined();
    expect(claudeEntry.status).toBe("changed");
  });

  // ──────────────────────────────────────────────────────────────────────
  // S15: Stale lock recovery (R41b) — `use` after a prior process crashed
  //      mid-write must auto-release the lock and proceed.
  // ──────────────────────────────────────────────────────────────────────
  it("S15: stale lock from dead PID auto-released; `use` proceeds", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    // Pre-plant a lock file naming a dead PID (well above any normal PID
    // range; kill(0, n) reports ESRCH for unused PIDs).
    const lockPath = path.join(fx.projectRoot, ".claude-profiles", ".meta", "lock");
    await fs.writeFile(lockPath, "99999998 2026-01-01T00:00:00.000Z\n");

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Switched to b");
    // The stale lock must have been replaced and then released cleanly.
    await expect(fs.access(lockPath)).rejects.toMatchObject({ code: "ENOENT" });
  });

  // ──────────────────────────────────────────────────────────────────────
  // S16: Crash mid-materialization (R16, R16a) — next CLI invocation
  //      reconciles via the .prior/ rename-back protocol BEFORE drift
  //      detection runs, so a bare `use` recovers cleanly without
  //      requiring --on-drift= (claude-code-profiles-ch5 fix). The
  //      reconcile clears both staging artifacts and proceeds with the
  //      requested swap.
  // ──────────────────────────────────────────────────────────────────────
  it("S16: stale .prior/.pending from a crashed materialize → bare `use` recovers cleanly (ch5)", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const profilesDir = path.join(fx.projectRoot, ".claude-profiles");
    const claudeDir = path.join(fx.projectRoot, ".claude");
    const metaDir = path.join(profilesDir, ".meta");
    const priorDir = path.join(metaDir, "prior");
    const pendingDir = path.join(metaDir, "pending");

    // Simulate post-step-(b) crash: .claude/ moved to .prior/, .pending/
    // partially staged.
    await fs.rename(claudeDir, priorDir);
    await fs.mkdir(pendingDir, { recursive: true });
    await fs.writeFile(path.join(pendingDir, "STALE.md"), "STALE");

    // ch5 followup: a bare non-interactive `use` MUST recover without
    // requiring an explicit --on-drift= flag. The swap orchestrator's
    // entrypoint reconcile renames .prior/ back to .claude/ before the
    // outside-lock drift detect runs, so the gate sees the live tree
    // intact (no false-positive drift) and the swap proceeds.
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    expect(
      await fs.readFile(path.join(claudeDir, "CLAUDE.md"), "utf8"),
    ).toBe("B\n");
    // Reconcile cleared both staging artifacts.
    await expect(fs.access(priorDir)).rejects.toMatchObject({ code: "ENOENT" });
    await expect(fs.access(pendingDir)).rejects.toMatchObject({ code: "ENOENT" });
  });

  // ──────────────────────────────────────────────────────────────────────
  // S17: Corrupted .state.json (R42) — treated as NoActive; warning
  // ──────────────────────────────────────────────────────────────────────
  it("S17: corrupted .state.json — status treats as NoActive; warning printed", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude-profiles", ".meta", "state.json"),
      "{not valid json",
    );
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "status"],
    });
    // R42: never abort on malformed state — degrade to NoActive.
    expect(r.exitCode).toBe(0);
    // The degraded UX must surface "no active profile" specifically; a
    // generic "warning" or "error" wouldn't tell the user the system is in
    // the documented NoActive state.
    const out = `${r.stdout}${r.stderr}`;
    expect(out.toLowerCase()).toMatch(/no active profile/);
  });

  // ──────────────────────────────────────────────────────────────────────
  // S18: Pre-commit hook exits 0 silently when binary not on PATH (R25a).
  //      Verified by running the hook script with a stripped PATH.
  // ──────────────────────────────────────────────────────────────────────
  // S18 spawns /bin/sh to run the hook script — Windows runners don't have
  // POSIX shell available on the runner image. The hook contract itself is
  // documented as POSIX (R25a, hook script starts with `#!/bin/sh`), so the
  // test is platform-correctly skipped on Windows.
  it.skipIf(process.platform === "win32")("S18: pre-commit hook with missing claude-profiles binary — exit 0 silent", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    await fs.mkdir(path.join(fx.projectRoot, ".git", "hooks"), { recursive: true });
    // Install the hook (writes the spec script).
    const install = await runCli({
      args: ["--cwd", fx.projectRoot, "hook", "install"],
    });
    expect(install.exitCode).toBe(0);
    const hookPath = path.join(fx.projectRoot, ".git", "hooks", "pre-commit");
    // Run the hook with a PATH that contains nothing — the `command -v`
    // guard must fail cleanly and the hook must exit 0.
    const { spawn } = await import("node:child_process");
    const result = await new Promise<{ code: number | null; out: string }>((res, rej) => {
      const child = spawn("/bin/sh", [hookPath], {
        cwd: fx!.projectRoot,
        env: { PATH: "/nonexistent" },
        stdio: ["ignore", "pipe", "pipe"],
      });
      let out = "";
      child.stdout.on("data", (d: Buffer) => (out += d.toString()));
      child.stderr.on("data", (d: Buffer) => (out += d.toString()));
      child.on("close", (code) => res({ code, out }));
      child.on("error", rej);
    });
    expect(result.code).toBe(0);
    // No output expected — the guard short-circuits silently.
    expect(result.out).toBe("");
  });
});

describe("ppo: error messages name the next step", () => {
  // Did-you-mean: typo within Levenshtein distance 2 → suggestion appended to
  // the MissingProfile message. Exit code stays exit-1 (CLI typo class —
  // referencedBy=undefined). Code field is unchanged ("MissingProfile") so
  // machines keying off it are unaffected.
  it("`use <typo>` near an existing profile → 'did you mean: <name>?' in stderr", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        ghost: { manifest: { name: "ghost" }, files: { "x.md": "x\n" } },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "ghst"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('Profile "ghst" does not exist');
    expect(r.stderr).toContain("did you mean: ghost?");
  });

  it("`diff <typo> <real>` near an existing profile → suggestion appended", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        alpha: { manifest: { name: "alpha" }, files: { "x.md": "a\n" } },
        beta: { manifest: { name: "beta" }, files: { "x.md": "b\n" } },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "diff", "alfa", "beta"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("did you mean: alpha?");
  });

  it("`validate <typo>` near an existing profile → suggestion in FAIL row", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        production: { manifest: { name: "production" }, files: {} },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "validate", "prodction"],
    });
    expect(r.exitCode).toBe(3); // validation failure → conflict class
    // Human output prints `[MissingProfile] Profile "..." does not exist (did you mean: ...)`
    const out = `${r.stdout}${r.stderr}`;
    expect(out).toContain("did you mean: production?");
  });

  it("typo with NO close match → no suggestion (current behaviour preserved)", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        alpha: { manifest: { name: "alpha" }, files: {} },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "zzzzz"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('Profile "zzzzz" does not exist');
    expect(r.stderr).not.toContain("did you mean");
  });

  it("multiple matches within distance 2 → comma-separated list, max 3", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        abcd: { manifest: { name: "abcd" }, files: {} },
        abce: { manifest: { name: "abce" }, files: {} },
        abcf: { manifest: { name: "abcf" }, files: {} },
        abcg: { manifest: { name: "abcg" }, files: {} },
        abch: { manifest: { name: "abch" }, files: {} },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "abc"],
    });
    expect(r.exitCode).toBe(1);
    // The suggestion list is bounded to 3 entries; the exact set is the
    // first 3 sorted lex (since distance is the same for all five).
    expect(r.stderr).toContain("did you mean: abcd, abce, abcf?");
  });

  // Path-traversal-shaped names: pre-flight rejects with the standardized
  // "invalid profile name (contains /, \\, leading . or _)" wording rather
  // than a generic missing-profile error.
  it("`use a/b` → 'invalid profile name' wording (path separator)", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: { real: { manifest: { name: "real" }, files: {} } },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "a/b"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('invalid profile name "a/b"');
    expect(r.stderr).toContain("contains /, \\, leading . or _");
  });

  it("`diff a/b real` → 'invalid profile name' wording", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: { real: { manifest: { name: "real" }, files: {} } },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "diff", "a/b", "real"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('invalid profile name "a/b"');
  });

  it("`validate a/b` → 'invalid profile name' wording", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "validate", "a/b"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('invalid profile name "a/b"');
  });

  // Init on already-initialised: hint suffix telling the user what to do next.
  it("`init` on already-initialised project → hint about status / new", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: { existing: { manifest: { name: "existing" }, files: {} } },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "init"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("already initialised");
    // Forward-momentum hint per ppo AC.
    expect(r.stderr).toContain('claude-profiles status');
    expect(r.stderr).toContain('claude-profiles new');
  });
});

describe("E7 contracts: ResolvedPlan provenance survives the CLI surface", () => {
  it("ResolvedPlan contributors persist into .state.json.resolvedSources via CLI use", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        base: { manifest: {}, files: { "CLAUDE.md": "B\n" } },
        leaf: {
          manifest: { extends: "base", includes: ["compA"] },
          files: { "CLAUDE.md": "L\n" },
        },
      },
      components: {
        compA: { files: { "CLAUDE.md": "A\n" } },
      },
    });

    // Drive the swap end-to-end through the spawned CLI (not a unit-level
    // service call). This is the cross-epic gate: ResolvedPlan provenance
    // must survive E1 → E2 → E3 → E5 and land in the on-disk state file.
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "leaf"],
    });
    expect(r.exitCode).toBe(0);

    const state = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", ".meta", "state.json"),
        "utf8",
      ),
    );
    expect(state.activeProfile).toBe("leaf");
    // resolvedSources is the persisted projection of ResolvedPlan.contributors.
    const ids = (state.resolvedSources as { id: string }[]).map((s) => s.id);
    expect(ids).toEqual(["base", "compA", "leaf"]);
    const kinds = (state.resolvedSources as { kind: string }[]).map((s) => s.kind);
    expect(kinds).toEqual(["ancestor", "include", "profile"]);

    // status --json exposes the active profile so consumers can confirm
    // which fixture is live (full resolvedSources are not part of the
    // public status payload by design — the CLI surface keeps it minimal).
    const status = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "status"],
    });
    expect(status.exitCode).toBe(0);
    const sp = JSON.parse(status.stdout);
    expect(sp.activeProfile).toBe("leaf");
  });

  it("R12 vs R8: hooks concat order survives through CLI sync", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        base: {
          manifest: {},
          files: {
            "settings.json": JSON.stringify({
              hooks: { PreToolUse: [{ src: "base" }] },
            }),
          },
        },
        leaf: {
          manifest: { extends: "base", includes: ["compA"] },
          files: {
            "settings.json": JSON.stringify({
              hooks: { PreToolUse: [{ src: "leaf" }] },
            }),
          },
        },
      },
      components: {
        compA: {
          files: {
            "settings.json": JSON.stringify({
              hooks: { PreToolUse: [{ src: "compA" }] },
            }),
          },
        },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "leaf"],
    });
    expect(r.exitCode).toBe(0);
    const settings = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude", "settings.json"),
        "utf8",
      ),
    );
    // Canonical order: base, compA, leaf.
    expect(settings.hooks.PreToolUse).toEqual([
      { src: "base" },
      { src: "compA" },
      { src: "leaf" },
    ]);
  });
});
