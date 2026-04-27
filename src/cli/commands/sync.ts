/**
 * `sync` command (R34). Re-materializes the active profile after passing the
 * drift gate. Same orchestration as `use <active>` but error-handles the
 * "no active profile" case explicitly.
 */

import process from "node:process";

import type { GateMode } from "../../drift/index.js";
import { buildStatePaths, readStateFile } from "../../state/index.js";
import { CliUserError, EXIT_USER_ERROR } from "../exit.js";
import { createStyle, resolveNoColor, type OutputChannel } from "../output.js";
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
  /** When true, force colour off (additive with NO_COLOR env). Default false. */
  noColor?: boolean;
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
  // Phase hints (3yy): emit transient progress lines on stderr through the
  // OutputChannel so --json and --quiet silence them automatically.
  const phaseStyle = createStyle({
    isTty: opts.output.isTty,
    platform: process.platform,
    noColor: resolveNoColor(opts.noColor === true),
  });
  const result = await runSwap({
    paths,
    targetProfile: state.activeProfile,
    mode: opts.mode,
    onDriftFlag: opts.onDriftFlag,
    prompt: readlinePrompt,
    isSync: true,
    signalHandlers: opts.signalHandlers,
    onPhase: (text) => opts.output.phase(phaseStyle.dim(text)),
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
    const style = createStyle({
      isTty: opts.output.isTty,
      platform: process.platform,
      noColor: resolveNoColor(opts.noColor === true),
    });
    if (result.choice === "discard") {
      opts.output.print(style.ok(`Synced ${result.activeAfter} (drift discarded).`));
      if (result.backupSnapshot) {
        opts.output.print(style.dim(`  Backup: ${result.backupSnapshot}`));
      }
    } else {
      opts.output.print(style.ok(`Synced ${result.activeAfter}.`));
    }
  }
  return 0;
}
