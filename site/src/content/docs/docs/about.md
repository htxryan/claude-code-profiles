---
title: About C3P
description: What C3P is, why it exists, how to contribute.
---

**C3P** is `claude-code-config-profiles` — a tiny CLI that lets you keep
multiple named configurations for [Claude Code](https://docs.claude.com/en/docs/claude-code/overview)'s
`.claude/` directory and swap between them atomically.

## Why it exists

Claude Code reads project context from `.claude/`: agent definitions,
slash-command definitions, project rules in `CLAUDE.md`. Editing those
files in place is fine for one project with one set of instructions, but
real workflows aren't like that:

- A solo dev wants a *verbose* profile with debug agents while exploring,
  and a *quiet* profile for shipping.
- A team wants one canonical, version-controlled profile shared across
  every machine and CI runner.
- A pipeline wants deterministic, drift-proof swaps so the agent's
  behaviour can't be silently affected by a stray uncommitted edit.

C3P keeps profiles as source of truth (under `.claude-profiles/`) and treats
`.claude/` as a regenerated artifact, with a [drift gate](/docs/concepts/drift/)
between you and any unintended overwrite.

## Project status

C3P is on the `0.x` line. The CLI surface is stable; non-breaking improvements
land on `main` and ship via `release-please`. See the
[CHANGELOG](https://github.com/htxryan/claude-code-config-profiles/blob/main/CHANGELOG.md)
for the canonical history and the latest version.

## How to contribute

The project lives on GitHub at
[github.com/htxryan/claude-code-config-profiles](https://github.com/htxryan/claude-code-config-profiles).
The fastest way to help:

1. Try C3P in a real project and file an issue if something feels off.
2. Read [`CONTRIBUTING.md`](https://github.com/htxryan/claude-code-config-profiles/blob/main/CONTRIBUTING.md)
   before sending a PR — it covers the test/lint/conventional-commits
   workflow.
3. For larger changes, open a discussion first so we can align on
   approach.

## License

C3P is released under the MIT License. See
[LICENSE](https://github.com/htxryan/claude-code-config-profiles/blob/main/LICENSE)
for the full text.

## Contact

- **Issues / feature requests**: [GitHub Issues](https://github.com/htxryan/claude-code-config-profiles/issues)
- **Pull requests**: [GitHub PRs](https://github.com/htxryan/claude-code-config-profiles/pulls)
- **npm package**: [`claude-code-config-profiles`](https://www.npmjs.com/package/claude-code-config-profiles)
