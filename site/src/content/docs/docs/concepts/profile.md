---
title: Profile
description: A named, versioned configuration for Claude Code's .claude/ tree.
---

A **profile** is a named, on-disk configuration for the project's `.claude/`
tree. Profiles live under `.claude-profiles/<name>/`, and exactly one profile
is *active* at a time. The active profile is the one whose files are
materialized into `.claude/` for Claude Code to read.

## Anatomy

```
.claude-profiles/
└── dev/
    ├── profile.json          # manifest — extends, includes, description, tags
    ├── CLAUDE.md             # optional: profile-managed section of project-root CLAUDE.md
    └── .claude/              # the files that get copied into ./.claude/ on `c3p use`
        ├── settings.json
        ├── agents/
        └── commands/
```

The `profile.json` manifest declares:

- **`extends`** — single-parent inheritance ([extends](/docs/concepts/extends/))
- **`includes`** — additive components spliced in ([includes](/docs/concepts/includes/))
- **`description`** — one-line description shown by [`c3p list`](/docs/cli/list/)
- **`tags`** — free-form labels (also shown by `list`)

## Lifecycle

1. **Create** a profile with [`c3p new <name>`](/docs/cli/new/), or seed one
   from your existing `.claude/` tree with [`c3p init`](/docs/cli/init/).
2. **Switch** to it with [`c3p use <name>`](/docs/cli/use/) — this resolves
   `extends`/`includes`, merges the result, and
   [materializes](/docs/concepts/materialize/) the merged tree into `.claude/`.
3. **Inspect** the live tree with [`c3p drift`](/docs/cli/drift/) and
   [`c3p status`](/docs/cli/status/); compare profiles with
   [`c3p diff`](/docs/cli/diff/).
4. **Re-apply** source edits with [`c3p sync`](/docs/cli/sync/).

## Why profiles, not branches?

Profiles are checked into git as source bytes. The active profile and the
materialized `.claude/` tree are *artifacts* — derived state. You commit the
profile sources; the live tree is regenerated on each swap.

This means switching contexts (solo dev → team lead → CI) takes a single
command, not a `git stash`/`git checkout` dance.
