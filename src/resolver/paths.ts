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
 * R2: a profile identifier is the bare directory name under
 * `.claude-profiles/`. Reject anything that could escape that boundary
 * (slashes, backslashes, parent-traversal, leading `.` or `_` per the
 * `_components`/hidden-dir convention enforced by listProfiles, empty).
 *
 * Predicate; callers convert a `false` result into MissingProfileError so the
 * caller can attach the appropriate `referencedBy` context for the message.
 */
export function isValidProfileName(name: string): boolean {
  if (name.length === 0) return false;
  if (name === "." || name === "..") return false;
  if (name.startsWith("_") || name.startsWith(".")) return false;
  if (name.includes("/") || name.includes("\\") || name.includes(path.sep)) return false;
  return true;
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
  return rel.startsWith("..") || path.isAbsolute(rel);
}
