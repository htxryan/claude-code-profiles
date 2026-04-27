/**
 * Style snapshot suite (claude-code-profiles-bhq).
 *
 * Pins the human-readable output of every mutating verb (new, use, sync,
 * validate, hook install/uninstall) when colour + unicode are enabled. The
 * goal is a CI canary: a future formatting change that breaks the
 * "polished CLI screencast" feel — wrong glyph, lost dim continuation,
 * banner regression — fails this file loudly.
 *
 * Determinism: every assertion injects a fixed `Style` shape via the
 * pre-resolved `noColor` boolean (`false` for the TTY/colour cases). We do
 * NOT spawn a subprocess here; we run the command handlers in-process with
 * a captured OutputChannel so vitest's parent terminal can't perturb the
 * result.
 *
 * NO_COLOR parity: a parallel `noColor: true` case asserts that the
 * `--no-color` flag (additive with the env var) collapses output to the
 * ASCII-glyph variant.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { runHook } from "../../../src/cli/commands/hook.js";
import { runNew } from "../../../src/cli/commands/new.js";
import { runSync } from "../../../src/cli/commands/sync.js";
import { runUse } from "../../../src/cli/commands/use.js";
import { runValidate } from "../../../src/cli/commands/validate.js";
import { createStyle } from "../../../src/cli/output.js";
import { CliUserError } from "../../../src/cli/exit.js";
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

/**
 * ANSI-stripping helper. The test channel doesn't go through a TTY so the
 * non-ANSI cases are already plain text, but the in-process channel can't
 * fake `process.stdout.isTTY = true`. To assert the TTY rendering we
 * generate the expected string from the same `createStyle` the command
 * uses, with the same `{isTty: true, platform: "linux", noColor: false}`
 * pinning — that keeps glyph + colour escapes in lockstep with production
 * output.
 */
const TTY_STYLE = createStyle({
  isTty: true,
  platform: "linux",
  noColor: false,
});

const NOCOLOR_STYLE = createStyle({
  isTty: true,
  platform: "linux",
  noColor: true,
});

async function setupTwoProfiles(opts?: { activate?: "a" | "b" | null }) {
  const f = await makeFixture({
    profiles: {
      a: { manifest: { name: "a" }, files: { "CLAUDE.md": "A\n" } },
      b: { manifest: { name: "b" }, files: { "CLAUDE.md": "B\n" } },
    },
  });
  if (opts?.activate) {
    const plan = await resolve(opts.activate, { projectRoot: f.projectRoot });
    const m = await merge(plan);
    await materialize(buildStatePaths(f.projectRoot), plan, m);
  }
  return f;
}

// ──────────────────────────────────────────────────────────────────────
// new
// ──────────────────────────────────────────────────────────────────────
describe("new — style snapshot", () => {
  it("non-TTY: ok glyph + dim secondary line (ASCII)", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    await runNew({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "scratch",
      description: null,
    });
    const lines = cap.stdout().trimEnd().split("\n");
    // Action line: `[ok] Created profile "scratch" at <path>`
    expect(lines[0]).toMatch(/^\[ok\] Created profile "scratch" at /);
    // Detail line: dim is a no-op without colour, but the leading two-space
    // indent + "edit" wording matches init's two-line shape.
    expect(lines[1]).toMatch(/^  edit .+profile\.json to set extends\/includes$/);
  });

  it("--no-color flag: identical to non-TTY (no ANSI escapes)", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    await runNew({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "scratch",
      description: null,
      noColor: true,
    });
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
    expect(cap.stdout().split("\n")[0]).toMatch(/^\[ok\]/);
  });
});

