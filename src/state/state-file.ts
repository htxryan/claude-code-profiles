/**
 * `.state.json` IO. R14, R14a, R42.
 *
 *  - Writes: temp-file + atomic rename, fsync of file and parent dir (R14a).
 *  - Reads: tolerate missing/unparseable/schema-mismatched files by returning
 *    `defaultState()` with a warning (R42). Never throws on a malformed state
 *    file — the rest of the system treats that as NoActive.
 */

import { promises as fs } from "node:fs";

import { atomicWriteFile, uniqueAtomicTmpPath } from "./atomic.js";
import type { StatePaths } from "./paths.js";
import {
  STATE_FILE_SCHEMA_VERSION,
  FINGERPRINT_SCHEMA_VERSION,
  type StateFile,
  defaultState,
} from "./types.js";

/**
 * Result of a state-file read. `warning` is non-null when the on-disk file
 * was missing/unparseable/schema-mismatched and we degraded to defaultState.
 * Callers (E5 status) decide whether to surface the warning to the user.
 */
export interface ReadStateResult {
  state: StateFile;
  warning: StateReadWarning | null;
}

export type StateReadWarning =
  | { code: "Missing"; path: string }
  | { code: "ParseError"; path: string; detail: string }
  | { code: "SchemaMismatch"; path: string; detail: string };

/**
 * Read `.state.json`, validating shape. Never throws on file-content
 * problems — those produce `defaultState()` + warning per R42. Filesystem
 * errors other than ENOENT (permission denied, IO error) are surfaced.
 */
export async function readStateFile(paths: StatePaths): Promise<ReadStateResult> {
  let raw: string;
  try {
    raw = await fs.readFile(paths.stateFile, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      return { state: defaultState(), warning: { code: "Missing", path: paths.stateFile } };
    }
    throw err;
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err: unknown) {
    const detail = err instanceof Error ? err.message : String(err);
    return {
      state: defaultState(),
      warning: { code: "ParseError", path: paths.stateFile, detail },
    };
  }

  const validation = validateStateShape(parsed);
  if (!validation.ok) {
    return {
      state: defaultState(),
      warning: {
        code: "SchemaMismatch",
        path: paths.stateFile,
        detail: validation.detail,
      },
    };
  }
  return { state: validation.value, warning: null };
}

interface Ok {
  ok: true;
  value: StateFile;
}
interface Bad {
  ok: false;
  detail: string;
}

/**
 * Validate the parsed JSON against the StateFile schema. We accept schemas
 * with the current version only; mismatches produce a SchemaMismatch warning
 * so we degrade gracefully (R42). The check is intentionally narrow — we
 * only verify enough structure to safely consume the file.
 */
