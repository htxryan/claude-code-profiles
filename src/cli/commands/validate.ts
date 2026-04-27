/**
 * `validate [<name>]` command (R33). Dry-run resolve + merge against one or
 * all profiles. Returns exit 0 on full pass, 3 on any failure (R33: pass/fail
 * report; non-zero exit on failures — S11).
 *
 * Composes E1 (resolve) + E2 (merge) without touching disk via E3. Errors
 * are caught per-profile so one bad manifest doesn't mask others.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";
import process from "node:process";

import { PipelineError, type PipelineErrorCode } from "../../errors/index.js";
import { parseMarkers } from "../../markers.js";
import { listProfiles, resolve } from "../../resolver/index.js";
import type { ResolvedPlan } from "../../resolver/types.js";
import { merge } from "../../merge/index.js";
import { buildStatePaths, readStateFile } from "../../state/index.js";
import { CliUserError, EXIT_CONFLICT, EXIT_USER_ERROR } from "../exit.js";
import { formatResolutionWarnings } from "../format.js";
import { createStyle, resolveNoColor, type OutputChannel } from "../output.js";
import { assertValidProfileName, enrichMissingProfileError } from "../suggest.js";

/**
 * Stable union of error-code strings the `validate` command may emit in its
 * structured payload. `Unknown` is the catch-all for non-PipelineError throws
 * (filesystem faults, etc.) so --json consumers see a closed set instead of
 * arbitrary class names like "TypeError" leaking through.
 */
export type ValidateErrorCode = PipelineErrorCode | "Unknown";

export interface ValidateProfileResult {
  profile: string;
  ok: boolean;
  /** Error code from a thrown PipelineError, or "Unknown" for non-pipeline errors. */
  errorCode: ValidateErrorCode | null;
  errorMessage: string | null;
  warnings: Array<{ code: string; message: string; source: string | null }>;
  externalPaths: string[];
}

export interface ValidatePayload {
  results: ValidateProfileResult[];
  pass: boolean;
}

export interface ValidateOptions {
  cwd: string;
  output: OutputChannel;
  /** Single profile to validate, or null to validate every profile in the project. */
  profile: string | null;
  /** When true, force colour off (additive with NO_COLOR env). Default false. */
  noColor?: boolean;
}

export async function runValidate(opts: ValidateOptions): Promise<number> {
  // ppo: when the user passes an explicit profile name, pre-validate it
  // against `isValidProfileName` so a path-traversal-shaped name surfaces
  // the standardized invalid-name message instead of a per-profile FAIL row.
  if (opts.profile !== null) {
    assertValidProfileName("validate", opts.profile);
  }

  // cw6 / R44: when a profile is active AND that profile's resolved plan
  // contributes to the project-root CLAUDE.md, verify the live root file
  // has the managed-block markers. This is a project-wide environment check,
  // not per-profile, so it runs before the per-target loop.
  //
  // Per docs/migration/cw6-section-ownership.md the marker check is
  // CONDITIONAL on a contribution being present. An active profile that
  // never touches projectRoot (the silent-majority v1 layout) must not trip
  // this check — otherwise users who haven't opted into section ownership
  // see a spurious failure. We resolve the active plan and look for any
  // PlanFile with destination === "projectRoot"; if none, skip.
  //
  // Idle state (NoActive, e.g. user just ran `init` but no `use` yet) also
  // skips the check. readStateFile degrades to defaultState() (activeProfile:
  // null) on any error per R42, so a missing/corrupt state.json correctly
  // idles here too. If resolve() throws (broken active profile), we skip the
  // marker check and let the per-profile validateOne loop surface the real
  // error rather than masking it behind an unrelated marker complaint.
  const paths = buildStatePaths(opts.cwd);
  const { state } = await readStateFile(paths);
  if (state.activeProfile !== null) {
    let activePlan: ResolvedPlan | null = null;
    try {
      activePlan = await resolve(state.activeProfile, { projectRoot: opts.cwd });
    } catch {
      // Active profile no longer resolvable — skip the marker check; the
      // per-profile loop below will surface the resolve error if the user
      // is validating that profile.
    }
    if (
      activePlan !== null &&
      activePlan.files.some((f) => f.destination === "projectRoot")
    ) {
      await assertRootClaudeMdMarkers(opts.cwd);
    }
  }

  const targets =
    opts.profile !== null
      ? [opts.profile]
      : await listProfiles({ projectRoot: opts.cwd });

  if (targets.length === 0) {
    if (opts.output.jsonMode) {
      opts.output.json({ results: [], pass: true } satisfies ValidatePayload);
    } else {
      opts.output.print("(no profiles to validate)");
    }
    return 0;
  }

  // Phase hints (3yy): per-profile progress emitted on stderr so a 50-profile
  // validate doesn't sit on a stuck cursor. Silenced under --json and --quiet
  // by the OutputChannel; we still emit when stdout is human (TTY or piped)
  // so a `claude-profiles validate > log.txt` run shows live progress.
  const phaseStyle = createStyle({
    isTty: opts.output.isTty,
    platform: process.platform,
    noColor: resolveNoColor(opts.noColor === true),
  });
  const results: ValidateProfileResult[] = [];
  for (const name of targets) {
    // phase() is a no-op under --json/--quiet — no extra branch needed here.
    opts.output.phase(phaseStyle.dim(`validating ${name}…`));
    results.push(await validateOne(opts.cwd, name, opts.profile !== null));
  }
  const pass = results.every((r) => r.ok);

  if (opts.output.jsonMode) {
    const payload: ValidatePayload = { results, pass };
    opts.output.json(payload);
  } else {
    const style = createStyle({
      isTty: opts.output.isTty,
      platform: process.platform,
      noColor: resolveNoColor(opts.noColor === true),
    });
    for (const r of results) {
      if (r.ok) {
        const warnNote = r.warnings.length > 0 ? ` (${r.warnings.length} warning${r.warnings.length === 1 ? "" : "s"})` : "";
        opts.output.print(style.ok(`${r.profile}${warnNote}`));
        if (r.warnings.length > 0) {
          const ws = r.warnings.map((w) => {
            const base = { code: w.code as never, message: w.message };
            return w.source !== null ? { ...base, source: w.source } : base;
          });
          opts.output.print(formatResolutionWarnings(ws));
        }
      } else {
        // FAIL rows: red glyph + bold profile name to draw the eye.
        opts.output.print(
          style.fail(`${style.bold(r.profile)}: [${r.errorCode}] ${r.errorMessage}`),
        );
      }
    }
    const failed = results.filter((r) => !r.ok).length;
    if (failed === 0) {
      opts.output.print(style.ok(`${results.length} pass`));
    } else {
      opts.output.print(
        style.fail(`${results.length - failed} pass, ${failed} fail`),
      );
    }
  }

  if (!pass) {
    // R33 + AC-9: any fail → exit 3 (conflict/cycle/missing class).
    throw new CliUserError(`validation failed for ${results.filter((r) => !r.ok).length} profile(s)`, EXIT_CONFLICT);
  }
  return 0;
}

