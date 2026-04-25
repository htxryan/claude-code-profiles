/**
 * Interactive gate prompt. Pure interface so the swap orchestrator can inject
 * a mock in tests; the production binding hands a readline-backed function.
 *
 * Decoupled from the gate decision (lesson L29affb99: pure decision in E4 +
 * IO in E5). The decision said "prompt"; this module asks the user.
 */

import * as readline from "node:readline/promises";

import type { OnDriftFlag } from "./types.js";

export interface PromptInput {
  /** Total drifted file count (informational header). */
  driftedCount: number;
  /** Active profile being moved away from. */
  activeProfile: string;
  /** Target profile being switched to. */
  targetProfile: string;
}

/**
 * The interactive prompt function shape. Returns the user's choice.
 *
 * Implementations should be cancellable: if the user hits Ctrl-D / EOF, they
 * should return "abort" — never throw.
 */
export type GatePrompt = (input: PromptInput) => Promise<OnDriftFlag>;

/**
 * Streams the readline prompt reads from / writes to. Production binds to
 * `process.stdin` / `process.stderr`; tests inject PassThrough pairs.
 */
export interface ReadlinePromptStreams {
  input: NodeJS.ReadableStream;
  output: NodeJS.WritableStream;
  /** True iff the input stream is a real TTY — affects readline's terminal mode. */
  terminal: boolean;
}

/**
 * Build a readline-backed prompt. Exported so tests can inject streams; the
 * bin entry uses the default `readlinePrompt` binding which captures
 * process.stdin / process.stderr.
 */
export function makeReadlinePrompt(streams: ReadlinePromptStreams): GatePrompt {
  return async (input) => {
    const rl = readline.createInterface({
      input: streams.input,
      output: streams.output,
      terminal: streams.terminal,
    });
    // On Node 20, closing the input stream (Ctrl-D, pipe end) does NOT reject a
    // pending rl.question() — it just stops feeding lines, and the await hangs
    // forever. The only way to observe EOF is to wire an AbortController to the
    // 'close' event and pass the signal into question(); that path *does*
    // reject (with AbortError), which we translate to "abort" per the
    // GatePrompt contract.
    const ac = new AbortController();
    const onClose = () => ac.abort();
    rl.once("close", onClose);
    try {
      streams.output.write(
        `\nDrift detected: ${input.driftedCount} file(s) in .claude/ vs active profile "${input.activeProfile}"\n` +
          `  discard - destroy live edits, materialize "${input.targetProfile}" (snapshots .claude/ first)\n` +
          `  persist - save live .claude/ into "${input.activeProfile}", then materialize "${input.targetProfile}"\n` +
          `  abort   - make no changes\n`,
      );
      while (true) {
        let answer: string;
        try {
          answer = (
            await rl.question("[d]iscard / [p]ersist / [a]bort? ", { signal: ac.signal })
          )
            .trim()
            .toLowerCase();
        } catch {
          streams.output.write(`\n(input closed — aborting)\n`);
          return "abort";
        }
        if (answer === "discard" || answer === "d") return "discard";
        if (answer === "persist" || answer === "p") return "persist";
        if (answer === "abort" || answer === "a") return "abort";
        streams.output.write(`Please answer discard, persist, or abort.\n`);
      }
    } finally {
      rl.off("close", onClose);
      rl.close();
    }
  };
}

/**
 * Default implementation backed by readline. Prints a brief explanation to
 * stderr (so it doesn't pollute --json stdout — but --json mode never reaches
 * this path because non-interactive sessions auto-resolve at the gate).
 */
export const readlinePrompt: GatePrompt = makeReadlinePrompt({
  input: process.stdin,
  output: process.stderr,
  terminal: true,
});
