/**
 * `use <name>` command (R13, R21, R22, R23, R23a, R24). Thin wrapper over
 * `runSwap` — all business logic lives in the service layer.
 */

import process from "node:process";

import type { GateMode } from "../../drift/index.js";
import { buildStatePaths } from "../../state/index.js";
import { createStyle, resolveNoColor, type OutputChannel } from "../output.js";
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
  /** When true, force colour off (additive with NO_COLOR env). Default false. */
  noColor?: boolean;
  /**
   * yd8 / AC-4: when set, the lock acquisition polls a held lock with
   * exponential backoff for up to this many ms before failing. Null/undefined
   * preserves the legacy fail-fast behaviour.
   */
  waitMs?: number | null;
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
      // yd8 / AC-2: structured pre-swap delta so --json consumers see the
      // dry-run breakdown (replace/add/delete counts + byte deltas).
      planSummary: result.planSummary,
    });
  } else {
    const style = createStyle({
      isTty: opts.output.isTty,
      platform: process.platform,
      noColor: resolveNoColor(opts.noColor === true),
    });
    if (result.action === "persisted-and-materialized") {
      opts.output.print(
        style.ok(`Switched to ${result.activeAfter} (drift saved into previous active profile).`),
      );
    } else if (result.choice === "discard") {
      opts.output.print(style.ok(`Switched to ${result.activeAfter} (drift discarded).`));
      if (result.backupSnapshot) {
        opts.output.print(style.dim(`  Backup: ${result.backupSnapshot}`));
      }
    } else {
      opts.output.print(style.ok(`Switched to ${result.activeAfter}.`));
    }
  }
  return 0;
}

async function runSwapWithSuggestions(opts: UseOptions): Promise<SwapResult> {
  // Phase hints (3yy): emit transient progress lines on stderr through the
  // OutputChannel so --json and --quiet silence them automatically. We pre-
  // dim them via the same Style the success line uses so they read as
  // secondary even on a colour-stripped terminal.
  const style = createStyle({
    isTty: opts.output.isTty,
    platform: process.platform,
    noColor: resolveNoColor(opts.noColor === true),
  });
  try {
    return await runSwap({
      paths: buildStatePaths(opts.cwd),
      targetProfile: opts.profile,
      mode: opts.mode,
      onDriftFlag: opts.onDriftFlag,
      prompt: readlinePrompt,
      signalHandlers: opts.signalHandlers,
      onPhase: (text) => opts.output.phase(style.dim(text)),
      // yd8 / AC-2: route the pre-swap dry-run line through the phase
      // channel so it inherits --json/--quiet silencing. Skip when the
      // line is null (true no-op swap — nothing useful to say).
      onPlanSummary: (line) => {
        if (line !== null) opts.output.phase(style.dim(line));
      },
      // yd8 / AC-4: opt-in lock polling. Emit a single "waiting…" line so
      // the user sees the CLI is alive while it waits.
      waitMs: opts.waitMs ?? null,
      onLockWait: (info) => {
        const cmd = info.cmdline !== null ? `: ${info.cmdline}` : "";
        opts.output.warn(
          style.dim(`waiting on lock held by PID ${info.pid} (acquired ${info.timestamp}${cmd})…`),
        );
      },
    });
  } catch (err) {
    throw await enrichMissingProfileError(err, opts.cwd, opts.profile);
  }
}
