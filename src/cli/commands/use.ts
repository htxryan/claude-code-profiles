/**
 * `use <name>` command (R13, R21, R22, R23, R23a, R24). Thin wrapper over
 * `runSwap` — all business logic lives in the service layer.
 */

import { buildStatePaths } from "../../state/paths.js";
import type { OutputChannel } from "../output.js";
import { readlinePrompt } from "../prompt.js";
import { runSwap } from "../service/swap.js";
import type { GateMode } from "../../drift/types.js";
import type { OnDriftFlag } from "../types.js";

export interface UseOptions {
  cwd: string;
  output: OutputChannel;
  profile: string;
  mode: GateMode;
  onDriftFlag: OnDriftFlag | null;
  /** Wired through to the lock for tests; production omits (defaults true). */
  signalHandlers?: boolean;
}

export async function runUse(opts: UseOptions): Promise<number> {
  const result = await runSwap({
    paths: buildStatePaths(opts.cwd),
    targetProfile: opts.profile,
    mode: opts.mode,
    onDriftFlag: opts.onDriftFlag,
    prompt: readlinePrompt,
    ...(opts.signalHandlers !== undefined ? { signalHandlers: opts.signalHandlers } : {}),
  });

  if (opts.output.jsonMode) {
    opts.output.json({
      action: result.action,
      activeProfile: result.activeAfter,
      choice: result.choice,
      backupSnapshot: result.backupSnapshot,
    });
  } else {
    if (result.action === "persisted-and-materialized") {
      opts.output.print(`Switched to ${result.activeAfter} (drift saved into previous active profile).`);
    } else if (result.choice === "discard") {
      const note = result.backupSnapshot ? ` Backup: ${result.backupSnapshot}` : "";
      opts.output.print(`Switched to ${result.activeAfter} (drift discarded).${note}`);
    } else {
      opts.output.print(`Switched to ${result.activeAfter}.`);
    }
  }
  return 0;
}
