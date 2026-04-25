/**
 * Output channel abstraction. Centralises stdout/stderr writes so:
 *   - `--json` mode silences ALL human-readable output (epic invariant)
 *   - JSON payloads always go to stdout, errors always to stderr
 *   - Tests can inject a buffer-backed channel for snapshot assertions
 *
 * Writers MUST go through this module — no `process.stdout.write` calls
 * outside src/cli/ are allowed (the bin entry is the only exception).
 */

import * as process from "node:process";

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
      writeSafe(out, JSON.stringify(payload) + "\n");
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
