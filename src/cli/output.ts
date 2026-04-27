/**
 * Output channel abstraction. Centralises stdout/stderr writes so:
 *   - `--json` mode silences ALL human-readable output (epic invariant)
 *   - JSON payloads always go to stdout, errors always to stderr
 *   - Tests can inject a buffer-backed channel for snapshot assertions
 *
 * Writers MUST go through this module — no `process.stdout.write` calls
 * outside src/cli/ are allowed (the bin entry is the only exception).
 */

import process from "node:process";

export interface OutputChannel {
  /** Print human-readable text to stdout. No-op in --json mode. */
  print(text: string): void;
  /** Print a single JSON-serializable payload to stdout. Always prints. */
  json(payload: unknown): void;
  /** Print human-readable warning to stderr. No-op in --json mode. */
  warn(text: string): void;
  /** Print error to stderr (always — even in --json mode). */
  error(text: string): void;
  /** True iff in --json mode (some commands need to know). */
  readonly jsonMode: boolean;
}

export interface OutputChannelOptions {
  json: boolean;
  /** Optional injection points for tests. Default to process streams. */
  stdout?: NodeJS.WritableStream;
  stderr?: NodeJS.WritableStream;
}

/**
 * Build the output channel. Production callers pass `{ json }`; tests inject
 * `{ json, stdout: capturingWriter, stderr: capturingWriter }` to assert.
 */
export function createOutput(opts: OutputChannelOptions): OutputChannel {
  const out = opts.stdout ?? process.stdout;
  const err = opts.stderr ?? process.stderr;
  const jsonMode = opts.json;

  return {
    jsonMode,
    print(text: string): void {
      if (jsonMode) return;
      writeSafe(out, text.endsWith("\n") ? text : text + "\n");
    },
    json(payload: unknown): void {
      // JSON output is canonical newline-terminated single-object-per-line so
      // downstream pipelines can `| jq -s 'add'` or read line-by-line.
      //
      // JSON.stringify can throw on BigInt or circular structures. Command
      // payloads are reviewed to be JSON-serializable, but a defensive
      // try/catch keeps a programmer-error throw from escaping through the
      // output channel and surfacing as the bin's exit-2 "internal error".
      let line: string;
      try {
        line = JSON.stringify(payload) + "\n";
      } catch (err) {
        line =
          JSON.stringify({
            error: "json-serialize-failed",
            detail: err instanceof Error ? err.message : String(err),
          }) + "\n";
      }
      writeSafe(out, line);
    },
    warn(text: string): void {
      if (jsonMode) return;
      writeSafe(err, text.endsWith("\n") ? text : text + "\n");
    },
    error(text: string): void {
      writeSafe(err, text.endsWith("\n") ? text : text + "\n");
    },
  };
}

/**
 * EPIPE-safe write. The CLI may be piped into `head`/`grep` which closes the
 * downstream side after a few lines; an EPIPE thrown synchronously must not
 * become an unhandled exception. Lesson Lca59f599 (E4 hook EPIPE handling)
 * applies here too — we own *all* writes so the safety belt is here, once.
 */
function writeSafe(stream: NodeJS.WritableStream, text: string): void {
  try {
    stream.write(text);
  } catch {
    // EPIPE / EBADF / write-after-close — abandon further output silently.
    // Better to lose the message than crash the CLI on a broken pipe.
  }
}

/**
 * Visual style helpers for human-readable CLI output (claude-code-profiles-pnf).
 *
 * All decisions about color and unicode are centralised here so individual
 * commands can ask `style.ok("...")` without each one re-checking TTY-ness.
 *
 * Rules:
 * - `process.env.NO_COLOR` (any value, even empty string per https://no-color.org/)
 *   disables colour. NO_COLOR also forces ASCII glyphs because the most
 *   common reason to set it is logging to a system that mangles them.
 * - Non-TTY stdout disables colour (CI logs, redirection, pipes).
 * - Windows defaults to ASCII glyphs even with colour on, because Windows
 *   terminals historically don't render the box-drawing/check glyphs we use.
 *
 * No external dependencies: raw ANSI escapes only.
 */

