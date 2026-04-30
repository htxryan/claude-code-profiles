/**
 * `doctor` command (claude-code-profiles-0zn). Read-only diagnostic that
 * surveys the project's c3p state and reports a per-check
 * status table:
 *   - state.json schema valid? (R42)
 *   - lock present + holder liveness? (R41)
 *   - gitignore entries present? (R15)
 *   - pre-commit hook byte-equality (R25a)
 *   - discard-backup retention <= 5 (R23a)
 *   - external-path reachability (R37a)
 *   - project-root CLAUDE.md markers well-formed when active profile
 *     contributes a projectRoot section (R44/R45)
 *   - active profile resolves cleanly
 *
 * Exit semantics:
 *   0  every check passed (or was skipped because it doesn't apply)
 *   1  any check reported "warn" or "fail" (actionable for the user)
 *   2  IO/permission fault inside the diagnostic itself (propagates as system error)
 *
 * Never writes anything: reads only. Safe to run from CI hooks, README
 * suggestions, etc.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";
import process from "node:process";

import { parseMarkers } from "../../markers.js";
import { resolve } from "../../resolver/index.js";
import {
  E3_GITIGNORE_ENTRIES,
  buildStatePaths,
  listSnapshots,
  readStateFile,
  type StateReadWarning,
} from "../../state/index.js";
import { CliUserError, EXIT_USER_ERROR } from "../exit.js";
import { createStyle, resolveNoColor, type OutputChannel } from "../output.js";

import { HOOK_SCRIPT } from "./hook.js";

/** One row in the doctor report. Stable shape for --json consumers. */
export interface DoctorCheck {
  /** Stable id (snake_case) — keys CI scripts can match on. */
  id: string;
  /** Short human-readable label (rendered as the row's first column). */
  label: string;
  /** Outcome: ok=passed, warn=actionable, fail=actionable, skip=N/A in this state. */
  status: "ok" | "warn" | "fail" | "skip";
  /** One-line detail (path, count, error message). Always present, never null. */
  detail: string;
  /** Suggested remediation when status !== ok. Empty otherwise. */
  remediation: string;
}

export interface DoctorPayload {
  pass: boolean;
  checks: DoctorCheck[];
}

export interface DoctorOptions {
  cwd: string;
  output: OutputChannel;
  /** When true, force colour off (additive with NO_COLOR env). Default false. */
  noColor?: boolean;
}

const MAX_RETAINED_SNAPSHOTS = 5;

export async function runDoctor(opts: DoctorOptions): Promise<number> {
  const checks: DoctorCheck[] = [];

  // Always run every check rather than short-circuiting — the user wants the
  // full picture in one shot, not stop-at-first-failure.
  checks.push(await checkProfilesDir(opts.cwd));
  checks.push(await checkStateFile(opts.cwd));
  checks.push(await checkLock(opts.cwd));
  checks.push(await checkGitignore(opts.cwd));
  checks.push(await checkHook(opts.cwd));
  checks.push(await checkBackupCount(opts.cwd));
  checks.push(await checkActiveProfile(opts.cwd));
  checks.push(await checkExternalPaths(opts.cwd));
  checks.push(await checkRootClaudeMdMarkers(opts.cwd));

  const pass = checks.every((c) => c.status === "ok" || c.status === "skip");

  if (opts.output.jsonMode) {
    const payload: DoctorPayload = { pass, checks };
    opts.output.json(payload);
  } else {
    renderHumanReport(checks, opts);
  }

  if (!pass) {
    // Propagate via CliUserError so the dispatcher's central catch maps the
    // exit code correctly. Use exit 1 (user-actionable warnings) — exit 2 is
    // reserved for IO faults inside the diagnostic itself which we let throw.
    throw new CliUserError(
      `doctor: ${checks.filter((c) => c.status === "warn" || c.status === "fail").length} check(s) need attention`,
      EXIT_USER_ERROR,
    );
  }
  return 0;
}

function renderHumanReport(checks: DoctorCheck[], opts: DoctorOptions): void {
  const style = createStyle({
    isTty: opts.output.isTty,
    platform: process.platform,
    noColor: resolveNoColor(opts.noColor === true),
  });
  const rendered = checks.map((c) => {
    let glyph: string;
    switch (c.status) {
      case "ok":
        glyph = style.ok(c.label);
        break;
      case "warn":
        glyph = style.warn(c.label);
        break;
      case "fail":
        glyph = style.fail(c.label);
        break;
      case "skip":
        glyph = style.skip(c.label);
        break;
    }
    const detail = c.detail.length > 0 ? `  ${style.dim(c.detail)}` : "";
    const remediation = c.remediation.length > 0 ? `\n      ${style.dim("→ " + c.remediation)}` : "";
    return `${glyph}${detail}${remediation}`;
  });
  opts.output.print(rendered.join("\n"));

  const failed = checks.filter((c) => c.status === "warn" || c.status === "fail").length;
  const ok = checks.filter((c) => c.status === "ok").length;
  const skipped = checks.filter((c) => c.status === "skip").length;
  const summaryParts: string[] = [`${ok} ok`];
  if (skipped > 0) summaryParts.push(`${skipped} skipped`);
  if (failed > 0) summaryParts.push(`${failed} need attention`);
  const summary = summaryParts.join(", ");
  if (failed === 0) {
    opts.output.print(style.ok(summary));
    // Round-2 flourish (claude-code-profiles-4b7 ID 30): all-pass quip,
    // human path only — JSON output (handled above) is byte-identical.
    opts.output.print(`all ${ok} checks passed (the odds, frankly, were against us)`);
  } else {
    opts.output.print(style.fail(summary));
  }
}

