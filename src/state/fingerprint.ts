/**
 * Two-tier fingerprint (R18). The fast path compares file size + mtime
 * against recorded values to short-circuit unchanged files. The slow path
 * recomputes a content hash only for files where metadata indicates a
 * possible change (or a missing recorded entry).
 *
 * Hash algorithm: sha256. Chosen for collision resistance over speed —
 * fingerprint computation is dominated by IO, not hashing, for the file
 * sizes typical in `.claude/` (kilobytes, not megabytes). Hex-encoded for
 * stable JSON serialization.
 *
 * E4 (Drift) is the consumer of `compareFingerprint`; we expose computation
 * here so E3's materialize() can record fingerprints at the moment of write
 * (when bytes are still in memory) without re-reading from disk.
 */

import { createHash } from "node:crypto";
import { promises as fs } from "node:fs";
import * as path from "node:path";

import type { MergedFile } from "../merge/types.js";

import {
  FINGERPRINT_SCHEMA_VERSION,
  type Fingerprint,
  type FingerprintEntry,
} from "./types.js";

/** Compute sha256 of bytes; hex-encoded. */
export function hashBytes(bytes: Buffer): string {
  return createHash("sha256").update(bytes).digest("hex");
}

/**
 * Compute a fingerprint from an in-memory list of merged files at the moment
 * they're about to be written. mtimeMs is filled in after the write completes
 * via `recordMtimes` — until then, we record `mtimeMs: 0` as a sentinel.
 *
 * Splitting hash-from-bytes (now) from mtime-from-disk (after write) avoids
 * a redundant read-after-write while preserving the two-tier semantic:
 * future drift detection still has a content hash to fall back on.
 */
export function fingerprintFromMergedFiles(files: ReadonlyArray<MergedFile>): Fingerprint {
  const entries: Record<string, FingerprintEntry> = {};
  for (const f of files) {
    entries[f.path] = {
      size: f.bytes.length,
      mtimeMs: 0,
      contentHash: hashBytes(f.bytes),
    };
  }
  return { schemaVersion: FINGERPRINT_SCHEMA_VERSION, files: entries };
}

/**
 * Stat each file under `claudeDir` referenced by `fingerprint` and fill in
 * mtimeMs values. Called immediately after a successful pending-prior commit
 * to capture the post-rename mtimes that drift detection will compare against.
 *
 * Any entry whose file is missing on disk is left with mtimeMs=0; the next
 * drift check will treat it as drifted (deleted).
 */
export async function recordMtimes(
  claudeDir: string,
  fingerprint: Fingerprint,
): Promise<Fingerprint> {
  const out: Record<string, FingerprintEntry> = {};
  for (const [relPath, entry] of Object.entries(fingerprint.files)) {
    try {
      const stat = await fs.stat(path.join(claudeDir, relPath));
      out[relPath] = { ...entry, mtimeMs: stat.mtimeMs };
    } catch {
      out[relPath] = { ...entry, mtimeMs: 0 };
    }
  }
  return { schemaVersion: fingerprint.schemaVersion, files: out };
}

/**
 * Walk the live `.claude/` tree and produce a fingerprint by reading every
 * file. Used by drift detection's slow-path / forced-recompute mode and as
 * the baseline for a fingerprint comparison. The fast path in
 * `compareFingerprint` avoids calling this for files whose stat metadata
 * has not changed.
 *
 * Returns posix-relative keys to match MergedFile.path conventions.
 */
export async function fingerprintTree(claudeDir: string): Promise<Fingerprint> {
  const out: Record<string, FingerprintEntry> = {};
  await walk(claudeDir, claudeDir, out, "hash");
  return { schemaVersion: FINGERPRINT_SCHEMA_VERSION, files: out };
}

/**
 * Metadata-only walk: returns stat-derived entries (size + mtime) without
 * reading file bytes or computing content hashes. Used by `compareFingerprint`
 * to avoid sha256-ing every file when most haven't changed.
 *
 * Two-tier (multi-reviewer P3, both): the fast path compares stat metadata;
 * the slow path opens and hashes only the small subset of files whose
 * metadata signals a possible change.
 */
async function fingerprintTreeMetadataOnly(
  claudeDir: string,
): Promise<Record<string, { size: number; mtimeMs: number; abs: string }>> {
  const out: Record<string, { size: number; mtimeMs: number; abs: string }> = {};
  await walkMetadata(claudeDir, claudeDir, out);
  return out;
}

