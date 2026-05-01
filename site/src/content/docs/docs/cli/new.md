---
title: c3p new
description: Scaffold an empty profile under .claude-profiles/.
---

`c3p new <name>` scaffolds an empty profile directory with a minimal
`profile.json`. It refuses if `.claude-profiles/<name>/` already exists, so
it is safe to run repeatedly without losing work.

After scaffolding, edit the generated `profile.json` to declare
[`extends`](/docs/concepts/extends/)/[`includes`](/docs/concepts/includes/),
then drop files into `.claude-profiles/<name>/.claude/`.

## Help

```text
c3p new — scaffold an empty profile — splendid!

USAGE
  c3p new <name> [--description=<text>] [options]

DESCRIPTION
  Creates .claude-profiles/<name>/ with a minimal profile.json. Refuses if
  the directory already exists. Edit the generated profile.json to set
  extends/includes, then add files under .claude-profiles/<name>/.claude/.

OPTIONS
  --description=<text>   one-line description recorded in profile.json

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json

EXAMPLES
  c3p new dev
  c3p new dev --description="local dev with verbose agents"
  c3p new ci --json

EXIT CODES
  0  success
  1  bad argv, invalid name, or profile already exists
  2  IO/permission fault
```

## Example

```bash
# Scaffold a new profile called "dev"
c3p new dev --description="local dev with verbose agents"

# Then add files under it; here, lift settings.json from the active tree as a starting point
mkdir -p .claude-profiles/dev/.claude
cp .claude/settings.json .claude-profiles/dev/.claude/settings.json

# Activate and materialize
c3p use dev
```

## See also

- [`c3p init`](/docs/cli/init/) — first-time bootstrap (also creates a
  starter profile)
- [`c3p use`](/docs/cli/use/) — activate the new profile
- [Profile concept](/docs/concepts/profile/)
