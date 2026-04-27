/**
 * `drift` command (R20, R40). Read-only: per-file drift report with
 * provenance for the active materialization.
 *
 * Special mode `--pre-commit-warn`: delegates to E4's `preCommitWarn`
 * which is fail-open (always exits 0) and writes a brief warning to stderr.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";

import process from "node:process";

import { detectDrift, preCommitWarn, type DriftReport } from "../../drift/index.js";
import { merge } from "../../merge/index.js";
import { resolve } from "../../resolver/index.js";
import { buildStatePaths, readStateFile, type StatePaths } from "../../state/index.js";
import { formatStateWarning, meaningfulStateWarning } from "../format.js";
import { createStyle, resolveNoColor, type OutputChannel } from "../output.js";
import { renderHeadPreview, renderUnifiedDiff } from "../preview.js";

export interface DriftCommandPayload {
  schemaVersion: number;
  active: string | null;
  fingerprintOk: boolean;
  entries: Array<{
    relPath: string;
    status: "modified" | "added" | "deleted" | "unrecoverable";
    provenance: Array<{ id: string; kind: "ancestor" | "include" | "profile"; rootPath: string; external: boolean }>;
    /**
     * cw6/T5: destination scope. Optional for back-compat with legacy
     * consumers that key on `relPath` alone — only set explicitly for
     * the project-root `CLAUDE.md` ('projectRoot').
     */
    destination?: ".claude" | "projectRoot";
    /**
     * cw6/T5: human-readable remediation when status is `unrecoverable`
     * (markers missing/malformed). Absent for ordinary modified/added/
     * deleted entries.
     */
    error?: string;
  }>;
  scannedFiles: number;
  fastPathHits: number;
  slowPathHits: number;
  /** azp: total bytes added (sum of live file sizes for `added` entries). */
  addedBytes: number;
  /** azp: total bytes removed (sum of recorded sizes for `deleted` entries). */
  removedBytes: number;
  /** azp: sum of |liveSize - resolvedSize| across `modified` entries. */
  changedBytes: number;
  warning: { code: string; detail: string } | null;
}

export interface DriftOptions {
  cwd: string;
  output: OutputChannel;
  preCommitWarn: boolean;
  /**
   * When true, the human summary line includes the scan-stats suffix
   * `(scanned N, fast=X, slow=Y)`. Off by default (skimmability — those
   * counts are diagnostic, not user-facing). The JSON payload always
   * exposes the same fields regardless of this flag.
   */
  verbose: boolean;
  /**
   * azp: when true, render unified-diff content for each `modified` entry
   * (and a head preview for `added` entries). Adds a small extra cost
   * (resolve + merge of the active profile + per-file reads) so it's
   * opt-in.
   */
  preview?: boolean;
  /** When true, force colour off (additive with NO_COLOR env). Default false. */
  noColor?: boolean;
}

