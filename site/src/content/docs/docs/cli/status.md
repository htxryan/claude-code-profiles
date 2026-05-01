---
title: c3p status
description: Show active profile, drift count, and stale-source warning.
---

`c3p status` reports the current active profile and three signals about how
the live tree compares to its source:

1. **Active profile** — name and (when present) description.
2. **Drift count** — files in `.claude/` that have been edited directly.
3. **Stale source** — set when the active profile's source bytes have
   changed (e.g. after a `git pull`) but `.claude/` hasn't picked them up.

Both signals are independent — you can have drift, stale source, both, or
neither. Stale source is the cue to run [`c3p sync`](/docs/cli/sync/).

## Help

```text
c3p status — print active profile, description, drift summary, warnings

USAGE
  c3p status [options]

DESCRIPTION
  Reports the active profile name (with its description, when present),
  the count of drifted files in the live .claude/ tree, any resolver
  warnings carried over from the last swap, AND a stale-source signal
  when the active profile's source files have changed since the last
  materialize (a teammate's `git pull` brings in new bytes that .claude/
  hasn't picked up yet — run `c3p sync` to apply them).

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p status
  c3p status --json

EXIT CODES
  0  success
  2  IO fault
```

## Example

```bash
# Quick check
c3p status

# Use --json in shell scripts to inspect specific fields
c3p status --json | jq '.active, .driftCount, .staleSource'
```

## See also

- [Drift concept](/docs/concepts/drift/)
- [`c3p drift`](/docs/cli/drift/) — per-file detail
- [`c3p sync`](/docs/cli/sync/) — apply new source bytes to `.claude/`
