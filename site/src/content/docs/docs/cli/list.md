---
title: c3p list
description: Show every profile, the active one marked, plus description, tags, and metadata.
---

`c3p list` prints one row per profile in `.claude-profiles/`. The active
profile is bolded and prefixed with `*`. Description and tag columns are
elided when no profile uses them, so the output stays compact for projects
that only need names.

## Help

```text
c3p list — allow me to introduce all profiles, with active marker, description, tags

USAGE
  c3p list [options]

DESCRIPTION
  Prints one row per profile: name (active is marked `*` and bold),
  description (column shown only when at least one profile has one),
  tags (column shown only when at least one profile has any), and a
  trailing meta column with extends/includes/last-materialized.

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p list
  c3p list --json | jq '.profiles[].name'

EXIT CODES
  0  success
  2  IO fault reading .claude-profiles/
```

## Example

```bash
# Human-readable
c3p list

# Pluck just the names
c3p list --json | jq -r '.profiles[].name'

# Find which profile is active
c3p list --json | jq -r '.profiles[] | select(.active) | .name'
```

## See also

- [Profile concept](/docs/concepts/profile/)
- [`c3p status`](/docs/cli/status/) — focused on the active profile
- [`c3p diff`](/docs/cli/diff/) — compare two profiles' resolved trees
