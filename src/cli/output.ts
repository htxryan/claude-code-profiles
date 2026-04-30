/**
 * Output channel abstraction. Centralises stdout/stderr writes so:
 *   - `--json` mode silences ALL human-readable output (epic invariant)
 *   - JSON payloads always go to stdout, errors always to stderr
 *   - Tests can inject a buffer-backed channel for snapshot assertions
 *
 * Writers shall go through this module — no `process.stdout.write` calls
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
  /**
   * Print a transient progress hint to stderr. Silenced under --json and
   * --quiet for the same reason print/warn are: a script asking for the exit
   * code or a structured payload doesn't want chatter. Goes to stderr so a
   * user piping `c3p use foo > out.txt` still sees the phase
   * lines on the terminal while stdout stays clean.
   */
  phase(text: string): void;
  /** True iff in --json mode (some commands need to know). */
  readonly jsonMode: boolean;
  /**
   * True iff stdout is a TTY (drives colour/unicode decisions in commands).
   * Threaded through OutputChannel so command handlers don't read
   * `process.stdout.isTTY` directly — that lets tests pin TTY truth via
   * `createOutput({ isTty: true, … })` and assert the colour-rendered output
   * shape without spawning a real terminal.
   */
  readonly isTty: boolean;
}

export interface OutputChannelOptions {
  json: boolean;
  /**
   * When true, silence human print()/warn() while keeping json()/error()
   * intact. Mutually-exclusive with `json` at the parser level (azp); this
   * field exists independently because the parser has already enforced the
   * exclusion by the time we build the channel.
   */
  quiet?: boolean;
  /**
   * Override for the channel's TTY signal. When undefined, defaults to
   * `process.stdout.isTTY === true`. Tests pin an explicit boolean to assert
   * colour-on / colour-off rendering deterministically.
   */
  isTty?: boolean;
  /** Optional injection points for tests. Default to process streams. */
  stdout?: NodeJS.WritableStream;
  stderr?: NodeJS.WritableStream;
}

/**
 * Build the output channel. Production callers pass `{ json }` or
 * `{ quiet }`; tests inject `{ json, stdout: capturingWriter,
 * stderr: capturingWriter }` to assert.
 *
 * Silencing model (azp):
 *   - jsonMode silences print() AND warn() (epic invariant: --json output is
 *     STRICTLY one JSON object per command — no human noise).
 *   - quiet silences print() AND warn() (script-friendly: side-effect-only
 *     mode; the user is asking for the exit code, not chatter).
 *   - error() always writes regardless — errors must surface.
 *   - json() always writes regardless — structured payloads do not vanish in
 *     quiet mode, even though no command currently emits json() in -q paths.
 */
export function createOutput(opts: OutputChannelOptions): OutputChannel {
  const out = opts.stdout ?? process.stdout;
  const err = opts.stderr ?? process.stderr;
  const jsonMode = opts.json;
  const quiet = opts.quiet === true;
  const silenced = jsonMode || quiet;
  // When `isTty` is explicitly set in opts (tests), trust the override.
  // Otherwise read the real stdout's TTY signal — but only when stdout
  // wasn't injected: a sink-injecting test that forgets to pass isTty
  // should land on `false`, not the parent terminal's TTY-ness.
  const isTty =
    opts.isTty !== undefined
      ? opts.isTty
      : opts.stdout === undefined && Boolean((process.stdout as { isTTY?: boolean }).isTTY);

  return {
    jsonMode,
    isTty,
    print(text: string): void {
      if (silenced) return;
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
      if (silenced) return;
      writeSafe(err, text.endsWith("\n") ? text : text + "\n");
    },
    error(text: string): void {
      writeSafe(err, text.endsWith("\n") ? text : text + "\n");
    },
    phase(text: string): void {
      // Silenced exactly like print()/warn(): --json must emit one structured
      // payload (no chatter), --quiet wants only exit codes. Production
      // callers pre-format with `style.dim(...)` so the line is visually
      // muted under a TTY even when colour is off.
      if (silenced) return;
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
  /**
   * Colour a drift status word per the spec's at-a-glance contract:
   *   - "clean"          → green (matches the `ok` glyph palette)
   *   - "modified"       → yellow (live edits — actionable but not lost)
   *   - "added"          → yellow (new file the user is choosing about)
   *   - "deleted"        → red (bytes that are gone; persist saves them)
   *   - "unrecoverable"  → red (gate cannot resolve; needs `init` remediation)
   *
   * Returns the input text untouched when colour is off so the no-color
   * branch is byte-identical to the historical drift output.
   */
  driftStatus(
    status: "clean" | "modified" | "added" | "deleted" | "unrecoverable",
    text: string,
  ): string;
  /**
   * Colour a byte-count delta (`+45`, `-1024`, `~12`) by magnitude so a
   * "+45,000 bytes" change visually outranks a "+45" change at a glance.
   *   - <100 bytes        → dim    (subtle — typo-fix-class change)
   *   - 100..10240 bytes  → normal (default colour, no decoration)
   *   - >10240 bytes      → bold   (bright — multi-KB delta worth noticing)
   *
   * Returns the raw text under no-color so JSON-equivalence tests stay easy
   * to write and the no-color shape stays byte-identical.
   */
  byteDelta(text: string, magnitude: number): string;
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
   * Production callers go through `resolveNoColor(opts.noColor === true)` so
   * the `--no-color` flag and the env var are combined identically at every
   * call site. Tests pin an explicit boolean for determinism.
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
    driftStatus(status, text): string {
      switch (status) {
        case "clean":
          return paint(ANSI.green, text);
        case "modified":
        case "added":
          return paint(ANSI.yellow, text);
        case "deleted":
        case "unrecoverable":
          return paint(ANSI.red, text);
      }
    },
    byteDelta(text: string, magnitude: number): string {
      // Thresholds picked to match the spec wording: <100 subtle, 100-10K
      // normal, >10K bright. Magnitude is always absolute — callers pass the
      // raw count already accumulated by upstream byte-counting logic.
      if (magnitude < 100) return paint(ANSI.dim, text);
      if (magnitude > 10240) return paint(ANSI.bold, text);
      return text;
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
