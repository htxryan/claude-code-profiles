/**
 * `sync` command (R34). Re-materializes the active profile after passing the
 * drift gate. Same orchestration as `use <active>` but error-handles the
 * "no active profile" case explicitly.
 */

import type { GateMode } from "../../drift/index.js";
import { buildStatePaths, readStateFile } from "../../state/index.js";
import { CliUserError, EXIT_USER_ERROR } from "../exit.js";
import type { OutputChannel } from "../output.js";
import { readlinePrompt } from "../prompt.js";
import { runSwap } from "../service/swap.js";
import type { OnDriftFlag } from "../types.js";

export interface SyncOptions {
  cwd: string;
  output: OutputChannel;
  mode: GateMode;
  onDriftFlag: OnDriftFlag | null;
  /** Bin always passes true; tests pass false. Required to avoid accidental skip. */
  signalHandlers: boolean;
}

export async function runSync(opts: SyncOptions): Promise<number> {
  const paths = buildStatePaths(opts.cwd);
  const { state } = await readStateFile(paths);
  if (state.activeProfile === null) {
    throw new CliUserError(
      `sync: no active profile (run "claude-profiles use <name>" first)`,
      EXIT_USER_ERROR,
    );
  }
  const result = await runSwap({
    paths,
    targetProfile: state.activeProfile,
    mode: opts.mode,
    onDriftFlag: opts.onDriftFlag,
    prompt: readlinePrompt,
    isSync: true,
    signalHandlers: opts.signalHandlers,
  });

  if (opts.output.jsonMode) {
    opts.output.json({
      action: result.action,
      activeProfile: result.activeAfter,
      choice: result.choice,
      backupSnapshot: result.backupSnapshot,
      sync: true,
    });
  } else {
    if (result.choice === "discard") {
      const note = result.backupSnapshot ? ` Backup: ${result.backupSnapshot}` : "";
      opts.output.print(`Synced ${result.activeAfter} (drift discarded).${note}`);
    } else {
      opts.output.print(`Synced ${result.activeAfter}.`);
    }
  }
  return 0;
}
