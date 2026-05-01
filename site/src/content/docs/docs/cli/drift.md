---
title: c3p drift
description: Per-file drift report — read-only.
---

`c3p drift` lists every file in `.claude/` that differs from what the active
profile would resolve to, naming the contributing profile so you can tell
where the canonical bytes came from. The summary line shows byte deltas:
`(+added -removed ~changed bytes)`.

The command is **read-only** — it never modifies anything on disk. Pair with
[`c3p use`](/docs/cli/use/) or [`c3p sync`](/docs/cli/sync/) to *resolve*
drift.

## Help

```text
c3p drift — per-file drift report with provenance

USAGE
  c3p drift [options]

DESCRIPTION
  Lists each file in .claude/ that differs from the active profile's
  resolved+merged tree, naming the contributor each file came from.
  Read-only — does not change anything on disk. The summary line shows
  byte deltas: `(+added -removed ~changed bytes)`.

OPTIONS
  --pre-commit-warn  fail-open hook entry point (always exits 0)
  --verbose          include scan stats (scanned N, fast=X, slow=Y) in the summary
  --preview          render unified-diff content for modified entries (capped at 20 lines per file)

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p drift
  c3p drift --json
  c3p drift --preview            # show what changed inside each drifted file
  c3p drift --pre-commit-warn   # used by the git hook; never blocks

EXIT CODES
  0  success (drift present or absent)
  2  IO fault
```

## Example

```bash
# What's drifted?
c3p drift

# What changed inside each drifted file?
c3p drift --preview

# In a script: branch on whether anything is drifted
if [ "$(c3p drift --json | jq '.driftedCount')" -gt 0 ]; then
  echo "Drift present — run 'c3p drift' for details"
fi
```

## See also

- [Drift concept](/docs/concepts/drift/) — definition + the drift gate
- [`c3p status`](/docs/cli/status/) — short summary
- [`c3p use`](/docs/cli/use/) / [`c3p sync`](/docs/cli/sync/) — gated swaps
- [`c3p hook`](/docs/cli/hook/) — install the pre-commit drift warning
