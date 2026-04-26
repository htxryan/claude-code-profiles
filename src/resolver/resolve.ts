import * as path from "node:path";

import {
  ConflictError,
  CycleError,
  MissingIncludeError,
  MissingProfileError,
} from "../errors/index.js";

import { loadManifest } from "./manifest.js";
import { isMergeable, policyFor } from "./merge-policy.js";
import {
  buildPaths,
  classifyInclude,
  isValidProfileName,
  profileDir,
  type ResolverPaths,
} from "./paths.js";
import type {
  Contributor,
  ExternalTrustEntry,
  IncludeRef,
  PlanFile,
  ProfileManifest,
  ResolutionWarning,
  ResolvedPlan,
} from "./types.js";
import { RESOLVED_PLAN_SCHEMA_VERSION } from "./types.js";
import { isDirectory, walkClaudeDir, walkProfileRoot } from "./walk.js";

export interface ResolveOptions {
  /** Project root containing `.claude-profiles/`. */
  projectRoot: string;
}

/**
 * Resolve `profileName` into a ResolvedPlan.
 *
 * Algorithm (deterministic, single pass per phase):
 *  1. Walk extends chain upward, detecting cycles and missing profiles.
 *  2. Reverse to oldest-first order (R3 / R9).
 *  3. For each profile in chain, classify and validate its includes (R6/R7/R37).
 *     Includes contribute *after* their referencing profile's ancestors but
 *     before the next descendant. Per R9 worked example, the canonical order
 *     for `base ← extended ← profile` with `profile.includes = [A, B]` is:
 *     base, extended, A, B, profile.  So includes attach to whichever profile
 *     declared them, immediately AFTER that profile in the chain — but only
 *     for the *leaf* profile per the worked example. Other ancestors' includes
 *     come before them. We implement: walk oldest→newest; for each profile
 *     emit the profile, then its includes. EXCEPT the leaf — emit its
 *     includes *before* itself, so the leaf is last. This matches the worked
 *     example exactly.
 *  4. Walk every contributor's `.claude/` directory, collecting PlanFiles.
 *  5. Detect R11 conflicts on non-mergeable files.
 *  6. Sort files lex by relPath, stable by contributorIndex.
 *
 * Throws ResolverError subclasses on R4/R5/R7/R11. Returns warnings for R36.
 */
