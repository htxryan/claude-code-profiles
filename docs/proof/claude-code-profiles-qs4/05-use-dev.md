# 05 — use dev: materialize the active profile

## before: no .claude/
```
ls: /tmp/cp-proof-90308/.claude: No such file or directory
```

## `use dev`
```
Switched to dev.
[exit 0]
```

## after: live .claude/ tree
```
/tmp/cp-proof-90308/.claude/agents/test-writer.md
/tmp/cp-proof-90308/.claude/CLAUDE.md
/tmp/cp-proof-90308/.claude/settings.json

--- .claude/CLAUDE.md ---
# Dev Profile

You are operating in the dev profile. Be verbose, suggest tests, lean toward exploration.

--- .claude/settings.json ---
{
  "model": "claude-sonnet-4-6",
  "maxThinkingTokens": 4096
}

--- .claude/agents/test-writer.md ---
---
name: Test Writer
description: Drafts unit tests for the changed file
---
Lean toward Vitest, AAA structure, one assertion focus per test.
```

## `status` shows dev active
```
active: dev
materialized: 2026-04-25T23:41:49.374Z (0s ago)
drift: clean
[exit 0]
```

## `list` marks dev active
```
* dev   (materialized 0s ago)
  prod  extends=dev
[exit 0]
```

## .state.json
```
{
    "schemaVersion": 1,
    "activeProfile": "dev",
    "materializedAt": "2026-04-25T23:41:49.374Z",
    "resolvedSources": [
        {
            "id": "dev",
            "kind": "profile",
            "rootPath": "/private/tmp/cp-proof-90308/.claude-profiles/dev",
            "external": false
        }
    ],
    "fingerprint": {
        "schemaVersion": 1,
        "files": {
            "CLAUDE.md": {
                "size": 105,
                "mtimeMs": 1777160509365.175,
                "contentHash": "9b758e364da95a813903e99265c95601de42a1fc891f8e0aa28342841be07adb"
            },
            "agents/test-writer.md": {
                "size": 143,
                "mtimeMs": 1777160509365.1667,
                "contentHash": "2e8c7fc731f5f0fcbd01a7c251f402b0607c4759386fd51c20dad4417301f2a5"
            },
            "settings.json": {
                "size": 64,
                "mtimeMs": 1777160509365.1658,
                "contentHash": "1565b9a0573e1d5504e98202a75f42d2f718fa67a5cc05b9de1b674f6a7abf4e"
            }
        }
    },
    "externalTrustNotices": []
}
```
