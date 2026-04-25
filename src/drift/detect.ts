/**
 * Drift detection (R18, R19, R20). Consumes E3's `compareFingerprintWithMetrics`
 * to compute per-file drift against the recorded fingerprint, then enriches
 * the result with provenance from `.state.json` to produce a `DriftReport`.
 *
 * Read-only and lock-free (epic invariant). May return slightly stale data
 * during a concurrent write — acceptable per R43.
 */

import { compareFingerprintWithMetrics } from "../state/fingerprint.js";
import type { StatePaths } from "../state/paths.js";
import { readStateFile } from "../state/state-file.js";

import {
  DRIFT_REPORT_SCHEMA_VERSION,
  type DriftEntry,
  type DriftReport,
} from "./types.js";

/**
 * Run drift detection on the live `.claude/` tree against the active
 * profile's recorded fingerprint.
 *
 * NoActive / schema-mismatch handling: `readStateFile` already degrades
 * gracefully (R42) — when the file is unparseable, we get a defaultState
 * back with `activeProfile: null`. We surface that as `fingerprintOk: false`
 * with empty entries so the gate can auto-pass and the pre-commit hook
 * stays silent.
 */
export async function detectDrift(paths: StatePaths): Promise<DriftReport> {
  const { state, warning } = await readStateFile(paths);

  if (state.activeProfile === null) {
    return {
      schemaVersion: DRIFT_REPORT_SCHEMA_VERSION,
      active: null,
      fingerprintOk: false,
      entries: [],
      scannedFiles: 0,
      fastPathHits: 0,
      slowPathHits: 0,
      warning,
    };
  }

  const result = await compareFingerprintWithMetrics(
    paths.claudeDir,
    state.fingerprint,
  );

  const entries: DriftEntry[] = [];
  for (const e of result.entries) {
    if (e.kind === "unchanged") continue;
    // Spread provenance per entry rather than sharing a single array
    // reference — defends against any future caller that mutates the
    // entry (e.g. sorts or filters provenance) from cross-contaminating
    // siblings (multi-reviewer P2-4).
    entries.push({
      relPath: e.relPath,
      status: e.kind,
      provenance: [...state.resolvedSources],
    });
  }

  return {
    schemaVersion: DRIFT_REPORT_SCHEMA_VERSION,
    active: state.activeProfile,
    fingerprintOk: true,
    entries,
    scannedFiles: result.metrics.scannedFiles,
    fastPathHits: result.metrics.fastPathHits,
    slowPathHits: result.metrics.slowPathHits,
    warning,
  };
}