export async function resolve(
  profileName: string,
  opts: ResolveOptions,
): Promise<ResolvedPlan> {
  const paths = buildPaths(opts.projectRoot);
  const warnings: ResolutionWarning[] = [];

  // 1. Build extends chain (newest → oldest), detecting cycle/missing.
  const newestFirst = await buildExtendsChain(profileName, paths);
  // 2. Reverse to oldest-first (canonical order for R3, R9).
  const oldestFirst = [...newestFirst].reverse();

  // 3. Build contributors in canonical order. Per the R9 worked example,
  // includes for the leaf profile precede the leaf itself; ancestors' includes
  // are emitted immediately after the ancestor that declared them.
  const contributors: Contributor[] = [];
  const includes: IncludeRef[] = [];
  const externalPaths: ExternalTrustEntry[] = [];
  const seenExternal = new Set<string>();
  // Dedup include contributors by resolved path. Two raw strings (e.g.
  // "compA" and "./../_components/compA") that resolve to the same dir
  // would otherwise produce two contributors and falsely trip R11
  // self-conflicts on non-mergeable files. Each duplicate yields a warning.
  const seenContributorPaths = new Set<string>();

  const leafIndex = oldestFirst.length - 1;
  for (let i = 0; i < oldestFirst.length; i++) {
    const entry = oldestFirst[i]!;
    const isLeaf = i === leafIndex;

    if (!isLeaf) {
      // Ancestor: emit ancestor first, then its includes.
      contributors.push(makeAncestorContributor(entry.name, paths, entry.manifest));
      await emitIncludes(
        entry.includes,
        entry.dir,
        entry.name,
        paths,
        contributors,
        includes,
        externalPaths,
        seenExternal,
        seenContributorPaths,
        warnings,
      );
    } else {
      // Leaf: emit its includes first, then the leaf last.
      await emitIncludes(
        entry.includes,
        entry.dir,
        entry.name,
        paths,
        contributors,
        includes,
        externalPaths,
        seenExternal,
        seenContributorPaths,
        warnings,
      );
      contributors.push(makeProfileContributor(entry.name, paths, entry.manifest));
    }
  }

  // 4. Walk every contributor's .claude/ and collect files.
  const files: PlanFile[] = await collectFiles(contributors);

  // 5. Conflict detection (R11). Group files by the composite key
  // (relPath, destination). cw6/T3: a file at the same relPath in two
  // different destinations is, by construction, NOT a conflict — the merge
  // engine groups by the same composite key and produces two independent
  // MergedFile entries. R11 must therefore be scoped to a single destination
  // group as well; otherwise two contributors at e.g. `.claude/agents/x.json`
  // and `<projectRoot>/agents/x.json` (hypothetical) would falsely conflict.
  // Today this is benign because the only projectRoot file is CLAUDE.md
  // (concat policy → exempt from R11), but we encode the correct invariant
  // rather than relying on that policy coincidence.
  const byKey = new Map<string, PlanFile[]>();
  for (const f of files) {
    const key = `${f.destination}::${f.relPath}`;
    let arr = byKey.get(key);
    if (!arr) {
      arr = [];
      byKey.set(key, arr);
    }
    arr.push(f);
  }
  for (const group of byKey.values()) {
    if (group.length < 2) continue;
    const relPath = group[0]!.relPath;
    if (isMergeable(relPath)) continue; // R8/R9/R12 handle these in E2
    detectConflict(relPath, group, contributors);
  }

  // 6. Sort files: lex by relPath, then contributorIndex, then destination
  // (preserves canonical last-wins ordering when downstream consumers iterate;
  // destination tiebreak makes ordering deterministic when a single contributor
  // emits the same relPath to both destinations — cw6/T2).
  files.sort((a, b) => {
    if (a.relPath < b.relPath) return -1;
    if (a.relPath > b.relPath) return 1;
    if (a.contributorIndex !== b.contributorIndex)
      return a.contributorIndex - b.contributorIndex;
    if (a.destination === b.destination) return 0;
    return a.destination < b.destination ? -1 : 1;
  });

  // Aggregate manifest warnings.
  for (const entry of oldestFirst) {
    warnings.push(...entry.warnings);
  }

  const chain = oldestFirst.map((e) => e.name);

  return {
    schemaVersion: RESOLVED_PLAN_SCHEMA_VERSION,
    profileName,
    chain,
    includes,
    contributors,
    files,
    warnings,
    externalPaths,
  };
}

interface ChainEntry {
  /** Canonical profile name (directory name). */
  name: string;
  /** Absolute profile directory. */
  dir: string;
  /** Full parsed manifest (used for Contributor.manifest). */
  manifest: ProfileManifest;
  /** Includes from this profile's manifest, in declaration order. */
  includes: string[];
  /** Manifest-level warnings from loading this profile. */
  warnings: ResolutionWarning[];
}

async function buildExtendsChain(
  profileName: string,
  paths: ResolverPaths,
): Promise<ChainEntry[]> {
  const chain: ChainEntry[] = [];
  const visitedOrder: string[] = []; // for ordered cycle messages
  const visitedSet = new Set<string>(); // O(1) membership for cycle check
  let current: string | undefined = profileName;
  let referencedBy: string | undefined;

  while (current !== undefined) {
    // R2: profile identifiers are bare directory names. Reject anything
    // that could escape `.claude-profiles/` via traversal, slashes, or the
    // hidden/`_components` conventions before touching the filesystem.
    if (!isValidProfileName(current)) {
      throw new MissingProfileError(current, referencedBy);
    }

    if (visitedSet.has(current)) {
      // Cycle: slice starting at the first occurrence and append `current`
      // again so the cycle reads naturally: a → b → a.
      const start = visitedOrder.indexOf(current);
      const cycle = [...visitedOrder.slice(start), current];
      throw new CycleError(cycle);
    }
    visitedSet.add(current);
    visitedOrder.push(current);

    const dir = profileDir(paths, current);
    if (!(await isDirectory(dir))) {
      throw new MissingProfileError(current, referencedBy);
    }

    const { manifest, warnings: manifestWarnings } = await loadManifest(dir, current);

    // Track the leaf-side warnings on the entry; the resolver concatenates
    // them into ResolvedPlan.warnings later.
    chain.push({
      name: current,
      dir,
      manifest,
      includes: manifest.includes ?? [],
      warnings: manifestWarnings,
    });

    referencedBy = current;
    current = manifest.extends;
  }

  return chain;
}

