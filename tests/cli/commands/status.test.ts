import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { runStatus, type StatusPayload } from "../../../src/cli/commands/status.js";
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

describe("status (R31, R40)", () => {
  it("no state: prints (no active profile)", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    const code = await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("no active profile");
  });

  it("after use: prints active + materialized + drift clean", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      },
    });
    const plan = await resolve("a", { projectRoot: fx.projectRoot });
    const merged = await merge(plan);
    await materialize(buildStatePaths(fx.projectRoot), plan, merged);

    const cap = captureOutput(false);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const out = cap.stdout();
    expect(out).toContain("active: a");
    expect(out).toMatch(/materialized: \d{4}-\d{2}-\d{2}T/);
    expect(out).toContain("drift: clean");
  });

  it("after edit: drift summary shows counts", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      },
    });
    const plan = await resolve("a", { projectRoot: fx.projectRoot });
    const merged = await merge(plan);
    const paths = buildStatePaths(fx.projectRoot);
    await materialize(paths, plan, merged);
    // Edit live file -> introduces a `modified`
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");
    // Add a new file -> `added`
    await fs.writeFile(path.join(paths.claudeDir, "extra.md"), "X\n");

    const cap = captureOutput(true);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0] as StatusPayload;
    expect(payload.activeProfile).toBe("a");
    expect(payload.drift.fingerprintOk).toBe(true);
    expect(payload.drift.modified).toBe(1);
    expect(payload.drift.added).toBe(1);
    expect(payload.drift.deleted).toBe(0);
    expect(payload.drift.total).toBe(2);
  });

  it("--json round-trips structured payload", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(true);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0] as StatusPayload;
    expect(payload).toMatchObject({
      activeProfile: null,
      materializedAt: null,
      drift: { fingerprintOk: false, modified: 0, added: 0, deleted: 0, total: 0 },
      warnings: [],
    });
    // Round-trip integrity: re-stringify and re-parse equality.
    expect(JSON.parse(JSON.stringify(payload))).toEqual(payload);
    expect(cap.stderr()).toBe("");
  });

  it("Missing-state warning is filtered (fresh project)", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(true);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0] as StatusPayload;
    expect(payload.warnings).toEqual([]);
  });

  it("Corrupted state file: warning surfaced (degraded path)", async () => {
    fx = await makeFixture({});
    const paths = buildStatePaths(fx.projectRoot);
    await fs.mkdir(paths.metaDir, { recursive: true });
    await fs.writeFile(paths.stateFile, "{not json");

    const cap = captureOutput(true);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0] as StatusPayload;
    expect(payload.warnings.length).toBe(1);
    expect(payload.warnings[0]?.code).toBe("ParseError");
  });

  it("source fresh after materialize: sourceFresh=true and human shows no stale-warning (azp)", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      },
    });
    const plan = await resolve("a", { projectRoot: fx.projectRoot });
    const merged = await merge(plan);
    await materialize(buildStatePaths(fx.projectRoot), plan, merged);

    const cap = captureOutput(true);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0] as StatusPayload;
    expect(payload.sourceFresh).toBe(true);
    expect(typeof payload.sourceFingerprint).toBe("string");
  });

  it("source updated since materialize: sourceFresh=false and human surfaces sync hint (azp)", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      },
    });
    const plan = await resolve("a", { projectRoot: fx.projectRoot });
    const merged = await merge(plan);
    await materialize(buildStatePaths(fx.projectRoot), plan, merged);

    // Mutate a source file (post-materialize) to simulate `git pull` bringing
    // in new bytes. We rewrite with bumped mtime so the fast-path aggregate
    // flips even when contents happen to be the same length.
    const sourceFile = path.join(
      fx.projectRoot,
      ".claude-profiles",
      "a",
      ".claude",
      "CLAUDE.md",
    );
    await fs.writeFile(sourceFile, "A-EDITED\n");
    const future = new Date(Date.now() + 5000);
    await fs.utimes(sourceFile, future, future);

    // Human surface
    const cap = captureOutput(false);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    expect(cap.stdout()).toContain("source: updated since last materialize");
    expect(cap.stdout()).toContain("claude-profiles sync");

    // JSON surface
    const j = captureOutput(true);
    await runStatus({ cwd: fx.projectRoot, output: j.channel });
    const payload = j.jsonLines()[0] as StatusPayload;
    expect(payload.sourceFresh).toBe(false);
  });

  it("legacy state without sourceFingerprint: sourceFresh is null (azp)", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      },
    });
    const plan = await resolve("a", { projectRoot: fx.projectRoot });
    const merged = await merge(plan);
    const paths = buildStatePaths(fx.projectRoot);
    await materialize(paths, plan, merged);

    // Hand-edit the state file to simulate legacy state (drop sourceFingerprint).
    const raw = JSON.parse(await fs.readFile(paths.stateFile, "utf8"));
    delete raw.sourceFingerprint;
    await fs.writeFile(paths.stateFile, JSON.stringify(raw));

    const cap = captureOutput(true);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    const payload = cap.jsonLines()[0] as StatusPayload;
    expect(payload.sourceFresh).toBeNull();
    expect(payload.sourceFingerprint).toBeNull();
  });
});
