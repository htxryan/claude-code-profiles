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
  type GateChoice,
  type GateMode,
} from "../../drift/index.js";
import { merge } from "../../merge/index.js";
import { resolve } from "../../resolver/index.js";
import {
  readStateFile,
  reconcileMaterialize,
  withLock,
  type StatePaths,
} from "../../state/index.js";
import { CliUserError, EXIT_USER_ERROR } from "../exit.js";
import {
  formatPlanSummaryLine,
  summarizePlan,
  type PlanSummary,
} from "../plan-summary.js";
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
  /**
   * Optional callback that emits a transient progress hint between phases
   * (resolve / merge / materialize). Wired by the use/sync handlers to
   * `output.phase(...)` — silenced under --json and --quiet via the channel
   * so callers don't need to re-implement the suppression logic. Undefined
   * (the swap.test default) skips emission entirely.
   */
  onPhase?: (text: string) => void;
  /**
   * yd8 / AC-2: optional callback fired with the pre-swap plan summary line
   * once the dry-run delta has been computed (after merge, before the gate).
   * Wired by use/sync to `output.phase(...)` so the line is silenced under
   * --json and --quiet but appears on stderr otherwise. The structured
   * payload is also returned on SwapResult for --json consumers.
   *
   * Receives null when the swap is a true no-op (no replace/add/delete).
   */
  onPlanSummary?: (line: string | null, summary: PlanSummary) => void;
  /**
   * yd8 / AC-4: when set, lock acquisition polls a held lock with backoff
   * for up to this many ms before failing. Null/undefined preserves the
   * legacy fail-fast behaviour.
   */
  waitMs?: number | null;
  /**
   * yd8 / AC-4: notification fired ONCE when the wait begins (lock held +
   * --wait was supplied). Wired by use/sync to a stderr "waiting on lock
   * held by PID N…" line so the user knows the CLI is alive.
   */
  onLockWait?: (info: { pid: number; timestamp: string; cmdline: string | null }) => void;
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
  /**
   * yd8 / AC-2: structured pre-swap plan summary so --json consumers see
   * counts/byte deltas alongside the choice. Always present (even on
   * abort) so a script inspecting a no-change run still has the dry-run
   * data it needs to decide whether to retry.
   */
  planSummary: PlanSummary;
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
  // 0. Reconcile any leftover .pending/.prior from a crashed prior
  //    materialize BEFORE the outside-lock drift detect (ch5 followup).
  //    Without this, a killed materialize that left `.claude/` rolled aside
  //    to `.prior/` (mid step b) would make the outside-lock detect see a
  //    missing live tree and treat it as drift, exiting 1 in non-interactive
  //    mode. The materialize() function reconciles internally on the next
  //    write path, but the drift gate runs OUTSIDE the lock first to drive
  //    the prompt — so the spec contract ("Next CLI invocation reconciles
  //    via .prior/ rename-back" — R16/R16a) needed an explicit reconcile
  //    at the swap entrypoint to hold at the binary surface.
  //
  //    Reconcile mutates the filesystem (renames .prior → .claude, etc.),
  //    so we run it under a short lock acquisition. The cost is one extra
  //    flock pair on the steady-state path (where reconcile is a no-op
  //    after two stat calls); the win is that a crashed-mid-write situation
  //    doesn't masquerade as drift. The main swap critical section below
  //    re-acquires the lock after drift detection, which is unavoidable —
  //    the user-facing prompt happens between the two acquisitions.
  await withLock(
    opts.paths,
    async () => {
      await reconcileMaterialize(opts.paths);
    },
    {
      signalHandlers: opts.signalHandlers,
      ...(opts.waitMs !== null && opts.waitMs !== undefined
        ? { wait: { totalMs: opts.waitMs, ...(opts.onLockWait ? { onWait: opts.onLockWait } : {}) } }
        : {}),
    },
  );

  // 1. Resolve + merge OUTSIDE the lock. Resolution is a read-only operation
  //    over .claude-profiles/ that doesn't conflict with concurrent work.
  //    R43: reads bypass the lock.
  //
  //    Phase hints (3yy): emitted before each long phase so a 1000-file
  //    profile doesn't sit on a stuck cursor. The callback is silenced
  //    under --json/--quiet at the OutputChannel level — no suppression
  //    logic needed here.
  opts.onPhase?.("resolving profile…");
  const plan = await resolve(opts.targetProfile, { projectRoot: opts.paths.projectRoot });
  opts.onPhase?.("merging files…");
  const merged = await merge(plan);

  // 2. First-pass drift detect outside the lock — used to drive the prompt
  //    without blocking other readers (apply.ts contract).
  const reportOutside = await detectDrift(opts.paths);

  // yd8 / AC-2: pre-swap dry-run summary. Computed once so the same payload
  // drives both the human stderr line (via onPlanSummary) and the --json
  // SwapResult.planSummary. Does NOT short-circuit any decision — this is
  // pure observation that the user reads to sanity-check their invocation.
  const planSummary = await summarizePlan(opts.paths, merged);
  const planSummaryLine = formatPlanSummaryLine(planSummary);
  // Always notify so callers can route to JSON / human channels themselves.
  // Suppression for non-drift/no-op paths happens at the call-site (the line
  // is null when there's nothing to report), and --json/--quiet silencing is
  // handled by the OutputChannel.phase shim used by use.ts/sync.ts.
  opts.onPlanSummary?.(planSummaryLine, planSummary);

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
    // yd8 / AC-1: surface up to 5 drifted file names so the prompt header
    // names what's at stake. The prompt module renders "and N more" when
    // the total exceeds the sample length.
    const driftedSample = reportOutside.entries.slice(0, 5).map((e) => e.relPath);
    const userChoice = await opts.prompt({
      driftedCount,
      activeProfile: reportOutside.active ?? "?",
      targetProfile: opts.targetProfile,
      driftedSample,
      driftedTotal: driftedCount,
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
  //
  //    The withLock callback returns the apply result so we don't have to
  //    declare an outer `let applyResult` and reach for non-null assertions —
  //    if anything inside the callback throws before applyGate completes,
  //    the throw propagates and we never read an uninitialized binding.
  const applyResult = await withLock(
    opts.paths,
    async () => {
      // Re-read state inside the lock — needed for persist's `activeProfileName`.
      const { state } = await readStateFile(opts.paths);
      const activeBeforeInside = state.activeProfile;

      // Re-detect inside the lock (TOCTOU defense per apply.ts).
      const reportInside = await detectDrift(opts.paths);
      // Re-decide using the inside report. Two distinct cases:
      //
      //  (a) reportInside.entries.length === 0 — drift cleared between the
      //      outside detect and lock acquisition (a parallel editor reverted
      //      the file). The user's chosen action is moot; downgrade to
      //      no-drift-proceed so we don't take a backup snapshot or persist
      //      an unchanged tree. This is a benign race.
      //
      //  (b) !reportInside.fingerprintOk — `.state.json` became unreadable or
      //      schema-mismatched between outside and inside. The user's chosen
      //      action depended on having a valid state file (persist needs the
      //      active profile name; discard's R23a backup is rooted in the
      //      live `.claude/`). Refuse rather than silently downgrade and
      //      overwrite live bytes without honouring the chosen contract —
      //      the user can re-run after investigating the state file.
      //      Exception: if we were going to no-op anyway (entries===0), the
      //      first branch already covered it; we only land here when the
      //      user explicitly chose discard/persist on a non-empty drift set.
      let appliedChoice: GateChoice = chosen;
      if (reportInside.entries.length === 0) {
        appliedChoice = "no-drift-proceed";
      } else if (!reportInside.fingerprintOk) {
        throw new CliUserError(
          `swap aborted: state file became unreadable between drift detection and lock acquisition; re-run after inspecting .claude-profiles/.meta/state.json`,
          EXIT_USER_ERROR,
        );
      }

      opts.onPhase?.("materializing…");
      return await applyGate(appliedChoice, {
        paths: opts.paths,
        plan,
        merged,
        activeProfileName: activeBeforeInside,
      });
    },
    {
      signalHandlers: opts.signalHandlers,
      ...(opts.waitMs !== null && opts.waitMs !== undefined
        ? { wait: { totalMs: opts.waitMs, ...(opts.onLockWait ? { onWait: opts.onLockWait } : {}) } }
        : {}),
    },
  );

  // The materialize() call already wrote the new state; we surface the
  // observable outcome to the caller for messaging.
  return {
    action: applyResult.action,
    choice: chosen,
    backupSnapshot: applyResult.backupSnapshot,
    activeAfter: applyResult.materializeResult?.state.activeProfile ?? null,
    planSummary,
  };
}
