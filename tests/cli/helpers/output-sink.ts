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

export function captureOutput(json: boolean): CapturedOutput {
  const out = new StringSink();
  const err = new StringSink();
  const channel = createOutput({
    json,
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
