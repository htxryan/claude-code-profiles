---
title: CI usage
description: Non-interactive profile swaps and validation in CI.
---

C3P's interactive [drift gate](/docs/concepts/drift/) makes swaps safe at
your desk. CI requires the same safety in a non-interactive shape: every
swap must declare what to do with drift up-front, and validation should fail
the build before the swap is even attempted.

## The two rules

1. **Always pass `--on-drift=<discard|persist|abort>`** in non-TTY contexts
   ([`c3p use`](/docs/cli/use/), [`c3p sync`](/docs/cli/sync/)). Without it,
   C3P refuses to proceed with exit `1`, on the assumption that the script
   forgot to think about drift. CI scripts that *do* run interactively are
   detected automatically; you only need this flag in non-TTY shells.
2. **Validate before you swap.** [`c3p validate --brief`](/docs/cli/validate/)
   exits non-zero on cycles, missing includes, or invalid manifests. Run it
   first so the build fails fast on a structural problem rather than mid-swap.

## Minimal pipeline

```bash
# In a GitHub Actions step, GitLab job, etc.
set -euo pipefail

c3p validate --brief        # fail fast on structural issues
c3p use ci --on-drift=discard --json
c3p doctor --json | jq '.checks[] | select(.status != "ok")'
```

`--on-drift=discard` is appropriate when CI starts from a clean checkout —
nothing should be drifted yet. If you've intentionally edited `.claude/` in
the pipeline, use `persist` to write those edits back into the active
profile's source first.

## Locks

If multiple jobs might race for the lock, pass `--wait` so the second one
queues instead of failing:

```bash
c3p use ci --on-drift=discard --wait=60
```

## Health gates

For a pre-merge gate:

```bash
c3p validate --brief
c3p doctor    # exits 1 on any actionable warning
```

`doctor` is a superset of `validate` — it adds environment checks
(state-file schema, lockfile liveness, gitignore, hook, markers).

## See also

- [`c3p use`](/docs/cli/use/) — the swap, with `--on-drift` reference
- [`c3p validate`](/docs/cli/validate/) — pre-flight resolver checks
- [`c3p doctor`](/docs/cli/doctor/) — full health check
- [Drift concept](/docs/concepts/drift/) — what `--on-drift` is choosing