async function emitIncludes(
  rawIncludes: string[],
  referencingProfileDir: string,
  referencedBy: string,
  paths: ResolverPaths,
  contributors: Contributor[],
  includes: IncludeRef[],
  externalPaths: ExternalTrustEntry[],
  seenExternal: Set<string>,
  seenContributorPaths: Set<string>,
  warnings: ResolutionWarning[],
): Promise<void> {
  for (const raw of rawIncludes) {
    const ref = classifyInclude(raw, referencingProfileDir, paths, referencedBy);
    includes.push(ref);

    if (!(await isDirectory(ref.resolvedPath))) {
      throw new MissingIncludeError(raw, ref.resolvedPath, referencedBy);
    }

    if (seenContributorPaths.has(ref.resolvedPath)) {
      // Same directory already contributed (possibly via a different raw
      // form). Skip to avoid duplicate Contributor entries; otherwise R11
      // would self-conflict on non-mergeable files and mergeable files
      // would be processed twice.
      warnings.push({
        code: "DuplicateInclude",
        message: `Include "${raw}" in profile "${referencedBy}" resolves to "${ref.resolvedPath}", which was already included; skipping duplicate`,
        source: referencedBy,
      });
      continue;
    }
    seenContributorPaths.add(ref.resolvedPath);

    contributors.push({
      kind: "include",
      id: raw,
      rootPath: ref.resolvedPath,
      claudeDir: path.join(ref.resolvedPath, ".claude"),
      external: ref.external,
    });

    if (ref.external && !seenExternal.has(ref.resolvedPath)) {
      seenExternal.add(ref.resolvedPath);
      externalPaths.push({ raw, resolvedPath: ref.resolvedPath });
    }
  }
}

function makeAncestorContributor(
  name: string,
  paths: ResolverPaths,
  manifest: ProfileManifest,
): Contributor {
  const dir = profileDir(paths, name);
  return {
    kind: "ancestor",
    id: name,
    rootPath: dir,
    claudeDir: path.join(dir, ".claude"),
    external: false,
    manifest,
  };
}

function makeProfileContributor(
  name: string,
  paths: ResolverPaths,
  manifest: ProfileManifest,
): Contributor {
  const dir = profileDir(paths, name);
  return {
    kind: "profile",
    id: name,
    rootPath: dir,
    claudeDir: path.join(dir, ".claude"),
    external: false,
    manifest,
  };
}

async function collectFiles(contributors: Contributor[]): Promise<PlanFile[]> {
  const out: PlanFile[] = [];
  for (let i = 0; i < contributors.length; i++) {
    const c = contributors[i]!;

    // 1. The historical .claude/ subtree → destination='.claude'.
    const claudeEntries = await walkClaudeDir(c.claudeDir);
    for (const e of claudeEntries) {
      out.push({
        relPath: e.relPath,
        absPath: e.absPath,
        contributorIndex: i,
        mergePolicy: policyFor(e.relPath),
        destination: ".claude",
      });
    }

    // 2. cw6/T2: profile-root files (CLAUDE.md peer-of-profile.json) →
    // destination='projectRoot'. Per spec §12 the merge policy is the same
    // (CLAUDE.md → 'concat'); only the destination grouping differs.
    const rootEntries = await walkProfileRoot(c.rootPath);
    for (const e of rootEntries) {
      out.push({
        relPath: e.relPath,
        absPath: e.absPath,
        contributorIndex: i,
        mergePolicy: policyFor(e.relPath),
        destination: "projectRoot",
      });
    }
  }
  return out;
}

/**
 * R11 conflict detection. Given multiple non-mergeable contributions for the
 * same relPath, decide if it's a conflict.
 *
 *  - profile-itself contributing → never a conflict (profile always overrides)
 *  - 2+ ancestors only           → no conflict (R10 last-wins among ancestors)
 *  - any include involved        → conflict
 */
function detectConflict(
  relPath: string,
  group: PlanFile[],
  contributors: Contributor[],
): void {
  const kinds = group.map((f) => contributors[f.contributorIndex]!.kind);
  if (kinds.includes("profile")) return; // profile overrides
  const hasInclude = kinds.includes("include");
  if (!hasInclude) return; // ancestor-only chain → R10 last-wins

  const offenders = group.map((f) => contributors[f.contributorIndex]!.id);
  // Deduplicate while preserving order (a single include directory could
  // theoretically be listed twice via different raw strings).
  const dedup = Array.from(new Set(offenders));
  throw new ConflictError(relPath, dedup);
}
