/**
 * Integration coverage for the `status` command (R31, R40, R42).
 *
 * scenarios.test.ts touches `status` indirectly (S17 covers the corrupted
 * state-file path), and the in-process status.test.ts in tests/cli/commands/
 * pins the human + JSON shape — but neither asserts the spawn-boundary
 * behaviour for the four states a user actually moves through:
 *
 *   1. NoActive (R42 fresh project) — exit 0, "no active profile"
 *   2. Clean — exit 0, "drift: clean", JSON `drift.total === 0`
 *   3. Drifted — exit 0, drift summary line + correct counts in JSON
 *   4. Stale source — exit 0, "source: updated since last materialize"
 *   5. Malformed state.json (R42 graceful degrade) — exit 0, treat as NoActive
 *
 * Reverting any of: the read-only-no-lock contract (R43), the R42
 * graceful-degrade fallback in readStateFile, or the stale-source signal
 * azp added must surface as a failure here.
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

describe("CLI rejects Windows-reserved profile names end-to-end (R39)", () => {
  // Whitebox tests for the predicate live in tests/resolver/windows-reserved-
  // names.test.ts. This pair is the spawn-boundary end-to-end check: a
  // refactor that bypasses assertValidProfileName, or a regression that
  // strips the actionable wording before it reaches stderr, fails here.
  it("`new CON` → exit 1 with actionable wording naming reserved family", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "new", "CON"] });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('invalid profile name "CON"');
    expect(r.stderr).toMatch(/CON\/PRN\/AUX\/NUL\/COM1-9\/LPT1-9/);
  });

  it("`use PRN.txt` → exit 1 (extension counts; rejected before swap)", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: { real: { manifest: { name: "real" }, files: { "x.md": "x" } } },
    });
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "PRN.txt"] });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain('invalid profile name "PRN.txt"');
  });
});

describe("status integration (R31, R40, R42, R43)", () => {
  // ──────────────────────────────────────────────────────────────────────
  // (1) NoActive — fresh project with no profiles defined
  // ──────────────────────────────────────────────────────────────────────
  it("no profiles, no state: exit 0, prints 'no active profile' nudge", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "status"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout.toLowerCase()).toContain("no active profile");
    // First-time nudge points at `new`, not `use`, when no profiles exist.
    expect(r.stdout).toContain("claude-profiles new");
  });

  // ──────────────────────────────────────────────────────────────────────
  // (2) Clean — active profile, no drift
  // ──────────────────────────────────────────────────────────────────────
  it("after materialize: 'active: a', 'drift: clean', exit 0", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "status"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("active: a");
    expect(r.stdout).toMatch(/drift:.*clean/);
    // Description from manifest is surfaced on a dim line.
    expect(r.stdout).toContain("alpha");
  });

  it("--json on clean state: drift.total === 0, fingerprintOk:true", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "--json", "status"] });
    expect(r.exitCode).toBe(0);
    const payload = JSON.parse(r.stdout);
    expect(payload.activeProfile).toBe("a");
    expect(payload.drift.fingerprintOk).toBe(true);
    expect(payload.drift.total).toBe(0);
    expect(payload.drift.modified).toBe(0);
    expect(payload.drift.added).toBe(0);
    expect(payload.drift.deleted).toBe(0);
    expect(payload.sourceFresh).toBe(true);
    expect(typeof payload.sourceFingerprint).toBe("string");
    expect(payload.warnings).toEqual([]);
  });

  // ──────────────────────────────────────────────────────────────────────
  // (3) Drifted — modified + added live files
  // ──────────────────────────────────────────────────────────────────────
  it("drifted live files: human surface counts modified/added; JSON parity", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const claudeDir = path.join(fx.projectRoot, ".claude");
    await fs.writeFile(path.join(claudeDir, "CLAUDE.md"), "EDITED\n");
    await fs.writeFile(path.join(claudeDir, "extra.md"), "X\n");

    const human = await runCli({ args: ["--cwd", fx.projectRoot, "status"] });
    expect(human.exitCode).toBe(0);
    expect(human.stdout).toContain("active: a");
    // Summary line shape: "drift: N (M modified, A added, D deleted)"
    expect(human.stdout).toMatch(/drift: \d+ \(\d+ modified, \d+ added, \d+ deleted/);

    const json = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "status"],
    });
    expect(json.exitCode).toBe(0);
    const p = JSON.parse(json.stdout);
    expect(p.drift.modified).toBe(1);
    expect(p.drift.added).toBe(1);
    expect(p.drift.deleted).toBe(0);
    expect(p.drift.total).toBe(2);
  });

  it("deleted live file is reflected as 'deleted' in JSON", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    await fs.rm(path.join(fx.projectRoot, ".claude", "CLAUDE.md"));

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "status"],
    });
    expect(r.exitCode).toBe(0);
    const p = JSON.parse(r.stdout);
    expect(p.drift.deleted).toBeGreaterThanOrEqual(1);
    expect(p.drift.total).toBeGreaterThanOrEqual(1);
  });

  // ──────────────────────────────────────────────────────────────────────
  // (4) Stale source (azp) — profile-source bytes changed post-materialize
  // ──────────────────────────────────────────────────────────────────────
  it("source bytes changed since materialize: surfaces 'source: updated' + sync hint", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const sourceFile = path.join(
      fx.projectRoot,
      ".claude-profiles",
      "a",
      ".claude",
      "CLAUDE.md",
    );
    await fs.writeFile(sourceFile, "A-EDITED\n");
    // Future mtime so the size+mtime fast-path aggregate flips even if the
    // edit happens to keep the same size.
    const future = new Date(Date.now() + 5000);
    await fs.utimes(sourceFile, future, future);

    const human = await runCli({ args: ["--cwd", fx.projectRoot, "status"] });
    expect(human.exitCode).toBe(0);
    expect(human.stdout).toContain("source: updated since last materialize");
    expect(human.stdout).toContain("claude-profiles sync");

    const json = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "status"],
    });
    const p = JSON.parse(json.stdout);
    expect(p.sourceFresh).toBe(false);
  });

  // ──────────────────────────────────────────────────────────────────────
  // (5) R42 — malformed state.json → NoActive + non-fatal warning
  // ──────────────────────────────────────────────────────────────────────
  it("R42: unparseable state.json → NoActive, warning, exit 0", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const stateFile = path.join(
      fx.projectRoot,
      ".claude-profiles",
      ".meta",
      "state.json",
    );
    await fs.writeFile(stateFile, "{not valid json");

    const r = await runCli({ args: ["--cwd", fx.projectRoot, "status"] });
    // Critical: NEVER abort on malformed state. The system shall degrade
    // to NoActive (R42) so the user can recover with `use <name>`.
    expect(r.exitCode).toBe(0);
    const out = `${r.stdout}${r.stderr}`;
    expect(out.toLowerCase()).toMatch(/no active profile/);

    // JSON path: warning surfaced explicitly so consumers can detect the
    // degraded state without parsing free-form stderr.
    const json = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "status"],
    });
    expect(json.exitCode).toBe(0);
    const p = JSON.parse(json.stdout);
    expect(p.activeProfile).toBeNull();
    expect(p.warnings.length).toBeGreaterThanOrEqual(1);
    // The warning's `code` is a stable identifier (ParseError/SchemaError);
    // assert presence rather than the human `detail` (which can shift).
    expect(typeof p.warnings[0].code).toBe("string");
    expect(p.warnings[0].code).not.toBe("Missing");
  });

  it("R42: schema-invalid state.json (valid JSON, wrong shape) also degrades", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const stateFile = path.join(
      fx.projectRoot,
      ".claude-profiles",
      ".meta",
      "state.json",
    );
    // Valid JSON but missing required fields → schema invalid, R42 path.
    await fs.writeFile(stateFile, JSON.stringify({ unrelated: true }));

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "status"],
    });
    expect(r.exitCode).toBe(0);
    const p = JSON.parse(r.stdout);
    expect(p.activeProfile).toBeNull();
    expect(p.warnings.length).toBeGreaterThanOrEqual(1);
  });

  // ──────────────────────────────────────────────────────────────────────
  // (6) R43 — read-only command takes no lock (parallel status calls)
  // ──────────────────────────────────────────────────────────────────────
  it("R43: status does not require the lock — concurrent calls succeed", async () => {
    await ensureBuilt();
    fx = await setupActive("a");
    const calls = await Promise.all(
      Array.from({ length: 4 }, () =>
        runCli({ args: ["--cwd", fx!.projectRoot, "status"] }),
      ),
    );
    for (const r of calls) {
      expect(r.exitCode).toBe(0);
      expect(r.stdout).toContain("active: a");
    }
    // Lock file must not exist after any of those calls (R43).
    await expect(
      fs.access(path.join(fx.projectRoot, ".claude-profiles", ".meta", "lock")),
    ).rejects.toMatchObject({ code: "ENOENT" });
  });
});
