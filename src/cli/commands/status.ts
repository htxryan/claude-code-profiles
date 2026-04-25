/**
 * `status` command (R31, R40). Read-only: prints active profile, drift
 * summary, and any unresolved warnings. JSON payload mirrors the shape with
 * structured fields that round-trip cleanly.
 */

import { detectDrift } from "../../drift/index.js";
import { buildStatePaths, readStateFile } from "../../state/index.js";
import {
  formatStateWarning,
  meaningfulStateWarning,
  timestampWithRelative,
} from "../format.js";
import type { OutputChannel } from "../output.js";

export interface StatusPayload {
  activeProfile: string | null;
  materializedAt: string | null;
  drift: {
    fingerprintOk: boolean;
    modified: number;
    added: number;
    deleted: number;
    total: number;
  };
  warnings: Array<{ code: string; detail: string }>;
}

export interface StatusOptions {
  cwd: string;
  output: OutputChannel;
}

export async function runStatus(opts: StatusOptions): Promise<number> {
  const paths = buildStatePaths(opts.cwd);
  const { state, warning } = await readStateFile(paths);
  const drift = await detectDrift(paths);

  const counts = countByStatus(drift.entries);
  const stateWarning = meaningfulStateWarning(warning);
  const warnings = stateWarning ? [stateWarning] : [];

  if (opts.output.jsonMode) {
    const payload: StatusPayload = {
      activeProfile: state.activeProfile,
      materializedAt: state.materializedAt,
      drift: {
        fingerprintOk: drift.fingerprintOk,
        modified: counts.modified,
        added: counts.added,
        deleted: counts.deleted,
        total: drift.entries.length,
      },
      warnings,
    };
    opts.output.json(payload);
    return 0;
  }

  if (state.activeProfile === null) {
    opts.output.print("(no active profile — run `claude-profiles use <name>` to activate)");
  } else {
    opts.output.print(`active: ${state.activeProfile}`);
    opts.output.print(`materialized: ${timestampWithRelative(state.materializedAt)}`);
    if (!drift.fingerprintOk) {
      opts.output.print("drift: (state file degraded — drift not assessable)");
    } else if (drift.entries.length === 0) {
      opts.output.print("drift: clean");
    } else {
      opts.output.print(
        `drift: ${drift.entries.length} (${counts.modified} modified, ${counts.added} added, ${counts.deleted} deleted)`,
      );
    }
  }

  if (warning && warning.code !== "Missing") {
    opts.output.warn(formatStateWarning(warning));
  }
  return 0;
}

function countByStatus(
  entries: ReadonlyArray<{ status: "modified" | "added" | "deleted" }>,
): { modified: number; added: number; deleted: number } {
  let modified = 0;
  let added = 0;
  let deleted = 0;
  for (const e of entries) {
    if (e.status === "modified") modified++;
    else if (e.status === "added") added++;
    else if (e.status === "deleted") deleted++;
  }
  return { modified, added, deleted };
}
