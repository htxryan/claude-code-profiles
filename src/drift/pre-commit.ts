/**
 * Pre-commit hook entry point (R25, R25a). The verbatim hook script
 * (installed by E6) invokes:
 *
 *   claude-profiles drift --pre-commit-warn
 *
 * which lands in this module. Fail-open invariant: this function NEVER
 * throws and always indicates exit code 0. If detection fails for any
 * reason — corrupted state, missing profile dir, FS error mid-walk — we
 * print a one-line diagnostic to stderr and exit silently. A drift check
 * must never block a commit.
 *
 * E5 owns the actual `drift --pre-commit-warn` argv parsing; this module
 * exposes the function E5 dispatches to.
 */

import { detectDrift } from "./detect.js";
import type { StatePaths } from "../state/paths.js";

import type { DriftReport } from "./types.js";

export interface PreCommitWarnResult {
  /** Always 0 — fail-open invariant. */
  exitCode: 0;
  /** Lines that would be written to stderr. Captured for testability. */
  warnings: string[];
  /** The report we produced, or null if detection itself failed. */
  report: DriftReport | null;
}

/**
 * Detect drift; if any, build a one-line + per-file warning. Always returns
 * exit code 0. The caller (E5) wires this into the `drift --pre-commit-warn`
 * code path; it MUST `process.exit(0)` regardless of return value.
 *
 * Output discipline (non-blocking quality bar §7):
 *   - When no drift: silent (no output). The hook should be invisible in
 *     the happy path.
 *   - When drift: a header line ("claude-profiles: <N> drifted file(s) in
 *     .claude/ vs active profile <name>") plus up to 10 entries (truncated
 *     with "...and N more" if longer) so the commit terminal isn't flooded.
 *   - When detection failed: a single line "claude-profiles: drift check
 *     skipped: <reason>". Never two lines, never a stack trace.
 */
export async function preCommitWarn(paths: StatePaths): Promise<PreCommitWarnResult> {
  const warnings: string[] = [];
  let report: DriftReport | null = null;
  try {
    report = await detectDrift(paths);
    if (report.fingerprintOk && report.entries.length > 0) {
      warnings.push(...formatWarning(report));
    }
  } catch (err: unknown) {
    const detail = err instanceof Error ? err.message : String(err);
    warnings.push(`claude-profiles: drift check skipped: ${detail}`);
  }

  // EPIPE-safe write loop (multi-reviewer P0-2): if stderr is a closed pipe
  // (e.g. git pre-commit driver disconnected before the hook finishes
  // writing), `process.stderr.write` can throw synchronously. Swallowing
  // the error here preserves the fail-open invariant — better to lose the
  // message than block the commit. We also catch the loop in a try/catch
  // for any other I/O surprise.
  try {
    for (const w of warnings) {
      try {
        process.stderr.write(w + "\n");
      } catch {
        // EPIPE or similar — abandon the rest of the output silently.
        break;
      }
    }
  } catch {
    // Last-ditch belt: nothing should escape this function with a
    // non-zero result, ever (R25a fail-open).
  }
  return { exitCode: 0, warnings, report };
}

const MAX_LINES = 10;

function formatWarning(report: DriftReport): string[] {
  const lines: string[] = [];
  const n = report.entries.length;
  lines.push(
    `claude-profiles: ${n} drifted file(s) in .claude/ vs active profile '${report.active}'`,
  );
  const shown = report.entries.slice(0, MAX_LINES);
  for (const e of shown) {
    lines.push(`  ${statusGlyph(e.status)} ${e.relPath}`);
  }
  if (n > MAX_LINES) {
    lines.push(`  ...and ${n - MAX_LINES} more`);
  }
  return lines;
}

function statusGlyph(status: "modified" | "added" | "deleted"): string {
  switch (status) {
    case "modified":
      return "M";
    case "added":
      return "A";
    case "deleted":
      return "D";
  }
}
