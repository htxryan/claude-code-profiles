/**
 * E3 cross-epic contracts: StateFile, Fingerprint, LockHandle.
 *
 * StateFile is the truth source for "what's currently active in `.claude/`".
 * Consumers (E4 drift, E5 CLI) read it; only E3 writes it. The schema is
 * versioned from day one (lesson L05660762944f8248): readers degrade to
 * `null` activeProfile on schemaVersion mismatch rather than aborting (R42).
 */

/**
 * Schema version for the state file. Bumped only when consumers (E4/E5) must
 * update for a breaking shape change.
 */
export const STATE_FILE_SCHEMA_VERSION = 1 as const;

/**
 * Schema version for the fingerprint format. Independent of state file
 * version because fingerprint storage may evolve (e.g. hash algorithm) faster
 * than the surrounding StateFile shape.
 */
export const FINGERPRINT_SCHEMA_VERSION = 1 as const;

/**
 * One file's recorded fingerprint. Two-tier: `size` and `mtimeMs` are the
 * fast-path metadata check (R18); `contentHash` is the slow-path verification
 * recomputed only when metadata differs.
 */
export interface FingerprintEntry {
  /** File size in bytes. */
  size: number;
  /** Modification time in millis since epoch (Date.now() compatible). */
  mtimeMs: number;
  /** Hex-encoded content hash. Algorithm is sha256. */
  contentHash: string;
}

/**
 * Section-only fingerprint for the project-root `CLAUDE.md` (R46, cw6/T4).
 * Distinct from {@link FingerprintEntry} because it intentionally tracks the
 * SECTION bytes (between the `:begin`/`:end` markers), NOT the whole-file
 * metadata. The whole-file mtime/size would change every time the user edits
 * outside the markers, which the spec explicitly says must NOT register as
 * drift.
 *
 * Why a separate type instead of unifying under FingerprintEntry: drift
 * detection (E4 / cw6/T5) needs to know which scope to use. Whole-file
 * fingerprints in `fingerprint.files` continue to compare by size+mtime fast
 * path; the section fingerprint always re-parses the live file and hashes the
 * inner bytes (no fast path possible — there's no per-section mtime).
 *
 * Schema migration: legacy `.state.json` files (written before cw6) have no
 * `rootClaudeMdSection` field. Readers tolerate its absence (treat as null);
 * the first cw6-aware materialize that touches a project-root CLAUDE.md
 * populates it. No schemaVersion bump because the addition is strictly
 * additive (R42 graceful-degradation contract preserved).
 */
export interface SectionFingerprint {
  /** Section byte length (the slice between markers, exclusive of marker bytes). */
  size: number;
  /** Hex-encoded sha256 of the section bytes. */
  contentHash: string;
}

export interface Fingerprint {
  schemaVersion: typeof FINGERPRINT_SCHEMA_VERSION;
  /**
   * Per-relPath fingerprint entries. Keys are posix-relative paths from the
   * `.claude/` root, matching `MergedFile.path` and `PlanFile.relPath`.
   */
  files: Record<string, FingerprintEntry>;
}

/**
 * One contributor source recorded at materialize time. E5's `status`/`list`
 * use this to render provenance without re-running the resolver. Subset of
 * `Contributor` — we only persist what's needed downstream.
 */
export interface ResolvedSourceRef {
  /** Contributor.id (profile name or raw includes string). */
  id: string;
  /** Contributor.kind. */
  kind: "ancestor" | "include" | "profile";
  /** Absolute resolvedPath (so E5 can show "from /abs/path"). */
  rootPath: string;
  /** Whether this source is outside the project root (for trust-notice telemetry). */
  external: boolean;
}

/**
 * Records that an external-trust notice has been printed for this resolved
 * path so we don't re-print it on every swap (R37a).
 */
export interface ExternalTrustNotice {
  /** Original includes string. */
  raw: string;
  /** Absolute resolved path the notice was issued for. */
  resolvedPath: string;
  /** When the notice was first issued. */
  noticedAt: string;
}

