/**
 * Test-only OutputChannel sink. Captures stdout/stderr lines so command tests
 * can assert on shape without spawning a subprocess.
 */

import { createOutput, type OutputChannel } from "../../../src/cli/output.js";

class StringSink {
  buf = "";
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  write(chunk: any): boolean {
    this.buf += String(chunk);
    return true;
  }
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  end(): any {
    return this;
  }
}

export interface CapturedOutput {
  channel: OutputChannel;
  stdout(): string;
  stderr(): string;
  /** Parse stdout as one JSON object per line; returns the array. */
  jsonLines(): unknown[];
}

export interface CaptureOptions {
  /**
   * Override the channel's TTY signal (3yy). Defaults to `false` so existing
   * tests that captured output via the sink see the no-colour rendering.
   * Snapshot tests that want to assert the TTY-mode shape pass `isTty: true`.
   */
  isTty?: boolean;
}

export function captureOutput(json: boolean, opts: CaptureOptions = {}): CapturedOutput {
  const out = new StringSink();
  const err = new StringSink();
  const channel = createOutput({
    json,
    isTty: opts.isTty ?? false,
    stdout: out as unknown as NodeJS.WritableStream,
    stderr: err as unknown as NodeJS.WritableStream,
  });
  return {
    channel,
    stdout: () => out.buf,
    stderr: () => err.buf,
    jsonLines: () => out.buf.trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)),
  };
}