function validateStateShape(value: unknown): Ok | Bad {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return { ok: false, detail: "top-level value is not a JSON object" };
  }
  const obj = value as Record<string, unknown>;
  if (obj["schemaVersion"] !== STATE_FILE_SCHEMA_VERSION) {
    return {
      ok: false,
      detail: `schemaVersion ${JSON.stringify(obj["schemaVersion"])} (expected ${STATE_FILE_SCHEMA_VERSION})`,
    };
  }
  // activeProfile: string | null
  const activeProfile = obj["activeProfile"];
  if (activeProfile !== null && typeof activeProfile !== "string") {
    return { ok: false, detail: "activeProfile must be string or null" };
  }
  // materializedAt: string | null
  const materializedAt = obj["materializedAt"];
  if (materializedAt !== null && typeof materializedAt !== "string") {
    return { ok: false, detail: "materializedAt must be string or null" };
  }
  // resolvedSources: array
  if (!Array.isArray(obj["resolvedSources"])) {
    return { ok: false, detail: "resolvedSources must be an array" };
  }
  // fingerprint: object with schemaVersion + files
  const fp = obj["fingerprint"];
  if (typeof fp !== "object" || fp === null || Array.isArray(fp)) {
    return { ok: false, detail: "fingerprint must be a JSON object" };
  }
  const fingerprint = fp as Record<string, unknown>;
  if (fingerprint["schemaVersion"] !== FINGERPRINT_SCHEMA_VERSION) {
    return {
      ok: false,
      detail: `fingerprint.schemaVersion ${JSON.stringify(fingerprint["schemaVersion"])} (expected ${FINGERPRINT_SCHEMA_VERSION})`,
    };
  }
  if (
    typeof fingerprint["files"] !== "object" ||
    fingerprint["files"] === null ||
    Array.isArray(fingerprint["files"])
  ) {
    return { ok: false, detail: "fingerprint.files must be a JSON object" };
  }
  // Validate per-file entries (Sonnet review #3): the R42 contract is "never
  // throws on malformed state". A corrupt entry like `{"a.md": null}` would
  // pass the previous shallow check and crash E4's compareFingerprint with
  // a null deref. We require numeric size/mtimeMs and string contentHash.
  for (const [k, v] of Object.entries(fingerprint["files"] as Record<string, unknown>)) {
    if (typeof v !== "object" || v === null || Array.isArray(v)) {
      return { ok: false, detail: `fingerprint.files[${JSON.stringify(k)}] must be an object` };
    }
    const e = v as Record<string, unknown>;
    if (typeof e["size"] !== "number" || !Number.isFinite(e["size"])) {
      return { ok: false, detail: `fingerprint.files[${JSON.stringify(k)}].size must be a number` };
    }
    if (typeof e["mtimeMs"] !== "number" || !Number.isFinite(e["mtimeMs"])) {
      return { ok: false, detail: `fingerprint.files[${JSON.stringify(k)}].mtimeMs must be a number` };
    }
    if (typeof e["contentHash"] !== "string") {
      return { ok: false, detail: `fingerprint.files[${JSON.stringify(k)}].contentHash must be a string` };
    }
  }
  if (!Array.isArray(obj["externalTrustNotices"])) {
    return { ok: false, detail: "externalTrustNotices must be an array" };
  }
  // cw6/T4: rootClaudeMdSection is OPTIONAL. Legacy state files (written
  // before cw6 landed) do not have this key; tolerate its absence by treating
  // it as null. When present, validate shape so a corrupt entry doesn't
  // crash the section-drift comparator (mirrors fingerprint.files entry
  // validation rationale).
  const rootSec = obj["rootClaudeMdSection"];
  if (rootSec !== undefined && rootSec !== null) {
    if (typeof rootSec !== "object" || Array.isArray(rootSec)) {
      return { ok: false, detail: "rootClaudeMdSection must be an object or null" };
    }
    const r = rootSec as Record<string, unknown>;
    if (typeof r["size"] !== "number" || !Number.isFinite(r["size"])) {
      return { ok: false, detail: "rootClaudeMdSection.size must be a number" };
    }
    if (typeof r["contentHash"] !== "string") {
      return { ok: false, detail: "rootClaudeMdSection.contentHash must be a string" };
    }
  }
  return { ok: true, value: obj as unknown as StateFile };
}

/**
 * Atomically write `.state.json`. Ensures `.claude-profiles/` exists, writes
 * to `.state.json.tmp`, fsyncs, and renames into place (R14a). The directory
 * fsync after rename is handled inside `atomicWriteFile`.
 *
 * Pretty-printed (2-space indent) so `git diff` on the .state.json (if
 * accidentally checked in) is readable. Trailing newline for tool friendliness.
 */
export async function writeStateFile(paths: StatePaths, state: StateFile): Promise<void> {
  // tmpDir is `.claude-profiles/.meta/tmp/` — recursive mkdir creates both
  // `.meta/` and the tmp staging directory in one call. The state file lives
  // beside tmpDir under `.meta/`, so we don't need a separate stateFile-parent
  // mkdir.
  await fs.mkdir(paths.tmpDir, { recursive: true });
  const json = `${JSON.stringify(state, null, 2)}\n`;
  // Per-call unique tmp path (multi-reviewer P2 Gemini #2): even though
  // writes happen under the lock, defense-in-depth prevents two concurrent
  // writers from clobbering each other's staging file.
  const tmpPath = uniqueAtomicTmpPath(paths.tmpDir, paths.stateFile);
  try {
    await atomicWriteFile(paths.stateFile, tmpPath, json);
  } catch (err) {
    await fs.unlink(tmpPath).catch(() => undefined);
    throw err;
  }
}