/**
 * cw6 / R44: when a profile is active, project-root CLAUDE.md must exist and
 * contain a well-formed managed-block marker pair. Throws CliUserError(exit
 * 1) with the spec-mandated remediation message if either condition fails.
 *
 * Three failure modes, all surfaced with the same actionable message because
 * the user's remediation is identical for every one (`claude-profiles init`):
 *   - File missing entirely.
 *   - File present but no markers (parseMarkers → reason: "absent").
 *   - File present with markers in a partial / multi-block / version-mismatch
 *     state (parseMarkers → reason: "malformed").
 */
async function assertRootClaudeMdMarkers(projectRoot: string): Promise<void> {
  const claudeMdPath = path.join(projectRoot, "CLAUDE.md");
  let content: string;
  try {
    content = await fs.readFile(claudeMdPath, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      throw new CliUserError(
        // Marker pair appended for ppo/AC: a user who accidentally deleted
        // the markers needs to know exactly what bytes to put back. The
        // `(file: <path>)` suffix matches the materialize-time and drift
        // detect error messages so grep/log scraping is consistent across
        // the three sites that emit this remediation (cw6.2 followup).
        `project-root CLAUDE.md is missing claude-profiles markers — run \`claude-profiles init\` to add them (file: ${claudeMdPath}; expected: <!-- claude-profiles:v1:begin --> ... <!-- claude-profiles:v1:end -->)`,
        EXIT_USER_ERROR,
      );
    }
    throw err;
  }
  const parsed = parseMarkers(content);
  if (!parsed.found) {
    throw new CliUserError(
      "project-root CLAUDE.md is missing claude-profiles markers — run `claude-profiles init` to add them (expected: <!-- claude-profiles:v1:begin --> ... <!-- claude-profiles:v1:end -->)",
      EXIT_USER_ERROR,
    );
  }
}

async function validateOne(
  cwd: string,
  name: string,
  /**
   * True iff this name came from `validate <name>` (an explicit user typed
   * argument). When false (`validate` over every profile in the project),
   * a missing profile means listProfiles disagreed with the filesystem mid-
   * walk — not a typo — so we skip suggestion enrichment.
   */
  enrichTopLevelTypo: boolean,
): Promise<ValidateProfileResult> {
  try {
    const plan = await resolve(name, { projectRoot: cwd });
    // Run merge to surface settings.json parse errors / read failures even
    // though we don't materialize. R33 lists "detect conflicts" — E2 surfaces
    // the rest.
    await merge(plan);
    return {
      profile: name,
      ok: true,
      errorCode: null,
      errorMessage: null,
      warnings: plan.warnings.map((w) => ({
        code: w.code,
        message: w.message,
        source: w.source ?? null,
      })),
      externalPaths: plan.externalPaths.map((e) => e.resolvedPath),
    };
  } catch (err: unknown) {
    // ppo: when the user typed `validate <typo>` and the typo doesn't exist
    // but a close in-project profile does, append the "did you mean" hint to
    // the FAIL row. The errorCode field stays "MissingProfile" so machines
    // can still key off it (per ppo AC: codes do not change).
    const enriched = enrichTopLevelTypo
      ? await enrichMissingProfileError(err, cwd, name)
      : err;
    // Pin errorCode to PipelineErrorCode | "Unknown" so the --json payload
    // never leaks arbitrary class names (e.g. "TypeError", "SyntaxError")
    // into a field downstream tooling parses against the documented union.
    const code: ValidateErrorCode = enriched instanceof PipelineError ? enriched.code : "Unknown";
    const message = enriched instanceof Error ? enriched.message : String(enriched);
    return {
      profile: name,
      ok: false,
      errorCode: code,
      errorMessage: message,
      warnings: [],
      externalPaths: [],
    };
  }
}
