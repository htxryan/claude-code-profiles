import { describe, expect, it } from "vitest";

import { createOutput } from "../../src/cli/output.js";

class StringSink {
  buf = "";
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  write(chunk: any): boolean {
    this.buf += String(chunk);
    return true;
  }
  // Other Writable methods that we never call but need to satisfy the type.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  end(): any {
    return this;
  }
}

function build(json: boolean): { ch: ReturnType<typeof createOutput>; out: StringSink; err: StringSink } {
  const out = new StringSink();
  const err = new StringSink();
  const ch = createOutput({
    json,
    stdout: out as unknown as NodeJS.WritableStream,
    stderr: err as unknown as NodeJS.WritableStream,
  });
  return { ch, out, err };
}

describe("OutputChannel — human mode", () => {
  it("print writes to stdout with trailing newline", () => {
    const { ch, out } = build(false);
    ch.print("hello");
    expect(out.buf).toBe("hello\n");
  });

  it("does not duplicate trailing newlines", () => {
    const { ch, out } = build(false);
    ch.print("a\n");
    expect(out.buf).toBe("a\n");
  });

  it("warn writes to stderr", () => {
    const { ch, out, err } = build(false);
    ch.warn("watch out");
    expect(err.buf).toBe("watch out\n");
    expect(out.buf).toBe("");
  });

  it("error writes to stderr (always)", () => {
    const { ch, out, err } = build(false);
    ch.error("boom");
    expect(err.buf).toBe("boom\n");
    expect(out.buf).toBe("");
  });

  it("json prints serialized payload to stdout even in human mode", () => {
    const { ch, out } = build(false);
    ch.json({ ok: true });
    expect(out.buf).toBe('{"ok":true}\n');
  });
});

describe("OutputChannel — JSON mode (epic invariant)", () => {
  it("print is silenced", () => {
    const { ch, out, err } = build(true);
    ch.print("hello");
    expect(out.buf).toBe("");
    expect(err.buf).toBe("");
  });

  it("warn is silenced", () => {
    const { ch, out, err } = build(true);
    ch.warn("hidden");
    expect(out.buf).toBe("");
    expect(err.buf).toBe("");
  });

  it("error still writes (errors must always surface)", () => {
    const { ch, err } = build(true);
    ch.error("real");
    expect(err.buf).toBe("real\n");
  });

  it("json writes the structured payload to stdout", () => {
    const { ch, out } = build(true);
    ch.json({ active: "minimal" });
    expect(out.buf).toBe('{"active":"minimal"}\n');
  });

  it("multiple json calls write one object per line", () => {
    const { ch, out } = build(true);
    ch.json({ a: 1 });
    ch.json({ a: 2 });
    expect(out.buf).toBe('{"a":1}\n{"a":2}\n');
    // Each line round-trips.
    const lines = out.buf.trim().split("\n").map((l) => JSON.parse(l));
    expect(lines).toEqual([{ a: 1 }, { a: 2 }]);
  });

  it("jsonMode flag is exposed for command code that needs to branch", () => {
    expect(build(true).ch.jsonMode).toBe(true);
    expect(build(false).ch.jsonMode).toBe(false);
  });
});

describe("OutputChannel — EPIPE-safety", () => {
  it("write that throws is swallowed", () => {
    const exploding = {
      write(): boolean {
        const err = new Error("EPIPE");
        (err as NodeJS.ErrnoException).code = "EPIPE";
        throw err;
      },
    } as unknown as NodeJS.WritableStream;
    const ch = createOutput({ json: false, stdout: exploding, stderr: exploding });
    // None of these should throw — broken pipe must not crash the CLI.
    expect(() => ch.print("x")).not.toThrow();
    expect(() => ch.error("y")).not.toThrow();
    expect(() => ch.json({ a: 1 })).not.toThrow();
  });
});
