import { describe, expect, it } from "vitest";

import { runHello, type HelloPayload } from "../../../src/cli/commands/hello.js";
import { captureOutput } from "../helpers/output-sink.js";

describe("hello (hidden verb, claude-code-profiles-7x4)", () => {
  it("human path: prints the greeting on a single line", async () => {
    const cap = captureOutput(false);
    const code = await runHello({ output: cap.channel });
    expect(code).toBe(0);
    expect(cap.stdout()).toBe("Hello there! At your service.\n");
    // No human noise on stderr — argless side-effect-free verb.
    expect(cap.stderr()).toBe("");
  });

  it("--json: emits a single structured payload and silences human output", async () => {
    const cap = captureOutput(true);
    const code = await runHello({ output: cap.channel });
    expect(code).toBe(0);
    const lines = cap.jsonLines();
    expect(lines).toHaveLength(1);
    const payload = lines[0] as HelloPayload;
    expect(payload).toEqual({ greeting: "Hello there! At your service." });
    // --json invariant: no human chatter to stderr.
    expect(cap.stderr()).toBe("");
  });
});