async function checkProfilesDir(cwd: string): Promise<DoctorCheck> {
  const paths = buildStatePaths(cwd);
  try {
    const stat = await fs.stat(paths.profilesDir);
    if (!stat.isDirectory()) {
      return mkCheck(
        "profiles_dir",
        ".claude-profiles/ directory",
        "fail",
        `${paths.profilesDir} exists but is not a directory`,
        "remove the file and run `c3p init`",
      );
    }
    return mkCheck(
      "profiles_dir",
      ".claude-profiles/ directory",
      "ok",
      paths.profilesDir,
      "",
    );
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      return mkCheck(
        "profiles_dir",
        ".claude-profiles/ directory",
        "fail",
        `not found at ${paths.profilesDir}`,
        "run `c3p init` to bootstrap this project",
      );
    }
    throw err;
  }
}

async function checkStateFile(cwd: string): Promise<DoctorCheck> {
  const paths = buildStatePaths(cwd);
  const { state, warning } = await readStateFile(paths);
  if (warning === null) {
    const desc = state.activeProfile === null
      ? `no active profile (NoActive)`
      : `active=${state.activeProfile}`;
    return mkCheck("state_file", "state.json schema", "ok", desc, "");
  }
  return mkCheck(
    "state_file",
    "state.json schema",
    stateWarningStatus(warning),
    formatStateWarningDetail(warning),
    stateWarningRemediation(warning),
  );
}

function stateWarningStatus(w: StateReadWarning): "ok" | "warn" | "skip" {
  // Missing is benign in a fresh project; surface as skip so it doesn't
  // count against the pass criterion.
  if (w.code === "Missing") return "skip";
  return "warn";
}

function formatStateWarningDetail(w: StateReadWarning): string {
  switch (w.code) {
    case "Missing":
      return `state file not yet written at ${w.path} (run \`c3p init\` then \`c3p use <profile>\`)`;
    case "ParseError":
      return `unparseable: ${w.detail}`;
    case "SchemaMismatch":
      return `schema mismatch: ${w.detail}`;
  }
}

function stateWarningRemediation(w: StateReadWarning): string {
  switch (w.code) {
    case "Missing":
      return "";
    case "ParseError":
    case "SchemaMismatch":
      return "delete the corrupted state file and re-run `c3p use <profile>` to rebuild it";
  }
}

async function checkLock(cwd: string): Promise<DoctorCheck> {
  const paths = buildStatePaths(cwd);
  let raw: string;
  try {
    raw = await fs.readFile(paths.lockFile, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      return mkCheck("lock", "lock file", "ok", "no lock held", "");
    }
    throw err;
  }
  const trimmed = raw.trim();
  const space = trimmed.indexOf(" ");
  if (space <= 0) {
    return mkCheck(
      "lock",
      "lock file",
      "warn",
      `lock file at ${paths.lockFile} is corrupt (no PID)`,
      "remove the file if no peer c3p process is running",
    );
  }
  const pidStr = trimmed.slice(0, space);
  const ts = trimmed.slice(space + 1).trim();
  const pid = Number.parseInt(pidStr, 10);
  if (!Number.isFinite(pid) || pid <= 0) {
    return mkCheck(
      "lock",
      "lock file",
      "warn",
      `lock file at ${paths.lockFile} contains an invalid PID`,
      "remove the file if no peer c3p process is running",
    );
  }
  // Best-effort liveness probe (matches lock.ts isPidAlive). EPERM is treated
  // as alive (conservative: prefer "ask the user" over stomping a foreign
  // lock); ESRCH is dead.
  let alive = true;
  try {
    process.kill(pid, 0);
  } catch (err: unknown) {
    const code = (err as NodeJS.ErrnoException).code;
    if (code === "ESRCH") alive = false;
  }
  if (alive) {
    return mkCheck(
      "lock",
      "lock file",
      "warn",
      `held by PID ${pid} since ${ts}`,
      "wait for the peer process to finish, or pass --wait to poll",
    );
  }
  return mkCheck(
    "lock",
    "lock file",
    "warn",
    `stale lock from PID ${pid} (process no longer alive)`,
    "next mutating command will reclaim it automatically; or remove manually",
  );
}

