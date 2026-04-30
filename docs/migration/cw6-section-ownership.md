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
`c3p use`. Back-compat is byte-exact.

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

### 2. Run `c3p init` once

Init guarantees the project-root `CLAUDE.md` exists with the marker pair:

```markdown
<!-- c3p:v1:begin -->
<!-- Managed block. Do not edit between markers — changes are overwritten on next `c3p use`. -->

<!-- c3p:v1:end -->
```

Init is **idempotent** and **non-destructive**:

- If `CLAUDE.md` does not exist, init creates it containing only the marker
  block.
- If `CLAUDE.md` exists without markers, init **appends** the marker block at
  the end of the file. Every byte above is preserved verbatim.
- If `CLAUDE.md` already has a well-formed marker pair, init is a no-op for
  that file.

You only need to run init once per project.

### 3. Run `c3p use <profile>`

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
├── CLAUDE.md                 # user-authored, untouched by c3p
├── .claude/
│   └── CLAUDE.md             # materialized by c3p
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

- `c3p validate` reports an actionable error (with a pointer at
  `init`) if a profile is active but the markers are missing or malformed.
- `c3p drift` and the pre-commit hook only consider the bytes
  **between** the markers. Edits above or below the markers are user-owned
  and never register as drift.
- `c3p use --on-drift=persist` writes the live section bytes
  back to `.claude-profiles/<active>/CLAUDE.md`, preserving your edits in
  the source profile.

### Persist + extends: snapshot semantics

When you run `c3p use <child> --on-drift=persist` and `<child>`
extends `<parent>` (the active profile), the persisted edits land in
`<parent>` but the `<child>` you just materialized is built from the
PRE-persist source of `<parent>`. In other words: the swap target was
resolved when you typed the command, and that snapshot is what gets
materialized — `--on-drift=persist` is purely a transactional write-back
to the previous profile so your edits aren't lost.

To pick up the just-persisted edits in `<child>`, run `c3p use
<child>` again after the persist completes. That second resolve sees the
freshly-persisted bytes in `<parent>` and merges them through the extends
chain.

Why this design: re-resolving after persist would make the materialized
output depend on whether drift was persisted vs discarded vs absent — the
same `use <child>` command would produce different bytes on disk based on
state you can't see. Snapshot semantics keep "what I named is what I get"
intact and is faster (one resolve instead of two on the swap path).

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

## Back-compat invariants (AC-10)

The following invariants are pinned by the regression suite
(`tests/cli/integration/back-compat-section-ownership.test.ts`) and form the
contract between the v1 layout and section ownership. They are documented
here so users can rely on them without reading the test source.

- **BC-1** — A profile that contains *only* `.claude/CLAUDE.md` (no
  profile-root `CLAUDE.md`) leaves project-root `CLAUDE.md` byte-identical
  through `c3p use`. The file is not opened, written, or even
  stat'd. This holds whether the user has run `init` or not.
- **BC-2** — A profile that contains *only* a profile-root `CLAUDE.md` (no
  `.claude/CLAUDE.md`) does not write a `CLAUDE.md` into `.claude/`. The two
  destinations are independent: contributing to one never implicitly
  contributes to the other.
- **BC-3** — A profile that supplies *both* `.claude/CLAUDE.md` and a profile-
  root `CLAUDE.md` writes both files independently. Bytes from the profile-
  root contribution do not appear in `.claude/CLAUDE.md`, and vice versa. No
  cross-destination content leak.
- **BC-4** — On a legacy project (no profile-root `CLAUDE.md` anywhere),
  running `c3p init` injects markers into the existing project-
  root `CLAUDE.md` (preserving every prior byte). A subsequent `use` of a
  profile that has no profile-root contribution leaves the file at exactly
  what `init` produced — the section between the markers stays empty
  (equivalent to `init`'s default block).

In short: the projectRoot destination is *opt-in*. If no contributor in the
resolution graph supplies a profile-root `CLAUDE.md`, the splice path is
never entered and the user's existing project-root file is untouched.

## Troubleshooting

**`use` aborts with "project-root CLAUDE.md is missing c3p markers"**

A profile in your active resolution graph supplies a profile-root `CLAUDE.md`
but `init` has not run, or someone deleted the markers. Run
`c3p init` to add them. Existing user content is preserved.

**My edits between the markers vanished after `use`**

Working as designed: the bytes between the markers are managed by the
active profile. To capture in-place edits before swapping, run
`c3p use <other> --on-drift=persist`.

**My edits above/below the markers vanished after `use`**

This should not happen. The splice protocol preserves bytes outside the
markers byte-for-byte. If you can reproduce, please file an issue with the
exact `CLAUDE.md` content before and after.
