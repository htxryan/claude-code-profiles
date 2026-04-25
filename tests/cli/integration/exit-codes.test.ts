/**
 * Exit-code matrix end-to-end. Each row asserts:
 *   given <fixture state> + <argv>, the spawned bin exits with <code>.
 *
 * Stable across releases (epic fitness function).
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { merge } from "../../../src/merge/merge.js";
import { resolve } from "../../../src/resolver/resolve.js";
import { materialize } from "../../../src/state/materialize.js";
import { buildStatePaths } from "../../../src/state/paths.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { BIN_PATH, runCli } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

async function ensureBuilt() {
  try {
    await fs.access(BIN_PATH);
  } catch {
    throw new Error(
      `dist/cli/bin.js not found at ${BIN_PATH} — run \`npm run build\` before integration tests`,
    );
  }
}

describe("exit-code matrix (AC-16)", () => {
  it("--version → 0", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["--version"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("claude-profiles");
  });

  it("unknown verb → 1", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["bogus"] });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("unknown command");
  });

  it("missing argv → 1", async () => {
    await ensureBuilt();
    const r = await runCli({ args: [] });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("missing command");
  });

  it("init in a fresh project (no .claude, no hook) → 0", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "init", "--no-seed", "--no-hook"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Initialised claude-profiles");
  });

  it("hook install in a project without .git/ → 2 (ENOENT)", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "hook", "install"],
    });
    expect(r.exitCode).toBe(2);
  });

  it("use missing profile → 3 (MissingProfile)", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "nonexistent"],
    });
    expect(r.exitCode).toBe(3);
    expect(r.stderr.toLowerCase()).toContain("nonexistent");
  });

  it("validate failing profile → 3", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        broken: { manifest: { name: "broken", extends: "nope" }, files: {} },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "validate"],
    });
    expect(r.exitCode).toBe(3);
  });

  it("non-TTY use with drift + no flag → 1", async () => {
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

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "b"],
    });
    expect(r.exitCode).toBe(1);
    expect(r.stderr).toContain("--on-drift=");
  });

  it("non-TTY use with drift + --on-drift=discard → 0", async () => {
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

    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--on-drift=discard", "use", "b"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Switched to b");
  });
});
