---
title: c3p init
description: Bootstrap .claude-profiles/ in this project.
---

`c3p init` bootstraps a project for C3P. It creates `.claude-profiles/`,
optionally seeds a starter profile from your existing `.claude/` tree,
updates `.gitignore`, installs the git pre-commit hook, and injects the
managed-block markers into project-root `CLAUDE.md`.

The command is idempotent in all but one direction — it refuses to clobber
an `.claude-profiles/` that's already initialised.

## Help

```text
c3p init — bootstrap .claude-profiles/ in this project; shall I prepare the way?

USAGE
  c3p init [options]

DESCRIPTION
  Creates .claude-profiles/, optionally seeds a starter profile from an
  existing .claude/ tree, updates .gitignore, and installs the pre-commit
  hook. Also injects c3p markers into project-root CLAUDE.md
  (preserves existing content) so profiles can manage a section of it.
  Refuses to overwrite an already-initialised .claude-profiles/.

OPTIONS
  --starter=<name>   starter profile name (default: "default")
  --no-seed          skip seeding from .claude/ even if present
  --no-hook          skip installing the pre-commit hook

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p init                    # bootstrap with defaults
  c3p init --no-hook          # skip pre-commit hook (CI / non-git)
  c3p init --starter=dev      # name the starter profile "dev"

EXIT CODES
  0  success
  1  already initialised; bad argv
  2  IO/permission fault (e.g. unwritable .git/hooks/)
```

## Example

```bash
# Bootstrap with defaults — the typical first-time path
c3p init

# Bootstrap without the pre-commit hook (CI workers, non-git checkouts)
c3p init --no-hook

# Bootstrap and call the starter "dev" instead of the default name
c3p init --starter=dev
```

## See also

- [Quickstart](/docs/guides/quickstart/) — full walk-through from `init` to first swap
- [`c3p new`](/docs/cli/new/) — scaffold an additional empty profile
- [`c3p hook`](/docs/cli/hook/) — install or uninstall the pre-commit hook later
- [Profile concept](/docs/concepts/profile/)
