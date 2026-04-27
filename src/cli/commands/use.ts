/**
 * `use <name>` command (R13, R21, R22, R23, R23a, R24). Thin wrapper over
 * `runSwap` — all business logic lives in the service layer.
 */

import type { GateMode } from "../../drift/index.js";
import { buildStatePaths } from "../../state/index.js";
import type { OutputChannel } from "../output.js";
import { readlinePrompt } from "../prompt.js";
import { runSwap } from "../service/swap.js";
import type { SwapResult } from "../service/swap.js";
import { assertValidProfileName, enrichMissingProfileError } from "../suggest.js";
import type { OnDriftFlag } from "../types.js";

export interface UseOptions {
  cwd: string;
  output: OutputChannel;
  profile: string;
  mode: GateMode;
  onDriftFlag: OnDriftFlag | null;
  /** Bin always passes true; tests pass false. Required to avoid accidental skip. */
  signalHandlers: boolean;
}

export async function runUse(opts: UseOptions): Promise<number> {
  // Pre-flight name validation (claude-code-profiles-ppo): reject path-like
  // names with the same wording `new` uses BEFORE handing off to runSwap.
  // Without this, `use a/b` would surface as a generic "Profile does not
  // exist" — accurate but missing the actionable "fix the name" cue.
  assertValidProfileName("use", opts.profile);

  // ppo: enrich top-level MissingProfileError with "did you mean: …"
  // suggestions when at least one in-project profile is within distance 2.
  // Pass-through for structural (extends-chain) misses — those need
  // "edit a profile.json" remediation, not a typo nudge.
  const result: SwapResult = await runSwapWithSuggestions(opts);

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

async function runSwapWithSuggestions(opts: UseOptions): Promise<SwapResult> {
  try {
    return await runSwap({
      paths: buildStatePaths(opts.cwd),
      targetProfile: opts.profile,
      mode: opts.mode,
      onDriftFlag: opts.onDriftFlag,
      prompt: readlinePrompt,
      signalHandlers: opts.signalHandlers,
    });
  } catch (err) {
    throw await enrichMissingProfileError(err, opts.cwd, opts.profile);
  }
}
