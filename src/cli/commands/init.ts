/**
 * `init` command (R26, R27, R28). Bootstraps `.claude-profiles/` in a project:
 *
 *   1. Detect / create `.claude-profiles/`. Refuses to overwrite if present
 *      (the user already initialised — re-running should be a no-op or an
 *      explicit error rather than blowing away their work).
 *   2. If `.claude/` exists at the project root AND no profiles exist yet,
 *      offer to seed a starter profile by copying `.claude/` into
 *      `.claude-profiles/<starter-name>/.claude/` and writing a minimal
 *      profile.json. The starter name defaults to "default".
 *   3. Append the canonical entries to `.gitignore` (creates the file if
 *      absent). Delegates to `state.ensureGitignoreEntries`.
 *   4. Offer to install the pre-commit hook. Delegates to `installHook`.
 *
 * Non-interactive vs interactive:
 *   - In `--json` or non-TTY mode, prompts are skipped and the seed/hook
 *     decisions are taken from explicit flags. We default to "seed if
 *     `.claude/` exists" + "install hook" so a scripted `init` produces a
 *     fully-configured project. Users can opt out via `--no-seed` /
 *     `--no-hook` (those are global init flags, not parsed yet — for now we
 *     accept them via InitOptions).
 *
 * Lock: init acquires the write lock (R41 — init is a mutating op). If the
 * lock is held, the user sees `LockHeldError` formatted via the dispatcher
 * catch.
 */

import { promises as fs } from "node:fs";
import * as path from "node:path";
import process from "node:process";

import {
  injectMarkersIntoFile,
  parseMarkers,
  renderManagedBlock,
} from "../../markers.js";
import { isValidProfileName } from "../../resolver/index.js";
import { atomicWriteFile, uniqueAtomicTmpPath } from "../../state/atomic.js";
import {
  buildStatePaths,
  ensureGitignoreEntries,
  withLock,
  type GitignoreUpdate,
} from "../../state/index.js";
import { CliUserError, EXIT_USER_ERROR } from "../exit.js";
import { createStyle, type OutputChannel } from "../output.js";

import { installHook, type InstallHookResult } from "./hook.js";

export interface InitOptions {
  cwd: string;
  output: OutputChannel;
  /**
   * Name to give the seeded starter profile. Default "default".
   */
  starterName: string;
  /**
   * When false, never seed a starter profile from `.claude/`. When true and
   * `.claude/` exists, always seed. Defaults to true (matches the spec
   * "offer to seed" behaviour for the scripted path).
   */
  seedFromClaudeDir: boolean;
  /**
   * When true, install the pre-commit hook as part of init. Defaults to true.
   */
  installHook: boolean;
  /**
   * Bin always passes true; tests pass false to skip process-level signal
   * handlers (matches every other write-locked verb).
   */
  signalHandlers: boolean;
}

/**
 * Outcome of the project-root CLAUDE.md marker step (cw6 / spec §12.4):
 *   - "created"    — file did not exist; we wrote a fresh one with markers.
 *   - "appended"   — file existed without markers; we appended a fresh block,
 *                    preserving every prior byte verbatim above.
 *   - "already"    — file existed with valid markers; no-op.
 *
 * `path` is the absolute project-root CLAUDE.md path so --json consumers can
 * report a stable artifact identifier.
 */
export interface RootClaudeMdMarkerResult {
  outcome: "created" | "appended" | "already";
  path: string;
}

export interface InitResult {
  profilesDirCreated: boolean;
  starterProfileSeeded: string | null;
  gitignore: GitignoreUpdate;
  hook: InstallHookResult | null;
  rootClaudeMd: RootClaudeMdMarkerResult;
}

