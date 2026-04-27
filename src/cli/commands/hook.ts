/**
 * `hook install|uninstall` command (R25a).
 *
 * Invariants (epic fitness function):
 *   - Hook script content is BYTE-IDENTICAL to R25a. Never edited at install.
 *   - `command -v claude-profiles` guard always present (fail-open).
 *   - Hook is fail-open: missing or broken `claude-profiles` binary never
 *     blocks commits. Drift is reported but never exits non-zero.
 *
 * Install rules:
 *   - Refuse to overwrite an existing pre-commit that is NOT our script
 *     (preExisting === "other"). Use `--force` to overwrite.
 *   - If our script already exists (byte-equal), no-op.
 *   - If `.git/hooks/` doesn't exist, we still write the hook (mkdir -p) so
 *     init works in a freshly-cloned bare repo.
 *
 * Uninstall rules:
 *   - Only remove the hook if it matches our script byte-for-byte. A user-
 *     edited or third-party hook is left untouched.
 *
 * .git layout: this command writes to `<projectRoot>/.git/hooks/pre-commit`.
 * For worktrees and submodules, `git rev-parse --git-dir` would be the
 * authoritative answer, but spawning `git` here would couple the CLI to a
 * git binary in $PATH. We instead read `.git` ourselves: if it's a regular
 * file (worktree linkage), parse the `gitdir:` pointer and follow it. If
 * absent, we fall through gracefully — `init` reports the failure rather
 * than aborting the rest of the bootstrap.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";
import process from "node:process";

import { CliUserError, EXIT_USER_ERROR } from "../exit.js";
import { createStyle, resolveNoColor, type OutputChannel } from "../output.js";

/**
 * Sentinel thrown by `resolveHooksDir` when the project has no `.git`
 * directory or `.git`-file pointer. Lets callers branch on a typed
 * `instanceof` check instead of string-sniffing the message (which would
 * silently break if the message text changed).
 */
export class NotAGitRepoError extends Error {
  readonly code = "NOT_GIT_REPO" as const;
  constructor(message: string) {
    super(message);
    this.name = "NotAGitRepoError";
  }
}

/**
 * The verbatim R25a script. Any change here MUST be accompanied by a
 * deliberate spec bump (the integration epic asserts byte equality).
 *
 * Trailing newline is intentional: POSIX shell scripts conventionally end
 * with `\n` and some shells warn otherwise.
 */
export const HOOK_SCRIPT = `#!/bin/sh
command -v claude-profiles >/dev/null 2>&1 || exit 0
claude-profiles drift --pre-commit-warn 2>&1
exit 0
`;

export type HookPreExisting = "absent" | "ours" | "other";

export interface InstallHookOptions {
  cwd: string;
  /** When true, overwrite a non-matching existing hook. */
  force: boolean;
  /**
   * When true, returning a `skippedReason` instead of throwing is acceptable
   * (e.g. the project isn't a git repo). The init flow uses this so a
   * non-git bootstrap doesn't abort the rest of init. The standalone
   * `hook install` verb passes false so the user sees the underlying error.
   */
  allowSkip?: boolean;
}

export interface InstallHookResult {
  hookPath: string;
  preExisting: HookPreExisting;
  /**
   * True iff this call wrote/updated the hook. False when the hook was
   * already correct (preExisting === "ours"), when we refused to overwrite
   * (preExisting === "other" && !force), or when skipped (no .git/).
   */
  installed: boolean;
  /**
   * Set when `allowSkip` is true and we couldn't install (e.g. no .git/).
   * Distinguishes "ran and decided not to write" from "didn't run".
   */
  skippedReason: string | null;
}

export interface UninstallHookOptions {
  cwd: string;
}

export interface UninstallHookResult {
  hookPath: string;
  preExisting: HookPreExisting;
  /** True iff this call removed the hook. */
  removed: boolean;
}

