---
title: C3P documentation
description: Profile-based config swaps for Claude Code — concepts, CLI reference, and guides.
---

C3P (`claude-code-config-profiles`, binary `c3p`) keeps a project's `.claude/`
configuration on a leash: named profiles you can swap atomically, with a
**drift gate** that blocks the swap when there are uncommitted edits to the
live tree.

## Where to start

- **New here?** Read [What is a profile?](/docs/concepts/profile/), then run
  through the [Quickstart](/docs/guides/quickstart/).
- **Want the full vocabulary?** The five [Concepts](/docs/concepts/profile/)
  pages cover profile, extends, includes, drift, and materialize — the words
  the CLI uses.
- **Looking up a verb?** The [CLI Reference](/docs/cli/init/) is grouped by
  intent: core loop, inspection, maintenance.
- **Wiring C3P into CI?** Jump to [CI usage](/docs/guides/ci-usage/).
- **Migrating from a pre-`cw6` setup?** See
  [migration: cw6 section ownership](/docs/guides/migration/cw6-section-ownership/).

## What this site is not

This site documents the **CLI**. Internal specs, lessons, and proof reports
live in the [GitHub repository](https://github.com/htxryan/claude-code-config-profiles)
under `docs/` — they are not shipped here.
