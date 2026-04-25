# MergedFile — Cross-Epic Contract (locked)

**Schema version**: 1
**Status**: Locked 2026-04-25
**Owner**: E2 (Merge Engine)
**Consumers**: E3 (Materialization + State + Concurrency), E5 (CLI — `validate` dry-run, `status`/`diff` provenance), E7 (Integration Verification)

The merge engine is a pure transformation: ordered contributors in, byte-level merged outputs out. The `MergedFile[]` returned by `merge(plan)` is the load-bearing contract that E3 writes to disk and E5 inspects without writing.

The authoritative type definitions live in `src/merge/types.ts`; this document is the consumer-facing summary.

## MergedFile

```ts
interface MergedFile {
  path: string;            // relative posix path inside `.claude/` (matches PlanFile.relPath)
  bytes: Buffer;           // exact bytes to materialize
  contributors: string[];  // contributor ids that contributed bytes, in canonical order
  mergePolicy: "deep-merge" | "concat" | "last-wins";
}
```

## Invariants (enforced by tests in `tests/merge/`)

1. One `MergedFile` per distinct relPath in the input plan; outputs are lex-sorted by `path` (matches `ResolvedPlan.files` ordering).
2. `contributors` is a non-empty subset of `plan.contributors[*].id`, in canonical order:
   - For `last-wins`: exactly one id (the last contributor).
   - For `deep-merge`: every contributor that supplied a parseable `settings.json` (including ones that parsed to `{}`) — the contributor still "owns" the file in provenance, even if removing it would produce identical output. E5 displays should treat empty-`{}` contributors as participants for traceability.
   - For `concat`: every contributor that supplied **non-empty** bytes, in canonical order. Empty contributors are skipped from both the output and provenance — symmetric with `settings.json`'s `{}` treatment in terms of behavioral impact.
3. `mergePolicy` mirrors the policy used by the registry (function of `path` only, classified once by E1).
4. `bytes` is byte-stable: the engine is a pure function of `(orderedContributors, mergeStrategy)` and never mutates inputs.

## Per-strategy semantics

### `deep-merge` — `settings.json` (R8 + R12)

Default (R8):
- Objects merge recursively.
- Arrays at the same path are **replaced** by the later contributor.
- Scalars and type-mismatches: later wins.

Carve-out (R12, takes precedence over R8):
- At the path `hooks.<EventName>` (depth 2: top-level `hooks` object → event-name key), action arrays are **concatenated** in canonical order rather than replaced.
- The carve-out fires only when both sides at that path are arrays. Type mismatches fall back to R8 last-wins. The carve-out does **not** fire at depth 1 (`{ hooks: [...] }`) or any depth deeper than 2.
- A non-array value at `hooks.<EventName>` from any contributor "resets" the slot under R8: previously-accumulated action entries are discarded, and a later array contributor concatenates from that reset point only. This is rare in practice but tested in `tests/merge/deep-merge.test.ts`.

Empty/whitespace bytes parse as `{}`. Unparseable JSON throws `InvalidSettingsJsonError`.

### `concat` — `*.md` (R9)

Concatenates contributor bytes in canonical resolution order:
ancestors (oldest → newest), then includes (declaration order),
then the leaf profile last.

Worked example: `base ← extended ← profile` with `profile.includes = [compA, compB]` produces `base, extended, compA, compB, profile`.

A separator `\n` is inserted only when the preceding chunk does not already end in `\n` — preserving existing trailing newlines without doubling them.

### `last-wins` — everything else (R10)

Returns the bytes of the last (highest contributor index) contributor verbatim. Single-contributor case is the trivial pass-through.

R11 conflict detection happens at resolve time (E1) — `merge` will never see an unresolvable conflict on a non-mergeable file. See `resolved-plan.md` for the conflict rules.

## Errors thrown by `merge()`

All extend `ResolverError`. Each carries enough context to satisfy §7 of the spec.

| Error                       | Trigger                                                  |
|-----------------------------|----------------------------------------------------------|
| `InvalidSettingsJsonError`  | A contributor's `settings.json` failed to parse as JSON. |
| `MergeReadFailedError`      | A file declared in the plan could not be read from disk. |

`MergeReadFailedError` indicates plan/disk drift between resolve and merge — most likely a contributor file was deleted while a swap was in flight.

## IO surface

`merge(plan, opts?)` reads bytes from `PlanFile.absPath` by default. Tests and orchestrators that already have bytes in memory may pass `opts.read` to bypass disk. The engine never writes — materialization is E3's job.

## Worked example (round-trip)

Given the resolver's worked example (see `resolved-plan.md`), `merge()` produces:

- `MergedFile { path: "CLAUDE.md", mergePolicy: "concat", contributors: ["base", "extended", "compA", "compB", "leaf"], bytes: <5-section concat> }`
- `MergedFile { path: "settings.json", mergePolicy: "deep-merge", contributors: <every contributor whose settings.json parsed>, bytes: <deep-merged JSON with hooks.<E> concatenated> }`
- One `MergedFile { mergePolicy: "last-wins" }` per non-mergeable path, contributing only the last source's bytes.

## Backward-compat guarantees while v1 is current

- New optional fields may be added to `MergedFile` without bumping `MERGED_FILE_SCHEMA_VERSION`.
- Existing fields' types and presence will not change without a bump.
- Strategy semantics (R8/R9/R10/R11/R12) will not change without a bump and a coordinated spec edit.

## Fitness function

The integration test in `tests/merge/integration.test.ts` ("E2 fitness function: hooks-precedence integration") is the durable gate. It must stay green across spec edits. Removing or relaxing it requires explicit coordination with E3 and a contract bump.