export interface HookCommandOptions {
  cwd: string;
  output: OutputChannel;
  action: "install" | "uninstall";
  force: boolean;
  /** When true, force colour off (additive with NO_COLOR env). Default false. */
  noColor?: boolean;
}

export async function runHook(opts: HookCommandOptions): Promise<number> {
  // Defer style creation until we know we're emitting human output. Skipping
  // the build under --json keeps every verb consistent (new/use/sync/validate
  // all build inside the human branch) and avoids reading process.stdout/env
  // in a path that never paints a glyph.
  function buildStyle() {
    return createStyle({
      isTty: Boolean(process.stdout.isTTY),
      platform: process.platform,
      noColor: resolveNoColor(opts.noColor === true),
    });
  }

  if (opts.action === "install") {
    const result = await installHook({
      cwd: opts.cwd,
      force: opts.force,
      // The standalone `hook install` verb does NOT skip: a non-git project
      // is a user error here (they explicitly asked for the hook). Init's
      // path opts in via allowSkip.
      allowSkip: false,
    });
    if (opts.output.jsonMode) {
      opts.output.json({
        action: "install",
        hookPath: result.hookPath,
        preExisting: result.preExisting,
        installed: result.installed,
      });
      // JSON mode still throws on the foreign-hook block path so the exit
      // code matches human mode (exit 1). Without this, scripts using
      // `--json` would see exit 0 + `installed: false` and have to parse
      // payload fields to detect failure — defeating the point of a
      // consistent exit-code policy.
      if (!result.installed && result.preExisting === "other") {
        throw new CliUserError(
          `hook install: ${result.hookPath} contains a different script; pass --force to overwrite`,
          EXIT_USER_ERROR,
        );
      }
    } else if (result.installed) {
      const style = buildStyle();
      const verb = result.preExisting === "absent" ? "Installed" : "Overwrote existing";
      opts.output.print(style.ok(`${verb} pre-commit hook`));
      opts.output.print(style.dim(`  ${result.hookPath}`));
    } else if (result.preExisting === "ours") {
      const style = buildStyle();
      opts.output.print(style.skip(`Pre-commit hook already installed`));
      opts.output.print(style.dim(`  ${result.hookPath}`));
    } else {
      // "other" + !force — surface as a user error so the user sees a non-
      // zero exit.
      throw new CliUserError(
        `hook install: ${result.hookPath} contains a different script; pass --force to overwrite`,
        EXIT_USER_ERROR,
      );
    }
    return 0;
  }

  const result = await uninstallHook({ cwd: opts.cwd });
  if (opts.output.jsonMode) {
    opts.output.json({
      action: "uninstall",
      hookPath: result.hookPath,
      preExisting: result.preExisting,
      removed: result.removed,
    });
  } else if (result.removed) {
    const style = buildStyle();
    opts.output.print(style.ok(`Removed pre-commit hook`));
    opts.output.print(style.dim(`  ${result.hookPath}`));
  } else if (result.preExisting === "absent") {
    const style = buildStyle();
    opts.output.print(style.skip(`No pre-commit hook to remove`));
    opts.output.print(style.dim(`  ${result.hookPath}`));
  } else {
    const style = buildStyle();
    opts.output.print(
      style.skip(`Pre-commit hook contains a different script; left untouched`),
    );
    opts.output.print(style.dim(`  ${result.hookPath}`));
  }
  return 0;
}

/**
 * Install the hook (programmatic API — `init` and the `hook install` verb
 * both go through this path). Returns a structured outcome rather than
 * throwing on the "already exists, different content" case so the caller
 * can render an appropriate message and exit code.
 */
