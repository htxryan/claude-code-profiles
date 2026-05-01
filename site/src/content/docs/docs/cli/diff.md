---
title: c3p diff
description: Compare two profiles' resolved file trees.
---

`c3p diff <a> [<b>]` compares the resolved (and merged) file lists of two
profiles. With one argument, it compares `<a>` against the currently active
profile. The summary line shows byte deltas:
`(+added -removed ~changed bytes)`.

This is the right tool for "what would happen if I switched from `dev` to
`ci`?" — it shows the file-level delta without touching disk.

## Help

```text
c3p diff — file-level diff — I do believe these two differ

USAGE
  c3p diff <a> [<b>] [options]

DESCRIPTION
  Compares two profiles' resolved+merged file lists. If <b> is omitted,
  compares <a> to the currently active profile. The summary line shows
  byte deltas: `(+added -removed ~changed bytes)`.

OPTIONS
  --preview          render unified-diff content for changed entries (capped at 20 lines per file)

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p diff dev ci          # compare two profiles
  c3p diff dev             # compare dev to the active profile
  c3p diff dev ci --preview # also show what changed inside each file

EXIT CODES
  0  success
  1  bad argv (missing required <a>)
  3  cycle / missing include / missing extends parent in either profile
```

## Example

```bash
# What changes between dev and ci?
c3p diff dev ci

# What changes if I swap from the active profile to ci?
c3p diff ci

# With unified diff content
c3p diff dev ci --preview
```

## See also

- [Extends concept](/docs/concepts/extends/) — single-parent layering
- [Includes concept](/docs/concepts/includes/) — additive splicing
- [`c3p drift`](/docs/cli/drift/) — diff against the live tree, not another profile
- [`c3p validate`](/docs/cli/validate/) — confirm both profiles resolve cleanly first