async function checkGitignore(cwd: string): Promise<DoctorCheck> {
  const paths = buildStatePaths(cwd);
  let content: string;
  try {
    content = await fs.readFile(paths.gitignoreFile, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      return mkCheck(
        "gitignore",
        ".gitignore entries",
        "warn",
        `no .gitignore at ${paths.gitignoreFile}`,
        "run `c3p init` to create it with the needed entries",
      );
    }
    throw err;
  }
  const present = new Set(
    content
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter((l) => l.length > 0 && !l.startsWith("#")),
  );
  const missing = E3_GITIGNORE_ENTRIES.filter((e) => !present.has(e));
  if (missing.length === 0) {
    return mkCheck(
      "gitignore",
      ".gitignore entries",
      "ok",
      `${E3_GITIGNORE_ENTRIES.join(", ")} present`,
      "",
    );
  }
  return mkCheck(
    "gitignore",
    ".gitignore entries",
    "warn",
    `missing: ${missing.join(", ")}`,
    "run `c3p init` to append the missing entries",
  );
}

async function checkHook(cwd: string): Promise<DoctorCheck> {
  // Resolve `.git/hooks/` the same way `hook install` does. If `.git` is
  // absent, the hook check is N/A — skip rather than fail.
  const dotGit = path.join(cwd, ".git");
  let stat: import("node:fs").Stats;
  try {
    stat = await fs.stat(dotGit);
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      return mkCheck("hook", "pre-commit hook", "skip", "not a git project", "");
    }
    throw err;
  }
  let hooksDir: string;
  if (stat.isDirectory()) {
    hooksDir = path.join(dotGit, "hooks");
  } else {
    // Worktree linkage. Best-effort parse — if we can't resolve, skip rather
    // than fail (the hook check is auxiliary, not load-bearing).
    let raw: string;
    try {
      raw = await fs.readFile(dotGit, "utf8");
    } catch {
      return mkCheck("hook", "pre-commit hook", "skip", "could not resolve .git pointer", "");
    }
    const m = raw.match(/^gitdir:\s*(.+?)\s*$/m);
    const captured = m ? m[1]!.trim() : "";
    if (!m || captured === "") {
      return mkCheck("hook", "pre-commit hook", "skip", "could not resolve .git pointer", "");
    }
    const gitdir = path.isAbsolute(captured) ? captured : path.resolve(cwd, captured);
    hooksDir = path.join(gitdir, "hooks");
  }
  const hookPath = path.join(hooksDir, "pre-commit");
  let raw: string;
  try {
    raw = await fs.readFile(hookPath, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      return mkCheck(
        "hook",
        "pre-commit hook",
        "warn",
        `no hook at ${hookPath}`,
        "run `c3p hook install` to install (drift advisory only — fail-open)",
      );
    }
    throw err;
  }
  if (raw === HOOK_SCRIPT) {
    return mkCheck("hook", "pre-commit hook", "ok", `installed at ${hookPath}`, "");
  }
  return mkCheck(
    "hook",
    "pre-commit hook",
    "warn",
    `pre-existing hook at ${hookPath} is not the canonical c3p script`,
    "if intentional, leave it; otherwise re-install via `c3p hook install --force`",
  );
}

async function checkBackupCount(cwd: string): Promise<DoctorCheck> {
  const paths = buildStatePaths(cwd);
  const snapshots = await listSnapshots(paths);
  if (snapshots.length === 0) {
    return mkCheck("backups", "discard-backup count", "ok", "0 snapshots", "");
  }
  if (snapshots.length <= MAX_RETAINED_SNAPSHOTS) {
    return mkCheck(
      "backups",
      "discard-backup count",
      "ok",
      `${snapshots.length}/${MAX_RETAINED_SNAPSHOTS} snapshots retained`,
      "",
    );
  }
  // Should never happen — snapshotForDiscard prunes after each write — but
  // surface as a warning so it doesn't go unnoticed if it ever does.
  return mkCheck(
    "backups",
    "discard-backup count",
    "warn",
    `${snapshots.length} snapshots present (cap is ${MAX_RETAINED_SNAPSHOTS})`,
    `manually prune the oldest dirs under ${paths.backupDir}`,
  );
}

