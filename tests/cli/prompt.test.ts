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

/**
 * yd8 / AC-1: the prompt must annotate each choice with its cost AND name
 * the affected files so a first-time user picks knowing the consequence.
 */
describe("readlinePrompt — yd8 AC-1: cost annotations + file list", () => {
  it("renders cost annotations for discard/persist/abort and includes the backup path note", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    let captured = "";
    output.on("data", (d: Buffer) => {
      captured += d.toString();
    });
    const prompt = makeReadlinePrompt({ input, output, terminal: false });
    setImmediate(() => {
      input.write("a\n");
      input.end();
    });
    await prompt({
      driftedCount: 2,
      activeProfile: "dev",
      targetProfile: "ci",
      driftedSample: ["CLAUDE.md", "agents/foo.md"],
      driftedTotal: 2,
    });
    expect(captured).toContain("[d]iscard");
    expect(captured).toContain("[p]ersist");
    expect(captured).toContain("[a]bort");
    // Cost annotations
    expect(captured).toContain("drop edits");
    expect(captured).toContain("snapshot saved to .meta/backup/");
    expect(captured).toContain("copy live tree");
    expect(captured).toContain("no change");
    // Files line names the affected files.
    expect(captured).toContain("CLAUDE.md");
    expect(captured).toContain("agents/foo.md");
  });

  it("renders 'and N more' when total exceeds the sample length", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    let captured = "";
    output.on("data", (d: Buffer) => {
      captured += d.toString();
    });
    const prompt = makeReadlinePrompt({ input, output, terminal: false });
    setImmediate(() => {
      input.write("a\n");
      input.end();
    });
    await prompt({
      driftedCount: 7,
      activeProfile: "dev",
      targetProfile: "ci",
      driftedSample: ["a.md", "b.md", "c.md"],
      driftedTotal: 7,
    });
    expect(captured).toContain("a.md");
    expect(captured).toContain("and 4 more");
  });

  it("omits the files line when no sample is provided (back-compat)", async () => {
    const input = new PassThrough();
    const output = new PassThrough();
    let captured = "";
    output.on("data", (d: Buffer) => {
      captured += d.toString();
    });
    const prompt = makeReadlinePrompt({ input, output, terminal: false });
    setImmediate(() => {
      input.write("a\n");
      input.end();
    });
    await prompt({
      driftedCount: 1,
      activeProfile: "dev",
      targetProfile: "ci",
    });
    // Header still names the active profile, but no `files: ...` line appears.
    expect(captured).toContain('vs active profile "dev"');
    expect(captured).not.toContain("files: ");
  });
});