export async function runInit(opts: InitOptions): Promise<number> {
  if (!isValidProfileName(opts.starterName)) {
    throw new CliUserError(
      `init: invalid starter profile name "${opts.starterName}"`,
      EXIT_USER_ERROR,
    );
  }

  const paths = buildStatePaths(opts.cwd);

  const result = await withLock(
    paths,
    async () => {
      // R26 invariant: never overwrite an existing `.claude-profiles/`.
      // `withLock` already created `.claude-profiles/` for the lock file —
      // so "already initialised" is "any non-lock/non-tmp content exists".
      const profilesDirCreated = await classifyProfilesDir(paths.profilesDir);
      if (profilesDirCreated === "already-initialised") {
        // ppo: append a forward-momentum hint so the user doesn't dead-end
        // on "refusing to overwrite" — they almost always want either
        // `status` (inspect) or `new <name>` (extend) next.
        throw new CliUserError(
          `init: ".claude-profiles/" is already initialised in this project; refusing to overwrite (run "claude-profiles status" to see current state, or "claude-profiles new <name>" to add a profile)`,
          EXIT_USER_ERROR,
        );
      }

      // R28 + R25a: install the pre-commit hook FIRST under the lock so an
      // unexpected hook failure (EACCES on `.git/hooks/`, EPERM on chmod,
      // malformed gitdir pointer, …) leaves no destructive state behind.
      // If we ran seed/gitignore first, an exception here would commit a
      // partial init that re-running could not recover from without the
      // user manually deleting `.claude-profiles/`. With this ordering, a
      // throw releases the lock, leaves only `.meta/` (which
      // classifyProfilesDir treats as "fresh"), and a retry just works.
      let hook: InstallHookResult | null = null;
      if (opts.installHook) {
        hook = await installHook({
          cwd: opts.cwd,
          force: false,
          allowSkip: true,
        });
      }

      // R27: seed from `.claude/` if present and the user hasn't disabled it.
      let starterProfileSeeded: string | null = null;
      if (opts.seedFromClaudeDir) {
        const seeded = await maybeSeedStarter(
          paths.claudeDir,
          paths.profilesDir,
          opts.starterName,
        );
        if (seeded) starterProfileSeeded = opts.starterName;
      }

      // R28: ensure `.gitignore` lists the canonical entries. The shared
      // writer already covers `.claude/`, `.state.json`, `.backup/`, etc.
      const gitignore = await ensureGitignoreEntries(paths);

      // cw6 / spec §12.4 (init): ensure project-root CLAUDE.md exists with a
      // well-formed managed-block marker pair. Run inside the lock so a
      // concurrent `use` cannot race against our (atomic) write.
      const rootClaudeMd = await ensureRootClaudeMdMarkers(opts.cwd, paths.tmpDir);

      return {
        profilesDirCreated: profilesDirCreated === "fresh",
        starterProfileSeeded,
        gitignore,
        hook,
        rootClaudeMd,
      } satisfies InitResult;
    },
    { signalHandlers: opts.signalHandlers },
  );

  emitOutput(opts.output, result, paths.projectRoot);
  return 0;
}

async function classifyProfilesDir(
  profilesDir: string,
): Promise<"fresh" | "already-initialised"> {
  // Invariant: `withLock` ran `fs.mkdir(metaDir, { recursive: true })` before
  // invoking us, so `profilesDir` always exists and contains `.meta/` (which
  // holds the lock, tmp staging, and any pending/prior reconciliation
  // artifacts). A pristine project therefore has exactly one entry: `.meta`.
  // Any sibling — a profile directory, stray file — means init has already
  // run.
  const entries = await fs.readdir(profilesDir);
  for (const e of entries) {
    if (e === ".meta") continue;
    return "already-initialised";
  }
  // Look inside .meta/ for prior-use evidence (Opus review P3 — pre-refactor
  // version of this check guarded against a populated top-level `.backup/`
  // being mistaken for fresh). The same concern applies one level down: a
  // populated `state.json` or `backup/` snapshot inside `.meta/` is proof of
  // prior init even when the user manually deleted their profile dirs. We
  // ignore lock/tmp/pending/prior because those are run-time artifacts that
  // a crash + retry can legitimately leave behind on a never-completed init.
  let metaEntries: string[];
  try {
    metaEntries = await fs.readdir(path.join(profilesDir, ".meta"));
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return "fresh";
    throw err;
  }
  for (const e of metaEntries) {
    if (e === "state.json") return "already-initialised";
    if (e === "backup") {
      const backupContents = await fs.readdir(path.join(profilesDir, ".meta", "backup"));
      if (backupContents.length > 0) return "already-initialised";
    }
  }
  return "fresh";
}

