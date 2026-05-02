# Integration test index — `tests/cli/integration/`

This directory contains the spawn-based CLI integration tests that the c3p
binary must pass on every supported platform. Each file exercises the bin
end-to-end via `tests/cli/integration/spawn.ts`; in-process internal-import
tests live elsewhere under `tests/`.

## Inventory

### Translated parity tests (PR4 — Go translation lives in `tests/integration/`)

| File | What it covers |
|------|----------------|
| `scenarios.test.ts`               | E7 cross-epic gate: S1–S18 + ppo did-you-mean + ResolvedPlan provenance |
| `exit-codes.test.ts`              | Exit-code matrix (R29 / E5 fitness function) |
| `gate-matrix.test.ts`             | Drift gate state machine — non-interactive cells |
| `concurrent.test.ts`              | S14: concurrent invocations, lock arbitration |
| `json-roundtrip.test.ts`          | `--json` byte-equivalence per verb (PR3 source-of-truth) |
| `hook-byte-equality.test.ts`      | R25a: pre-commit hook script bytes are frozen |
| `help-version.test.ts`            | `--help` / `--version` dispatch |
| `completions.test.ts`             | bash/zsh/fish completion script generation |
| `doctor.test.ts`                  | `c3p doctor` health-check command |
| `epipe.test.ts`                   | EPIPE-safe stdio (qga: pipe-into-grep) |
| `preview-snapshots.test.ts`       | `--preview` body rendering |
| `status.test.ts`                  | `c3p status` — clean / drifted / stale / R42 corruption |
| `back-compat-section-ownership.test.ts` | cw6 backwards-compat for legacy `.state.json` files |
| `root-claude-md-preflight.test.ts`      | cw6 R45 markers preflight |
| `chaos-merge-read-failed.test.ts`       | E2 `MergeReadFailed` runtime drift |
| `section-ownership-e2e.test.ts`         | cw6 destination='projectRoot' end-to-end |

### Rewritten (was TS-internal, now spawn-only — PR5)

| File | Notes |
|------|-------|
| `style-snapshots.test.ts`         | Color/glyph rendering — currently in-process, IV will rewrite spawn-only |
| `skim-output.test.ts`             | `runDiff` output — currently in-process, IV rewrite spawn-only |
| `sigint.test.ts`                  | SIGINT releases lock — exercises `dist/state/lock.js` directly via lock-holder.mjs |

### Gap-closure tests — F2 epic claude-code-profiles-yhb (PR6 #1–11)

These tests pin contracts the existing TS suite did not cover. Each is
written against the TS bin first; the Go translation in IV (epic
claude-code-profiles-ijw) ports them assertion-for-assertion.

| # | File | Coverage |
|---|------|----------|
| 1  | `non-interactive.test.ts`   | Drift gate via non-TTY spawn (PR6 #1; PTY harness deferred per PR6a) |
| 2  | `sigint-bin.test.ts`        | SIGINT delivered to actual bin under held lock (PR6 #2; complements `sigint.test.ts`) |
| 3  | `crash-recovery.test.ts`    | 2 mandatory crash-injection cases pre-1.0 (PR6 #3; remaining 3 deferred per port spec §8) |
| 4  | `windows-platform.test.ts`  | Windows S18 unskipped (PR6 #4) — currently `it.skipIf(!win32)`; gap deferred to Go PR15 |
| 5  | `windows-platform.test.ts`  | Windows file-lock race (PR6 #5) — runs cross-platform |
| 6  | `manifest-malformed.test.ts`| Malformed manifest variants incl. PR16a path-traversal `../../../.ssh/config` |
| 7  | `state-corruption.test.ts`  | State-file corruption beyond S17 — truncated, schema-skew, type errors, NUL bytes |
| 8  | `drift-taxonomy.test.ts`    | Drift type taxonomy: modified / added / deleted / binary / unrecoverable |
| 9  | `argv-mutex.test.ts`        | Argv mutual-exclusion exhaustive + unknown-flag handling |
| 10 | `output-modes.test.ts`      | NO_COLOR / `--no-color` / `--quiet` / `--json` combinatorics |
| 11 | `large-profile-perf.test.ts`| R38: 1000-file profile, ≤5 s on CI |

### Gap-closure deferrals (per port spec §8)

- **PR6a**: PTY-driven interactive drift gate testing — deferred post-1.0.
  Pre-merge, the gate is exercised through non-TTY spawn (every test in
  `non-interactive.test.ts` runs the bin without a TTY).
- **3 of 5 originally-listed crash-injection cases** (mid-`.pending/` write,
  mid-backup-snapshot, post-persist-but-pre-materialize) are filed as
  post-1.0 hardening tasks. The 2 most dangerous transitions are pinned in
  `crash-recovery.test.ts`.
- **Windows S18** with `.bat` companion (PR15) — pinned in
  `windows-platform.test.ts` as `it.skipIf(!win32)` with documented gap.
  Go translation closes the loop via PR15's POSIX + `.bat` install.

### Helpers

- `spawn.ts` — `runCli`, `ensureBuilt` — every test depends on these.
- `lock-holder.mjs` — Node script that holds the project lock indefinitely
  via `dist/state/lock.js#withLock`. Used by `sigint.test.ts` and
  `windows-platform.test.ts`.

## Convention

- Every spawn-based test calls `ensureBuilt()` before its first `runCli()`
  to fail fast with an actionable message when `npm run build` was skipped.
- Tests use `tests/helpers/fixture.ts#makeFixture` for in-tree profiles.
  Procedural fixtures (e.g. 1000-file synthetic profile in
  `large-profile-perf.test.ts`) build directly with `fs.mkdir` / `fs.writeFile`
  to keep the JS object size manageable.
- `tests/helpers/fixture.ts` and `tests/integration/helpers/fixture.go`
  (Go side, lives at `go/tests/integration/helpers/fixture.go`) MUST be
  audited for semantic equivalence per PR4 / R13. A divergent helper
  produces false-green tests on one side or the other.
