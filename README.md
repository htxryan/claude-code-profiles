# claude-code-config-profiles (C3P — humbly at your service)

Fluent in many forms of `.claude/` configuration.

> **npm package**: `claude-code-config-profiles` &nbsp;·&nbsp; **CLI binary**: `c3p`

C3P lets you maintain multiple named `.claude/` configurations in a project —
for example a `dev` profile with verbose agents and looser permissions, and a
`ci` profile with terse output and locked-down permissions — and switch between
them atomically. Profiles compose via single-parent `extends` and additive
`includes` (composable components), and a **drift gate** ensures, if I may, that
uncommitted edits to the active `.claude/` are never lost when switching.

> **Status**: alpha. The CLI surface is stable. Pre-1.0 means the on-disk layout
> may still change in minor versions.

## Install

```bash
npm install -g claude-code-config-profiles
```

Requires Node 20+. The installed binary is `c3p`.

### Upgrading from 0.2.x

0.3.0 renames the CLI binary from `claude-profiles` to `c3p`, along with the
gitignore section header, CLAUDE.md managed-block markers, and pre-commit hook
script. The rename is hard — legacy markers and headers in your repo are **not**
auto-migrated. After upgrading the package, manually remove the old
`# Added by claude-profiles` gitignore section and
`<!-- claude-profiles:v1:… -->` markers from project-root `CLAUDE.md`, then
re-run `c3p init` and `c3p hook install --force`. See the
[CHANGELOG entry for 0.3.0](./CHANGELOG.md) for full details.

## Quickstart

```bash
# 1. Bootstrap a project (run inside a git repo)
c3p init

# 2. Scaffold a profile
c3p new dev --description="local dev with verbose agents"

# 3. Edit the profile
$EDITOR .claude-profiles/dev/.claude/settings.json

# 4. Activate it (materializes the profile into .claude/)
c3p use dev

# 5. See what's active and whether .claude/ has drifted
c3p status
```

> **`CLAUDE.md` section ownership is opt-in.** By default `c3p` only
> touches `.claude/`. To *also* let a profile manage a section of your
> **project-root** `CLAUDE.md`, you need both (a) a `CLAUDE.md` next to the
> profile's `profile.json`, and (b) `c3p init` (which injects
> markers into the root `CLAUDE.md`). Skip both, and project-root `CLAUDE.md` is
> never opened or written. See [profile-managed `CLAUDE.md` sections](#profile-managed-claudemd-sections)
> below and [docs/migration/cw6-section-ownership.md](docs/migration/cw6-section-ownership.md).

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
| `doctor`                    | Read-only health check (state, lock, gitignore, hook, markers, externals)    |
| `completions <shell>`       | Emit a `bash`/`zsh`/`fish` completion script (eval to install)               |

Run `c3p <verb> --help` for full per-verb help.

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
└── .gitignore                     # c3p appends its entries here
```

`.claude/` is in `.gitignore` — it's an output, derived from the active profile.
Profiles themselves (`.claude-profiles/dev/`, etc.) **are** checked in. The
`.meta/` subtree (state, lock, backups) is ignored.

## Profile-managed `CLAUDE.md` sections

By default, `c3p` only touches `.claude/`. If you also want a profile
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

Then run `c3p init` once to add a marker pair to your project-root
`CLAUDE.md` (preserving any existing user content above):

```markdown
# Your existing CLAUDE.md content stays here, untouched.

<!-- c3p:v1:begin -->
<!-- Managed block. Do not edit between markers — changes are overwritten on next `c3p use`. -->

...active profile's profile-root CLAUDE.md content lands here...

<!-- c3p:v1:end -->

# Anything below the end marker is also preserved verbatim.
```

On every `c3p use <profile>`:

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

## Exit codes

Every `c3p` invocation returns one of four codes. CI scripts can
gate on these without parsing stdout/stderr:

| Code | Meaning                  | Examples                                                         |
|------|--------------------------|------------------------------------------------------------------|
| `0`  | Success                  | `use` swap completed; `validate` passed; `drift` ran read-only   |
| `1`  | User error               | bad argv; `use <typo>`; drift `--on-drift=abort`; `validate` failed; `init` already-initialised; missing `--on-drift` in non-TTY |
| `2`  | System error             | IO/permission fault; ENOSPC; unwritable `.git/hooks/`            |
| `3`  | Structural conflict      | cycle in `extends`; missing `extends` parent or include; lock held by another process |

```bash
# Skip the script if the project is unhealthy:
c3p doctor || exit 0

# Branch on conflict vs. user error:
c3p use ci --on-drift=abort
case $? in
  0) echo "swapped to ci" ;;
  1) echo "drift abort or typo — fix and retry" ;;
  3) echo "structural problem — run \`c3p validate\`" ;;
