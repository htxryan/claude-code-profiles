/**
 * Drift detection (R18, R19, R20, R46). Consumes E3's
 * `compareFingerprintWithMetrics` to compute per-file drift against the
 * recorded fingerprint, then enriches the result with provenance from
 * `.state.json` to produce a `DriftReport`.
 *
 * cw6/T5 (R46): the project-root `CLAUDE.md` is special-cased. Whole-file
 * fingerprinting (R18/R19) is the wrong scope here — the user owns every
 * byte outside the `<!-- c3p:vN:begin/end -->` markers. We
 * fingerprint only the section bytes between the markers and compare
 * against `state.rootClaudeMdSection.contentHash`. Edits OUTSIDE the
 * markers are invisible to drift; the AC-7 invariant.
 *
 * Read-only and lock-free (epic invariant). May return slightly stale data
 * during a concurrent write — acceptable per R43.
 */

import { promises as fs } from "node:fs";

import { parseMarkers } from "../markers.js";
import { compareFingerprintWithMetrics, hashBytes } from "../state/fingerprint.js";
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
      destination: ".claude",
    });
  }

  // cw6/T5 (R46): section-only drift check for project-root CLAUDE.md. Only
  // runs when the prior materialize recorded a section fingerprint — legacy
  // state files (cw6-pre) and clean .claude/-only profiles leave the field
  // null and we correctly skip the check.
  if (state.rootClaudeMdSection !== null && state.rootClaudeMdSection !== undefined) {
    const sectionEntry = await compareRootClaudeMdSection(
      paths,
      state.rootClaudeMdSection,
      [...state.resolvedSources],
    );
    if (sectionEntry !== null) {
      entries.push(sectionEntry);
    }
  }

  // Re-sort: the .claude/ entries from compareFingerprintWithMetrics arrive
  // pre-sorted, but the appended projectRoot entry (always "CLAUDE.md") may
  // need to slot into the lex order to maintain the contract invariant.
  entries.sort((a, b) => {
    // Stable secondary sort on destination so a hypothetical future
    // collision (a `.claude/CLAUDE.md` AND a project-root `CLAUDE.md` both
    // drifted) renders deterministically — `.claude` first, `projectRoot`
    // second by alphabetic destination.
    const cmp = a.relPath.localeCompare(b.relPath);
    if (cmp !== 0) return cmp;
    const da = a.destination ?? ".claude";
    const db = b.destination ?? ".claude";
    return da.localeCompare(db);
  });

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

/**
 * cw6/T5 (R46): compare the live project-root CLAUDE.md section against the
 * recorded section fingerprint. Returns:
 *   - null  → no drift (section bytes unchanged) OR the file/markers state
 *             is benign-skipped (we never produce false positives)
 *   - entry → drift detected; status is `'modified'` for content drift, or
 *             `'unrecoverable'` when the live file is missing/markers are
 *             gone (the standard discard/persist gate cannot resolve this —
 *             the user must run `init` to repair the markers first)
 *
 * Why no fast-path: there's no per-section mtime to short-circuit on. The
 * file's whole-file mtime is meaningless for section drift (the user could
 * have touched the file without changing the section). We always read +
 * parse + hash the section bytes. This is fine: it's exactly one extra file
 * read per drift call, and the file is small (a CLAUDE.md is kilobytes).
 */
async function compareRootClaudeMdSection(
  paths: StatePaths,
  recorded: { size: number; contentHash: string },
  provenance: DriftEntry["provenance"],
): Promise<DriftEntry | null> {
  let content: string;
  try {
    content = await fs.readFile(paths.rootClaudeMdFile, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      // We have a recorded section but the file is gone. The file itself
      // is structurally lost — `unrecoverable` rather than `deleted` because
      // the user's remediation is `init` (recreate the file with markers),
      // not the standard discard/persist gate.
      return {
        relPath: "CLAUDE.md",
        status: "unrecoverable",
        provenance,
        destination: "projectRoot",
        error: `project-root CLAUDE.md is missing — run \`c3p init\` to recreate, then \`c3p validate\` to verify (file: ${paths.rootClaudeMdFile}; see docs/migration/cw6-section-ownership.md)`,
      };
    }
    // Other IO errors (EACCES, EIO) propagate — those are not user-content
    // drift signals; they're environment problems the caller should see.
    throw err;
  }

  const parsed = parseMarkers(content);
  if (!parsed.found) {
    // Both "absent" (file present, no markers) and "malformed" (lone /
    // partial / multi-block / version-mismatch) collapse to the same
    // `unrecoverable` status: the user has broken the structural contract.
    // The error message names the file and points at the two commands that
    // can fix it. We deliberately do NOT include the raw parse reason —
    // users don't care about "lone :begin"; they care about the next
    // command to run.
    return {
      relPath: "CLAUDE.md",
      status: "unrecoverable",
      provenance,
      destination: "projectRoot",
      error: `project-root CLAUDE.md markers are missing or malformed — run \`c3p init\` to repair, then \`c3p validate\` to verify (file: ${paths.rootClaudeMdFile}; see docs/migration/cw6-section-ownership.md)`,
    };
  }

  // Section is locatable: compare bytes between markers against the recorded
  // fingerprint. Hash the section as utf8 bytes, matching the materialize-
  // side hash (the splice writer encodes via Buffer.from(sectionBytes,
  // "utf8") before hashing — see applyRootSplice in src/state/materialize.ts).
  //
  // cw6.1 followup: a size mismatch is a sufficient drift signal on its own
  // (sha256 collisions across different byte lengths are not the threat
  // model — content with a different size is by definition different
  // content). Returning early on size mismatch saves the hash on a guaranteed
  // drift, and short-circuits content equality on size match without hashing
  // when sizes differ. We still hash on size-match because two different
  // sections of equal length must hash to the same value to be equal, and
  // the recorded `contentHash` is the source of truth for byte-equality.
  const sectionBuf = Buffer.from(parsed.section, "utf8");
  if (sectionBuf.length !== recorded.size) {
    return {
      relPath: "CLAUDE.md",
      status: "modified",
      provenance,
      destination: "projectRoot",
    };
  }
  const liveHash = hashBytes(sectionBuf);
  if (liveHash === recorded.contentHash) {
    return null;
  }
  return {
    relPath: "CLAUDE.md",
    status: "modified",
    provenance,
    destination: "projectRoot",
  };
}
