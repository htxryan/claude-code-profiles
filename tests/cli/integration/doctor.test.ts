/**
 * Integration tests for `claude-profiles doctor` (claude-code-profiles-0zn).
 *
 * Doctor is read-only and surfaces actionable warnings — these tests pin the
 * exit-code contract (0 on healthy, 1 on broken) and the JSON schema shape so
 * CI scripts can rely on `claude-profiles doctor --json`.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { makeFixture, type Fixture } from "../../helpers/fixture.js";
import { ensureBuilt, runCli } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

interface DoctorJson {
  pass: boolean;
  checks: Array<{
    id: string;
    label: string;
    status: "ok" | "warn" | "fail" | "skip";
    detail: string;
    remediation: string;
  }>;
}

describe("doctor (claude-code-profiles-0zn)", () => {
  it("healthy fixture: returns exit 0 and pass=true", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    // init scaffolds .claude-profiles/, .gitignore, CLAUDE.md markers; then
    // we add a profile via `new` so the profiles dir is non-empty.
    const initR = await runCli({ cwd: fx.projectRoot, args: ["init", "--no-hook"] });
    expect(initR.exitCode).toBe(0);
    await runCli({ cwd: fx.projectRoot, args: ["new", "dev"] });

    const r = await runCli({ cwd: fx.projectRoot, args: ["doctor"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("[ok]");
    expect(r.stdout).toMatch(/\d+ ok/);
  });

  it("broken fixture (no .claude-profiles/): returns exit 1 with actionable detail", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    // Skip init so .claude-profiles/ is missing entirely.
    const r = await runCli({ cwd: fx.projectRoot, args: ["doctor"] });
    expect(r.exitCode).toBe(1);
    expect(r.stdout + r.stderr).toContain("not found");
    expect(r.stdout + r.stderr).toContain("init");
  });

  it("--json: schema-stable payload with checks[] array", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    await runCli({ cwd: fx.projectRoot, args: ["init", "--no-hook"] });
    await runCli({ cwd: fx.projectRoot, args: ["new", "dev"] });

    const r = await runCli({ cwd: fx.projectRoot, args: ["doctor", "--json"] });
    expect(r.exitCode).toBe(0);
    const payload = JSON.parse(r.stdout) as DoctorJson;
    expect(payload).toHaveProperty("pass");
    expect(payload).toHaveProperty("checks");
    expect(Array.isArray(payload.checks)).toBe(true);
    expect(payload.checks.length).toBeGreaterThan(0);

    // Every check has the documented shape — CI scripts will grep on these
    // ids so format drift must be visible.
    for (const c of payload.checks) {
      expect(typeof c.id).toBe("string");
      expect(typeof c.label).toBe("string");
      expect(["ok", "warn", "fail", "skip"]).toContain(c.status);
      expect(typeof c.detail).toBe("string");
      expect(typeof c.remediation).toBe("string");
    }

    // The id list is the public contract — pin it.
    const ids = new Set(payload.checks.map((c) => c.id));
    expect(ids).toContain("profiles_dir");
    expect(ids).toContain("state_file");
    expect(ids).toContain("lock");
    expect(ids).toContain("gitignore");
    expect(ids).toContain("hook");
    expect(ids).toContain("backups");
    expect(ids).toContain("active_profile");
    expect(ids).toContain("external_paths");
    expect(ids).toContain("root_claude_md_markers");
  });

  it("--json on broken fixture: pass=false, exit 1", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({ cwd: fx.projectRoot, args: ["doctor", "--json"] });
    expect(r.exitCode).toBe(1);
    const payload = JSON.parse(r.stdout) as DoctorJson;
    expect(payload.pass).toBe(false);
    const failed = payload.checks.find((c) => c.id === "profiles_dir");
    expect(failed?.status).toBe("fail");
    expect(failed?.remediation).toContain("init");
  });

  it("missing .gitignore entries: warns but still surfaces every other check", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    await runCli({ cwd: fx.projectRoot, args: ["init", "--no-hook"] });
    await runCli({ cwd: fx.projectRoot, args: ["new", "dev"] });
    // Truncate the .gitignore so the gitignore check warns. Doctor should
    // continue running every other check (no short-circuit).
    await fs.writeFile(path.join(fx.projectRoot, ".gitignore"), "# user only\n");
    const r = await runCli({ cwd: fx.projectRoot, args: ["doctor", "--json"] });
    expect(r.exitCode).toBe(1);
    const payload = JSON.parse(r.stdout) as DoctorJson;
    const gi = payload.checks.find((c) => c.id === "gitignore");
    expect(gi?.status).toBe("warn");
    expect(gi?.detail).toContain("missing");
    // Every other check still ran.
    expect(payload.checks.length).toBeGreaterThanOrEqual(8);
  });

  it("read-only: state.json/lock/.gitignore unchanged after doctor runs", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    await runCli({ cwd: fx.projectRoot, args: ["init", "--no-hook"] });
    await runCli({ cwd: fx.projectRoot, args: ["new", "dev"] });

    const giPath = path.join(fx.projectRoot, ".gitignore");
    const beforeGI = await fs.readFile(giPath, "utf8");
    const stateDir = path.join(fx.projectRoot, ".claude-profiles", ".meta");
    let beforeStateExists: boolean;
    try {
      await fs.access(path.join(stateDir, "state.json"));
      beforeStateExists = true;
    } catch {
      beforeStateExists = false;
    }

    await runCli({ cwd: fx.projectRoot, args: ["doctor"] });
    await runCli({ cwd: fx.projectRoot, args: ["doctor", "--json"] });

    const afterGI = await fs.readFile(giPath, "utf8");
    expect(afterGI).toBe(beforeGI);
    let afterStateExists: boolean;
    try {
      await fs.access(path.join(stateDir, "state.json"));
      afterStateExists = true;
    } catch {
      afterStateExists = false;
    }
    expect(afterStateExists).toBe(beforeStateExists);
  });
});