async function walkMetadata(
  root: string,
  current: string,
  out: Record<string, { size: number; mtimeMs: number; abs: string }>,
): Promise<void> {
  let entries;
  try {
    entries = await fs.readdir(current, { withFileTypes: true });
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return;
    throw err;
  }
  for (const e of entries) {
    const abs = path.join(current, e.name);
    if (e.isDirectory()) {
      await walkMetadata(root, abs, out);
    } else if (e.isFile()) {
      const rel = path.relative(root, abs).split(path.sep).join("/");
      // Tolerate stat ENOENT (Opus review #4): a file deleted between
      // readdir and stat (editor atomic-write swap) shouldn't abort drift
      // detection. Skip the entry — the next pass will reflect actual state.
      try {
        const stat = await fs.stat(abs);
        out[rel] = { size: stat.size, mtimeMs: stat.mtimeMs, abs };
      } catch (err: unknown) {
        if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
      }
    }
  }
}

async function walk(
  root: string,
  current: string,
  out: Record<string, FingerprintEntry>,
  mode: "hash",
): Promise<void> {
  let entries;
  try {
    entries = await fs.readdir(current, { withFileTypes: true });
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return;
    throw err;
  }
  for (const e of entries) {
    const abs = path.join(current, e.name);
    if (e.isDirectory()) {
      await walk(root, abs, out, mode);
    } else if (e.isFile()) {
      const rel = path.relative(root, abs).split(path.sep).join("/");
      // Tolerate stat/readFile ENOENT (Opus review #4): a file deleted
      // between readdir and stat/readFile shouldn't abort the walk. Skip
      // — the next walk picks up reality.
      try {
        const stat = await fs.stat(abs);
        const bytes = await fs.readFile(abs);
        out[rel] = {
          size: stat.size,
          mtimeMs: stat.mtimeMs,
          contentHash: hashBytes(bytes),
        };
      } catch (err: unknown) {
        if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
      }
    }
    // Symlinks and other non-file entries are skipped — `.claude/` is a copy
    // tree by R39, and any symlinks would be artifacts the user created post-
    // materialization. Skipping makes drift detection robust to those.
  }
}

/**
 * One file's drift status. Used by E4 but produced by the comparator here so
 * E3 can expose a self-contained fingerprint module. `kind` distinguishes
 * the three drift cases (R19) plus an unchanged result for fast-path filtering.
 */
export type DriftKind = "unchanged" | "modified" | "added" | "deleted";

export interface FileDrift {
  relPath: string;
  kind: DriftKind;
}

/**
 * Compare a recorded fingerprint against the live tree at `claudeDir`. Two-
 * tier (multi-reviewer P3, both): the fast path is a metadata-only walk
 * (stat for size+mtime); only files whose metadata signals a possible
 * change are opened and sha256-hashed.
 *
 * Returns one entry per relPath in the union of recorded + live trees.
 * Unchanged files are included so callers can compute summaries by kind.
 */
export async function compareFingerprint(
  claudeDir: string,
  recorded: Fingerprint,
): Promise<FileDrift[]> {
  const liveMeta = await fingerprintTreeMetadataOnly(claudeDir);

  const recordedKeys = new Set(Object.keys(recorded.files));
  const liveKeys = new Set(Object.keys(liveMeta));
  const union = new Set([...recordedKeys, ...liveKeys]);

  const out: FileDrift[] = [];
  for (const relPath of union) {
    const r = recorded.files[relPath];
    const l = liveMeta[relPath];
    if (!r && l) {
      out.push({ relPath, kind: "added" });
      continue;
    }
    if (r && !l) {
      out.push({ relPath, kind: "deleted" });
      continue;
    }
    if (r && l) {
      // Fast path: stat metadata matches → definitely unchanged.
      if (r.size === l.size && r.mtimeMs !== 0 && l.mtimeMs === r.mtimeMs) {
        out.push({ relPath, kind: "unchanged" });
        continue;
      }
      // Slow path: hash this single file and compare against the recorded
      // hash. Avoids hashing the whole tree. Tolerate ENOENT (Opus review
      // #4): the file may have been deleted between metadata walk and
      // readFile; treat as deleted.
      try {
        const bytes = await fs.readFile(l.abs);
        const liveHash = hashBytes(bytes);
        out.push({
          relPath,
          kind: liveHash === r.contentHash ? "unchanged" : "modified",
        });
      } catch (err: unknown) {
        if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
        out.push({ relPath, kind: "deleted" });
      }
    }
  }
  out.sort((a, b) => a.relPath.localeCompare(b.relPath));
  return out;
}