async function maybeSeedStarter(
  claudeDir: string,
  profilesDir: string,
  starterName: string,
): Promise<boolean> {
  // Only seed if `.claude/` exists and the target profile dir does not.
  let claudeStat: import("node:fs").Stats;
  try {
    claudeStat = await fs.stat(claudeDir);
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return false;
    throw err;
  }
  if (!claudeStat.isDirectory()) return false;

  const profileDir = path.join(profilesDir, starterName);
  try {
    await fs.mkdir(profileDir);
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "EEXIST") {
      // Conservative: if a same-named profile already exists, refuse rather
      // than merge. The user can re-run init with a different starter name.
      throw new CliUserError(
        `init: starter profile dir "${profileDir}" already exists; pick a different starter name`,
        EXIT_USER_ERROR,
      );
    }
    throw err;
  }

  // Copy `.claude/` into `<profileDir>/.claude/` (R27). Use fs.cp directly
  // here rather than going through state/copy.copyTree because that helper
  // is internal to the materialization path; init's copy is a one-shot
  // bootstrap operation, not a transactional rename.
  //
  // `dereference: true` follows symlinks: if the user's `.claude/` contains
  // links pointing outside the project (e.g. a shared config in $HOME),
  // those targets are physically copied into the seeded profile. This is
  // intentional — a profile must be self-contained so it can later be
  // checked into git, and a dangling cross-project symlink would defeat
  // that — but it does mean the seed may bring in more bytes than a naive
  // tree-copy would.
  const targetClaude = path.join(profileDir, ".claude");
  await fs.cp(claudeDir, targetClaude, {
    recursive: true,
    force: false,
    dereference: true,
    errorOnExist: false,
  });

  // Minimal profile.json (R27).
  const manifest = { name: starterName };
  const json = `${JSON.stringify(manifest, null, 2)}\n`;
  await fs.writeFile(path.join(profileDir, "profile.json"), json);
  return true;
}

/**
 * cw6 / spec §12.4: guarantee project-root CLAUDE.md exists and contains the
 * managed-block markers. Three branches:
 *
 *   - File absent → create from scratch with the canonical empty managed
 *     block. The `renderManagedBlock("")` output is what the spec example
 *     §12.2 shows.
 *   - File present without markers → append the marker block at end of file,
 *     preserving every prior byte byte-for-byte (handled inside
 *     `injectMarkersIntoFile`). The result still parses cleanly via
 *     parseMarkers, so the next `validate` / `use` invocation is happy.
 *   - File present with valid markers → no-op. Init is idempotent.
 *
 * All writes go through atomicWriteFile (temp + fsync + rename) so a crash
 * mid-init never leaves a half-written CLAUDE.md on disk. We share the
 * `.claude-profiles/.meta/tmp/` staging dir with the rest of E3 because it's
 * already on the same filesystem as the project root and gitignored.
 */
async function ensureRootClaudeMdMarkers(
  projectRoot: string,
  tmpDir: string,
): Promise<RootClaudeMdMarkerResult> {
  const claudeMdPath = path.join(projectRoot, "CLAUDE.md");

  let existing: string | null;
  try {
    existing = await fs.readFile(claudeMdPath, "utf8");
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      existing = null;
    } else {
      throw err;
    }
  }

  if (existing === null) {
    // Fresh file: emit the canonical marker block as the entire file body.
    // No leading content because there is no user content to preserve.
    const fresh = renderManagedBlock("");
    await fs.mkdir(tmpDir, { recursive: true });
    const tmpPath = uniqueAtomicTmpPath(tmpDir, claudeMdPath);
    try {
      await atomicWriteFile(claudeMdPath, tmpPath, fresh);
    } catch (err) {
      await fs.unlink(tmpPath).catch(() => undefined);
      throw err;
    }
    return { outcome: "created", path: claudeMdPath };
  }

  const parsed = parseMarkers(existing);
  if (parsed.found) {
    // Idempotent: well-formed markers already present.
    return { outcome: "already", path: claudeMdPath };
  }

  // File exists, no markers (or malformed — injectMarkersIntoFile handles
  // both by appending). Preserve all prior bytes; append marker block.
  const updated = injectMarkersIntoFile(existing);
  await fs.mkdir(tmpDir, { recursive: true });
  const tmpPath = uniqueAtomicTmpPath(tmpDir, claudeMdPath);
  try {
    await atomicWriteFile(claudeMdPath, tmpPath, updated);
  } catch (err) {
    await fs.unlink(tmpPath).catch(() => undefined);
    throw err;
  }
  return { outcome: "appended", path: claudeMdPath };
}

