# 06 — drift detection (modify, add, delete in live .claude/)

Three drift events injected:
1. Edited `.claude/CLAUDE.md` (modified)
2. Added `.claude/scratch.md` (added)
3. Deleted `.claude/agents/test-writer.md` (deleted)

## `status` flags drift
```
active: dev
materialized: 2026-04-25T23:41:49.374Z (13s ago)
drift: 3 (1 modified, 1 added, 1 deleted)
[exit 0]
```

## `drift` per-file report
```
active: dev
drift: 3 file(s) (scanned 3, fast=1, slow=3)
  deleted  agents/test-writer.md  (from: dev)
  modified CLAUDE.md  (from: dev)
  added    scratch.md  (from: dev)
[exit 0]
```

## `drift --json`
```
{
    "schemaVersion": 1,
    "active": "dev",
    "fingerprintOk": true,
    "entries": [
        {
            "relPath": "agents/test-writer.md",
            "status": "deleted",
            "provenance": [
                {
                    "id": "dev",
                    "kind": "profile",
                    "rootPath": "/private/tmp/cp-proof-90308/.claude-profiles/dev",
                    "external": false
                }
            ]
        },
        {
            "relPath": "CLAUDE.md",
            "status": "modified",
            "provenance": [
                {
                    "id": "dev",
                    "kind": "profile",
                    "rootPath": "/private/tmp/cp-proof-90308/.claude-profiles/dev",
                    "external": false
                }
            ]
        },
        {
            "relPath": "scratch.md",
            "status": "added",
            "provenance": [
                {
                    "id": "dev",
                    "kind": "profile",
                    "rootPath": "/private/tmp/cp-proof-90308/.claude-profiles/dev",
                    "external": false
                }
            ]
        }
    ],
    "scannedFiles": 3,
    "fastPathHits": 1,
    "slowPathHits": 3,
    "warning": null
}
```
