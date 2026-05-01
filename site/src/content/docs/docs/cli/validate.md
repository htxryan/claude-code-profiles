---
title: c3p validate
description: Dry-run resolve + merge over one profile or all.
---

`c3p validate [<name>]` walks the resolver and merger without writing
anything. With no name it validates every profile in the project, reporting
pass/fail per profile. When a profile is active, it also confirms that
project-root `CLAUDE.md` carries the C3P managed-block markers.

Use `validate` in CI to catch broken `extends`/`includes` references before
they trip up an interactive `use`.

## Help

```text
c3p validate — dry-run resolve+merge — I do try to be thorough

USAGE
  c3p validate [<name>] [options]

DESCRIPTION
  Walks the resolver and merger without writing anything. With no name,
  validates every profile in the project and reports pass/fail per profile.
  When a profile is active, also checks that project-root CLAUDE.md has the
  c3p markers (run `init` to add them).

OPTIONS
  --brief            collapse FAIL rows to one line each (CI-friendly)

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p validate              # validate all profiles (full error per FAIL)
  c3p validate dev          # validate just dev
  c3p validate --brief      # one-line FAIL rows (CI scripts)
  c3p validate --json

EXIT CODES
  0  all profiles validated cleanly
  1  project-root CLAUDE.md is missing/malformed c3p markers (run `c3p init`); bad argv
  2  IO fault (read failure on profiles dir or CLAUDE.md)
  3  any profile failed (cycle, missing include, missing extends parent, conflict, unparseable profile.json)
```

## Example

```bash
# Validate every profile (CI-friendly)
c3p validate --brief

# Validate one profile, get JSON for downstream tooling
c3p validate dev --json
```

## See also

- [CI usage guide](/docs/guides/ci-usage/) — wiring `validate` into a pipeline
- [`c3p doctor`](/docs/cli/doctor/) — superset (validate + environment checks)
- [Extends concept](/docs/concepts/extends/) / [Includes concept](/docs/concepts/includes/)
