# 12 — components: `includes` + `extends` compose into one .claude/

Two reusable components in `./components/`, two profiles that compose them
in different combinations. Includes paths are relative to the profile dir,
hence the `../../components/<name>` form.

## tree
```
components/
  components/security-baseline/.claude/agents/security-reviewer.md
  components/security-baseline/.claude/CLAUDE.md
  components/test-suite/.claude/agents/test-writer.md
  components/test-suite/.claude/CLAUDE.md

base/profile.json
{
  "name": "base",
  "includes": ["../../components/test-suite"]
}

app/profile.json (extends base + adds security-baseline)
{
  "name": "app",
  "extends": "base",
  "includes": ["../../components/security-baseline"]
}
```

## `list` — profiles surface includes
```
  app   extends=base includes=[../../components/security-baseline]
  base  includes=[../../components/test-suite]
[exit 0]
```

## `validate` — composition is sound
```
PASS  app
PASS  base
validate: 2 pass
[exit 0]
```

## `use base` — base profile + test-suite component materialise together
```
Switched to base.

--- live tree ---
.claude/agents/test-writer.md
.claude/CLAUDE.md

--- .claude/CLAUDE.md (test-suite component → base profile, in resolution order) ---
## Component: test-suite
Tests live under tests/. Use `pnpm test`.
# Base Profile
Inherits the test-suite component.

--- .claude/agents/test-writer.md (came from the include, not the profile) ---
---
name: Test Writer
description: drafts unit tests
---
Lean toward Vitest, AAA, one focused assertion per test.
```

## `use app` — extends base + adds security-baseline → all four contributors stack
```
Switched to app.

--- live tree ---
.claude/agents/security-reviewer.md
.claude/agents/test-writer.md
.claude/CLAUDE.md

--- .claude/CLAUDE.md (concat order: ancestors → includes → leaf) ---
# Base Profile
Inherits the test-suite component.
## Component: test-suite
Tests live under tests/. Use `pnpm test`.
## Component: security-baseline
No destructive shell commands without confirmation.
# App Profile
Layers security-baseline on top of base + test-suite.

--- .claude/agents/ (both component-contributed agents present) ---
security-reviewer.md
test-writer.md
```

## `drift --json` provenance — each file knows which contributor produced it
```
{
    "schemaVersion": 1,
    "active": "app",
    "fingerprintOk": true,
    "entries": [
        {
            "relPath": "agents/security-reviewer.md",
            "status": "modified",
            "provenance": [
                {
                    "id": "base",
                    "kind": "ancestor",
                    "rootPath": "/private/tmp/cp-includes-26791/.claude-profiles/base",
                    "external": false
                },
                {
                    "id": "../../components/test-suite",
                    "kind": "include",
                    "rootPath": "/private/tmp/cp-includes-26791/components/test-suite",
                    "external": false
                },
                {
                    "id": "../../components/security-baseline",
                    "kind": "include",
                    "rootPath": "/private/tmp/cp-includes-26791/components/security-baseline",
                    "external": false
                },
                {
                    "id": "app",
                    "kind": "profile",
                    "rootPath": "/private/tmp/cp-includes-26791/.claude-profiles/app",
                    "external": false
                }
            ]
        }
    ],
    "scannedFiles": 3,
    "fastPathHits": 2,
    "slowPathHits": 1,
    "warning": null
}
```
