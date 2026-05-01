---
title: c3p use
description: Switch to a profile; runs the drift gate before materializing.
---

`c3p use <name>` makes `<name>` the active profile and
[materializes](/docs/concepts/materialize/) its resolved tree into `.claude/`.
If the live tree has [drift](/docs/concepts/drift/), the swap is gated:

- **Interactive (TTY):** prompts you to discard, persist, or abort.
- **Non-interactive (CI):** refuses with exit `1` unless `--on-drift=<choice>`
  is provided. This prevents CI scripts from hanging on a hidden prompt.

The swap is atomic — either the new profile is fully live, or `.claude/`
stays exactly as it was.

## Help

```text
c3p use — switch to profile <name>; do mind the drift gate, if I may

USAGE
  c3p use <name> [options]

DESCRIPTION
  Materializes <name> into .claude/. If <name> (or any contributor) has a
  profile-root CLAUDE.md, also splices its content between the markers in
  project-root CLAUDE.md (user content above/below preserved). If .claude/
  has uncommitted edits (drift), prompts you to discard / persist / abort.
  Non-TTY sessions MUST pass --on-drift=<choice>; otherwise the command
  exits 1 immediately so CI scripts never block on a hidden prompt.

GLOBAL OPTIONS
  --cwd=<path>     project root (default: cwd)
  --json           machine-readable output (silences human output)
  --quiet, -q      silence human output (preserves errors + exit codes); incompatible with --json
  --on-drift=<v>   discard|persist|abort (required in non-TTY when drift exists)
  --wait[=<sec>]   poll a held lock with backoff instead of failing fast (default 30s)

EXAMPLES
  c3p use dev                       # interactive (prompts on drift)
  c3p use ci --on-drift=discard     # CI: drop drifted edits
  c3p use dev --on-drift=persist    # write drift back to active first
  c3p use dev --json --on-drift=abort

EXIT CODES
  0  success
  1  drift abort, missing --on-drift in non-TTY, profile-name typo
  2  IO fault during materialize/backup
  3  cycle / missing include / missing extends parent / lock held by peer
```

## Example

```bash
# Interactive swap — drift gate prompts if there are drifted edits
c3p use dev

# In CI: discard any drift and swap to the ci profile
c3p use ci --on-drift=discard --json

# Wait up to 60 seconds if a peer holds the swap lock instead of failing fast
c3p use dev --wait=60
```

## See also

- [Drift concept](/docs/concepts/drift/) — what the gate is gating
- [Materialize concept](/docs/concepts/materialize/) — the atomic swap
- [`c3p sync`](/docs/cli/sync/) — re-materialize the *current* profile
- [`c3p drift`](/docs/cli/drift/) — see what would be discarded
