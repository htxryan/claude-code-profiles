/**
 * `drift` command (R20, R40). Read-only: per-file drift report with
 * provenance for the active materialization.
 *
 * Special mode `--pre-commit-warn`: delegates to E4's `preCommitWarn`
 * which is fail-open (always exits 0) and writes a brief warning to stderr.
 */

import { detectDrift, preCommitWarn, type DriftReport } from "../../drift/index.js";
import { buildStatePaths } from "../../state/index.js";
import { formatStateWarning, meaningfulStateWarning } from "../format.js";
import type { OutputChannel } from "../output.js";

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
  warning: { code: string; detail: string } | null;
}

export interface DriftOptions {
  cwd: string;
  output: OutputChannel;
  preCommitWarn: boolean;
}

export async function runDrift(opts: DriftOptions): Promise<number> {
  const paths = buildStatePaths(opts.cwd);

  if (opts.preCommitWarn) {
    // Hook entry point — fail-open by contract (R25a). Delegate fully.
    const r = await preCommitWarn(paths);
    return r.exitCode;
  }

  const report = await detectDrift(paths);

  if (opts.output.jsonMode) {
    opts.output.json(reportToPayload(report));
    return 0;
  }

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
    opts.output.print("drift: clean");
    return 0;
  }

  opts.output.print(`active: ${report.active}`);
  opts.output.print(
    `drift: ${report.entries.length} file(s) (scanned ${report.scannedFiles}, fast=${report.fastPathHits}, slow=${report.slowPathHits})`,
  );
  for (const e of report.entries) {
    const prov = e.provenance.map((p) => p.id).join(", ");
    // padEnd(13) widens for 'unrecoverable' so columns line up; older
    // statuses (modified/added/deleted) still fit comfortably.
    opts.output.print(`  ${e.status.padEnd(13)} ${e.relPath}${prov ? `  (from: ${prov})` : ""}`);
    // cw6/T5: surface the actionable remediation immediately under the
    // entry so the user doesn't have to hunt for it.
    if (e.error) {
      opts.output.print(`                 ${e.error}`);
    }
  }
  if (report.warning && report.warning.code !== "Missing") {
    opts.output.warn(formatStateWarning(report.warning));
  }
  return 0;
}

function reportToPayload(report: DriftReport): DriftCommandPayload {
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
    warning: meaningfulStateWarning(report.warning),
  };
}