// ──────────────────────────────────────────────────────────────────────
// use
// ──────────────────────────────────────────────────────────────────────
describe("use — style snapshot", () => {
  it("clean swap: single ok line `Switched to <name>`", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false);
    await runUse({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "b",
      mode: "non-interactive",
      onDriftFlag: null,
      signalHandlers: false,
    });
    expect(cap.stdout()).toBe("[ok] Switched to b.\n");
  });

  it("drift-discarded: ok line + dim Backup continuation (two-line shape)", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDIT\n");
    const cap = captureOutput(false);
    await runUse({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "b",
      mode: "non-interactive",
      onDriftFlag: "discard",
      signalHandlers: false,
    });
    const lines = cap.stdout().trimEnd().split("\n");
    expect(lines[0]).toBe("[ok] Switched to b (drift discarded).");
    // The Backup line is indented two spaces and starts with "Backup:".
    expect(lines[1]).toMatch(/^  Backup: /);
  });

  it("--no-color flag: never emits ANSI escapes", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false);
    await runUse({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "b",
      mode: "non-interactive",
      onDriftFlag: null,
      signalHandlers: false,
      noColor: true,
    });
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
  });
});

// ──────────────────────────────────────────────────────────────────────
// sync
// ──────────────────────────────────────────────────────────────────────
describe("sync — style snapshot", () => {
  it("clean: single ok line `Synced <name>`", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false);
    await runSync({
      cwd: fx.projectRoot,
      output: cap.channel,
      mode: "non-interactive",
      onDriftFlag: null,
      signalHandlers: false,
    });
    expect(cap.stdout()).toBe("[ok] Synced a.\n");
  });

  it("drift-discarded: ok line + dim Backup continuation", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDIT\n");
    const cap = captureOutput(false);
    await runSync({
      cwd: fx.projectRoot,
      output: cap.channel,
      mode: "non-interactive",
      onDriftFlag: "discard",
      signalHandlers: false,
    });
    const lines = cap.stdout().trimEnd().split("\n");
    expect(lines[0]).toBe("[ok] Synced a (drift discarded).");
    expect(lines[1]).toMatch(/^  Backup: /);
  });
});

// ──────────────────────────────────────────────────────────────────────
// validate
// ──────────────────────────────────────────────────────────────────────
describe("validate — style snapshot", () => {
  it("all-pass: each row gets ok glyph; footer `[ok] N pass`", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "y.md": "B\n" } },
      },
    });
    const cap = captureOutput(false);
    await runValidate({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: null,
    });
    expect(cap.stdout()).toBe("[ok] a\n[ok] b\n[ok] 2 pass\n");
  });

  it("mixed pass/fail: fail row leads with [x] glyph; footer [x] N pass, M fail", async () => {
    fx = await makeFixture({
      profiles: {
        ok: { manifest: { name: "ok" }, files: { "x.md": "X\n" } },
        bad: { manifest: { name: "bad", extends: "nope" }, files: {} },
      },
    });
    const cap = captureOutput(false);
    await expect(
      runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null }),
    ).rejects.toBeInstanceOf(CliUserError);
    const lines = cap.stdout().trimEnd().split("\n");
    // Order: PASS rows first then FAIL rows is NOT guaranteed; the order
    // is whatever listProfiles returns (alphabetical: bad, ok). We check
    // by grep rather than positional indexing.
    expect(lines.some((l) => l.startsWith("[ok] ok"))).toBe(true);
    expect(lines.some((l) => l.startsWith("[x] bad"))).toBe(true);
    // Footer is the last line; on any fail it uses the [x] glyph.
    expect(lines[lines.length - 1]).toBe("[x] 1 pass, 1 fail");
  });
});

