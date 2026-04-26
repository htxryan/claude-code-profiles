# claude-code-profiles

Swappable `.claude/` configurations for Claude Code projects.

`claude-code-profiles` (CLI: `claude-profiles`) lets you maintain multiple named
`.claude/` configurations in a project — for example a `dev` profile with verbose
agents and looser permissions, and a `ci` profile with terse output and locked-down
permissions — and switch between them atomically. Profiles compose via
single-parent `extends` and additive `includes` (composable components), and a
**drift gate** ensures uncommitted edits to the active `.claude/` are never lost
when switching.

> **Status**: alpha. The CLI surface is stable. Pre-1.0 means the on-disk layout
> may still change in minor versions.

## Install

```bash
npm install -g claude-code-profiles
```

Requires Node 20+.

## Quickstart

```bash
# 1. Bootstrap a project (run inside a git repo)
claude-profiles init

# 2. Scaffold a profile
claude-profiles new dev --description="local dev with verbose agents"

# 3. Edit the profile
$EDITOR .claude-profiles/dev/.claude/settings.json

# 4. Activate it (materializes the profile into .claude/)
claude-profiles use dev

# 5. See what's active and whether .claude/ has drifted
claude-profiles status
```

## Concepts

- **Profile** — a named directory under `.claude-profiles/<name>/` containing a
  `profile.json` manifest and a `.claude/` source tree.
- **extends** — single-parent inheritance. Child profile manifests can declare
  `"extends": "<parent-name>"`; the resolver layers child files over parent
  files.
- **includes** — additive components. A profile can `"includes": ["<comp1>", ...]`
  to splice in shared `.claude/` fragments. Includes don't form an inheritance
  chain — they're concatenated.
- **drift** — the diff between your live, materialized `.claude/` and the active
  profile's resolved+merged tree. Detected via byte comparison.
- **materialize** — copy the resolved+merged profile into the project's `.claude/`,
  using an atomic swap (rename) so no half-states are ever observable.

## Command reference

| Command                     | What it does                                                                |
|-----------------------------|------------------------------------------------------------------------------|
| `init`                      | Bootstrap `.claude-profiles/`, update `.gitignore`, install pre-commit hook |
| `new <name>`                | Scaffold an empty profile                                                    |
| `list`                      | Show all profiles with active marker, extends, includes                      |
| `use <name>`                | Switch to `<name>`; runs the drift gate                                      |
| `status`                    | Show active profile + drift summary                                          |
| `drift`                     | Per-file drift report (read-only)                                            |
| `diff <a> [<b>]`            | File-level diff of two profiles' resolved trees                              |
| `validate [<name>]`         | Dry-run resolve+merge over one or all profiles                               |
| `sync`                      | Re-materialize the active profile (drift-gated)                              |
| `hook install\|uninstall`   | Manage the git pre-commit hook                                               |

Run `claude-profiles <verb> --help` for full per-verb help.

## Layout on disk

```
project-root/
├── .claude/                       # live, materialized — Claude Code reads this
│   ├── agents/
│   ├── settings.json
│   └── ...
├── .claude-profiles/              # source of truth
│   ├── .meta/                     # internal: state.json, locks, backups, tmp/
│   │   ├── state.json
│   │   ├── backup/                # rolling backups of replaced .claude/ trees
│   │   └── ...
│   ├── dev/
│   │   ├── profile.json
│   │   └── .claude/
│   ├── ci/
│   │   ├── profile.json
│   │   └── .claude/
│   └── _components/
│       └── strict-perms/
│           ├── profile.json
│           └── .claude/
└── .gitignore                     # claude-profiles appends its entries here
```

`.claude/` is in `.gitignore` — it's an output, derived from the active profile.
Profiles themselves (`.claude-profiles/dev/`, etc.) **are** checked in. The
`.meta/` subtree (state, lock, backups) is ignored.

## Drift gate

If you've edited `.claude/` since the last materialization (drift), `use` and
`sync` won't silently overwrite your work. They prompt:

| Choice    | Effect                                                                |
|-----------|------------------------------------------------------------------------|
| `discard` | Drop drifted edits; materialize the requested profile cleanly          |
| `persist` | Write drifted edits back into the active profile, then materialize     |
| `abort`   | Cancel the swap; live `.claude/` is untouched                          |

In non-TTY contexts (CI, scripts) you must pass `--on-drift=discard|persist|abort`
explicitly — the gate refuses to default, so a CI job never silently destroys
work.

## FAQ

**Is this safe to run in CI?**
Yes — pass `--on-drift=discard` and `--json` for machine-readable output. The
drift gate refuses to default in non-TTY mode, and all swaps go through an
atomic rename.

**How do I share a profile fragment across multiple profiles?**
Put it in `.claude-profiles/_components/<name>/` and reference it from each
profile's `includes` array. Components compose additively (no inheritance).

**Why does my live edit not appear after I swap profiles?**
Swap discards drift unless you pre-answer `persist`. Use
`claude-profiles use <other> --on-drift=persist` (or pick "persist" at the
interactive prompt) to roll the edits into the active profile first.

## License

[MIT](./LICENSE)

## Issues

Report bugs and request features at
<https://github.com/htxryan/claude-code-profiles/issues>.
