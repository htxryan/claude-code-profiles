---
title: Materialize
description: Atomically writing a resolved profile tree into .claude/ via copy-then-rename.
---

**Materialize** is the act of resolving a profile (`extends` + `includes`),
merging its files into a single tree, and writing that tree into `.claude/`
on disk. It's what [`c3p use`](/docs/cli/use/) and [`c3p sync`](/docs/cli/sync/)
do after the [drift gate](/docs/concepts/drift/) clears.

Materialize is **atomic**: either the swap completes fully or `.claude/`
stays exactly as it was. There is no half-applied state.

## How the atomic swap works

1. **Resolve** — walk `extends` and `includes`, building a list of every
   file the active profile contributes and where each came from.
2. **Merge** — apply the layering rules ([extends](/docs/concepts/extends/)
   and [includes](/docs/concepts/includes/)) to produce the final byte
   stream for each path.
3. **Stage** — write the merged tree into a sibling temp directory next to
   `.claude/` (same volume, so the rename in step 5 is atomic).
4. **Backup** — move the existing `.claude/` aside (kept for the most recent
   N swaps, configurable; default 3).
5. **Rename** — `rename(staging, .claude)` — a single POSIX `rename(2)` /
   Windows `MoveFileEx` swap. Either it happens or it doesn't.
6. **Update state** — record the new active profile, resolve hash, and
   warnings in the state file.

## What you can rely on

- **No torn writes** — readers never see a half-merged tree.
- **Backups** — the previous `.claude/` is retained until the configured
  count is exceeded.
- **A lockfile** — only one C3P process can be materializing at a time;
  peer attempts get exit code `3` (or wait, with `--wait`).
- **Project-root `CLAUDE.md` is byte-exact above and below the markers** —
  if a profile manages a section, only the bytes between the
  `<!-- c3p:v1:begin -->` / `<!-- c3p:v1:end -->` markers are touched.

## What materialize does not do

- It does not edit profile source files. (Source-edit propagation is
  [`c3p sync`](/docs/cli/sync/), not materialize.)
- It does not consult git. Materialize is a pure file-system operation.
- It does not fix drift on its own — that's the [drift gate's](/docs/concepts/drift/)
  job, before materialize runs.

## See also

- [`c3p use`](/docs/cli/use/) — switch active profile, materialize
- [`c3p sync`](/docs/cli/sync/) — re-materialize after a source edit
- [`c3p doctor`](/docs/cli/doctor/) — verifies the lockfile, backups, and
  state-file invariants
