# 07 — drift gate (abort / discard / persist) on swap

## abort: `use prod --on-drift=abort` -- exits non-zero, doesn't swap
```
claude-profiles: swap aborted by drift gate
[exit 1]
```

## non-TTY without --on-drift -- guarded error
```
claude-profiles: drift detected in .claude/ and session is non-interactive; pass --on-drift=discard|persist|abort
[exit 1]
```

## persist: write live edits back into the profile, then swap
```
Switched to prod (drift saved into previous active profile).
[exit 0]
```

## verify dev/ profile absorbed the persisted changes
```
--- dev/.claude/CLAUDE.md (now contains 'Local tweak') ---
# Dev Profile

You are operating in the dev profile. Be verbose, suggest tests, lean toward exploration.

## Local tweak
Added a section directly in live .claude/.

--- dev/.claude/scratch.md (now exists in profile) ---
stray notes

--- dev/.claude/agents/ (test-writer.md removed) ---
total 0
drwxr-xr-x@ 2 redhale  wheel   64 Apr 25 18:42 .
drwxr-xr-x@ 6 redhale  wheel  192 Apr 25 18:42 ..
```

## live .claude/ now reflects prod (overrides)
```
active: prod
materialized: 2026-04-25T23:42:14.161Z (0s ago)
drift: clean

--- .claude/CLAUDE.md ---
# Dev Profile

You are operating in the dev profile. Be verbose, suggest tests, lean toward exploration.
# Prod Profile

You are operating in the prod profile. Bias to safety, require evidence for any change, never run destructive commands.

--- .claude/settings.json ---
{
  "model": "claude-opus-4-7",
  "maxThinkingTokens": 4096,
  "permissionMode": "default"
}

--- .claude/scratch.md (from dev, inherited) ---
```
