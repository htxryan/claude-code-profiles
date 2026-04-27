/**
 * Public API surface for E3: Materialization + State + Concurrency.
 *
 * Downstream epics (E4 drift, E5 CLI, E6 init) consume only this surface.
 * Internal modules (atomic primitives, copy strategy) are not re-exported.
 *
 * Cross-epic contracts produced by E3:
 *   - StateFile (with schemaVersion) — the truth source for active profile
 *   - LockHandle — guards every mutating op
 *   - Atomic-rename protocol — pending/prior, exposed via materialize/persist
 *   - Fingerprint — two-tier (mtime+size fast path, sha256 slow path)
 */

export {
  STATE_FILE_SCHEMA_VERSION,
  FINGERPRINT_SCHEMA_VERSION,
  defaultState,
} from "./types.js";
export type {
  ExternalTrustNotice,
  Fingerprint,
  FingerprintEntry,
  LockHandle,
  ResolvedSourceRef,
  SectionFingerprint,
  SourceFingerprint,
  StateFile,
} from "./types.js";

export { buildStatePaths, buildPersistPaths } from "./paths.js";
export type { StatePaths, PersistPaths } from "./paths.js";

export { readStateFile, writeStateFile } from "./state-file.js";
export type { ReadStateResult, StateReadWarning } from "./state-file.js";

export { acquireLock, withLock, LockHeldError, LockCorruptError } from "./lock.js";

export {
  reconcileMaterialize,
  reconcilePersist,
  reconcilePendingPrior,
} from "./reconcile.js";
export type { ReconcileOutcome } from "./reconcile.js";

export {
  compareFingerprint,
  compareFingerprintWithMetrics,
  computeSourceFingerprint,
  fingerprintFromMergedFiles,
  fingerprintTree,
  hashBytes,
  recordMtimes,
} from "./fingerprint.js";
export type {
  CompareMetrics,
  CompareResult,
  DriftKind,
  FileDrift,
} from "./fingerprint.js";

export { snapshotForDiscard, listSnapshots } from "./backup.js";

export {
  ensureGitignoreEntries,
  E3_GITIGNORE_ENTRIES,
} from "./gitignore.js";
export type { GitignoreUpdate } from "./gitignore.js";

export { materialize, isLiveConsistentWithRecord, readRecordedFingerprint } from "./materialize.js";
export type { MaterializeOptions, MaterializeResult } from "./materialize.js";

export { persistAndMaterialize, persistLiveIntoProfile } from "./persist.js";
export type { PersistAndMaterializeOptions } from "./persist.js";
