---
title: CLAUDE.md section ownership
description: Let a profile manage a section of your project-root CLAUDE.md.
---

Project-root `CLAUDE.md` is read by Claude Code as part of project context.
Most teams want some of it to be hand-written and stable (project overview,
conventions) and some of it to vary by *active profile* (a "dev" profile
might want one set of agent instructions; a "ci" profile a different set).

C3P solves this with a **managed block**: a marker pair inside `CLAUDE.md`
whose contents are owned by the active profile, while everything outside
the markers is yours.

## How it looks on disk

```markdown
# My project

User-authored content lives here, untouched by C3P.

<!-- c3p:v1:begin -->
<!-- Managed block. Do not edit between markers — changes are overwritten on next `c3p use`. -->

…profile-managed content goes here…

<!-- c3p:v1:end -->

More user-authored content here, also untouched.
```

The bytes **above and below** the markers are exact-preserved on every
swap. The bytes **between** the markers come from the active profile's
contributions to project-root `CLAUDE.md`.

## How a profile contributes content

Place a `CLAUDE.md` file as a peer of `profile.json`:

```
.claude-profiles/
└── dev/
    ├── profile.json
    ├── CLAUDE.md          # ← profile-managed project-root content
    └── .claude/
        └── ...
```

On `c3p use dev`, the contents of that `CLAUDE.md` are spliced into the
managed block of project-root `CLAUDE.md`.

If `dev` extends `base` and `base` also has a profile-root `CLAUDE.md`, both
contribute — same layering rules as any other file
([extends](/docs/concepts/extends/), [includes](/docs/concepts/includes/)).

## What if no profile contributes?

If the active profile (and none of its contributors) supplies a profile-root
`CLAUDE.md`, project-root `CLAUDE.md` is **never** opened, written, or
stat'd by `c3p use`. Back-compat with non-CLAUDE.md profiles is byte-exact.

## First-time setup

`c3p init` injects the marker pair into your project-root `CLAUDE.md` (or
creates the file with just the markers, if absent). [`c3p validate`](/docs/cli/validate/)
checks that the markers are present when a profile is active.

If you remove the markers by accident, run [`c3p init`](/docs/cli/init/)
again — it's idempotent and will re-add them without touching your other
content.

## Migrating from earlier C3P versions

If you have profiles from before this feature shipped, see the
[cw6 migration guide](/docs/guides/migration/cw6-section-ownership/).

## See also

- [Profile concept](/docs/concepts/profile/)
- [Extends](/docs/concepts/extends/) / [Includes](/docs/concepts/includes/)
- [`c3p init`](/docs/cli/init/) — adds the markers
- [`c3p validate`](/docs/cli/validate/) — checks the markers
