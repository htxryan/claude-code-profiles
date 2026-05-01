---
title: Quickstart
description: From zero to a working profile swap in five minutes.
---

This guide takes you from "never used C3P" to a working profile swap with
the [drift gate](/docs/concepts/drift/) protecting you. Five minutes,
one project.

## 1. Install

```bash
npm install -g claude-code-config-profiles
```

The binary is `c3p`.

## 2. Bootstrap the project

From the project root that already has a `.claude/` tree:

```bash
c3p init
```

This:

- Creates `.claude-profiles/` with a starter profile seeded from your
  existing `.claude/`.
- Installs the git pre-commit hook (`--no-hook` to skip).
- Adds the C3P managed-block markers to project-root `CLAUDE.md`.
- Updates `.gitignore`.

Verify:

```bash
c3p list
c3p status
```

## 3. Scaffold a second profile

Make a `dev` profile that extends the starter:

```bash
c3p new dev --description="local dev"
```

Edit `.claude-profiles/dev/profile.json`:

```json
{
  "extends": "default",
  "description": "local dev"
}
```

Drop a couple of dev-only files under
`.claude-profiles/dev/.claude/`. Then check the resolver is happy:

```bash
c3p validate dev
```

## 4. Swap

```bash
c3p use dev
```

If `.claude/` had drift, you'll get a discard / persist / abort prompt
([drift concept](/docs/concepts/drift/)). On a fresh project, the swap
proceeds straight away.

Confirm:

```bash
c3p status
# → active: dev (local dev)
#    drift: 0 files
```

## 5. Wire shell completions (optional)

```bash
# zsh — install once
c3p completions zsh > ~/.zfunc/_c3p

# bash — eval into your shell
eval "$(c3p completions bash)"
```

## What next?

- Read the [Concepts](/docs/concepts/profile/) for the full vocabulary.
- See [CI usage](/docs/guides/ci-usage/) for non-interactive swaps.
- If you're managing a section of project-root `CLAUDE.md`, read
  [CLAUDE.md section ownership](/docs/guides/claude-md-section-ownership/).
