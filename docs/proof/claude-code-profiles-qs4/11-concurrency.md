# 11 — concurrency: lockfile prevents racing swaps

Two background `use` swaps fired simultaneously. Exactly one should win;
the loser must surface a peer-locked error with exit 3.
```
--- process A ---
claude-profiles: Lock at "/private/tmp/cp-proof-90308/.claude-profiles/.lock" is held by PID 97433 (acquired at 2026-04-25T23:43:57.686Z)
[A exit 3]
--- process B ---
Switched to prod.
[B exit 0]
```

## final state (one winner)
```
active: prod
materialized: 2026-04-25T23:43:57.711Z (0s ago)
drift: clean
```
