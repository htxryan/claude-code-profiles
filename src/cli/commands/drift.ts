/**
 * `drift` command (R20, R40). Read-only: per-file drift report with
 * provenance for the active materialization.
 *
 * Special mode `--pre-commit-warn`: delegates to E4's `preCommitWarn`
 * which is fail-open (always exits 0) and writes a brief warning to stderr.
 */

import { detectDrift } from "../../drift/detect.js";
import { preCommitWarn } from "../../drift/pre-commit.js";
import type { DriftReport } from "../../drift/types.js";
import { buildStatePaths } from "../../state/paths.js";
import { formatStateWarning } from "../format.js";
import type { OutputChannel } from "../output.js";

export interface DriftCommandPayload {
  schemaVersion: number;
  active: string | null;
  fingerprintOk: boolean;
  entries: Array<{
    relPath: string;
    status: "modified" | "added" | "deleted";
    provenance: Array<{ id: string; kind: "ancestor" | "include" | "profile"; rootPath: string; external: boolean }>;
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
    opts.output.print(`  ${e.status.padEnd(8)} ${e.relPath}${prov ? `  (from: ${prov})` : ""}`);
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
    })),
    scannedFiles: report.scannedFiles,
    fastPathHits: report.fastPathHits,
    slowPathHits: report.slowPathHits,
    warning: report.warning && report.warning.code !== "Missing"
      ? {
          code: report.warning.code,
          detail:
            report.warning.code === "ParseError" || report.warning.code === "SchemaMismatch"
              ? report.warning.detail
              : "",
        }
      : null,
  };
}
