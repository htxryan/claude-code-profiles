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

## Profile-managed `CLAUDE.md` sections

By default, `claude-profiles` only touches `.claude/`. If you also want a profile
to manage part of your **project-root** `CLAUDE.md` — Claude Code reads both
locations — put a `CLAUDE.md` next to the profile's `profile.json`:

```
.claude-profiles/
└── dev/
    ├── profile.json
    ├── CLAUDE.md          # ← profile-managed root section (NEW, opt-in)
    └── .claude/
        └── ...
```

Then run `claude-profiles init` once to add a marker pair to your project-root
`CLAUDE.md` (preserving any existing user content above):

```markdown
# Your existing CLAUDE.md content stays here, untouched.

<!-- claude-profiles:v1:begin -->
<!-- Managed block. Do not edit between markers — changes are overwritten on next `claude-profiles use`. -->

...active profile's profile-root CLAUDE.md content lands here...

<!-- claude-profiles:v1:end -->

# Anything below the end marker is also preserved verbatim.
```

On every `claude-profiles use <profile>`:

- Bytes **between** the markers are replaced with the resolved content from
  the active profile's `CLAUDE.md` (and any extends/includes contributors,
  concatenated in the same order as `.claude/CLAUDE.md`).
- Bytes **above and below** the markers are preserved byte-for-byte.
- Drift detection and `--on-drift=persist` only see the section bytes; edits
  to the user-owned regions never register as drift.

**Opt out**: don't put `CLAUDE.md` at the profile root. With no contributor
for the projectRoot destination, the project-root `CLAUDE.md` is never opened
or written — back-compat is byte-exact.

See [docs/migration/cw6-section-ownership.md](docs/migration/cw6-section-ownership.md)
for the full migration story.

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

## Power-user affordances

`claude-profiles` is built to be embedded in scripts and pipelines. A few
flags you'll reach for once profiles are part of your workflow:

### `--quiet` / `-q`

Silences human output but preserves errors and exit codes. Useful for shell
chains where the side-effect is what you want, not the chatter:

```bash
claude-profiles use ci -q && ./run.sh
```

`--quiet` is **mutually exclusive** with `--json` (a script that asks for
both is ambiguous; the parser rejects the combination).

### Stale-source detection (`status`)

When a teammate edits `.claude-profiles/dev/.claude/` and you `git pull`,
the bytes in `.claude/` are now stale relative to the source. `status`
surfaces this:

```text
$ claude-profiles status
active: dev
materialized: 2026-04-25T09:12:34.567Z (3h ago)
✓ drift: clean
! source: updated since last materialize — run `claude-profiles sync`
```

Under `--json`, the same signal is `sourceFresh: false` plus the new
`sourceFingerprint` field for round-tripping the value across runs.

### Content previews (`diff --preview`, `drift --preview`)

By default, `diff` and `drift` show one line per affected path:

```text
$ claude-profiles diff dev ci
a=dev b=ci: 2 changes (1 added, 0 removed, 1 changed) (+45 -0 ~12 bytes)
  + dev-only.md
  ~ shared.md
```

Pass `--preview` to inline a unified diff (capped at 20 lines per file,
with a `(truncated, N more lines)` footer when over):

```text
$ claude-profiles diff dev ci --preview
a=dev b=ci: 2 changes (1 added, 0 removed, 1 changed) (+45 -0 ~12 bytes)
  + dev-only.md
  ~ shared.md
       alpha
      -BETA
      +beta
       gamma
```

`drift --preview` works the same way for files that have been edited in
the live `.claude/` tree, plus a head preview for newly-added files.
Binary files (NUL byte in the first 8KB) are summarised as
`(binary file — N bytes)` rather than rendered.

### Byte-count summaries

Both `diff` and `drift` summary lines report byte deltas:

- `+N`  — bytes contributed by added files (size of files only on the `a`
  side for `diff`, or newly-created files in `.claude/` for `drift`)
- `-N`  — bytes contributed by removed files (size of files only on the
  `b` side for `diff`, or recorded sizes of deleted files for `drift`)
- `~N`  — magnitude of the changed-file size deltas (sum of
  `|bytesA − bytesB|` for `changed` / `modified` entries)

Tells you the magnitude of a change before you drill in.

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
