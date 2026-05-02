# Contributing

Thanks for your interest. This is a small, focused tool — keep PRs small and
focused too. A brief note on protocol: one change per PR, please, and do mind
the conventional-commit prefix on the title — reviewers shall thank you.

## Repo layout

```
cmd/c3p/             # main package (cobra root + dispatch entry)
internal/
├── resolver/        # parse profile.json, walk extends/includes, build the file list
├── merge/           # merge strategies (last-wins, JSON merge, etc.)
├── state/           # on-disk state: lock, backup, gitignore, atomic copy
├── drift/           # detect/report byte differences between profile and live tree
├── cli/             # argv parser, dispatcher, command handlers, output channel
│   ├── commands/    # one file per verb (init, use, list, status, ...)
│   ├── jsonout/     # single deterministic JSON marshaller (D7 PR3)
│   └── service/     # orchestration shared by use + sync (the swap pipeline)
├── errors/          # typed error sentinels consumed by exit-code mapping
└── markers/         # source of truth for project-root CLAUDE.md managed-block markers (cw6 / spec §12)
tests/integration/   # spawn-based CLI integration suite (helpers/{fixture.go, spawn.go})
site/                # Astro docs site (separate package; deployed to CF Pages)
```

## Build & test

```bash
task vet               # go vet ./...
task build             # go build ./... (CGO_ENABLED=0)
task test              # go test ./... (unit + integration; ~30s)
task lint-jsonout      # D7 PR3 gate — forbids json.Marshal outside internal/cli/jsonout/
task ci                # full local CI mirror
```

Requires Go 1.25+ and [Task](https://taskfile.dev/). Direct `go test ./...`
also works.

## Test discipline

- **Standard Go testing**, AAA structure (Arrange / Act / Assert).
- **Integration tests live under `tests/integration/`** and use real
  `os/exec` against the built CLI. They exercise the full
  argv → dispatcher → side-effect path.
- **No DB / no network** — every test runs against a tmp project root created
  via `t.TempDir()`.
- Cross-platform behavior is non-trivial: atomic-rename semantics, lockfile
  handling, signal delivery (SIGINT/SIGTERM/SIGHUP), and Windows-reserved
  filename validation. CI runs the 5-platform matrix; please don't merge a
  Linux-only green.

## Conventional commits

We use [Conventional Commits](https://www.conventionalcommits.org/) for the
changelog and tag-derived release notes.

Common prefixes:
- `feat:` — user-visible new behavior (minor bump)
- `fix:` — user-visible bug fix (patch bump)
- `chore:` — internal/no-user-impact (no bump)
- `docs:` — documentation only
- `refactor:` — code reshape, no behavior change
- `test:` — tests only

Breaking changes: append `!` after the type (`feat!:`) **and** include a
`BREAKING CHANGE:` footer describing the migration.

Scope is optional but encouraged: `feat(cli): ...`, `fix(drift): ...`,
`refactor(state): ...`.

## Beads issue tracker

Active work is tracked in [beads](https://github.com/steveyegge/beads).

```bash
bd ready                  # find available work
bd show <id>              # view issue details
bd update <id> --claim    # claim it
bd close <id>             # close on merge
```

See `AGENTS.md` for the agent-collaboration workflow (multi-reviewer, lesson
capture, etc.).

## Cross-platform considerations

- **Atomic rename** (`os.Rename`) behaves slightly differently on Windows
  (won't replace a non-empty target dir, can fail across drive letters).
  The `state/` layer normalises this by staging into `.tmp/` and using
  two-step rename + cleanup; on Windows it prefers `MoveFileEx` semantics.
- **Signal handling** — POSIX `SIGINT`/`SIGTERM`/`SIGHUP` are wired to release
  the lock. Windows lacks `SIGHUP`; the handler installer checks for support.
- **Reserved filenames** — `CON`, `PRN`, `AUX`, `NUL`, `COM1..9`, `LPT1..9`
  (and any with extensions) are rejected as profile names regardless of host
  OS, so a profile authored on Linux remains valid on Windows.
- **Tmp paths** — every staging/backup operation uses unique per-PID,
  per-call suffixes to avoid collisions when multiple processes race.

## Adding a new verb

1. Add `internal/cli/commands/<verb>.go` exporting a `RunX(opts)` function.
2. Register the verb in `internal/cli/parse.go` and `internal/cli/dispatch.go`.
3. Add help text to the verbs map in `internal/cli/help.go`.
4. Add an entry to the top-level commands block in the help renderer.
5. Add an integration test under `tests/integration/scenarios_test.go`.
6. Document the verb in `README.md`.

The verb pattern (lock if mutating, output channel for human/JSON, error
sentinel mapped via `internal/cli/exit.go`) is consistent across existing
commands — copy from the closest neighbour.

## Code review

For external PRs, the GitHub Actions CI matrix (vet + build + tests on the
5-platform matrix) is the required gate.

During internal development we also use a multi-model review fleet
(`/compound:review`) for major changes — see `AGENTS.md`.

## Reporting bugs

File issues at <https://github.com/htxryan/claude-code-config-profiles/issues>.
Include:
- `c3p --version`
- OS + version
- Output of `c3p status --json` if relevant
- Minimum reproduction (a `.claude-profiles/<name>/profile.json` is usually
  enough)
