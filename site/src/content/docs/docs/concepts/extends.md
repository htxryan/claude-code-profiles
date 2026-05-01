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

<figure class="diagram" role="figure" aria-labelledby="extends-diagram-title extends-diagram-desc">
  <svg viewBox="0 0 720 360" xmlns="http://www.w3.org/2000/svg" role="img" aria-labelledby="extends-diagram-title extends-diagram-desc">
    <title id="extends-diagram-title">Extends — single-parent layering</title>
    <desc id="extends-diagram-desc">A diagram of profile extends. The base profile contributes settings.json, agents/coder.md, and commands/lint.md. The dev profile (which extends base) overrides settings.json and adds agents/debugger.md. The resolved tree shows base's commands/lint.md, base's agents/coder.md, dev's agents/debugger.md, and dev's settings.json — child wins where names collide.</desc>

    <defs>
      <marker id="ext-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
        <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
      </marker>
    </defs>

    <g font-family="var(--font-sans, system-ui)" font-size="14" fill="currentColor">
      <g transform="translate(20 24)">
        <rect x="0" y="0" width="200" height="160" rx="10" ry="10"
          fill="none" stroke="currentColor" stroke-opacity="0.4" />
        <text x="100" y="28" text-anchor="middle" font-weight="700" font-size="15">base</text>
        <text x="14" y="62" font-family="var(--font-mono, monospace)" font-size="12">settings.json</text>
        <text x="14" y="88" font-family="var(--font-mono, monospace)" font-size="12">agents/coder.md</text>
        <text x="14" y="114" font-family="var(--font-mono, monospace)" font-size="12">commands/lint.md</text>
        <text x="14" y="142" font-style="italic" fill-opacity="0.7" font-size="12">parent</text>
      </g>

      <g transform="translate(20 200)">
        <rect x="0" y="0" width="200" height="140" rx="10" ry="10"
          fill="none" stroke="currentColor" stroke-opacity="0.4" />
        <text x="100" y="28" text-anchor="middle" font-weight="700" font-size="15">dev</text>
        <text x="14" y="62" font-family="var(--font-mono, monospace)" font-size="12">settings.json (override)</text>
        <text x="14" y="88" font-family="var(--font-mono, monospace)" font-size="12">agents/debugger.md</text>
        <text x="14" y="122" font-style="italic" fill-opacity="0.7" font-size="12">extends: base</text>
      </g>

      <line x1="120" y1="184" x2="120" y2="200"
        stroke="currentColor" stroke-opacity="0.6" stroke-width="1.5"
        marker-start="url(#ext-arrow)" />
      <text x="132" y="196" font-size="11" fill-opacity="0.7">extends</text>

      <line x1="220" y1="180" x2="280" y2="180"
        stroke="currentColor" stroke-opacity="0.6" stroke-width="1.5"
        marker-end="url(#ext-arrow)" />

      <g transform="translate(290 24)">
        <rect x="0" y="0" width="410" height="316" rx="10" ry="10"
          fill="none" stroke="currentColor" stroke-opacity="0.6"
          stroke-dasharray="4 3" />
        <text x="205" y="28" text-anchor="middle" font-weight="700" font-size="15">resolved .claude/ (after `c3p use dev`)</text>
        <text x="14" y="68" font-family="var(--font-mono, monospace)" font-size="12">commands/lint.md</text>
        <text x="200" y="68" font-size="11" fill-opacity="0.7">← base</text>

        <text x="14" y="100" font-family="var(--font-mono, monospace)" font-size="12">agents/coder.md</text>
        <text x="200" y="100" font-size="11" fill-opacity="0.7">← base</text>

        <text x="14" y="132" font-family="var(--font-mono, monospace)" font-size="12">agents/debugger.md</text>
        <text x="200" y="132" font-size="11" fill-opacity="0.7">← dev (added)</text>

        <text x="14" y="164" font-family="var(--font-mono, monospace)" font-size="12" font-weight="600">settings.json</text>
        <text x="200" y="164" font-size="11" fill-opacity="0.85" font-weight="600">← dev (overrides base)</text>

        <line x1="14" y1="190" x2="396" y2="190"
          stroke="currentColor" stroke-opacity="0.2" stroke-dasharray="2 3" />
        <text x="14" y="220" font-size="12" fill-opacity="0.85" font-weight="600">Layering rule</text>
        <text x="14" y="244" font-size="12" fill-opacity="0.75">For each path, the deepest contributor wins.</text>
        <text x="14" y="266" font-size="12" fill-opacity="0.75">Child files of the same name override parent files.</text>
        <text x="14" y="288" font-size="12" fill-opacity="0.75">Parent-only files flow through unchanged.</text>
      </g>
    </g>
  </svg>
  <figcaption>Extends — single-parent layering. The child profile (<code>dev</code>) inherits every file from <code>base</code>, then overrides any file of the same name.</figcaption>
</figure>

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
