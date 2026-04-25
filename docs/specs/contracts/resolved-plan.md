# ResolvedPlan — Cross-Epic Contract (locked)

**Schema version**: 1
**Status**: Locked 2026-04-25
**Owner**: E1 (Manifest + Resolution)
**Consumers**: E2 (Merge), E3 (Materialization + State + Concurrency), E4 (Drift), E5 (CLI + Swap Orchestration), E7 (Integration Verification)

Per the E1 fitness function this schema must remain stable for ≥ 2 weeks once locked. Breaking changes require coordinated bumps across all consumer epics and a `schemaVersion` increment.

The authoritative type definitions live in `src/resolver/types.ts`; this document is the consumer-facing summary.

## ResolvedPlan

```ts
interface ResolvedPlan {
  schemaVersion: 1;
  profileName: string;            // canonical id; chain[chain.length-1]
  chain: string[];                // R3, oldest ancestor first → leaf last
  includes: IncludeRef[];         // R6/R37, declaration order across the whole chain
  contributors: Contributor[];    // canonical resolution order (see below)
  files: PlanFile[];              // lex-sorted by relPath, stable by contributorIndex
  warnings: ResolutionWarning[];  // R36 + missing manifests, non-fatal
  externalPaths: ExternalTrustEntry[]; // R37a, deduped by resolvedPath
}
```

## Invariants (enforced by tests in `tests/resolver/`)

1. `chain[chain.length - 1] === profileName`.
2. `chain[0]` is the most distant ancestor; the chain is linear (single parent per R3).
3. `contributors` is in canonical merge order:
   - For each ancestor in the chain (oldest → newest): the ancestor itself, then its includes (declaration order).
   - For the leaf profile: includes (declaration order) **first**, then the leaf itself **last**.
   - This matches the R9 worked example: `base ← extended ← profile` with `profile.includes = [A, B]` → `base, extended, A, B, profile`.
4. `files` is lex-sorted by `relPath`; ties broken by ascending `contributorIndex`.
5. Every `PlanFile.contributorIndex` is a valid index into `contributors`.
6. The same `(relPath, contributorIndex)` pair never appears twice.
7. Conflict files (R11) **never** appear in a returned plan — the resolver throws `ConflictError` instead. Callers do not need to filter conflicts.
8. `externalPaths` contains at most one entry per `resolvedPath`. `seenExternal` dedup happens at resolve time.

## Conflict semantics (R11) — what triggers a throw

A non-mergeable file (anything that's not `settings.json` or `*.md`, see `policyFor()`) defined by ≥ 2 contributors throws `ConflictError`, **except**:

- The profile itself (`kind === "profile"`) being a contributor suppresses the conflict — the profile always overrides.
- Two or more **ancestors** contributing the same path is **not** a conflict (R10 last-wins applies among ancestors).
- The conflict only fires when ≥ 1 `include` contributor is involved.

Mergeable files (`settings.json`, `*.md`) never throw at this layer — E2 handles their byte-level merge.

## Errors thrown by `resolve()`

All extend `ResolverError`. Each carries enough context to satisfy §7 of the spec ("must always name the file/profile/path").

| Error                  | EARS  | Trigger                                                         |
|------------------------|-------|-----------------------------------------------------------------|
| `MissingProfileError`  | R5    | `extends` references a non-existent profile (or root missing)   |
| `CycleError`           | R4    | extends chain contains a cycle                                  |
| `MissingIncludeError`  | R7    | includes entry's `resolvedPath` does not exist                  |
| `ConflictError`        | R11   | two contributors define same non-mergeable file (rules above)   |
| `InvalidManifestError` | —     | manifest unparseable, type-mismatched, or include syntactically invalid |

## R37 include syntactic forms

`IncludeRef.kind` enumerates exactly:

- `"component"` — bare name; resolved against `.claude-profiles/_components/<name>/`
- `"relative"` — `./...` or `../...`; resolved from the referencing profile's directory
- `"absolute"` — `/...`
- `"tilde"` — `~/...` or bare `~`; expanded against `os.homedir()`

`external: boolean` is **orthogonal** to `kind`. Both `absolute` and `tilde` may or may not be external (`absolute` paths inside the project root are non-external; `tilde` paths are external if `$HOME` is outside the project root, which is almost always).

Anything else — bare-with-slashes (`foo/bar`), `~user/...`, empty string — throws `InvalidManifestError` at classification time.

## Worked example

Given:
```
.claude-profiles/
  base/{profile.json: {}, .claude/CLAUDE.md, .claude/settings.json}
  extended/{profile.json: {extends: "base"}, .claude/CLAUDE.md}
  leaf/{profile.json: {extends: "extended", includes: ["compA", "compB"]}, .claude/CLAUDE.md}
  _components/
    compA/.claude/CLAUDE.md
    compB/.claude/CLAUDE.md
```

`resolve("leaf", ...)` produces:

- `chain = ["base", "extended", "leaf"]`
- `contributors[*].id = ["base", "extended", "compA", "compB", "leaf"]`
- `contributors[*].kind = ["ancestor", "ancestor", "include", "include", "profile"]`
- `files` lex-sorted; `CLAUDE.md` appears 5 times (once per contributor), `mergePolicy === "concat"`; `settings.json` appears 1 time, `mergePolicy === "deep-merge"`.

## Backward-compat guarantees while v1 is current

- New optional fields may be added to `Contributor`, `PlanFile`, `ResolvedPlan` without bumping schemaVersion.
- Existing fields' types and presence will not change without a schemaVersion bump.
- Conflict semantics (which paths throw) will not change without a bump.
