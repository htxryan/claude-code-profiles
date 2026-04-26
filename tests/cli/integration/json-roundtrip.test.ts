/**
 * --json round-trip smoke tests for every read command (AC-3, AC-5, AC-8,
 * AC-14). Each command's stdout must parse cleanly with JSON.parse, and
 * --json must silence all human-readable output.
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

async function setup() {
  const f = await makeFixture({
    profiles: {
      a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      b: { manifest: { name: "b" }, files: { "CLAUDE.md": "B\n" } },
    },
  });
  const planA = await resolve("a", { projectRoot: f.projectRoot });
  const m = await merge(planA);
  await materialize(buildStatePaths(f.projectRoot), planA, m);
  return f;
}

describe("--json round-trip (AC-14)", () => {
  it("list --json: parses; stderr empty", async () => {
    await ensureBuilt();
    fx = await setup();
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "list"],
    });
    expect(r.exitCode).toBe(0);
    const payload = JSON.parse(r.stdout);
    expect(Array.isArray(payload.profiles)).toBe(true);
    expect(payload.profiles.find((p: { name: string }) => p.name === "a")).toBeDefined();
    expect(r.stderr).toBe("");
  });

  it("status --json: parses; stderr empty", async () => {
    await ensureBuilt();
    fx = await setup();
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "status"],
    });
    expect(r.exitCode).toBe(0);
    const payload = JSON.parse(r.stdout);
    expect(payload.activeProfile).toBe("a");
    expect(payload.drift).toBeDefined();
    expect(r.stderr).toBe("");
  });

  it("drift --json: parses; stderr empty", async () => {
    await ensureBuilt();
    fx = await setup();
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "EDIT\n");
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "drift"],
    });
    expect(r.exitCode).toBe(0);
    const payload = JSON.parse(r.stdout);
    expect(payload.schemaVersion).toBe(1);
    expect(payload.entries.length).toBeGreaterThan(0);
    expect(r.stderr).toBe("");
  });

  it("diff --json: parses; stderr empty", async () => {
    await ensureBuilt();
    fx = await setup();
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "diff", "a", "b"],
    });
    expect(r.exitCode).toBe(0);
    const payload = JSON.parse(r.stdout);
    expect(payload.entries.length).toBeGreaterThan(0);
    expect(r.stderr).toBe("");
  });

  it("validate --json: parses pass:true on healthy fixture", async () => {
    await ensureBuilt();
    fx = await setup();
    // cw6 / R44: validate verifies project-root CLAUDE.md markers when an
    // active profile is set. setup() materializes profile "a" without
    // running init, so seed the markers manually for this happy-path test.
    await fs.writeFile(
      path.join(fx.projectRoot, "CLAUDE.md"),
      "<!-- claude-profiles:v1:begin -->\n<!-- Managed block. -->\n\n<!-- claude-profiles:v1:end -->\n",
    );
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "validate"],
    });
    expect(r.exitCode).toBe(0);
    const payload = JSON.parse(r.stdout);
    expect(payload.pass).toBe(true);
    expect(r.stderr).toBe("");
  });
});