export async function installHook(opts: InstallHookOptions): Promise<InstallHookResult> {
  let hooksDir: string;
  try {
    hooksDir = await resolveHooksDir(opts.cwd);
  } catch (err: unknown) {
    if (opts.allowSkip && err instanceof NotAGitRepoError) {
      return {
        hookPath: path.join(opts.cwd, ".git", "hooks", "pre-commit"),
        preExisting: "absent",
        installed: false,
        skippedReason: "no-git-dir",
      };
    }
    throw err;
  }
  const hookPath = path.join(hooksDir, "pre-commit");
  const preExisting = await classifyExisting(hookPath);

  if (preExisting === "ours") {
    return { hookPath, preExisting, installed: false, skippedReason: null };
  }
  if (preExisting === "other" && !opts.force) {
    return { hookPath, preExisting, installed: false, skippedReason: null };
  }

  await fs.mkdir(hooksDir, { recursive: true });
  await fs.writeFile(hookPath, HOOK_SCRIPT, { mode: 0o755 });
  // writeFile honours `mode` only on file *creation*; chmod ensures perms
  // are 0o755 even when overwriting a non-executable existing file.
  await fs.chmod(hookPath, 0o755);
  return { hookPath, preExisting, installed: true, skippedReason: null };
}

/**
 * Uninstall the hook. Only removes the file if the contents are our exact
 * R25a script — a user-edited pre-commit is left untouched.
 */
export async function uninstallHook(
  opts: UninstallHookOptions,
): Promise<UninstallHookResult> {
  let hooksDir: string;
  try {
    hooksDir = await resolveHooksDir(opts.cwd);
  } catch (err: unknown) {
    if (err instanceof NotAGitRepoError) {
      // No git dir → no hook to uninstall. Treat as a no-op rather than
      // erroring; the caller's intent ("ensure no hook is present") is
      // satisfied vacuously.
      return {
        hookPath: path.join(opts.cwd, ".git", "hooks", "pre-commit"),
        preExisting: "absent",
        removed: false,
      };
    }
    throw err;
  }
  const hookPath = path.join(hooksDir, "pre-commit");
  const preExisting = await classifyExisting(hookPath);

  if (preExisting !== "ours") {
    return { hookPath, preExisting, removed: false };
  }

  await fs.unlink(hookPath);
  return { hookPath, preExisting, removed: true };
}

async function classifyExisting(hookPath: string): Promise<HookPreExisting> {
  let raw: string;
  try {
    raw = await fs.readFile(hookPath, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return "absent";
    throw err;
  }
  return raw === HOOK_SCRIPT ? "ours" : "other";
}

/**
 * Resolve the `.git/hooks` directory for the project. Handles the worktree
 * `.git` file linkage where `<projectRoot>/.git` is a regular file with a
 * `gitdir: <abs path>` pointer rather than a directory.
 *
 * Refuses with a system error if `.git` is absent — installing a pre-commit
 * hook in a non-git project is meaningless and silently creating `.git/`
 * ourselves would mask the underlying user mistake.
 */
async function resolveHooksDir(cwd: string): Promise<string> {
  const dotGit = path.join(cwd, ".git");
  let stat: import("node:fs").Stats;
  try {
    stat = await fs.stat(dotGit);
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      throw new NotAGitRepoError(
        `hook: not a git project — ${dotGit} does not exist (run "git init" first)`,
      );
    }
    throw err;
  }
  if (stat.isDirectory()) {
    return path.join(dotGit, "hooks");
  }
  // Worktree linkage: `.git` is a file with `gitdir: <path>` pointer.
  const raw = await fs.readFile(dotGit, "utf8");
  const m = raw.match(/^gitdir:\s*(.+?)\s*$/m);
  // Defence in depth: even if the regex captures a whitespace-only sliver
  // (edge cases with mixed-whitespace content + the multiline flag), reject
  // empty/blank captures so we never `path.resolve(cwd, "")` and silently
  // land back inside the project root.
  const captured = m ? m[1]!.trim() : "";
  if (!m || captured === "") {
    throw new NotAGitRepoError(
      `hook: cannot resolve .git directory — ${dotGit} is not a directory and contains no usable gitdir: pointer`,
    );
  }
  const gitdir = path.isAbsolute(captured) ? captured : path.resolve(cwd, captured);
  return path.join(gitdir, "hooks");
}