const ANSI = {
  reset: "\x1b[0m",
  bold: "\x1b[1m",
  dim: "\x1b[2m",
  green: "\x1b[32m",
  yellow: "\x1b[33m",
  red: "\x1b[31m",
  cyan: "\x1b[36m",
} as const;

export interface Style {
  /** True iff colour escapes will be emitted. */
  readonly color: boolean;
  /** True iff unicode glyphs (✓, ⊙, ╭, ─) are safe to emit. */
  readonly unicode: boolean;

  /** Status glyph for a successful step. */
  ok(text: string): string;
  /** Status glyph for a skipped/no-op step. */
  skip(text: string): string;
  /** Status glyph for a warning step. */
  warn(text: string): string;
  /** Status glyph for a hard failure (red). Used by validate FAIL rows. */
  fail(text: string): string;
  /** Render a one-line banner header with the given title. */
  banner(title: string): string;
  /** Dim secondary text (e.g. paths, counts). */
  dim(text: string): string;
  /** Bold text (no glyph; for inline emphasis like the active profile name). */
  bold(text: string): string;
}

export interface StyleOptions {
  /**
   * Whether the destination supports colour. Production callers pass
   * `process.stdout.isTTY`; tests pass an explicit boolean.
   */
  isTty: boolean;
  /**
   * Platform string (defaults to `process.platform`). Pulled out so tests
   * can pin a specific OS without monkey-patching globals.
   */
  platform?: NodeJS.Platform;
  /**
   * Pre-resolved no-colour decision. When `true`, colour and unicode are
   * forced off (matches NO_COLOR env semantics from https://no-color.org/).
   * When `undefined` or `false`, colour is gated on `isTty + platform` only.
   *
   * Production callers should pass
   *   `global.noColor || process.env.NO_COLOR !== undefined`
   * so the `--no-color` flag and the env var are equivalent. Tests pin an
   * explicit boolean for determinism.
   */
  noColor?: boolean;
}

/**
 * Build a Style helper. Pure — every decision is derived from the inputs, no
 * hidden module-level state. That keeps `--json` callers reproducible and
 * tests deterministic regardless of which terminal vitest happens to launch in.
 */
export function createStyle(opts: StyleOptions): Style {
  const platform = opts.platform ?? process.platform;
  const noColor = opts.noColor === true;
  const color = opts.isTty && !noColor;
  // Windows historically chokes on box-drawing + check glyphs in cmd.exe; we
  // also gate unicode on NO_COLOR because users frequently set NO_COLOR in
  // log capture pipelines that strip non-ASCII bytes too.
  const unicode = color && platform !== "win32";

  function paint(code: string, text: string): string {
    return color ? `${code}${text}${ANSI.reset}` : text;
  }

  return {
    color,
    unicode,
    ok(text: string): string {
      const glyph = unicode ? "✓" : "[ok]";
      return `${paint(ANSI.green, glyph)} ${text}`;
    },
    skip(text: string): string {
      const glyph = unicode ? "⊙" : "[skip]";
      return `${paint(ANSI.dim, glyph)} ${paint(ANSI.dim, text)}`;
    },
    warn(text: string): string {
      const glyph = unicode ? "!" : "[warn]";
      return `${paint(ANSI.yellow, glyph)} ${text}`;
    },
    fail(text: string): string {
      const glyph = unicode ? "✗" : "[x]";
      return `${paint(ANSI.red, glyph)} ${text}`;
    },
    banner(title: string): string {
      if (!unicode) return `== ${title} ==`;
      return paint(ANSI.cyan, `╭ ${title} ─`);
    },
    dim(text: string): string {
      return paint(ANSI.dim, text);
    },
    bold(text: string): string {
      return paint(ANSI.bold, text);
    },
  };
}

/**
 * Resolve the effective "disable colour" decision from the CLI's two inputs:
 * the `--no-color` flag (parsed into `globalNoColor`) and the `NO_COLOR` env
 * var. Either being set disables colour. Pulled out so every command site
 * combines the two inputs identically.
 *
 * Pass `env` explicitly in tests to avoid leaking the host environment into
 * the assertion; production callers default to `process.env`.
 */
export function resolveNoColor(
  globalNoColor: boolean,
  env: NodeJS.ProcessEnv = process.env,
): boolean {
  return globalNoColor || env["NO_COLOR"] !== undefined;
}
