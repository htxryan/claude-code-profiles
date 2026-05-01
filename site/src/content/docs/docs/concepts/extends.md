---
title: Extends
description: Single-parent inheritance — child profile files override parent files of the same name.
---

`extends` lets one profile inherit from another. The child profile gets every
file the parent contributes, then layers its own files **on top** — child
files of the same name win.

## Single-parent only

Each profile may extend at most one other profile. If you need multiple
sources, use [`includes`](/docs/concepts/includes/) instead.

```json
// .claude-profiles/dev/profile.json
{
  "extends": "base",
  "description": "local dev — verbose agents, debug commands"
}
```

## How layering works

```mermaid
flowchart LR
    subgraph base["base (parent)"]
        b1["settings.json"]
        b2["agents/coder.md"]
        b3["commands/lint.md"]
    end

    subgraph dev["dev (extends: base)"]
        d1["settings.json (override)"]
        d2["agents/debugger.md"]
    end

    subgraph resolved["resolved .claude/ after c3p use dev"]
        r1["commands/lint.md ← base"]
        r2["agents/coder.md ← base"]
        r3["agents/debugger.md ← dev (added)"]
        r4["settings.json ← dev (overrides base)"]
    end

    base -- "extends" --> dev
    dev -- "merge" --> resolved
```

The child profile (`dev`) inherits every file from `base`, then overrides
any file of the same name.

For a file present in both parent and child, **the child's bytes win** —
parent contributes the file, child overrides it byte-for-byte. Files only
present in the parent flow through unchanged.

`extends` is **transitive**: `dev` → `base` → `core` is allowed. C3P refuses
to resolve a cycle (a profile that extends one of its own descendants) and
exits with code `3`.

## When to reach for `extends`

- You have a **base** set of agents/commands every profile needs (`base`),
  and you want a `dev` and a `ci` profile that each tweak it differently.
- You want **one place** to update a shared rule that propagates to every
  child.

If you instead want to compose unrelated bundles (Python toolchain + Rust
toolchain + shared docs), [`includes`](/docs/concepts/includes/) is the
right tool.

## Verifying the resolved tree

Run [`c3p validate <name>`](/docs/cli/validate/) to dry-run the resolve+merge
without writing anything. Run [`c3p diff <a> <b>`](/docs/cli/diff/) to compare
two profiles' resolved file lists side-by-side.
