import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { runDrift, type DriftCommandPayload } from "../../../src/cli/commands/drift.js";
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

async function setupActive(): Promise<Fixture> {
  const f = await makeFixture({
    profiles: {
      a: {
        manifest: { name: "a" },
        files: { "CLAUDE.md": "A\n", "agents/x.md": "X\n" },
      },
    },
  });
  const plan = await resolve("a", { projectRoot: f.projectRoot });
  const m = await merge(plan);
  await materialize(buildStatePaths(f.projectRoot), plan, m);
  return f;
}

describe("drift (R20, R40)", () => {
  it("clean: prints active + drift: clean", async () => {
    fx = await setupActive();
    const cap = captureOutput(false);
    const code = await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("active: a");
    expect(cap.stdout()).toContain("drift: clean");
  });

  it("modified file: per-file report includes status + relPath + provenance", async () => {
    fx = await setupActive();
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");

    const cap = captureOutput(false);
    await runDrift({ cwd: fx.projectRoot, output: cap.channel, preCommitWarn: false, verbose: false });
    const out = cap.stdout();
    expect(out).toContain("modified");
    expect(out).toContain("CLAUDE.md");
    expect(out).toContain("from: a");
  });

  it("--json round-trips full DriftReport shape", async () => {
    fx = await setupActive();
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");
    await fs.writeFile(path.join(paths.claudeDir, "extra.md"), "X\n");

    const cap = captureOutput(true);
    await runDrift({ cwd: fx.projectRoot, output: cap.channel, preCommitWarn: false, verbose: false });
    const payload = cap.jsonLines()[0] as DriftCommandPayload;
    expect(payload.schemaVersion).toBe(1);
    expect(payload.active).toBe("a");
    expect(payload.fingerprintOk).toBe(true);
    expect(payload.entries.length).toBe(2);
    const modified = payload.entries.find((e) => e.status === "modified");
    const added = payload.entries.find((e) => e.status === "added");
    expect(modified?.relPath).toBe("CLAUDE.md");
    expect(added?.relPath).toBe("extra.md");
    expect(modified?.provenance.length).toBeGreaterThan(0);
    expect(JSON.parse(JSON.stringify(payload))).toEqual(payload);
    expect(cap.stderr()).toBe("");
  });

  it("no active profile: prints (no active profile) and exits 0", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    const code = await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("no active profile");
  });

  it("--pre-commit-warn delegates to E4 fail-open path (always exit 0)", async () => {
    fx = await setupActive();
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");

    const cap = captureOutput(false);
    const code = await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: true,
      verbose: false,
    });
    expect(code).toBe(0);
  });

  it("--verbose: human summary includes scan stats", async () => {
    fx = await setupActive();
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");

    const cap = captureOutput(false);
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: true,
    });
    expect(cap.stdout()).toMatch(/scanned \d+, fast=\d+, slow=\d+/);
  });

  it("default (non-verbose): human summary omits scan stats", async () => {
    fx = await setupActive();
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");

    const cap = captureOutput(false);
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    expect(cap.stdout()).not.toMatch(/scanned/);
    expect(cap.stdout()).not.toMatch(/fast=/);
  });
});