export async function runDrift(opts: DriftOptions): Promise<number> {
  const paths = buildStatePaths(opts.cwd);

  if (opts.preCommitWarn) {
    // Hook entry point — fail-open by contract (R25a). Delegate fully.
    const r = await preCommitWarn(paths);
    return r.exitCode;
  }

  const report = await detectDrift(paths);

  // azp: compute byte counts for the summary line. We need the recorded
  // sizes from the state's fingerprint (for `deleted` and the resolved side
  // of `modified`) and the live sizes via stat (for `added` and the live
  // side of `modified`). Done upfront so the summary line and JSON payload
  // both see the same numbers.
  const byteCounts = await computeByteCounts(paths, report);

  if (opts.output.jsonMode) {
    opts.output.json(reportToPayload(report, byteCounts));
    return 0;
  }

  const style = createStyle({
    isTty: opts.output.isTty,
    platform: process.platform,
    noColor: resolveNoColor(opts.noColor === true),
  });

  if (!report.fingerprintOk) {
    if (report.active === null) {
      opts.output.print("(no active profile)");
    } else {
      opts.output.print("(state file degraded — drift not assessable)");
    }
    if (report.warning && report.warning.code !== "Missing") {
      opts.output.warn(formatStateWarning(report.warning));
    }
    return 0;
  }

  if (report.entries.length === 0) {
    opts.output.print(`active: ${report.active}`);
    // Green ok glyph for clean drift mirrors `status` (which already prints
    // `style.ok("drift: clean")`) — keeps the at-a-glance signal consistent
    // across the two read-only commands.
    opts.output.print(style.ok("drift: clean"));
    return 0;
  }

  // azp: when --preview is set, resolve+merge the active profile so we have
  // the canonical "should be" bytes to diff against the live drifted files.
  // Skipped when preview is off — drift's standard call path is read-only
  // and lock-free; we don't pay the resolve cost when the user didn't ask.
  const resolvedBytes = opts.preview ? await loadResolvedBytes(opts.cwd, report) : null;

  opts.output.print(`active: ${report.active}`);
  const scanSuffix = opts.verbose
    ? ` (scanned ${report.scannedFiles}, fast=${report.fastPathHits}, slow=${report.slowPathHits})`
    : "";
  // Byte-count intensity (bhq/3yy): the three magnitudes get independent
  // intensity bumps so an outsized delta in one column ("+45000") visually
  // outranks the others ("-12 ~5") instead of all three reading flat.
  const addedSeg = style.byteDelta(`+${byteCounts.addedBytes}`, byteCounts.addedBytes);
  const removedSeg = style.byteDelta(`-${byteCounts.removedBytes}`, byteCounts.removedBytes);
  const changedSeg = style.byteDelta(`~${byteCounts.changedBytes}`, byteCounts.changedBytes);
  opts.output.print(
    `drift: ${report.entries.length} file(s) (${addedSeg} ${removedSeg} ${changedSeg} bytes)${scanSuffix}`,
  );
  for (const e of report.entries) {
    const prov = e.provenance.map((p) => p.id).join(", ");
    // padEnd is applied to the RAW status word so column alignment is byte-
    // identical regardless of colour escapes (renderTable uses padEnd-visible
    // logic; this row is built by hand so we pad the visible width manually).
    const padded = e.status.padEnd(13);
    const colored = style.driftStatus(e.status, padded);
    // Provenance is dimmed so the status + relPath read first; the "from:"
    // tail is reference-grade context, not the headline.
    const provTail = prov ? `  ${style.dim(`(from: ${prov})`)}` : "";
    opts.output.print(`  ${colored} ${e.relPath}${provTail}`);
    // cw6/T5: surface the actionable remediation immediately under the
    // entry so the user doesn't have to hunt for it.
    if (e.error) {
      opts.output.print(`                 ${e.error}`);
    }
    if (opts.preview && (e.status === "modified" || e.status === "added")) {
      const body = await renderEntryPreview(paths, e, resolvedBytes);
      if (body !== null) {
        for (const line of body.split("\n")) {
          opts.output.print(`      ${line}`);
        }
      }
    }
  }
  if (report.warning && report.warning.code !== "Missing") {
    opts.output.warn(formatStateWarning(report.warning));
  }
  return 0;
}

interface ByteCounts {
  addedBytes: number;
  removedBytes: number;
  changedBytes: number;
}

/**
 * azp: aggregate byte counts for drift's summary line. `modified` files use
 * |liveSize - recordedSize|, `added` use the live size, `deleted` use the
 * recorded size. `unrecoverable` entries (project-root CLAUDE.md with
 * missing markers) contribute zero — there's no meaningful byte delta when
 * we can't even locate the section.
 *
 * Live sizes come from stat(); recorded sizes come from `state.fingerprint.
 * files`. The state file is already read by `detectDrift`, but it's cheap
 * to re-read here (single small JSON file, already in OS cache) — keeps
 * this helper self-contained.
 */
async function computeByteCounts(
  paths: StatePaths,
  report: DriftReport,
): Promise<ByteCounts> {
  if (!report.fingerprintOk || report.entries.length === 0) {
    return { addedBytes: 0, removedBytes: 0, changedBytes: 0 };
  }
  const { state } = await readStateFile(paths);
  let addedBytes = 0;
  let removedBytes = 0;
  let changedBytes = 0;
  for (const e of report.entries) {
    if (e.status === "unrecoverable") continue;
    const recordedSize =
      e.destination === "projectRoot"
        ? state.rootClaudeMdSection?.size ?? 0
        : state.fingerprint.files[e.relPath]?.size ?? 0;
    let liveSize = 0;
    try {
      const filePath = liveFilePathFor(paths, e);
      const stat = await fs.stat(filePath);
      liveSize = stat.size;
    } catch {
      liveSize = 0;
    }
    if (e.status === "added") {
      addedBytes += liveSize;
    } else if (e.status === "deleted") {
      removedBytes += recordedSize;
    } else if (e.status === "modified") {
      changedBytes += Math.abs(liveSize - recordedSize);
    }
  }
  return { addedBytes, removedBytes, changedBytes };
}