/**
 * The on-disk state file at `.claude-profiles/.meta/state.json`. R14, R14a, R42.
 *
 * Always written via temp+rename so partial writes are not observable.
 * `activeProfile === null` means "no active profile" (NoActive state in §4.3),
 * either because `init` ran but no `use` has occurred, or because the file
 * was unparseable / schema-mismatched and treated as NoActive (R42).
 */
export interface StateFile {
  schemaVersion: typeof STATE_FILE_SCHEMA_VERSION;
  /** Active profile name or null (NoActive). */
  activeProfile: string | null;
  /** ISO 8601 timestamp of last successful materialization. */
  materializedAt: string | null;
  /** Sources that produced the active materialization. */
  resolvedSources: ResolvedSourceRef[];
  /** Two-tier fingerprint of the materialized `.claude/` tree. */
  fingerprint: Fingerprint;
  /** R37a: external-path notices already shown. */
  externalTrustNotices: ExternalTrustNotice[];
  /**
   * cw6/T4 (R46): section-only fingerprint of the bytes between the managed-
   * block markers in project-root `CLAUDE.md`. Null when the active profile
   * contributes no project-root CLAUDE.md body — the field is absent from
   * legacy state files and is also absent when materialize ran for a profile
   * that has only `.claude/` contributors. The presence of this field is
   * orthogonal to the rest of `fingerprint.files` (which keeps `.claude/`
   * whole-file entries).
   */
  rootClaudeMdSection?: SectionFingerprint | null;
  /**
   * azp: aggregate fingerprint of the *resolved source files* the active
   * materialization came from — keyed by absolute path → size+mtime+(present-
   * bit). Cached on the state file so `status` can detect "the source files
   * changed since last materialize — run sync" without re-resolving and
   * re-merging every time.
   *
   * Schema migration: legacy state files (written before azp landed) have no
   * `sourceFingerprint` field. Readers tolerate its absence (treat as
   * undefined) and treat the source as "freshness unknown" — `status` shows
   * no stale-source warning until the first azp-aware materialize lands and
   * populates the field. This is strictly additive (R42 graceful-degradation
   * contract preserved); no schemaVersion bump.
   */
  sourceFingerprint?: SourceFingerprint | null;
}

/**
 * azp: aggregate fingerprint of the resolved-source files at materialize
 * time. Used by `status` to surface "source updated since last materialize"
 * without doing a full re-resolve+merge. The aggregate hash is sufficient
 * (mtime+size, hashed) to flip the freshness bit; we don't need per-file
 * granularity here — the next `sync` will do the precise work.
 *
 * Mtime+size is the same fast-path signal {@link FingerprintEntry} uses
 * (R18). Hashing the per-file (path,size,mtime) tuples produces a compact
 * fingerprint that survives state-file bloat over time.
 */
export interface SourceFingerprint {
  /** Number of files included in the aggregate (sanity-check signal). */
  fileCount: number;
  /** Hex sha256 of `path|size|mtimeMs` lines, sorted by path. */
  aggregateHash: string;
}

/**
 * The default "NoActive" state — used when the file doesn't exist, when it
 * fails to parse, when `schemaVersion` doesn't match, or when the structure
 * is otherwise invalid (R42 graceful degradation).
 */
export function defaultState(): StateFile {
  return {
    schemaVersion: STATE_FILE_SCHEMA_VERSION,
    activeProfile: null,
    materializedAt: null,
    resolvedSources: [],
    fingerprint: { schemaVersion: FINGERPRINT_SCHEMA_VERSION, files: {} },
    externalTrustNotices: [],
    rootClaudeMdSection: null,
    sourceFingerprint: null,
  };
}

/**
 * A held lock on `.claude-profiles/.meta/lock`. Returned from `acquireLock`. The
 * caller MUST call `release()` (or use `withLock`) — the destructor pattern
 * is enforced by signal handlers registered when the lock is acquired.
 */
export interface LockHandle {
  /** Absolute path to the lock file we hold. */
  path: string;
  /** PID we wrote into the lock file (this process's PID). */
  pid: number;
  /** ISO 8601 timestamp written when the lock was acquired. */
  acquiredAt: string;
  /** Idempotent release (safe to call after signal-handler release). */
  release(): Promise<void>;
}
