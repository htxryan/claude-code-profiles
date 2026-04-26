# Migration: project-root `CLAUDE.md` section ownership (cw6)

This guide covers what changed in the `cw6` feature ("section ownership in
project-root `CLAUDE.md`") and how to opt in. Most users do not need to do
anything.

## TL;DR by user type

| You currently have... | What changes | Action required |
|---|---|---|
| Profiles with only `.claude/CLAUDE.md` | Nothing. Your profiles still materialize the same way. | None. |
| A user-authored project-root `CLAUDE.md`, no profile-root `CLAUDE.md` files | Nothing. The file is left untouched. | None. |
| You want a profile to manage part of project-root `CLAUDE.md` per profile | New: drop `CLAUDE.md` next to `profile.json`, run `init`, run `use`. | See below. |

If neither of your active profiles supplies a profile-root `CLAUDE.md`, the
project-root `CLAUDE.md` file is **never** opened, written, or stat'd by
`claude-profiles use`. Back-compat is byte-exact.

## How to opt in

### 1. Add a `CLAUDE.md` peer of your `profile.json`

Place the per-profile content where the resolver looks for it:

```
.claude-profiles/
└── dev/
    ├── profile.json
    ├── CLAUDE.md          # ← NEW: profile-managed project-root content
    └── .claude/           # ← unchanged
        └── ...
```

The bytes you put in this file become the "managed section" of your
project-root `CLAUDE.md`.

### 2. Run `claude-profiles init` once

Init guarantees the project-root `CLAUDE.md` exists with the marker pair:

```markdown
<!-- claude-profiles:v1:begin -->
<!-- Managed block. Do not edit between markers — changes are overwritten on next `claude-profiles use`. -->

<!-- claude-profiles:v1:end -->
```

Init is **idempotent** and **non-destructive**:

- If `CLAUDE.md` does not exist, init creates it containing only the marker
  block.
- If `CLAUDE.md` exists without markers, init **appends** the marker block at
  the end of the file. Every byte above is preserved verbatim.
- If `CLAUDE.md` already has a well-formed marker pair, init is a no-op for
  that file.

You only need to run init once per project.

### 3. Run `claude-profiles use <profile>`

`use` splices your profile's `CLAUDE.md` content **between the markers** via
an atomic temp-file rename. Bytes above `:begin` and below `:end` are
preserved byte-for-byte.

If multiple contributors (extends ancestors, includes, the profile itself)
each supply a profile-root `CLAUDE.md`, they are concatenated in the same
order the resolver already uses for `.claude/CLAUDE.md` (oldest ancestor
first; one newline between contributors). This is identical to v1's
`.claude/CLAUDE.md` concat policy — no new merge semantics.

## Before/after directory layouts

### Before (v1 layout)

```
my-project/
├── CLAUDE.md                 # user-authored, untouched by claude-profiles
├── .claude/
│   └── CLAUDE.md             # materialized by claude-profiles
└── .claude-profiles/
    └── dev/
        ├── profile.json
        └── .claude/
            └── CLAUDE.md     # contributes to .claude/CLAUDE.md
```

### After (opt-in to section ownership)

```
my-project/
├── CLAUDE.md                 # partially managed: marker-bounded section
│                             # bytes outside the markers preserved verbatim
├── .claude/
│   └── CLAUDE.md             # unchanged: materialized as before
└── .claude-profiles/
    └── dev/
        ├── profile.json
        ├── CLAUDE.md         # NEW: contributes to project-root section
        └── .claude/
            └── CLAUDE.md     # unchanged: contributes to .claude/CLAUDE.md
```

A profile may supply both, either, or neither file. The two destinations are
independent: bytes from `.claude-profiles/dev/.claude/CLAUDE.md` never leak
into project-root `CLAUDE.md`, and vice versa.

## Validation and drift

- `claude-profiles validate` reports an actionable error (with a pointer at
  `init`) if a profile is active but the markers are missing or malformed.
- `claude-profiles drift` and the pre-commit hook only consider the bytes
  **between** the markers. Edits above or below the markers are user-owned
  and never register as drift.
- `claude-profiles use --on-drift=persist` writes the live section bytes
  back to `.claude-profiles/<active>/CLAUDE.md`, preserving your edits in
  the source profile.

## Opting out

You don't have to. Either:

1. Don't put `CLAUDE.md` at the profile root (peer of `profile.json`). Then
   no profile contributes to the projectRoot destination, the splice path is
   never entered, and the project-root `CLAUDE.md` is never touched — even
   if markers are present.
2. If you previously ran `init` and then changed your mind, delete the
   marker block from `CLAUDE.md`. As long as no active profile has a
   profile-root `CLAUDE.md`, the missing markers will not trip `validate`
   (the marker check is conditional on a contribution being present).

## Troubleshooting

**`use` aborts with "project-root CLAUDE.md is missing claude-profiles markers"**

A profile in your active resolution graph supplies a profile-root `CLAUDE.md`
but `init` has not run, or someone deleted the markers. Run
`claude-profiles init` to add them. Existing user content is preserved.

**My edits between the markers vanished after `use`**

Working as designed: the bytes between the markers are managed by the
active profile. To capture in-place edits before swapping, run
`claude-profiles use <other> --on-drift=persist`.

**My edits above/below the markers vanished after `use`**

This should not happen. The splice protocol preserves bytes outside the
markers byte-for-byte. If you can reproduce, please file an issue with the
exact `CLAUDE.md` content before and after.
