# Contributing

Thanks for your interest. This is a small, focused tool — keep PRs small and
focused too.

## Repo layout

```
src/
├── resolver/        # parse profile.json, walk extends/includes, build the file list
├── merge/           # merge strategies (last-wins, JSON merge, etc.)
├── state/           # on-disk state: lock, backup, gitignore, atomic copy
├── drift/           # detect/report byte differences between profile and live tree
├── cli/             # argv parser, dispatcher, command handlers, output channel
│   ├── commands/    # one file per verb (init, use, list, status, ...)
│   └── service/     # orchestration shared by use + sync (the swap pipeline)
├── errors/          # typed error classes consumed by exit-code mapping
├── markers.ts       # source of truth for project-root CLAUDE.md managed-block markers (cw6 / spec §12)
└── index.ts         # public package entry
tests/               # mirrors src/; integration suites under tests/cli/integration/
```

## Build & test

```bash
pnpm install
pnpm run build         # tsc → dist/
pnpm run typecheck     # src + tests
pnpm test              # vitest run (446 tests, ~10s)
pnpm run test:watch    # iterate
```

There is no separate lint step — `pnpm run typecheck` is the lint gate.

## Test discipline

- **Vitest**, AAA structure (Arrange / Act / Assert).
- **Integration tests live under `tests/cli/integration/`** and use real
  `child_process.spawn` against the built CLI. They exercise the full
  argv → dispatcher → side-effect path.
- **No DB / no network** — every test runs against a tmp project root created
  via `mkdtempSync(os.tmpdir(), ...)`.
- Cross-platform behavior is non-trivial: atomic-rename semantics, lockfile
  handling, signal delivery (SIGINT/SIGTERM/SIGHUP), and Windows-reserved
  filename validation. CI runs the matrix; please don't merge a Linux-only
  green.

## Conventional commits

We use [Conventional Commits](https://www.conventionalcommits.org/) so
`release-please` can derive the next version automatically.

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

- **Atomic rename** (`fs.rename`) behaves slightly differently on Windows
  (won't replace a non-empty target dir). The `state/` layer normalises this
  by staging into `.tmp/` and using two-step rename + cleanup.
- **Signal handling** — POSIX `SIGINT`/`SIGTERM`/`SIGHUP` are wired to release
  the lock. Windows lacks `SIGHUP`; the handler installer checks for support.
- **Reserved filenames** — `CON`, `PRN`, `AUX`, `NUL`, `COM1..9`, `LPT1..9`
  (and any with extensions) are rejected as profile names regardless of host
  OS, so a profile authored on Linux remains valid on Windows.
- **Tmp paths** — every staging/backup operation uses unique per-PID,
  per-call suffixes to avoid collisions when multiple processes race.

## Adding a new verb

1. Add `src/cli/commands/<verb>.ts` exporting a `runX(opts)` function.
2. Register the verb in `src/cli/parse.ts` and `src/cli/dispatch.ts`.
3. Add help text to the `VERBS` map in `src/cli/help.ts`.
4. Add an entry to the top-level `COMMANDS` block in `topLevelHelp()`.
5. Add an integration test under `tests/cli/integration/scenarios.test.ts`.
6. Document the verb in `README.md`.

The verb pattern (lock if mutating, output channel for human/JSON, error class
mapped via `src/cli/exit.ts`) is consistent across existing commands — copy
from the closest neighbour.

## Code review

For external PRs, the GitHub Actions CI matrix (build + typecheck + tests on
ubuntu/macos/windows) is the required gate.

During internal development we also use a multi-model review fleet
(`/compound:review`) for major changes — see `AGENTS.md`.

## Reporting bugs

File issues at <https://github.com/htxryan/claude-code-profiles/issues>.
Include:
- Node version (`node --version`)
- OS + version
- Output of `claude-profiles status --json` if relevant
- Minimum reproduction (a `.claude-profiles/<name>/profile.json` is usually
  enough)
