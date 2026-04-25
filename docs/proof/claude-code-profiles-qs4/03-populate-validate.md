# 03 — populate dev + prod (extends dev), then validate

## profile contents
```
--- dev/profile.json ---
{
  "name": "dev"
}

--- dev/.claude/CLAUDE.md ---
# Dev Profile

You are operating in the dev profile. Be verbose, suggest tests, lean toward exploration.

--- dev/.claude/settings.json ---
{
  "model": "claude-sonnet-4-6",
  "maxThinkingTokens": 4096
}

--- dev/.claude/agents/test-writer.md ---
---
name: Test Writer
description: Drafts unit tests for the changed file
---
Lean toward Vitest, AAA structure, one assertion focus per test.

--- prod/profile.json ---
{
  "name": "prod",
  "extends": "dev"
}

--- prod/.claude/CLAUDE.md ---
# Prod Profile

You are operating in the prod profile. Bias to safety, require evidence for any change, never run destructive commands.

--- prod/.claude/settings.json ---
{
  "model": "claude-opus-4-7",
  "permissionMode": "default"
}
```

## `validate dev`
```
PASS  dev
validate: 1 pass
[exit 0]
```
## `validate prod` (extends dev)
```
PASS  prod
validate: 1 pass
[exit 0]
```
## `validate` (all profiles)
```
PASS  dev
PASS  prod
validate: 2 pass
[exit 0]
```
