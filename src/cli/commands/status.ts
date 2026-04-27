/**
 * `status` command (R31, R40). Read-only: prints active profile, drift
 * summary, and any unresolved warnings. JSON payload mirrors the shape with
 * structured fields that round-trip cleanly.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";
import process from "node:process";

import { detectDrift } from "../../drift/index.js";
import {
  buildPaths,
  listProfiles,
  type ProfileManifest,
} from "../../resolver/index.js";
import { buildStatePaths, readStateFile } from "../../state/index.js";
import {
  formatStateWarning,
  meaningfulStateWarning,
  timestampWithRelative,
} from "../format.js";
import { createStyle, resolveNoColor, type OutputChannel } from "../output.js";

export interface StatusPayload {
  activeProfile: string | null;
  materializedAt: string | null;
  drift: {
    fingerprintOk: boolean;
    modified: number;
    added: number;
    deleted: number;
    /**
     * cw6/T5: count of `unrecoverable` entries — currently only the project-
     * root CLAUDE.md when the user has deleted/malformed its markers. Carried
     * as its own field so consumers can treat it as a distinct severity (the
     * standard discard/persist gate cannot resolve it).
     */
    unrecoverable: number;
    total: number;
  };
  warnings: Array<{ code: string; detail: string }>;
}

export interface StatusOptions {
  cwd: string;
  output: OutputChannel;
  /** When true, force colour off (additive with NO_COLOR env). Default false. */
  noColor?: boolean;
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
        unrecoverable: counts.unrecoverable,
        total: drift.entries.length,
      },
      warnings,
    };
    opts.output.json(payload);
    return 0;
  }

  const style = createStyle({
    isTty: Boolean(process.stdout.isTTY),
    platform: process.platform,
    noColor: resolveNoColor(opts.noColor === true),
  });

  if (state.activeProfile === null) {
    // Differentiate "no profiles at all" from "profiles exist but none
    // active" — the next-step the user needs to take is different.
    const names = await listProfiles({ projectRoot: opts.cwd });
    if (names.length === 0) {
      opts.output.print("(no active profile — run `claude-profiles new <name>` first)");
    } else {
      opts.output.print("(no active profile — run `claude-profiles use <name>` to activate)");
    }
  } else {
    opts.output.print(`active: ${state.activeProfile}`);
    // Surface the active profile's manifest description on a dim line so
    // status answers "what is this profile for?" at a glance. Best-effort
    // (a malformed profile.json should not crash status).
    const description = await tryLoadActiveDescription(opts.cwd, state.activeProfile);
    if (description !== null && description !== "") {
      opts.output.print(style.dim(`  ${description}`));
    }
    opts.output.print(`materialized: ${timestampWithRelative(state.materializedAt)}`);
    if (!drift.fingerprintOk) {
      opts.output.print("drift: (state file degraded — drift not assessable)");
    } else if (drift.entries.length === 0) {
      // Visual style consistency with init: clean state earns a green ok glyph.
      opts.output.print(style.ok("drift: clean"));
    } else {
      // Show unrecoverable in the summary only when non-zero so existing
      // golden output for the common case (modified/added/deleted only)
      // stays unchanged.
      const tail = counts.unrecoverable > 0
        ? `, ${counts.unrecoverable} unrecoverable`
        : "";
      opts.output.print(
        `drift: ${drift.entries.length} (${counts.modified} modified, ${counts.added} added, ${counts.deleted} deleted${tail})`,
      );
    }
  }

  if (warning && warning.code !== "Missing") {
    opts.output.warn(formatStateWarning(warning));
  }
  return 0;
}

/**
 * Best-effort manifest read for the active profile's description. We don't
 * want a malformed profile.json to blow up `status` — degrade to "no
 * description shown". `validate` will surface the details.
 */
async function tryLoadActiveDescription(
  projectRoot: string,
  profileName: string,
): Promise<string | null> {
  try {
    const paths = buildPaths(projectRoot);
    const raw = await fs.readFile(
      path.join(paths.profilesDir, profileName, "profile.json"),
      "utf8",
    );
    const parsed = JSON.parse(raw) as ProfileManifest | unknown;
    if (typeof parsed === "object" && parsed !== null) {
      const desc = (parsed as ProfileManifest).description;
      return typeof desc === "string" ? desc : null;
    }
    return null;
  } catch {
    return null;
  }
}

function countByStatus(
  entries: ReadonlyArray<{ status: "modified" | "added" | "deleted" | "unrecoverable" }>,
): { modified: number; added: number; deleted: number; unrecoverable: number } {
  let modified = 0;
  let added = 0;
  let deleted = 0;
  let unrecoverable = 0;
  for (const e of entries) {
    if (e.status === "modified") modified++;
    else if (e.status === "added") added++;
    else if (e.status === "deleted") deleted++;
    else if (e.status === "unrecoverable") unrecoverable++;
  }
  return { modified, added, deleted, unrecoverable };
}
