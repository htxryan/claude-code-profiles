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
 * Default implementation backed by readline. Prints a brief explanation to
 * stderr (so it doesn't pollute --json stdout — but --json mode never reaches
 * this path because non-interactive sessions auto-resolve at the gate).
 */
export const readlinePrompt: GatePrompt = async (input) => {
  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stderr,
    terminal: true,
  });
  try {
    process.stderr.write(
      `\nDrift detected: ${input.driftedCount} file(s) in .claude/ vs active profile "${input.activeProfile}"\n` +
        `  discard - destroy live edits, materialize "${input.targetProfile}" (snapshots .claude/ first)\n` +
        `  persist - save live .claude/ into "${input.activeProfile}", then materialize "${input.targetProfile}"\n` +
        `  abort   - make no changes\n`,
    );
    while (true) {
      const answer = (await rl.question("[d]iscard / [p]ersist / [a]bort? ")).trim().toLowerCase();
      if (answer === "discard" || answer === "d") return "discard";
      if (answer === "persist" || answer === "p") return "persist";
      if (answer === "abort" || answer === "a") return "abort";
      process.stderr.write(`Please answer discard, persist, or abort.\n`);
    }
  } finally {
    rl.close();
  }
};
