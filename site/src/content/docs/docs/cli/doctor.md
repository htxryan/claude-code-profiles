---
title: c3p doctor
description: Read-only health check across state, lock, gitignore, hook, markers.
---

`c3p doctor` runs the same checks as [`c3p validate`](/docs/cli/validate/)
plus environment diagnostics: state-file schema, lock liveness, `.gitignore`
correctness, pre-commit hook byte-equality, backup retention count,
external-path reachability, and the project-root `CLAUDE.md` managed-block
markers.

`doctor` is **read-only** — it never writes. Returns `0` when every check
passes and `1` on any actionable warning, so CI scripts can either gate on
it (`c3p doctor || exit 1`) or treat it as soft (`c3p doctor || exit 0`).

## Help

```text
c3p doctor — if anything is amiss, I shall fret about it for you

USAGE
  c3p doctor [options]

DESCRIPTION
  Runs the same checks as `validate` plus environment diagnostics:
  state-file schema (R42), lock liveness (R41), gitignore correctness
  (R15), pre-commit hook byte-equality (R25a), backup retention count
  (R23a), external-path reachability (R37a), and managed-block markers
  in project-root CLAUDE.md (R44/R45). Read-only — never writes.
  Returns 0 when every check passes and 1 on any actionable warning so
  CI scripts can `c3p doctor || exit 0` for soft checks.

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p doctor                # human-readable status table
  c3p doctor --json         # machine-readable summary for CI
  c3p doctor || echo "check failed"  # gate a script on health

EXIT CODES
  0  all checks passed
  1  one or more checks reported a warning or failure
  2  IO/permission fault while running checks
```

## Example

```bash
# Show the status table
c3p doctor

# Use in CI (treats any warning as fatal)
c3p doctor --json | jq '.checks[] | select(.status != "ok")'
```

## See also

- [`c3p validate`](/docs/cli/validate/) — subset (resolver-only checks)
- [`c3p status`](/docs/cli/status/) — runtime state, not health
- [CI usage guide](/docs/guides/ci-usage/)