function emitOutput(output: OutputChannel, result: InitResult, projectRoot: string): void {
  if (output.jsonMode) {
    output.json({
      projectRoot,
      profilesDirCreated: result.profilesDirCreated,
      starterProfileSeeded: result.starterProfileSeeded,
      gitignoreCreated: result.gitignore.created,
      gitignoreAdded: result.gitignore.added,
      hook: result.hook
        ? {
            installed: result.hook.installed,
            hookPath: result.hook.hookPath,
            preExisting: result.hook.preExisting,
            skippedReason: result.hook.skippedReason,
          }
        : null,
      rootClaudeMd: {
        outcome: result.rootClaudeMd.outcome,
        path: result.rootClaudeMd.path,
      },
    });
    return;
  }

  // First-run UX (claude-code-profiles-pnf): banner, coloured ✓/⊙ status
  // glyphs, suggested next-step block. Style is auto-derived from TTY +
  // NO_COLOR + platform; non-TTY runs degrade to plain ASCII.
  const style = createStyle({
    isTty: Boolean(process.stdout.isTTY),
    platform: process.platform,
    noColor: process.env["NO_COLOR"],
  });

  output.print(style.banner(`claude-profiles initialised`));
  output.print(style.dim(`  ${projectRoot}`));
  output.print("");

  // Profiles dir
  output.print(`  ${style.ok(`Created .claude-profiles/`)}`);

  // Starter seed
  if (result.starterProfileSeeded !== null) {
    output.print(
      `  ${style.ok(`Seeded starter profile "${result.starterProfileSeeded}" from .claude/`)}`,
    );
  } else {
    output.print(`  ${style.skip(`No .claude/ to seed (skipped)`)}`);
  }

  // Gitignore
  if (result.gitignore.created) {
    output.print(
      `  ${style.ok(`Wrote .gitignore (${result.gitignore.added.length} entries)`)}`,
    );
  } else if (result.gitignore.added.length > 0) {
    output.print(
      `  ${style.ok(`Updated .gitignore (${result.gitignore.added.length} new entries)`)}`,
    );
  } else {
    output.print(`  ${style.skip(`.gitignore already up to date`)}`);
  }

  // Project-root CLAUDE.md marker outcome (cw6 / spec §12.4). Wording matches
  // T6 AC: the "appended" path uses the exact phrase the test asserts so
  // downstream tooling (and our own scenario fixtures) have a stable hook.
  if (result.rootClaudeMd.outcome === "created") {
    output.print(`  ${style.ok(`Created CLAUDE.md with claude-profiles markers`)}`);
  } else if (result.rootClaudeMd.outcome === "appended") {
    output.print(
      `  ${style.ok(`added claude-profiles markers to existing CLAUDE.md (your content preserved)`)}`,
    );
  } else {
    output.print(`  ${style.skip(`project-root CLAUDE.md markers already present`)}`);
  }

  // Hook
  if (result.hook) {
    if (result.hook.skippedReason === "no-git-dir") {
      output.print(
        `  ${style.warn(`Pre-commit hook NOT installed`)} ${style.dim(`(not a git project — run "git init" then "claude-profiles hook install")`)}`,
      );
    } else if (result.hook.installed) {
      output.print(
        `  ${style.ok(`Installed pre-commit hook`)} ${style.dim(`(${result.hook.hookPath})`)}`,
      );
    } else if (result.hook.preExisting === "ours") {
      output.print(
        `  ${style.skip(`Pre-commit hook already installed (${result.hook.hookPath})`)}`,
      );
    } else {
      output.print(
        `  ${style.warn(`Pre-commit hook NOT installed`)} ${style.dim(`(${result.hook.hookPath} contains a different script)`)}`,
      );
    }
  }

  // Next steps footer — small nudge so the user has somewhere to go after a
  // bare `init`. Suggested verbs cover the 90% first-run path.
  output.print("");
  output.print(`Next:`);
  output.print(
    `  ${style.dim("claude-profiles new <name>")}     scaffold a profile`,
  );
  output.print(
    `  ${style.dim("claude-profiles list")}           see profiles`,
  );
  output.print(
    `  ${style.dim("claude-profiles use <name>")}     activate a profile`,
  );
}
