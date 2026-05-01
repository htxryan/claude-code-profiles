---
title: c3p sync
description: Re-materialize the active profile after source-side edits.
---

`c3p sync` re-resolves and re-materializes the *active* profile so edits
made directly to its source tree (e.g. `git pull` brought in new bytes, or
you edited `.claude-profiles/dev/.claude/...`) land in `.claude/`.

Sync goes through the same [drift gate](/docs/concepts/drift/) as
[`c3p use`](/docs/cli/use/) — uncommitted edits to `.claude/` itself trigger
the discard / persist / abort prompt (or refusal in non-TTY).

## Help

```text
c3p sync — re-materialize the active profile — at your service

USAGE
  c3p sync [options]

DESCRIPTION
  Picks up edits made directly to the active profile's source tree and
  writes them into .claude/. Same drift gate as 'use' — uncommitted edits
  to .claude/ trigger the discard/persist/abort prompt.

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json
  --on-drift=<v>   discard|persist|abort (required in non-TTY when drift exists)
  --wait[=<sec>]   poll a held lock with backoff instead of failing fast (default 30s)

EXAMPLES
  c3p sync
  c3p sync --on-drift=discard
```

## Example

```bash
# Pick up source-side changes (e.g. a teammate's edits via git pull)
c3p sync

# In CI, drop drift and re-materialize
c3p sync --on-drift=discard
```

## See also

- [Materialize concept](/docs/concepts/materialize/) — what sync does
  under the hood
- [Drift concept](/docs/concepts/drift/) — what the gate is gating
- [`c3p use`](/docs/cli/use/) — switch to a *different* profile
- [`c3p status`](/docs/cli/status/) — surfaces the stale-source signal
