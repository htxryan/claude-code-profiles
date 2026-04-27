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
  /**
   * yd8 / AC-1: a sample of drifted file names to surface above the prompt
   * so the user sees WHICH files they're choosing about. Capped to a small
   * number by the caller (e.g. 5) so the prompt stays one screenful even
   * for a large drift set. Empty array hides the line entirely.
   */
  driftedSample?: ReadonlyArray<string>;
  /**
   * yd8 / AC-1: total drifted count when `driftedSample` was capped, so the
   * prompt can render "and N more" without recomputing. When equal to the
   * sample length, no overflow note is rendered.
   */
  driftedTotal?: number;
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
      // yd8 / AC-1: name the affected files so the user sees WHICH bytes
      // they're choosing about. We cap the visible list at a few names
      // (caller-provided sample) and append "and N more" if the total
      // exceeds the cap — keeps the prompt one screenful even when a
      // big edit batch drifts.
      const sample = input.driftedSample ?? [];
      const total = input.driftedTotal ?? input.driftedCount;
      const overflow = total > sample.length ? ` and ${total - sample.length} more` : "";
      const filesLine =
        sample.length > 0
          ? `  files: ${sample.join(", ")}${overflow}\n`
          : "";
      streams.output.write(
        `\nDrift detected: ${input.driftedCount} file(s) in .claude/ vs active profile "${input.activeProfile}"\n` +
          filesLine +
          `  discard - destroy live edits, materialize "${input.targetProfile}" (snapshots .claude/ first)\n` +
          `  persist - save live .claude/ into "${input.activeProfile}", then materialize "${input.targetProfile}"\n` +
          `  abort   - make no changes\n`,
      );
      while (true) {
        let answer: string;
        try {
          // yd8 / AC-1: extend the choice line with one-line cost annotations
          // so a first-time user picks knowing the consequence — including
          // the R23a backup behaviour for discard, which the original prompt
          // hid behind a bare "destroy" verb.
          answer = (
            await rl.question(
              "[d]iscard — drop edits (snapshot saved to .meta/backup/) | [p]ersist — copy live tree into active profile | [a]bort — no change\n? ",
              { signal: ac.signal },
            )
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
