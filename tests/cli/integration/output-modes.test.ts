/**
 * Gap closure #10 (PR6 #10, F2 epic claude-code-profiles-yhb):
 *
 * Output-mode combinatorics: every cell of the matrix
 *
 *   { NO_COLOR env, --no-color flag, --quiet, --json } × { TTY, non-TTY }
 *
 * produces the documented output. The spawn harness always runs the bin
 * non-TTY (pipes), so the TTY=true cells are exercised via the
 * `FORCE_COLOR=1` env-var hint where applicable; the no-colour pathway is
 * the documented default for non-TTY anyway, so tests below validate:
 *
 *   - non-TTY default → no ANSI escapes (plain text)
 *   - --no-color flag forces no ANSI even if FORCE_COLOR is set
 *   - NO_COLOR env forces no ANSI (any value, including empty string per
 *     https://no-color.org)
 *   - --quiet silences print() and warn() but keeps error() and exit codes
 *   - --json silences print()/warn()/error()/progress; emits one JSON object
 *   - --json on a verb that has structured output is parseable
 *   - --json + error path emits a JSON error envelope (or empty stdout,
 *     depending on contract)
 *
 * The full 2×4×2 matrix is huge — we exercise representative cells per
 * dimension, asserting the property each flag controls (escapes / silence /
 * JSON-shape) without snapshotting full bytes.
 */

import { afterEach, describe, expect, it } from "vitest";

import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { ensureBuilt, runCli } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

const ANSI_ESC = /\x1b\[[0-9;]*m/;

describe("gap closure #10: output-mode combinatorics (PR6 #10)", () => {
  // ──────────────────────────────────────────────────────────────────────
  // Default (non-TTY) → no ANSI
  // ──────────────────────────────────────────────────────────────────────
  it("non-TTY default: no ANSI escapes in --version output", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["--version"] });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).not.toMatch(ANSI_ESC);
  });

  it("non-TTY default: no ANSI in `init` output", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "init", "--no-seed", "--no-hook"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).not.toMatch(ANSI_ESC);
  });

  // ──────────────────────────────────────────────────────────────────────
  // --no-color flag (additive with NO_COLOR env per parse.ts:127–130)
  // ──────────────────────────────────────────────────────────────────────
  it("--no-color flag: no ANSI even when FORCE_COLOR=1 is set", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--no-color", "init", "--no-seed", "--no-hook"],
      env: { FORCE_COLOR: "1" },
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).not.toMatch(ANSI_ESC);
  });

  // ──────────────────────────────────────────────────────────────────────
  // NO_COLOR env (per https://no-color.org — any value disables colour,
  // even an empty string)
  // ──────────────────────────────────────────────────────────────────────
  it("NO_COLOR env (any value) disables ANSI", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "init", "--no-seed", "--no-hook"],
      env: { NO_COLOR: "1", FORCE_COLOR: "1" },
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).not.toMatch(ANSI_ESC);
  });

  // Windows skip: empty-string env values are dropped or transformed by
  // the Win32 process-creation API (Node falls through to CreateProcessW,
  // which collapses `""` to "unset" in the child's block). The NO_COLOR
  // contract under empty-string is therefore unobservable on Windows.
  // Posix runners exercise it.
  it.skipIf(process.platform === "win32")(
    "NO_COLOR env empty string still disables ANSI (per no-color.org)",
    async () => {
      await ensureBuilt();
      fx = await makeFixture({});
      const r = await runCli({
        args: ["--cwd", fx.projectRoot, "init", "--no-seed", "--no-hook"],
        env: { NO_COLOR: "", FORCE_COLOR: "1" },
      });
      expect(r.exitCode).toBe(0);
      expect(r.stdout).not.toMatch(ANSI_ESC);
    },
  );

  // ──────────────────────────────────────────────────────────────────────
  // --quiet silences print() + warn(), preserves error() + exit codes
  // ──────────────────────────────────────────────────────────────────────
  it("--quiet: success path produces empty stdout, exit 0", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--quiet", "init", "--no-seed", "--no-hook"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("");
  });

  it("-q (short) silences stdout for an argless verb", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "-q", "list"],
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("");
  });

  it("--quiet preserves error() and exit codes (use missing profile → exit 1, stderr non-empty)", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--quiet", "use", "nonexistent"],
    });
    expect(r.exitCode).toBe(1);
    // Errors are NOT silenced under --quiet (the contract).
    expect(r.stderr.length).toBeGreaterThan(0);
  });

  // ──────────────────────────────────────────────────────────────────────
  // --json mode
  // ──────────────────────────────────────────────────────────────────────
  it("--json: status emits exactly one JSON object on stdout, parseable", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "status"],
    });
    expect(r.exitCode).toBe(0);
    // Must be one valid JSON object — not multiple, not concatenated.
    const parsed = JSON.parse(r.stdout);
    expect(typeof parsed).toBe("object");
    expect(parsed).not.toBeNull();
  });

  it("--json: list emits one JSON object on stdout", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: { a: { manifest: { name: "a" }, files: { "x.md": "x\n" } } },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "list"],
    });
    expect(r.exitCode).toBe(0);
    const parsed = JSON.parse(r.stdout);
    expect(parsed).toHaveProperty("profiles");
    expect(Array.isArray(parsed.profiles)).toBe(true);
  });

  it("--json: --version emits one JSON object on stdout", async () => {
    await ensureBuilt();
    const r = await runCli({ args: ["--json", "--version"] });
    expect(r.exitCode).toBe(0);
    // dispatch.ts emits a structured payload under --json (see comment).
    const parsed = JSON.parse(r.stdout);
    expect(parsed).toHaveProperty("version");
  });

  it("--json: error path keeps stdout JSON-clean (no human chatter)", async () => {
    // Under --json, error envelope (if any) goes to stderr — stdout MUST
    // remain a valid JSON object OR be empty so consumers parsing stdout
    // never see mixed content. We pin the weaker contract (empty-or-JSON)
    // because the TS impl currently emits empty stdout on the error path
    // and a stderr message, which is the de-facto target for Go.
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "use", "nonexistent"],
    });
    expect(r.exitCode).toBe(1);
    if (r.stdout !== "") {
      // If the impl chose to emit an error-envelope JSON object, it must
      // still be parseable.
      JSON.parse(r.stdout);
    }
    // Error message is on stderr.
    expect(r.stderr.length).toBeGreaterThan(0);
  });

  // ──────────────────────────────────────────────────────────────────────
  // Combinatoric edge: --json + NO_COLOR + --no-color stacking is harmless
  // ──────────────────────────────────────────────────────────────────────
  it("stacking --json + --no-color + NO_COLOR=1 still emits parseable JSON", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "--json", "--no-color", "status"],
      env: { NO_COLOR: "1" },
    });
    expect(r.exitCode).toBe(0);
    JSON.parse(r.stdout); // throws if invalid
  });

  it("--quiet + NO_COLOR + --no-color: success path is byte-empty", async () => {
    await ensureBuilt();
    fx = await makeFixture({});
    const r = await runCli({
      args: [
        "--cwd",
        fx.projectRoot,
        "--quiet",
        "--no-color",
        "init",
        "--no-seed",
        "--no-hook",
      ],
      env: { NO_COLOR: "1" },
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("");
  });
});
