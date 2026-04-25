/**
 * Swap orchestrator (R13, R21, R22, R23, R24, R34). Composes:
 *
 *   E1.resolve  →  E2.merge  →  E4.detectDrift  →  E4.decideGate
 *                                                       │
 *                              ┌────────────────────────┤
 *                              │                        │
 *                          (no-drift /                  prompt
 *                            auto)                        │
 *                              │                          ▼
 *                              │                       prompt user
 *                              ▼                          │
 *                          (choice)  ◄────────────────────┘
 *                              │
 *                              ▼
 *                   withLock { detectDrift again
 *                              applyGate(choice) }   ← E4.applyGate
 *
 * Key invariants enforced here (epic):
 *   - Non-TTY swap with no `--on-drift=` flag NEVER prompts; auto-aborts
 *     with exit-1 message naming the flag (lesson L29affb99 — gate is hard-
 *     blocking in non-interactive mode)
 *   - The drift detect runs TWICE per the apply.ts contract: once outside
 *     the lock to drive the prompt, once inside the lock so applyGate sees
 *     ground truth
 *   - withLock brackets the entire mutating sequence (detect-inside-lock +
 *     applyGate + state-write happen under one acquisition)
 */

import {
  applyGate,
  decideGate,
  detectDrift,
  type ApplyGateResult,
  type GateChoice,
  type GateMode,
} from "../../drift/index.js";
import { merge } from "../../merge/index.js";
import { resolve } from "../../resolver/index.js";
import {
  readStateFile,
  withLock,
  type StatePaths,
} from "../../state/index.js";
import { CliUserError, EXIT_USER_ERROR } from "../exit.js";
import type { GatePrompt } from "../prompt.js";
import type { OnDriftFlag } from "../types.js";

export interface SwapOptions {
  paths: StatePaths;
  /** Profile to switch to. For `sync`, pass the current active profile name. */
  targetProfile: string;
  /** TTY/non-TTY mode (drives gate decision). */
  mode: GateMode;
  /** --on-drift= value if user passed one. */
  onDriftFlag: OnDriftFlag | null;
  /** Interactive prompt (used iff decideGate returns "prompt"). */
  prompt: GatePrompt;
  /**
   * If true, treat this as a `sync` (don't change activeProfile). Wired into
   * E3.materialize via the new plan having profileName === activeProfile.
   * Behaviorally identical to `use <activeProfile>` from E3's POV.
   */
  isSync?: boolean;
  /**
   * Whether the lock module should install per-acquire SIGINT/SIGTERM
   * handlers that synchronously release the lock. The bin entry passes true
   * (production: SIGINT-to-lock-release invariant). Tests pass false because
   * vitest workers reject `process.on('SIGINT', ...)`. Required (no default)
   * so callers can't accidentally skip wiring it through.
   */
  signalHandlers: boolean;
}

export interface SwapResult {
  /**
   * What actually happened. Mirrors ApplyGateResult.action plus a "no-op"
   * value for the sync-clean case (which materialize handles internally).
   */
  action: "materialized" | "persisted-and-materialized" | "aborted";
  /** Choice that was applied. Useful for status messages. */
  choice: GateChoice;
  /** Backup snapshot path if discard branch took one. */
  backupSnapshot: string | null;
  /** Active profile after the swap (== targetProfile unless aborted). */
  activeAfter: string | null;
}

/**
 * Run the swap. Throws CliUserError(exit 1) on:
 *   - non-interactive without --on-drift= when drift is present
 *   - the user choosing abort (per AC-16: drift abort is exit 1)
 *
 * Other errors propagate as-is so the dispatcher's error mapper assigns
 * the right exit code (LockHeldError → 3, ResolverError → 3, etc.).
 */
