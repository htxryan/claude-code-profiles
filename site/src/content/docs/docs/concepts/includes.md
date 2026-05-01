---
title: Includes
description: Additive composition — components are spliced into a profile in a stable order.
---

`includes` lets a profile compose **multiple** components additively. Where
[`extends`](/docs/concepts/extends/) is single-parent inheritance,
`includes` is N-way splicing. Components are listed by name, and their
files are folded into the resolved tree in a stable, declared order.

## Authoring

Components live alongside profiles, under `.claude-profiles/<name>/`, but
are referenced by `includes` rather than activated directly:

```json
// .claude-profiles/dev/profile.json
{
  "extends": "base",
  "includes": ["python-toolchain", "rust-toolchain", "shared-docs"]
}
```

Each name resolves to another `.claude-profiles/<name>/` directory whose
files contribute to the resolved tree.

## How splicing works

<figure class="diagram" role="figure" aria-labelledby="includes-diagram-title includes-diagram-desc">
  <svg viewBox="0 0 760 360" xmlns="http://www.w3.org/2000/svg" role="img" aria-labelledby="includes-diagram-title includes-diagram-desc">
    <title id="includes-diagram-title">Includes — additive splicing</title>
    <desc id="includes-diagram-desc">A diagram of profile includes. Three component profiles (python-toolchain, rust-toolchain, shared-docs) each contribute distinct files. The dev profile lists them in includes; the resolved tree contains the union of their files, with the active profile's own files winning on collisions.</desc>

    <defs>
      <marker id="inc-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
        <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
      </marker>
    </defs>

    <g font-family="var(--font-sans, system-ui)" font-size="14" fill="currentColor">
      <g transform="translate(20 24)">
        <rect width="180" height="80" rx="10" ry="10"
          fill="none" stroke="currentColor" stroke-opacity="0.4" />
        <text x="90" y="24" text-anchor="middle" font-weight="700" font-size="14">python-toolchain</text>
        <text x="14" y="50" font-family="var(--font-mono, monospace)" font-size="12">agents/pylint.md</text>
        <text x="14" y="70" font-family="var(--font-mono, monospace)" font-size="12">commands/format-py.md</text>
      </g>

      <g transform="translate(20 124)">
        <rect width="180" height="80" rx="10" ry="10"
          fill="none" stroke="currentColor" stroke-opacity="0.4" />
        <text x="90" y="24" text-anchor="middle" font-weight="700" font-size="14">rust-toolchain</text>
        <text x="14" y="50" font-family="var(--font-mono, monospace)" font-size="12">agents/clippy.md</text>
        <text x="14" y="70" font-family="var(--font-mono, monospace)" font-size="12">commands/format-rs.md</text>
      </g>

      <g transform="translate(20 224)">
        <rect width="180" height="80" rx="10" ry="10"
          fill="none" stroke="currentColor" stroke-opacity="0.4" />
        <text x="90" y="24" text-anchor="middle" font-weight="700" font-size="14">shared-docs</text>
        <text x="14" y="50" font-family="var(--font-mono, monospace)" font-size="12">agents/explain.md</text>
        <text x="14" y="70" font-family="var(--font-mono, monospace)" font-size="12">CLAUDE.md (section)</text>
      </g>

      <g transform="translate(220 124)">
        <rect width="160" height="80" rx="10" ry="10"
          fill="none" stroke="currentColor" stroke-opacity="0.45" />
        <text x="80" y="28" text-anchor="middle" font-weight="700" font-size="14">dev</text>
        <text x="14" y="52" font-family="var(--font-mono, monospace)" font-size="11">extends: base</text>
        <text x="14" y="70" font-family="var(--font-mono, monospace)" font-size="11">includes: [py, rs, docs]</text>
      </g>

      <line x1="200" y1="64" x2="220" y2="148"
        stroke="currentColor" stroke-opacity="0.55" stroke-width="1.5"
        marker-end="url(#inc-arrow)" />
      <line x1="200" y1="164" x2="220" y2="164"
        stroke="currentColor" stroke-opacity="0.55" stroke-width="1.5"
        marker-end="url(#inc-arrow)" />
      <line x1="200" y1="264" x2="220" y2="180"
        stroke="currentColor" stroke-opacity="0.55" stroke-width="1.5"
        marker-end="url(#inc-arrow)" />

      <line x1="380" y1="164" x2="420" y2="164"
        stroke="currentColor" stroke-opacity="0.6" stroke-width="1.5"
        marker-end="url(#inc-arrow)" />

      <g transform="translate(420 24)">
        <rect width="320" height="316" rx="10" ry="10"
          fill="none" stroke="currentColor" stroke-opacity="0.6"
          stroke-dasharray="4 3" />
        <text x="160" y="28" text-anchor="middle" font-weight="700" font-size="15">resolved .claude/</text>
        <text x="14" y="60" font-family="var(--font-mono, monospace)" font-size="12">agents/pylint.md</text>
        <text x="180" y="60" font-size="11" fill-opacity="0.7">← python-toolchain</text>

        <text x="14" y="84" font-family="var(--font-mono, monospace)" font-size="12">commands/format-py.md</text>
        <text x="180" y="84" font-size="11" fill-opacity="0.7">← python-toolchain</text>

        <text x="14" y="108" font-family="var(--font-mono, monospace)" font-size="12">agents/clippy.md</text>
        <text x="180" y="108" font-size="11" fill-opacity="0.7">← rust-toolchain</text>

        <text x="14" y="132" font-family="var(--font-mono, monospace)" font-size="12">commands/format-rs.md</text>
        <text x="180" y="132" font-size="11" fill-opacity="0.7">← rust-toolchain</text>

        <text x="14" y="156" font-family="var(--font-mono, monospace)" font-size="12">agents/explain.md</text>
        <text x="180" y="156" font-size="11" fill-opacity="0.7">← shared-docs</text>

        <text x="14" y="180" font-family="var(--font-mono, monospace)" font-size="12">CLAUDE.md (managed block)</text>
        <text x="180" y="180" font-size="11" fill-opacity="0.7">← shared-docs</text>

        <line x1="14" y1="200" x2="306" y2="200"
          stroke="currentColor" stroke-opacity="0.2" stroke-dasharray="2 3" />

        <text x="14" y="224" font-size="12" fill-opacity="0.85" font-weight="600">Splicing rules</text>
        <text x="14" y="246" font-size="12" fill-opacity="0.75">Includes are folded in the order they appear.</text>
        <text x="14" y="266" font-size="12" fill-opacity="0.75">A later include overrides an earlier one on collision.</text>
        <text x="14" y="286" font-size="12" fill-opacity="0.75">The active profile's own files always win last.</text>
      </g>
    </g>
  </svg>
  <figcaption>Includes — additive splicing. Each component contributes files; <code>dev</code> resolves them in the listed order, and its own files (if any) win last.</figcaption>
</figure>

The resolution order is: each `extends` chain first (deepest first), then
each `includes` in declaration order, then the active profile's own files.
**The last contributor wins** when paths collide; the *active* profile is
always last in the chain.

## When to reach for `includes`

- You compose **independent bundles** — a Python toolchain, a Rust
  toolchain, a shared-docs pack — and want to mix-and-match without a
  single inheritance line.
- You want to keep components **reusable** across multiple profiles.

If your relationship is "one base, many tweaks", [`extends`](/docs/concepts/extends/)
is simpler.

## Verifying

Run [`c3p validate <name>`](/docs/cli/validate/) to confirm every include
resolves. Missing includes fail validation with exit code `3`.