async function checkActiveProfile(cwd: string): Promise<DoctorCheck> {
  const paths = buildStatePaths(cwd);
  const { state, warning } = await readStateFile(paths);
  if (warning !== null && warning.code !== "Missing") {
    return mkCheck(
      "active_profile",
      "active profile resolves",
      "skip",
      "state file degraded — see state.json check",
      "",
    );
  }
  if (state.activeProfile === null) {
    // No active profile is a valid post-init state; not a failure.
    return mkCheck(
      "active_profile",
      "active profile resolves",
      "skip",
      "no active profile",
      "",
    );
  }
  try {
    await resolve(state.activeProfile, { projectRoot: cwd });
    return mkCheck(
      "active_profile",
      "active profile resolves",
      "ok",
      `${state.activeProfile} resolves cleanly`,
      "",
    );
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    return mkCheck(
      "active_profile",
      "active profile resolves",
      "fail",
      `${state.activeProfile}: ${message.split("\n", 1)[0]}`,
      `run \`c3p validate ${state.activeProfile}\` for the full diagnostic`,
    );
  }
}

async function checkExternalPaths(cwd: string): Promise<DoctorCheck> {
  const paths = buildStatePaths(cwd);
  const { state, warning } = await readStateFile(paths);
  if (warning !== null && warning.code !== "Missing") {
    return mkCheck(
      "external_paths",
      "external include paths reachable",
      "skip",
      "state file degraded",
      "",
    );
  }
  // Walk the recorded resolved sources and check `external` paths only —
  // they're the ones a `git pull` on a teammate's machine could have rendered
  // unreachable. In-repo paths are walked by checkActiveProfile via resolve.
  const externals = state.resolvedSources.filter((s) => s.external);
  if (externals.length === 0) {
    return mkCheck(
      "external_paths",
      "external include paths reachable",
      "skip",
      "no external contributors",
      "",
    );
  }
  const missing: string[] = [];
  for (const ext of externals) {
    try {
      await fs.stat(ext.rootPath);
    } catch {
      missing.push(`${ext.id} → ${ext.rootPath}`);
    }
  }
  if (missing.length === 0) {
    return mkCheck(
      "external_paths",
      "external include paths reachable",
      "ok",
      `${externals.length} reachable`,
      "",
    );
  }
  return mkCheck(
    "external_paths",
    "external include paths reachable",
    "warn",
    `unreachable: ${missing.join("; ")}`,
    "restore the missing paths or remove them from the active profile's includes",
  );
}

async function checkRootClaudeMdMarkers(cwd: string): Promise<DoctorCheck> {
  const paths = buildStatePaths(cwd);
  const { state, warning } = await readStateFile(paths);
  if (warning !== null && warning.code !== "Missing") {
    return mkCheck(
      "root_claude_md_markers",
      "project-root CLAUDE.md markers",
      "skip",
      "state file degraded",
      "",
    );
  }
  if (state.activeProfile === null) {
    return mkCheck(
      "root_claude_md_markers",
      "project-root CLAUDE.md markers",
      "skip",
      "no active profile",
      "",
    );
  }
  // Only run when the active profile actually contributes a projectRoot file
  // (matches validate.ts's conditional).
  let plan;
  try {
    plan = await resolve(state.activeProfile, { projectRoot: cwd });
  } catch {
    return mkCheck(
      "root_claude_md_markers",
      "project-root CLAUDE.md markers",
      "skip",
      "active profile does not resolve — see active_profile check",
      "",
    );
  }
  const hasRootContribution = plan.files.some((f) => f.destination === "projectRoot");
  if (!hasRootContribution) {
    return mkCheck(
      "root_claude_md_markers",
      "project-root CLAUDE.md markers",
      "skip",
      "active profile does not contribute a projectRoot section",
      "",
    );
  }
  let content: string;
  try {
    content = await fs.readFile(paths.rootClaudeMdFile, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      return mkCheck(
        "root_claude_md_markers",
        "project-root CLAUDE.md markers",
        "fail",
        `CLAUDE.md missing at ${paths.rootClaudeMdFile}`,
        "run `c3p init` to create it with the managed-block markers",
      );
    }
    throw err;
  }
  const parsed = parseMarkers(content);
  if (parsed.found) {
    return mkCheck(
      "root_claude_md_markers",
      "project-root CLAUDE.md markers",
      "ok",
      `well-formed v${parsed.version} markers at ${paths.rootClaudeMdFile}`,
      "",
    );
  }
  if (parsed.reason === "absent") {
    return mkCheck(
      "root_claude_md_markers",
      "project-root CLAUDE.md markers",
      "fail",
      `markers absent in ${paths.rootClaudeMdFile}`,
      "run `c3p init` to add them (preserves existing content)",
    );
  }
  return mkCheck(
    "root_claude_md_markers",
    "project-root CLAUDE.md markers",
    "fail",
    `malformed markers in ${paths.rootClaudeMdFile}`,
    "manually delete the partial marker text and re-run `c3p init`",
  );
}

function mkCheck(
  id: string,
  label: string,
  status: DoctorCheck["status"],
  detail: string,
  remediation: string,
): DoctorCheck {
  return { id, label, status, detail, remediation };
}
