# Proof: c3p Go Migration v1.0 (META `claude-code-profiles-7fe`)

End-to-end evidence that the TypeScript→Go atomic cutover landed correctly. The binary at `cmd/c3p/main.go` was built from `feat/go-rewrite` HEAD (`81d3b5b`) and exercised against fresh fixtures.

## What was migrated

14 epics, 50+ commits, ~8 hours of compound-agent loop work, multi-reviewer fleet (claude-sonnet, claude-opus, gemini, codex). The Go port is now at the repo root; `src/`, `tests/` (TS), `package.json`, `tsconfig*`, `vitest.config.ts`, `release-please` scaffolding all deleted in `ce1f74b feat!: cutover`.

## Build evidence

| Property | Target | Measured |
|---|---|---|
| Binary size | ≤15 MB | **4.4 MB** ([10-perf.txt](10-perf.txt)) |
| Cold start (--version) | ≤25 ms | **2.8 ms median** ([10-perf.txt](10-perf.txt)) |
| CGO | disabled | `CGO_ENABLED=0` build, only libSystem stubs ([10-perf.txt](10-perf.txt)) |
| Architecture | universal | Mach-O arm64 dev build; goreleaser builds 6 platforms ([14-release-snapshot.txt](14-release-snapshot.txt)) |

## Test evidence

| Package | Top-level tests |
|---|---|
| `internal/cli` | 28 |
| `internal/cli/commands` | 20 |
| `internal/cli/jsonout` | 4 |
| `internal/cli/service` | 3 |
| `internal/drift` | 66 |
| `internal/errors` | 9 |
| `internal/markers` | 42 |
| `internal/merge` | 66 |
| `internal/resolver` | 99 |
| `internal/state` | 100 |
| `tests/integration` | 240 |
| **Total** | **677** (840 RUN events with subtests) |

Full uncached run wall: **36s** (21.7s integration). All packages green. `go vet` clean. `task lint-jsonout` (D7 PR3 gate forbidding `json.Marshal` outside `internal/cli/jsonout/`) passes. ([11-test-suite.txt](11-test-suite.txt), [12-tests-uncached.txt](12-tests-uncached.txt), [13-test-counts.txt](13-test-counts.txt))

## CLI surface evidence

Real binary, real fixtures, real terminal transcripts.

| File | What it proves |
|---|---|
| [01-help-version.txt](01-help-version.txt) | `--help` (R3 risk closure: byte-stable global help) and `--version` |
| [02-list-status.txt](02-list-status.txt) | `list` and `status` (text + `--json`) on a repo with no active profile |
| [03-init-greenfield.txt](03-init-greenfield.txt) | `c3p init` greenfield: scaffolds `.claude-profiles/`, default profile, `.gitignore`, pre-commit hook, R44 markers in CLAUDE.md |
| [04-list-validate.txt](04-list-validate.txt) | `list --json` deterministic via single jsonout marshaller (PR3); `validate` |
| [05-use-default.txt](05-use-default.txt) | `c3p use default` swap orchestrator: resolve→merge→drift-check→re-check-under-lock→materialize→splice→persist; full state.json schema-v1 with two-tier fingerprint, R45 root section hash, source fingerprint |
| [06-status-drift.txt](06-status-drift.txt) | Drift detection (R20): clean baseline, then user edits to `.claude/settings.json` are detected as `M settings.json` with full slow-path provenance; `--json` shape pinned |
| [07-swap-with-drift.txt](07-swap-with-drift.txt) | **PR29 hard-block**: `CI=true c3p use strict` with drift exits 1 with R21 message — *no hidden prompt blocking CI*. With `--on-drift=discard`, swap succeeds, backup snapshot written under `.meta/backup/<ISO>/` |
| [08-edge-cases.txt](08-edge-cases.txt) | **PR6 #1**: typo profile name → exit 1 with "Did you perhaps mean: strict?" suggestion; `doctor` (R7) — 8 health checks all OK, `--json` shape pinned; `diff default strict`; `completions bash` |
| [09-hook-sync-new.txt](09-hook-sync-new.txt) | `hook uninstall`/`install` (PR15): atomic, fail-open pre-commit shim that calls `c3p drift --pre-commit-warn`; `sync` re-materializes; `new minimal` scaffolds |
| [14-release-snapshot.txt](14-release-snapshot.txt) | Goreleaser snapshot builds all 6 targets (linux/darwin/windows × amd64/arm64), Homebrew Cask, WinGet manifest, checksums file. `release succeeded after 1s`. |

## Notable behaviors confirmed

- **Drift gate (R21/PR29)**: non-TTY without `--on-drift` exits 1 immediately with the canonical message — proves the hard-block keeps CI safe.
- **Atomic three-step rename pair (R13/R14/R16)**: state.json's `materializedAt` advances on every successful swap; `lock` file present and free between operations (per `doctor`).
- **R45 root CLAUDE.md splice**: managed-block content swaps cleanly between profile contexts; user-content above/below preserved.
- **Backup retention (R23a)**: discarded drift produces a snapshot; doctor reports `backup count <= 5  OK  1 snapshots`.
- **Two-tier fingerprint**: state.json fingerprint includes both `size+mtimeMs` (fast path) and `contentHash` (slow path); drift `--json` reports `fastPathHits` and `slowPathHits` separately.
- **JSON byte-parity**: status/list/drift/doctor JSON shapes have stable keys including `null` for unset fields (no empty-string substitution) — matches TS reference.

## Reproducing

```bash
git checkout feat/go-rewrite
CGO_ENABLED=0 go build -o /tmp/c3p ./cmd/c3p
task vet
task lint-jsonout
task test
task release:check
task release:dry-run
```

Then run any `*.txt` file's commands against a fresh `mktemp -d` fixture.