export async function runSwap(opts: SwapOptions): Promise<SwapResult> {
  // 1. Resolve + merge OUTSIDE the lock. Resolution is a read-only operation
  //    over .claude-profiles/ that doesn't conflict with concurrent work.
  //    R43: reads bypass the lock.
  const plan = await resolve(opts.targetProfile, { projectRoot: opts.paths.projectRoot });
  const merged = await merge(plan);

  // 2. First-pass drift detect outside the lock — used to drive the prompt
  //    without blocking other readers (apply.ts contract).
  const reportOutside = await detectDrift(opts.paths);

  // 3. Decide.
  const outcome = decideGate({
    report: reportOutside,
    mode: opts.mode,
    ...(opts.onDriftFlag !== null ? { onDriftFlag: opts.onDriftFlag } : {}),
  });

  // 4. Interactive: prompt for the choice if needed.
  let chosen: GateChoice;
  if (outcome.kind === "prompt") {
    // Defense-in-depth: decideGate guarantees prompt only fires in
    // interactive mode. If somehow we got here non-interactive, refuse.
    if (opts.mode !== "interactive") {
      throw new CliUserError(
        `non-interactive session has drift but no --on-drift= flag; pass --on-drift=discard|persist|abort`,
        EXIT_USER_ERROR,
      );
    }
    const driftedCount = reportOutside.entries.length;
    const userChoice = await opts.prompt({
      driftedCount,
      activeProfile: reportOutside.active ?? "?",
      targetProfile: opts.targetProfile,
    });
    chosen = userChoice;
  } else if (outcome.choice !== null) {
    chosen = outcome.choice;
    // The non-interactive auto-abort case is the canonical "user error" exit:
    // surface it as exit 1 with a message naming the flag so CI/scripts know
    // exactly what to add (epic invariant: non-TTY never blocks).
    if (outcome.kind === "auto" && chosen === "abort" && opts.mode === "non-interactive" && opts.onDriftFlag === null) {
      throw new CliUserError(
        `drift detected in .claude/ and session is non-interactive; pass --on-drift=discard|persist|abort`,
        EXIT_USER_ERROR,
      );
    }
  } else {
    // Should be unreachable: decideGate either returns choice + auto/no-drift
    // or kind === "prompt".
    throw new Error("unreachable: gate decision yielded null choice without prompt");
  }

  // 5. Abort short-circuit before lock.
  if (chosen === "abort") {
    // R24: abort makes no changes. Throw exit 1 so the CLI surfaces it
    // distinctly from "success" — important for scripts.
    throw new CliUserError(`swap aborted by drift gate`, EXIT_USER_ERROR);
  }

  // 6. Lock + re-detect + apply. Lock brackets detect-inside + applyGate +
  //    state-write so partial-success windows are unobservable.
  let applyResult: ApplyGateResult;
  let activeBefore: string | null = null;
  await withLock(
    opts.paths,
    async () => {
    // Re-read state inside the lock — needed for persist's `activeProfileName`.
    const { state } = await readStateFile(opts.paths);
    activeBefore = state.activeProfile;

    // Re-detect inside the lock (TOCTOU defense per apply.ts).
    const reportInside = await detectDrift(opts.paths);
    // Re-decide using the inside report; if drift cleared between the
    // outside read and the lock, downgrade choice to no-drift-proceed.
    let appliedChoice: GateChoice = chosen;
    if (
      reportInside.entries.length === 0 ||
      !reportInside.fingerprintOk
    ) {
      appliedChoice = "no-drift-proceed";
    }

    applyResult = await applyGate(appliedChoice, {
      paths: opts.paths,
      plan,
      merged,
      activeProfileName: activeBefore,
    });
    },
    { signalHandlers: opts.signalHandlers },
  );

  // The materialize() call already wrote the new state; we surface the
  // observable outcome to the caller for messaging.
  return {
    action: applyResult!.action,
    choice: chosen,
    backupSnapshot: applyResult!.backupSnapshot,
    activeAfter: applyResult!.materializeResult?.state.activeProfile ?? null,
  };
}
