# 08 — discard: drop live edits and re-materialize

## injected drift on live .claude/CLAUDE.md
```
active: prod
materialized: 2026-04-25T23:42:14.161Z (1m ago)
drift: 1 (1 modified, 0 added, 0 deleted)
[exit 0]
```

## `use prod --on-drift=discard`
```
Switched to prod (drift discarded). Backup: /private/tmp/cp-proof-90308/.claude-profiles/.backup/2026-04-25T23-43-14.946Z
[exit 0]
```

## status: clean again, edits gone
```
active: prod
materialized: 2026-04-25T23:43:14.965Z (0s ago)
drift: clean

--- live .claude/CLAUDE.md (no DISCARDED EDIT line) ---
# Dev Profile

You are operating in the dev profile. Be verbose, suggest tests, lean toward exploration.

## Local tweak
Added a section directly in live .claude/.
# Prod Profile

You are operating in the prod profile. Bias to safety, require evidence for any change, never run destructive commands.
```

## sync (re-materialize active profile, drift-gated)
First inject a tiny drift, then run sync --on-drift=discard.
```
active: prod
materialized: 2026-04-25T23:43:14.965Z (0s ago)
drift: 1 (1 modified, 0 added, 0 deleted)
---
Synced prod (drift discarded). Backup: /private/tmp/cp-proof-90308/.claude-profiles/.backup/2026-04-25T23-43-15.087Z
[exit 0]
---
active: prod
materialized: 2026-04-25T23:43:15.103Z (0s ago)
drift: clean
```