esac
```

## Power-user affordances

`c3p` is built to be embedded in scripts and pipelines. A few
flags you'll reach for once profiles are part of your workflow:

### `--quiet` / `-q`

Silences human output but preserves errors and exit codes. Useful for shell
chains where the side-effect is what you want, not the chatter:

```bash
c3p use ci -q && ./run.sh
```

`--quiet` is **mutually exclusive** with `--json` (a script that asks for
both is ambiguous; the parser rejects the combination).

### Stale-source detection (`status`)

When a teammate edits `.claude-profiles/dev/.claude/` and you `git pull`,
the bytes in `.claude/` are now stale relative to the source. `status`
surfaces this:

```text
$ c3p status
active: dev
materialized: 2026-04-25T09:12:34.567Z (3h ago)
✓ drift: clean
! source: updated since last materialize — run `c3p sync`
```

Under `--json`, the same signal is `sourceFresh: false` plus the new
`sourceFingerprint` field for round-tripping the value across runs.

### Content previews (`diff --preview`, `drift --preview`)

By default, `diff` and `drift` show one line per affected path:

```text
$ c3p diff dev ci
a=dev b=ci: 2 changes (1 added, 0 removed, 1 changed) (+45 -0 ~12 bytes)
  + dev-only.md
  ~ shared.md
```

Pass `--preview` to inline a unified diff (capped at 20 lines per file,
with a `(truncated, N more lines)` footer when over):

```text
$ c3p diff dev ci --preview
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

### Colour and progress hints

Under a TTY, the read-only commands (`list`, `status`, `drift`, `diff`,
`validate`) colour-code their output so a busy tree can be skimmed at a
glance:

- `list` — the active profile's name is bold; `*` marks the row.
- `status` / `drift` — `drift: clean` lights up green; status words
  (`modified`, `added`, `deleted`, `unrecoverable`) are colour-coded.
- `drift` / `diff` — the `+N -N ~N bytes` summary intensifies by
  magnitude (subtle under 100 B, bright over 10 KB) so an outsized
  delta visually outranks the others.

The mutating verbs (`use`, `sync`, `validate`) emit transient
phase-progress hints to stderr (`resolving profile…`, `merging files…`,
`materializing…`, `validating <name>…`) so a 1000-file profile doesn't
sit on a stuck cursor.

Colour and progress hints are suppressed by:

- `--no-color` (CLI flag)
- `NO_COLOR=1` (env var, per <https://no-color.org>)
- `--quiet` / `-q` (silences phase progress in addition to other human output)
- `--json` (always emits a single structured payload, no human chatter)
- Non-TTY stdout (CI logs, redirected output, pipes)

`--json` output is byte-identical regardless of colour settings —
machine consumers see the same payload everywhere.

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
`c3p use <other> --on-drift=persist` (or pick "persist" at the
interactive prompt) to roll the edits into the active profile first.

## About the name

C3P is short for *claude code config profiles*; any further resemblance is purely coincidental, naturally.

## License

[MIT](./LICENSE)

## Issues

Report bugs and request features at
<https://github.com/htxryan/claude-code-config-profiles/issues>.
