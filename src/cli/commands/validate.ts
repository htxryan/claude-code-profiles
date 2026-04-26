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

import { PipelineError, type PipelineErrorCode } from "../../errors/index.js";
import { parseMarkers } from "../../markers.js";
import { listProfiles, resolve } from "../../resolver/index.js";
import { merge } from "../../merge/index.js";
import { buildStatePaths, readStateFile } from "../../state/index.js";
import { CliUserError, EXIT_CONFLICT, EXIT_USER_ERROR } from "../exit.js";
import { formatResolutionWarnings } from "../format.js";
import type { OutputChannel } from "../output.js";

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
}

export async function runValidate(opts: ValidateOptions): Promise<number> {
  // cw6 / R44: when a profile is active, verify the project-root CLAUDE.md
  // has the managed-block markers. This is a project-wide environment check,
  // not per-profile, so it runs before the per-target loop.
  //
  // Idle state (NoActive, e.g. user just ran `init` but no `use` yet) skips
  // the check — pestering users who haven't adopted profiles would be noisy
  // and the markers don't actually do anything until materialize wires them.
  // readStateFile degrades to defaultState() (activeProfile: null) on any
  // error per R42, so a missing/corrupt state.json correctly idles here too.
  const paths = buildStatePaths(opts.cwd);
  const { state } = await readStateFile(paths);
  if (state.activeProfile !== null) {
    await assertRootClaudeMdMarkers(opts.cwd);
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

  const results: ValidateProfileResult[] = [];
  for (const name of targets) {
    results.push(await validateOne(opts.cwd, name));
  }
  const pass = results.every((r) => r.ok);

  if (opts.output.jsonMode) {
    const payload: ValidatePayload = { results, pass };
    opts.output.json(payload);
  } else {
    for (const r of results) {
      if (r.ok) {
        const warnNote = r.warnings.length > 0 ? ` (${r.warnings.length} warning${r.warnings.length === 1 ? "" : "s"})` : "";
        opts.output.print(`PASS  ${r.profile}${warnNote}`);
        if (r.warnings.length > 0) {
          const ws = r.warnings.map((w) => {
            const base = { code: w.code as never, message: w.message };
            return w.source !== null ? { ...base, source: w.source } : base;
          });
          opts.output.print(formatResolutionWarnings(ws));
        }
      } else {
        opts.output.print(`FAIL  ${r.profile}: [${r.errorCode}] ${r.errorMessage}`);
      }
    }
    const failed = results.filter((r) => !r.ok).length;
    if (failed === 0) {
      opts.output.print(`validate: ${results.length} pass`);
    } else {
      opts.output.print(`validate: ${results.length - failed} pass, ${failed} fail`);
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
        "project-root CLAUDE.md is missing claude-profiles markers — run `claude-profiles init` to add them",
        EXIT_USER_ERROR,
      );
    }
    throw err;
  }
  const parsed = parseMarkers(content);
  if (!parsed.found) {
    throw new CliUserError(
      "project-root CLAUDE.md is missing claude-profiles markers — run `claude-profiles init` to add them",
      EXIT_USER_ERROR,
    );
  }
}

async function validateOne(cwd: string, name: string): Promise<ValidateProfileResult> {
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
    // Pin errorCode to PipelineErrorCode | "Unknown" so the --json payload
    // never leaks arbitrary class names (e.g. "TypeError", "SyntaxError")
    // into a field downstream tooling parses against the documented union.
    const code: ValidateErrorCode = err instanceof PipelineError ? err.code : "Unknown";
    const message = err instanceof Error ? err.message : String(err);
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
