import * as os from "node:os";
import * as path from "node:path";

import { InvalidManifestError } from "../errors/index.js";

import type { IncludeRef, IncludeSourceKind } from "./types.js";

export interface ResolverPaths {
  /** Absolute project root. */
  projectRoot: string;
  /** Absolute path to .claude-profiles. */
  profilesDir: string;
  /** Absolute path to .claude-profiles/_components. */
  componentsDir: string;
}

export function buildPaths(projectRoot: string): ResolverPaths {
  const root = path.resolve(projectRoot);
  return {
    projectRoot: root,
    profilesDir: path.join(root, ".claude-profiles"),
    componentsDir: path.join(root, ".claude-profiles", "_components"),
  };
}

export function profileDir(paths: ResolverPaths, name: string): string {
  return path.join(paths.profilesDir, name);
}

/**
 * Windows-reserved DOS device names. Rejected on every host OS so a profile
 * authored on Linux/macOS cannot land on Windows under a name the kernel
 * refuses to open. Match is case-insensitive and includes any extension
 * (CONFIG.SYS-style "CON.txt" → CON-the-device on Windows).
 *
 * Source: CONTRIBUTING.md "Reserved filenames" + R39 cross-platform contract.
 */
const WIN_RESERVED_NAMES = /^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\..*)?$/i;

/**
 * R2: a profile identifier is the bare directory name under
 * `.claude-profiles/`. Reject anything that could escape that boundary
 * (slashes, backslashes, parent-traversal, leading `.` or `_` per the
 * `_components`/hidden-dir convention enforced by listProfiles, empty).
 *
 * Cross-platform safety (R39 / CONTRIBUTING.md): also reject names that
 * Windows's filesystem refuses — the DOS-device names CON/PRN/AUX/NUL/
 * COM1-9/LPT1-9 (with or without an extension), and trailing dot or space
 * (silently stripped by Win32 → name collisions). Enforced on every host
 * so a profile authored on Linux/macOS remains valid on Windows.
 *
 * Predicate; callers convert a `false` result into a CliUserError or
 * MissingProfileError depending on context.
 */
export function isValidProfileName(name: string): boolean {
  if (name.length === 0) return false;
  if (name === "." || name === "..") return false;
  if (name.startsWith("_") || name.startsWith(".")) return false;
  if (name.includes("/") || name.includes("\\") || name.includes(path.sep)) return false;
  if (name.includes("\0")) return false;
  if (WIN_RESERVED_NAMES.test(name)) return false;
  // Win32 silently strips trailing dot and space → 'foo.' and 'foo ' both
  // resolve to 'foo'. Reject so a profile authored on POSIX cannot collide
  // with another profile on Windows after path normalisation.
  if (name.endsWith(".") || name.endsWith(" ")) return false;
  return true;
}

/**
 * True iff `name` matches a Windows reserved DOS-device name. Exported for
 * defense-in-depth callers (state/paths.ts) that re-validate at the persist
 * boundary. Predicate-only; callers shape the error themselves.
 */
export function isWindowsReservedName(name: string): boolean {
  return WIN_RESERVED_NAMES.test(name);
}

/**
 * Classify and resolve an includes entry per R37. The four valid forms:
 *  - `bare-name`         → component (rooted at `_components/<name>/`)
 *  - `./...` or `../...` → relative (resolved from the referencing profile dir)
 *  - `/...`              → absolute
 *  - `~/...` or `~`      → tilde (expanded against $HOME)
 *
 * Anything else — notably bare strings containing `/` or platform separators
 * that aren't `./`/`../`/absolute — is rejected via InvalidManifestError.
 *
 * `external` is the orthogonal semantic flag: true if the resolved path
 * falls outside the project root. `component` is always non-external (rooted
 * under `_components/`). `absolute`, `tilde`, and `relative` may be external
 * — for example, `./../../outside-project` is a valid relative form that
 * resolves outside the project root and is correctly flagged.
 */
export function classifyInclude(
  raw: string,
  referencingProfileDir: string,
  paths: ResolverPaths,
  referencedBy: string,
): IncludeRef {
  let kind: IncludeSourceKind;
  let resolvedPath: string;

  if (raw.length === 0) {
    throw new InvalidManifestError(
      referencingProfileDir,
      `empty include string in profile "${referencedBy}"`,
    );
  }

  // `.` and `..` are not legal R37 forms — they have no `./`/`../` prefix
  // so they would otherwise fall through to the bare-component branch and
  // resolve to `_components/` itself or `.claude-profiles/`, neither of
  // which is the author's intent. Reject up-front.
  if (raw === "." || raw === "..") {
    throw new InvalidManifestError(
      referencingProfileDir,
      `include "${raw}" in profile "${referencedBy}" is not a valid form — use a bare component name, "./..." / "../..." for relative, "/..." for absolute, or "~/..." for home-relative`,
    );
  }

  if (raw === "~" || raw.startsWith("~/")) {
    const home = os.homedir();
    const rest = raw === "~" ? "" : raw.slice(2); // strip "~/"
    resolvedPath = rest === "" ? path.resolve(home) : path.resolve(home, rest);
    kind = "tilde";
  } else if (raw.startsWith("~")) {
    // "~user" form: not portable, not in R37. Reject.
    throw new InvalidManifestError(
      referencingProfileDir,
      `include "${raw}" in profile "${referencedBy}" — "~user" form is not supported; use "~/path" or an absolute path`,
    );
  } else if (path.isAbsolute(raw)) {
    resolvedPath = path.resolve(raw);
    kind = "absolute";
  } else if (raw.startsWith("./") || raw.startsWith("../")) {
    resolvedPath = path.resolve(referencingProfileDir, raw);
    kind = "relative";
  } else if (raw.includes("/") || raw.includes("\\") || raw.includes(path.sep)) {
    // R37 admits exactly four forms; a bare-with-slashes string matches
    // none of them. Reject explicitly rather than silently routing. We
    // reject backslashes on every platform so a manifest authored on
    // Windows behaves identically on Linux/macOS.
    throw new InvalidManifestError(
      referencingProfileDir,
      `include "${raw}" in profile "${referencedBy}" is not a valid form — use a bare component name, "./..." / "../..." for relative, "/..." for absolute, or "~/..." for home-relative`,
    );
  } else {
    resolvedPath = path.join(paths.componentsDir, raw);
    kind = "component";
  }

  const external = isExternal(resolvedPath, paths.projectRoot);
  return { raw, kind, resolvedPath, external };
}

export function isExternal(absPath: string, projectRoot: string): boolean {
  const rel = path.relative(projectRoot, absPath);
  if (rel === "") return false;
  // Match only `..` (the parent itself) or a `..` segment followed by the
  // platform separator. A bare filename like `..hidden` whose first
  // characters happen to be `..` is a legitimate in-root path, not a
  // traversal segment — `rel.startsWith("..")` would have wrongly flagged
  // it external.
  if (rel === ".." || rel.startsWith(".." + path.sep)) return true;
  return path.isAbsolute(rel);
}