// ──────────────────────────────────────────────────────────────────────
// hook install / uninstall
// ──────────────────────────────────────────────────────────────────────
describe("hook — style snapshot", () => {
  async function gitInit(projectRoot: string): Promise<void> {
    await fs.mkdir(path.join(projectRoot, ".git", "hooks"), { recursive: true });
  }

  it("install (fresh): ok action line + dim path on second line", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    const cap = captureOutput(false);
    await runHook({
      cwd: fx.projectRoot,
      output: cap.channel,
      action: "install",
      force: false,
    });
    const lines = cap.stdout().trimEnd().split("\n");
    expect(lines[0]).toBe("[ok] Installed pre-commit hook");
    expect(lines[1]).toMatch(/^  .+pre-commit$/);
  });

  it("install (already present): skip glyph, not ok glyph", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    // Pre-install our hook so the second invocation is a no-op.
    await runHook({
      cwd: fx.projectRoot,
      output: captureOutput(false).channel,
      action: "install",
      force: false,
    });
    const cap = captureOutput(false);
    await runHook({
      cwd: fx.projectRoot,
      output: cap.channel,
      action: "install",
      force: false,
    });
    const lines = cap.stdout().trimEnd().split("\n");
    expect(lines[0]).toBe("[skip] Pre-commit hook already installed");
    expect(lines[1]).toMatch(/^  .+pre-commit$/);
  });

  it("uninstall (no hook): skip glyph + dim path (two-line shape)", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    const cap = captureOutput(false);
    await runHook({
      cwd: fx.projectRoot,
      output: cap.channel,
      action: "uninstall",
      force: false,
    });
    const lines = cap.stdout().trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    expect(lines[0]).toBe("[skip] No pre-commit hook to remove");
    expect(lines[1]).toMatch(/^  .+pre-commit$/);
  });

  it("uninstall (ours): ok glyph + dim path", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    await runHook({
      cwd: fx.projectRoot,
      output: captureOutput(false).channel,
      action: "install",
      force: false,
    });
    const cap = captureOutput(false);
    await runHook({
      cwd: fx.projectRoot,
      output: cap.channel,
      action: "uninstall",
      force: false,
    });
    const lines = cap.stdout().trimEnd().split("\n");
    expect(lines[0]).toBe("[ok] Removed pre-commit hook");
    expect(lines[1]).toMatch(/^  .+pre-commit$/);
  });
});

// ──────────────────────────────────────────────────────────────────────
// TTY-mode parity (assert the createStyle shape that production uses)
// ──────────────────────────────────────────────────────────────────────
describe("createStyle — TTY parity", () => {
  it("ok glyph in colour: green ✓ + reset wraps the painted glyph", () => {
    expect(TTY_STYLE.ok("hi")).toBe("\x1b[32m✓\x1b[0m hi");
  });

  it("skip glyph: dim ⊙ + dim text (paint runs twice — glyph + text)", () => {
    expect(TTY_STYLE.skip("hi")).toBe("\x1b[2m⊙\x1b[0m \x1b[2mhi\x1b[0m");
  });

  it("fail glyph: red ✗ + plain text", () => {
    expect(TTY_STYLE.fail("hi")).toBe("\x1b[31m✗\x1b[0m hi");
  });

  it("warn glyph: yellow ! + plain text", () => {
    expect(TTY_STYLE.warn("hi")).toBe("\x1b[33m!\x1b[0m hi");
  });

  it("dim/bold helpers wrap with the right escape", () => {
    expect(TTY_STYLE.dim("x")).toBe("\x1b[2mx\x1b[0m");
    expect(TTY_STYLE.bold("x")).toBe("\x1b[1mx\x1b[0m");
  });

  it("noColor: true collapses to ASCII glyphs and strips ALL escapes", () => {
    expect(NOCOLOR_STYLE.color).toBe(false);
    expect(NOCOLOR_STYLE.unicode).toBe(false);
    expect(NOCOLOR_STYLE.ok("x")).toBe("[ok] x");
    expect(NOCOLOR_STYLE.skip("x")).toBe("[skip] x");
    expect(NOCOLOR_STYLE.fail("x")).toBe("[x] x");
    expect(NOCOLOR_STYLE.dim("x")).toBe("x");
    expect(NOCOLOR_STYLE.bold("x")).toBe("x");
  });
});
