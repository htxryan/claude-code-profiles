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
  resolve,
  type ProfileManifest,
} from "../../resolver/index.js";
import {
  buildStatePaths,
  computeSourceFingerprint,
  readStateFile,
} from "../../state/index.js";
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
  /**
   * azp: false when the active profile's source files have changed since the
   * last materialize (a teammate's `git pull` brings in new bytes that
   * `.claude/` hasn't picked up yet). True when source bytes match the
   * recorded sourceFingerprint. `null` when freshness cannot be determined
   * (no active profile, no recorded fingerprint on legacy state files, or
   * resolution failed — see {@link sourceFreshError}).
   */
  sourceFresh: boolean | null;
  /**
   * azp: hex aggregate of the live source files (mtime+size). Always populated
   * alongside `sourceFresh`; null in the same cases. Surfaced so JSON
   * consumers can compare across runs.
   */
  sourceFingerprint: string | null;
  /**
   * azp: human-readable reason source freshness could not be evaluated, when
   * `sourceFresh` is null but a freshness check WAS attempted (e.g. resolve
   * threw due to a deleted include). Absent in the legacy-state and
   * no-active cases — those are not failures.
   */
  sourceFreshError?: string;
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
  const freshness = await assessSourceFreshness(opts.cwd, state);

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
      sourceFresh: freshness.fresh,
      sourceFingerprint: freshness.liveAggregate,
      ...(freshness.error !== undefined ? { sourceFreshError: freshness.error } : {}),
      warnings,
    };
    opts.output.json(payload);
    return 0;
  }

  const style = createStyle({
    isTty: opts.output.isTty,
    platform: process.platform,
    noColor: resolveNoColor(opts.noColor === true),
  });

  if (state.activeProfile === null) {
    // Differentiate "no profiles at all" from "profiles exist but none
    // active" — the next-step the user needs to take is different.
    const names = await listProfiles({ projectRoot: opts.cwd });
    if (names.length === 0) {
      opts.output.print("(no active profile — run `c3p new <name>` first)");
    } else {
      opts.output.print("(no active profile — run `c3p use <name>` to activate)");
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
    // azp: surface stale-source signal AFTER the drift summary so users
    // reading top-down see "live edits" first, then "source bytes that need
    // pulling in" — both kinds of staleness are visible at a glance.
    if (freshness.fresh === false) {
      opts.output.print(
        style.warn(`source: updated since last materialize — perhaps run \`c3p sync\``),
      );
    } else if (freshness.error !== undefined) {
      // Resolve failed (e.g. missing include); surface so the user knows
      // freshness can't be evaluated and points at the same `validate`
      // remediation a swap would.
      opts.output.print(style.dim(`source: freshness unavailable — ${freshness.error}`));
    }
  }

  if (warning && warning.code !== "Missing") {
    opts.output.warn(formatStateWarning(warning));
  }
  return 0;
}

/**
 * azp: assess whether the active profile's source files have changed since
 * the last materialize. Returns a struct describing what to display:
 *
 *   - fresh:true  → live source matches recorded sourceFingerprint
 *   - fresh:false → live source differs (user needs to run `sync`)
 *   - fresh:null  → cannot determine (no active profile, no recorded
 *                   fingerprint on legacy state, or resolve threw)
 *
 * The freshness check is the inverse of drift: drift compares the live
 * `.claude/` against the recorded merged-output fingerprint; freshness
 * compares the live source files against the recorded source fingerprint.
 * They surface different problems (drifted edits vs stale `.claude/`).
 *
 * Performance budget (epic AC #1): must stay <500ms on a 1000-file profile.
 * Achieved by reusing the resolver (already O(N)) and the fast-path size+
 * mtime aggregate (no file reads, no hashing of contents). The resolver
 * walks profile/include/extends directories — that's the dominant cost.
 */
interface SourceFreshness {
  /** True/false/null per the rules above. */
  fresh: boolean | null;
  /** Live aggregate hash, or null when not computed. */
  liveAggregate: string | null;
  /** Human-readable error when freshness was attempted but failed. */
  error?: string;
}

async function assessSourceFreshness(
  cwd: string,
  state: { activeProfile: string | null; sourceFingerprint?: { aggregateHash: string } | null },
): Promise<SourceFreshness> {
  if (state.activeProfile === null) {
    return { fresh: null, liveAggregate: null };
  }
  const recorded = state.sourceFingerprint;
  if (recorded === null || recorded === undefined) {
    // Legacy state: no recorded fingerprint to compare against. We don't
    // recompute — there's nothing to compare to, so freshness is unknown.
    // The next materialize will populate the field.
    return { fresh: null, liveAggregate: null };
  }
  let plan;
  try {
    plan = await resolve(state.activeProfile, { projectRoot: cwd });
  } catch (err) {
    // Resolver throws on cycles / missing-includes / missing-extends. Don't
    // crash status — surface the reason and let `validate` give the full
    // diagnostic. The error message already names the offending source.
    const detail = err instanceof Error ? err.message : String(err);
    return { fresh: null, liveAggregate: null, error: detail };
  }
  const live = await computeSourceFingerprint(plan);
  return {
    fresh: live.aggregateHash === recorded.aggregateHash,
    liveAggregate: live.aggregateHash,
  };
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
