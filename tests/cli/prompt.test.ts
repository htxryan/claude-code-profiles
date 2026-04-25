import { PassThrough } from "node:stream";

import { describe, expect, it } from "vitest";

import { makeReadlinePrompt, readlinePrompt } from "../../src/cli/prompt.js";

/**
 * Exercise `makeReadlinePrompt` against PassThrough streams so the EOF
 * contract — "Ctrl-D / stdin close returns 'abort', never throws" — is
 * verified end-to-end against the real readline algorithm. The exported
 * `readlinePrompt` is built from `makeReadlinePrompt(process streams)`, so
 * regressions to the underlying implementation are caught here.
 */
describe("readlinePrompt — EOF / Ctrl-D contract", () => {
  it("returns 'abort' when stdin closes without an answer (does not throw)", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const prompt = makeReadlinePrompt({ input, output, terminal: false });
    // Close stdin AFTER the readline interface has hooked up — simulates
    // Ctrl-D once the prompt is waiting for an answer. Closing before
    // readline attaches its 'end' handler is a different race that doesn't
    // model the "user hits Ctrl-D at the prompt" path.
    setImmediate(() => input.end());
    const result = await prompt({
      driftedCount: 1,
      activeProfile: "a",
      targetProfile: "b",
    });
    expect(result).toBe("abort");
  });

  it("accepts a typed answer and returns the parsed choice", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const prompt = makeReadlinePrompt({ input, output, terminal: false });
    setImmediate(() => {
      input.write("d\n");
      input.end();
    });
    const result = await prompt({
      driftedCount: 1,
      activeProfile: "a",
      targetProfile: "b",
    });
    expect(result).toBe("discard");
  });

  it("re-prompts on invalid input then accepts a valid answer", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    const prompt = makeReadlinePrompt({ input, output, terminal: false });
    setImmediate(() => {
      input.write("nope\n");
      // Give the readline loop a tick to consume the first line + reprompt.
      setImmediate(() => {
        input.write("persist\n");
        input.end();
      });
    });
    const result = await prompt({
      driftedCount: 1,
      activeProfile: "a",
      targetProfile: "b",
    });
    expect(result).toBe("persist");
  });

  it("exports a real readlinePrompt bound to process streams (smoke check)", () => {
    expect(typeof readlinePrompt).toBe("function");
  });
});
