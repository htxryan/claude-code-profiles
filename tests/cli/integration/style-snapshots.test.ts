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

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { runDrift } from "../../../src/cli/commands/drift.js";
import { runHook } from "../../../src/cli/commands/hook.js";
import { runList } from "../../../src/cli/commands/list.js";
import { runNew } from "../../../src/cli/commands/new.js";
import { runStatus } from "../../../src/cli/commands/status.js";
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

/**
 * Style mirror that follows the *host* platform — for assertions that compare
 * against production-command output (runStatus/runDrift/etc.), where the CLI
 * code uses `process.platform` to pick glyphs. On Windows this falls back to
 * ASCII (`[ok]` instead of `✓`) per the documented invariant in output.ts.
 */
const RUNTIME_TTY_STYLE = createStyle({
  isTty: true,
  platform: process.platform,
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
// list — TTY golden + active-profile bold
// ──────────────────────────────────────────────────────────────────────
describe("list — style snapshot (3yy)", () => {
  it("non-TTY: active profile rendered with `* ` prefix and plain name", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false);
    await runList({ cwd: fx.projectRoot, output: cap.channel });
    const lines = cap.stdout().trimEnd().split("\n");
    // Active row leads with `*`; inactive row leads with a space-padded prefix.
    expect(lines[0]).toMatch(/^\* a\b/);
    expect(lines[1]).toMatch(/^ {2}b\b/);
    // No ANSI escapes anywhere.
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
  });

  it("TTY: active profile name wrapped in bold ANSI escapes", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false, { isTty: true });
    await runList({ cwd: fx.projectRoot, output: cap.channel });
    // The active row contains `\x1b[1ma\x1b[0m` — bold + reset around the name.
    expect(cap.stdout()).toMatch(/\* \x1b\[1ma\x1b\[0m/);
    // The inactive row contains plain "b" (no bold) — assert the row is
    // present and unboldened by checking neighbour bytes.
    expect(cap.stdout()).toContain("  b");
  });

  it("--no-color collapses TTY output to plain ASCII (no escapes anywhere)", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false, { isTty: true });
    await runList({ cwd: fx.projectRoot, output: cap.channel, noColor: true });
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
    expect(cap.stdout().split("\n")[0]).toMatch(/^\* a\b/);
  });
});

// ──────────────────────────────────────────────────────────────────────
// status — clean drift uses ok glyph
// ──────────────────────────────────────────────────────────────────────
describe("status — style snapshot (3yy)", () => {
  it("non-TTY clean: `[ok] drift: clean`", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false);
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    expect(cap.stdout()).toContain("[ok] drift: clean");
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
  });

  it("TTY clean: green ✓ + `drift: clean`", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false, { isTty: true });
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    expect(cap.stdout()).toContain(RUNTIME_TTY_STYLE.ok("drift: clean"));
  });
});