function liveFilePathFor(
  paths: Pick<StatePaths, "claudeDir" | "rootClaudeMdFile">,
  entry: { relPath: string; destination?: ".claude" | "projectRoot" },
): string {
  if (entry.destination === "projectRoot") return paths.rootClaudeMdFile;
  return path.join(paths.claudeDir, entry.relPath);
}

/**
 * azp: resolve+merge the active profile and return a path→bytes map. Used
 * by --preview rendering to compare drifted live files against the canonical
 * resolved bytes. Returns null when resolve fails (unusual, but possible if
 * a contributor disappeared between materialize and the drift check) — the
 * preview just skips its body in that case; the entry summary still prints.
 */
async function loadResolvedBytes(
  cwd: string,
  report: DriftReport,
): Promise<Map<string, Buffer> | null> {
  if (report.active === null) return null;
  try {
    const plan = await resolve(report.active, { projectRoot: cwd });
    const merged = await merge(plan);
    const map = new Map<string, Buffer>();
    for (const m of merged) {
      // Key by relPath only — we never have a `.claude/` and a `projectRoot`
      // collision in the report (the destination disambiguates), and the
      // resolver guarantees one merged entry per (relPath, destination).
      // For preview purposes the relPath is the unambiguous key into the
      // entry's bytes.
      map.set(m.path, Buffer.isBuffer(m.bytes) ? m.bytes : Buffer.from(m.bytes));
    }
    return map;
  } catch {
    return null;
  }
}

/**
 * azp: render a unified-diff or head preview body for one drift entry.
 * Returns null when there's nothing meaningful to render (binary, missing
 * resolved bytes, file unreadable). The caller indents the body when it
 * prints it.
 */
async function renderEntryPreview(
  paths: Pick<StatePaths, "claudeDir" | "rootClaudeMdFile">,
  entry: { relPath: string; status: string; destination?: ".claude" | "projectRoot" },
  resolvedBytes: Map<string, Buffer> | null,
): Promise<string | null> {
  const livePath = liveFilePathFor(paths, entry);
  if (entry.status === "modified") {
    if (resolvedBytes === null) return null;
    const resolved = resolvedBytes.get(entry.relPath);
    if (resolved === undefined) return null;
    let live: Buffer;
    try {
      live = await fs.readFile(livePath);
    } catch {
      return null;
    }
    return renderUnifiedDiff(resolved, live);
  }
  if (entry.status === "added") {
    let live: Buffer;
    try {
      live = await fs.readFile(livePath);
    } catch {
      return null;
    }
    return renderHeadPreview(live);
  }
  // 'deleted' entries get nothing extra per spec.
  return null;
}

function reportToPayload(report: DriftReport, byteCounts: ByteCounts): DriftCommandPayload {
  return {
    schemaVersion: report.schemaVersion,
    active: report.active,
    fingerprintOk: report.fingerprintOk,
    entries: report.entries.map((e) => ({
      relPath: e.relPath,
      status: e.status,
      provenance: e.provenance.map((p) => ({
        id: p.id,
        kind: p.kind,
        rootPath: p.rootPath,
        external: p.external,
      })),
      ...(e.destination !== undefined ? { destination: e.destination } : {}),
      ...(e.error !== undefined ? { error: e.error } : {}),
    })),
    scannedFiles: report.scannedFiles,
    fastPathHits: report.fastPathHits,
    slowPathHits: report.slowPathHits,
    addedBytes: byteCounts.addedBytes,
    removedBytes: byteCounts.removedBytes,
    changedBytes: byteCounts.changedBytes,
    warning: meaningfulStateWarning(report.warning),
  };
}
