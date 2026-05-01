---
title: Drift
description: Byte-level differences between the live .claude/ tree and the active profile's resolved bytes.
---

**Drift** is the byte-level difference between what's currently in `.claude/`
and what the active profile's source files would resolve to. Drift accrues
when you (or a tool) edit files inside `.claude/` directly instead of editing
the profile source — the live tree pulls away from the canonical bytes.

C3P treats drift as a first-class signal. Two mechanisms surround it:

1. The **drift report** — a read-only inspection produced by
   [`c3p drift`](/docs/cli/drift/) and summarised in
   [`c3p status`](/docs/cli/status/).
2. The **drift gate** — a prompt or hard refusal that blocks
   [`c3p use`](/docs/cli/use/) and [`c3p sync`](/docs/cli/sync/) when drift
   exists, so an unconscious swap can't silently throw away your edits.

## How drift is detected

For each file in `.claude/`:

1. Compute the resolved-and-merged bytes the active profile would produce.
2. Compare to the bytes on disk.
3. If they differ, mark the file as drifted; record the contributing
   profile in the drift report.

The check is read-only and never writes to disk.

## The drift gate

When a swap is requested:

| Drift state | Interactive (TTY) | Non-interactive (CI) |
|---|---|---|
| No drift | Swap proceeds. | Swap proceeds. |
| Drift present | Prompt: discard / persist / abort. | Refuses with exit `1` unless `--on-drift=<choice>` is passed. |

The three choices:

- **`discard`** — drop the drifted edits; resolve the active profile fresh
  and overwrite.
- **`persist`** — write the drifted bytes back into the active profile's
  source tree first, then proceed with the swap so nothing is lost.
- **`abort`** — make no change.

The non-TTY refusal is intentional: a CI script that doesn't pass
`--on-drift` would otherwise hang on a hidden prompt.

## What drift is not

Drift is *not* the same as a stale source. **Stale source** means the
active profile's source bytes have changed (e.g. via `git pull`) and
`.claude/` hasn't picked them up yet — fixed by [`c3p sync`](/docs/cli/sync/).
**Drift** means *the live tree was edited directly*. Status surfaces both as
distinct signals.

## See also

- [`c3p drift`](/docs/cli/drift/) — per-file report
- [`c3p status`](/docs/cli/status/) — short summary
- [`c3p use`](/docs/cli/use/) — drift-gated swap
- [`c3p sync`](/docs/cli/sync/) — drift-gated re-materialize
