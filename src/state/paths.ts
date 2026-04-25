/**
 * Canonical paths for E3. All on-disk artifacts are addressed through this
 * single helper so reconcilation, materialization, and persist agree on what
 * lives where.
 *
 * Co-located with the resolver's path module (resolver/paths.ts) which owns
 * the resolution-side path conventions. E3 paths are deliberately a separate
 * struct because they include staging/reconciliation paths that the resolver
 * has no business reading.
 */

import * as path from "node:path";

export interface StatePaths {
  /** Absolute project root. */
  projectRoot: string;
  /** Absolute path to `.claude-profiles/`. */
  profilesDir: string;
  /** Absolute path to the live `.claude/` tree. */
  claudeDir: string;
  /** Absolute path to `.claude-profiles/.state.json`. */
  stateFile: string;
  /** Absolute path to `.claude-profiles/.lock`. */
  lockFile: string;
  /**
   * Directory inside `.claude-profiles/` reserved for atomic-write staging
   * (`.tmp/`). Per-call unique tmp filenames live here so concurrent writers
   * never share a staging path and crash debris stays isolated.
   */
  tmpDir: string;
  /** Materialize-side staging (R16 step a). */
  pendingDir: string;
  /** Materialize-side prior backup (R16 step b). */
  priorDir: string;
  /** Discard backup root `.claude-profiles/.backup/`. */
  backupDir: string;
  /** Project-root `.gitignore`. */
  gitignoreFile: string;
}

/**
 * Build the canonical path bundle. `projectRoot` is resolved to an absolute
 * path so callers may pass relative roots in tests.
 */
export function buildStatePaths(projectRoot: string): StatePaths {
  const root = path.resolve(projectRoot);
  const profilesDir = path.join(root, ".claude-profiles");
  return {
    projectRoot: root,
    profilesDir,
    claudeDir: path.join(root, ".claude"),
    stateFile: path.join(profilesDir, ".state.json"),
    lockFile: path.join(profilesDir, ".lock"),
    tmpDir: path.join(profilesDir, ".tmp"),
    pendingDir: path.join(profilesDir, ".pending"),
    priorDir: path.join(profilesDir, ".prior"),
    backupDir: path.join(profilesDir, ".backup"),
    gitignoreFile: path.join(root, ".gitignore"),
  };
}

/**
 * Persist-side paths for the active profile (R22b transactional pair). The
 * persist flow uses the same pending/prior protocol as materialize, but
 * targeting the *profile's* `.claude/` directory rather than the live one.
 */
export interface PersistPaths {
  /** Absolute path to `.claude-profiles/<profile>/`. */
  profileDir: string;
  /** Absolute path to `.claude-profiles/<profile>/.claude/` (target). */
  targetClaudeDir: string;
  /** Persist staging dir. */
  pendingDir: string;
  /** Persist prior backup. */
  priorDir: string;
}

// Windows-reserved device names per MS-DOS legacy. Reject these regardless
// of platform so a profile created on POSIX cannot land on Windows under a
// reserved name.
const WIN_RESERVED = /^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\..*)?$/i;

export function buildPersistPaths(paths: StatePaths, profileName: string): PersistPaths {
  // Defense-in-depth (multi-reviewer P2, Gemini #5; Opus #2): profile names
  // are validated by the resolver (`isValidProfileName`) before they reach
  // this path, but we re-validate here so any caller that bypasses the
  // resolver can't traverse the profilesDir boundary. The check uses BOTH
  // POSIX and Win32 basename so `"a\\b"` is rejected on Linux/macOS — those
  // bytes would be a separator if the path were ever consumed by a Windows
  // process via shared state. We also reject NUL bytes and Windows-reserved
  // device names for cross-platform safety.
  if (
    profileName.length === 0 ||
    profileName.startsWith(".") ||
    profileName.includes("\0") ||
    path.posix.basename(profileName) !== profileName ||
    path.win32.basename(profileName) !== profileName ||
    WIN_RESERVED.test(profileName)
  ) {
    throw new Error(`Invalid profile name for persist target: ${JSON.stringify(profileName)}`);
  }
  const profileDir = path.join(paths.profilesDir, profileName);
  return {
    profileDir,
    targetClaudeDir: path.join(profileDir, ".claude"),
    pendingDir: path.join(profileDir, ".pending"),
    priorDir: path.join(profileDir, ".prior"),
  };
}