// ──────────────────────────────────────────────────────────────────────
// drift — status word colour, dim provenance, byte intensity
// ──────────────────────────────────────────────────────────────────────
describe("drift — style snapshot (3yy)", () => {
  it("clean: `[ok] drift: clean` (non-TTY) and green ✓ (TTY)", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    // Non-TTY first.
    let cap = captureOutput(false);
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    expect(cap.stdout()).toContain("[ok] drift: clean");
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
    // TTY shape: green-painted glyph wraps the label. Glyph itself is
    // platform-dependent (✓ on POSIX, [ok] on Windows) so build the expected
    // string from a host-pinned style instead of hardcoding the unicode form.
    cap = captureOutput(false, { isTty: true });
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    expect(cap.stdout()).toContain(RUNTIME_TTY_STYLE.ok("drift: clean"));
  });

  it("modified entry: status word coloured yellow under TTY; dim `(from: …)` provenance", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");
    const cap = captureOutput(false, { isTty: true });
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    const out = cap.stdout();
    // Status word is yellow (33) and padded to 13 chars before the relPath.
    expect(out).toMatch(/\x1b\[33mmodified {5}\x1b\[0m CLAUDE\.md/);
    // Provenance is wrapped in dim escapes.
    expect(out).toMatch(/\x1b\[2m\(from: a\)\x1b\[0m/);
  });

  it("non-TTY: identical text plus column padding (no escapes); --no-color matches", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");
    const cap = captureOutput(false);
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    const out = cap.stdout();
    expect(out).not.toMatch(/\x1b\[/);
    // Column-padded raw status word still aligns.
    expect(out).toMatch(/  modified {5} CLAUDE\.md  \(from: a\)/);
    // --no-color over a TTY-pinned channel produces byte-identical output.
    const capNoColor = captureOutput(false, { isTty: true });
    await runDrift({
      cwd: fx.projectRoot,
      output: capNoColor.channel,
      preCommitWarn: false,
      verbose: false,
      noColor: true,
    });
    expect(capNoColor.stdout()).toBe(out);
  });

  it("byte-count intensity: small modification dims +/-/~ segments under TTY", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const paths = buildStatePaths(fx.projectRoot);
    // Add a small delta so addedBytes/changedBytes both land under 100.
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDITED\n");
    const cap = captureOutput(false, { isTty: true });
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    // Small magnitudes get the dim escape; the exact numbers aren't important
    // for the test — just that the dim wrapping fires for sub-100 deltas.
    expect(cap.stdout()).toMatch(/\x1b\[2m\+\d+\x1b\[0m/);
    expect(cap.stdout()).toMatch(/\x1b\[2m~\d+\x1b\[0m/);
  });
});

// ──────────────────────────────────────────────────────────────────────
// phase() — progress hints emitted on stderr (3yy)
// ──────────────────────────────────────────────────────────────────────
describe("phase progress — use/sync/validate (3yy)", () => {
  it("use emits resolving/merging/materializing on stderr in human mode", async () => {
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
    const stderr = cap.stderr();
    // All three phases fire; order matters (resolve → merge → materialize).
    const idxResolve = stderr.indexOf("resolving profile");
    const idxMerge = stderr.indexOf("merging files");
    const idxMaterial = stderr.indexOf("materializing");
    expect(idxResolve).toBeGreaterThanOrEqual(0);
    expect(idxMerge).toBeGreaterThan(idxResolve);
    expect(idxMaterial).toBeGreaterThan(idxMerge);
    // stdout still carries the success line — phase hints don't crowd it.
    expect(cap.stdout()).toBe("[ok] Switched to b.\n");
  });

  it("use under --json emits NO phase hints (channel silences phase())", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(true);
    await runUse({
      cwd: fx.projectRoot,
      output: cap.channel,
      profile: "b",
      mode: "non-interactive",
      onDriftFlag: null,
      signalHandlers: false,
    });
    expect(cap.stderr()).toBe("");
  });

  it("validate emits per-profile phase hints on stderr", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
        b: { manifest: { name: "b" }, files: { "y.md": "B\n" } },
      },
    });
    const cap = captureOutput(false);
    await runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null });
    expect(cap.stderr()).toContain("validating a");
    expect(cap.stderr()).toContain("validating b");
  });

  it("use under --quiet emits NO phase hints (channel silences phase())", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    // captureOutput doesn't take a quiet flag yet — build the channel inline
    // to pin quiet=true while still injecting sinks.
    const { createOutput } = await import("../../../src/cli/output.js");
    class StringSink {
      buf = "";
      write(c: unknown): boolean {
        this.buf += String(c);
        return true;
      }
      end(): unknown {
        return this;
      }
    }
    const out = new StringSink();
    const err = new StringSink();
    const ch = createOutput({
      json: false,
      quiet: true,
      isTty: false,
      stdout: out as unknown as NodeJS.WritableStream,
      stderr: err as unknown as NodeJS.WritableStream,
    });
    await runUse({
      cwd: fx.projectRoot,
      output: ch,
      profile: "b",
      mode: "non-interactive",
      onDriftFlag: null,
      signalHandlers: false,
    });
    // No phase hints, no success line — quiet silences both.
    expect(err.buf).toBe("");
    expect(out.buf).toBe("");
  });

  it("validate under --json emits NO phase hints", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
      },
    });
    const cap = captureOutput(true);
    await runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null });
    expect(cap.stderr()).toBe("");
  });
});

// ──────────────────────────────────────────────────────────────────────
// NO_COLOR env — zero ANSI escapes anywhere even when isTty is true (3yy)
// ──────────────────────────────────────────────────────────────────────
describe("NO_COLOR env — zero ANSI escapes (3yy AC-4)", () => {
  let savedNoColor: string | undefined;
  beforeEach(() => {
    savedNoColor = process.env["NO_COLOR"];
    process.env["NO_COLOR"] = "1";
  });
  afterEach(() => {
    if (savedNoColor === undefined) delete process.env["NO_COLOR"];
    else process.env["NO_COLOR"] = savedNoColor;
  });

  it("list with isTty=true + NO_COLOR=1: stdout has zero ANSI escapes", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false, { isTty: true });
    await runList({ cwd: fx.projectRoot, output: cap.channel });
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
    expect(cap.stderr()).not.toMatch(/\x1b\[/);
  });

  it("status with isTty=true + NO_COLOR=1: stdout has zero ANSI escapes", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const cap = captureOutput(false, { isTty: true });
    await runStatus({ cwd: fx.projectRoot, output: cap.channel });
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
    expect(cap.stderr()).not.toMatch(/\x1b\[/);
  });

  it("drift (modified) with isTty=true + NO_COLOR=1: zero ANSI escapes", async () => {
    fx = await setupTwoProfiles({ activate: "a" });
    const paths = buildStatePaths(fx.projectRoot);
    await fs.writeFile(path.join(paths.claudeDir, "CLAUDE.md"), "EDIT\n");
    const cap = captureOutput(false, { isTty: true });
    await runDrift({
      cwd: fx.projectRoot,
      output: cap.channel,
      preCommitWarn: false,
      verbose: false,
    });
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
    expect(cap.stderr()).not.toMatch(/\x1b\[/);
  });

  it("validate with isTty=true + NO_COLOR=1: zero ANSI escapes", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "A\n" } },
      },
    });
    const cap = captureOutput(false, { isTty: true });
    await runValidate({ cwd: fx.projectRoot, output: cap.channel, profile: null });
    expect(cap.stdout()).not.toMatch(/\x1b\[/);
    expect(cap.stderr()).not.toMatch(/\x1b\[/);
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
