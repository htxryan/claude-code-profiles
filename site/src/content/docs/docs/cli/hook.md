---
title: c3p hook
description: Install or uninstall the git pre-commit drift hook.
---

`c3p hook install|uninstall` manages the project's git pre-commit hook. The
hook script is **fixed and minimal**, and is **fail-open** — a missing or
broken `c3p` binary never blocks a commit. Its only job is to surface a
warning if you're about to commit drifted `.claude/` files.

## Help

```text
c3p hook — manage the git pre-commit hook

USAGE
  c3p hook install|uninstall [options]

DESCRIPTION
  The hook script content is fixed and minimal (R25a) and fail-open: a
  missing or broken c3p binary never blocks commits.
  
  install:    writes .git/hooks/pre-commit (preserves existing hook unless --force)
  uninstall:  removes the hook only if its content matches the canonical
              script (a user-edited or third-party hook is left untouched)

OPTIONS
  --force            (install only) overwrite an existing pre-commit hook

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p hook install
  c3p hook install --force
  c3p hook uninstall
  c3p hook install --json

EXIT CODES
  0  success (or no-op if hook is already in the desired state)
  1  bad argv (missing install|uninstall, install with conflicting hook + no --force)
  2  IO/permission fault, missing .git/ directory
```

## Example

```bash
# Install (or no-op if already installed)
c3p hook install

# Replace an existing hook with the canonical c3p one
c3p hook install --force

# Remove the hook (only if it's the canonical script — user-edited hooks left alone)
c3p hook uninstall
```

## See also

- [`c3p init`](/docs/cli/init/) — installs the hook by default; `--no-hook` opts out
- [`c3p drift --pre-commit-warn`](/docs/cli/drift/) — the fail-open hook entry point
- [Drift concept](/docs/concepts/drift/)
