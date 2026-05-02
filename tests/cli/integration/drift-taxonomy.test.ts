/**
 * Gap closure #8 (PR6 #8, F2 epic claude-code-profiles-yhb):
 *
 * Drift type taxonomy at the boundary — every combination of the documented
 * DriftStatus values must surface correctly in --json AND human output:
 *
 *   - "modified"      : live bytes differ from recorded fingerprint
 *   - "added"         : live file present, no fingerprint entry
 *   - "deleted"       : fingerprint entry present, no live file
 *   - "binary" (proxy): see note — there is no `binary` DriftStatus in the
 *     documented schema; the spec phrasing refers to the `--preview` rendering
 *     path treating non-text content separately. We exercise that surface
 *     here by drifting a binary file (NUL bytes) and asserting it surfaces as
 *     "modified" in both shapes without crashing the renderer.
 *   - "unrecoverable" : project-root CLAUDE.md with missing/malformed markers
 *
 * The status command's JSON shape is the load-bearing test (consumed by
 * scripts). The human surface is asserted as a minimum-set check (each status
 * label appears in the human output when the corresponding entry exists).
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

async function setupTwoFile() {
  const f = await makeFixture({
    profiles: {
      a: {
        manifest: { name: "a" },
        files: {
          "CLAUDE.md": "A\n",
          "settings.json": '{"v":"a"}',
        },
      },
    },
  });
  const planA = await resolve("a", { projectRoot: f.projectRoot });
  const m = await merge(planA);
  await materialize(buildStatePaths(f.projectRoot), planA, m);
  return f;
}

describe("gap closure #8: drift type taxonomy at the boundary (PR6 #8)", () => {
  it("modified entry surfaces as 'modified' in both --json and human", async () => {
    await ensureBuilt();
    fx = await setupTwoFile();
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "EDITED\n");

    const json = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "drift"],
    });
    expect(json.exitCode).toBe(0);
    const p = JSON.parse(json.stdout);
    expect(p.entries.some((e: { status: string }) => e.status === "modified")).toBe(true);

    const human = await runCli({ args: ["--cwd", fx.projectRoot, "drift"] });
    expect(human.exitCode).toBe(0);
    expect(human.stdout).toContain("modified");
  });

  it("added entry surfaces as 'added' in both --json and human", async () => {
    await ensureBuilt();
    fx = await setupTwoFile();
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "fresh.md"), "NEW\n");

    const json = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "drift"],
    });
    expect(json.exitCode).toBe(0);
    const p = JSON.parse(json.stdout);
    expect(p.entries.some((e: { status: string }) => e.status === "added")).toBe(true);

    const human = await runCli({ args: ["--cwd", fx.projectRoot, "drift"] });
    expect(human.stdout).toContain("added");
  });

  it("deleted entry surfaces as 'deleted' in both --json and human", async () => {
    await ensureBuilt();
    fx = await setupTwoFile();
    await fs.rm(path.join(fx.projectRoot, ".claude", "CLAUDE.md"));

    const json = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "drift"],
    });
    expect(json.exitCode).toBe(0);
    const p = JSON.parse(json.stdout);
    expect(p.entries.some((e: { status: string }) => e.status === "deleted")).toBe(true);

    const human = await runCli({ args: ["--cwd", fx.projectRoot, "drift"] });
    expect(human.stdout).toContain("deleted");
  });

  it("binary file modification surfaces as 'modified' (does not crash --preview)", async () => {
    // The spec mentions "binary" in the gap-closure description; the schema
    // has no separate binary status. The contract pinned here is: a binary
    // (NUL-byte-bearing) modified file MUST surface as "modified" without
    // crashing the diff/preview renderer.
    await ensureBuilt();
    const f = await makeFixture({
      profiles: {
        a: {
          manifest: { name: "a" },
          files: { "blob.bin": "ORIGINAL_BYTES\n" },
        },
      },
    });
    fx = f;
    const planA = await resolve("a", { projectRoot: f.projectRoot });
    const m = await merge(planA);
    await materialize(buildStatePaths(f.projectRoot), planA, m);
    // Replace with a NUL-bearing buffer (binary content).
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "blob.bin"),
      Buffer.from([0x00, 0x01, 0x02, 0xff, 0x00, 0xfe]),
    );

    const json = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "drift", "--preview"],
    });
    expect(json.exitCode).toBe(0);
    const p = JSON.parse(json.stdout);
    expect(p.entries.some((e: { status: string }) => e.status === "modified")).toBe(true);

    // --preview must not throw on the binary path.
    const human = await runCli({
      args: ["--cwd", fx.projectRoot, "drift", "--preview"],
    });
    expect(human.exitCode).toBe(0);
  });

  it("unrecoverable entry surfaces as 'unrecoverable' in both --json and human (cw6/T5)", async () => {
    // Project-root CLAUDE.md with managed-block markers wiped out is
    // exercise of the only path that yields DriftStatus="unrecoverable".
    //
    // Setup steps:
    //   1. init in an empty fixture — creates `.claude-profiles/` plus the
    //      project-root CLAUDE.md WITH the canonical managed-block markers
    //   2. write profile `a` directly (manifest + rootFiles) — bypasses
    //      makeFixture so the init pre-condition (no `.claude-profiles/`)
    //      holds
    //   3. `use a` — materializes; live tree + project-root CLAUDE.md are
    //      now under management
    //   4. wipe the markers from the live project-root CLAUDE.md
    //   5. drift / drift --json — must surface a single unrecoverable entry
    await ensureBuilt();
    fx = await makeFixture({});
    const init = await runCli({
      args: ["--cwd", fx.projectRoot, "init", "--no-hook", "--no-seed"],
    });
    expect(init.exitCode).toBe(0);

    // Create profile `a` post-init so init's "refusing to overwrite" guard
    // doesn't fire (it triggers when `.claude-profiles/` already exists).
    const profileADir = path.join(fx.projectRoot, ".claude-profiles", "a");
    const profileAClaude = path.join(profileADir, ".claude");
    await fs.mkdir(profileAClaude, { recursive: true });
    await fs.writeFile(
      path.join(profileADir, "profile.json"),
      JSON.stringify({ name: "a" }),
    );
    await fs.writeFile(path.join(profileAClaude, "x.md"), "x\n");
    // rootFiles: profile-root CLAUDE.md for cw6 destination='projectRoot'.
    await fs.writeFile(path.join(profileADir, "CLAUDE.md"), "ROOT\n");

    const use = await runCli({ args: ["--cwd", fx.projectRoot, "use", "a"] });
    expect(use.exitCode).toBe(0);

    // Now wipe the markers in the live project-root CLAUDE.md.
    const rootClaude = path.join(fx.projectRoot, "CLAUDE.md");
    await fs.writeFile(rootClaude, "no markers here\n");

    const json = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "drift"],
    });
    // drift command exits 0 even with unrecoverable entries — the gate is
    // surfaced by use/sync, not by the read-only drift command.
    expect(json.exitCode).toBe(0);
    const p = JSON.parse(json.stdout);
    const hasUnrecoverable = p.entries.some(
      (e: { status: string }) => e.status === "unrecoverable",
    );
    expect(hasUnrecoverable).toBe(true);

    const human = await runCli({ args: ["--cwd", fx.projectRoot, "drift"] });
    expect(human.stdout).toContain("unrecoverable");
  });

  it("--json output keys are deterministic for a stable input (PR3 reflex test)", async () => {
    // Pin the --json byte shape so the Go `--json` byte-equality tests have
    // a stable target. We don't snapshot the full bytes (timestamps and
    // hashes vary), but we DO assert the top-level key set + per-entry key
    // set so the Go translator's deterministic-marshalling target is fixed.
    await ensureBuilt();
    fx = await setupTwoFile();
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "CLAUDE.md"), "EDITED\n");
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "extra.md"), "X\n");

    const json = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "drift"],
    });
    expect(json.exitCode).toBe(0);
    const p = JSON.parse(json.stdout);
    // Top-level keys (subset). Adding new keys is backwards-compatible.
    expect(p).toHaveProperty("schemaVersion");
    expect(p).toHaveProperty("entries");
    expect(p).toHaveProperty("scannedFiles");
    // Each entry has the documented per-status shape.
    for (const e of p.entries as Array<{ relPath: string; status: string }>) {
      expect(typeof e.relPath).toBe("string");
      expect(["modified", "added", "deleted", "unrecoverable"]).toContain(e.status);
    }
  });
});
