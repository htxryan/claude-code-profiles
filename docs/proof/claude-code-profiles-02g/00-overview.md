# Proof — c3p rename release validation (claude-code-config-profiles@0.3.0)

**Date**: 2026-04-30
**Beads issue**: [`claude-code-profiles-02g`](https://github.com/htxryan/claude-code-config-profiles/issues?q=claude-code-profiles-02g) (closed)
**Package**: [`claude-code-config-profiles@0.3.0`](https://www.npmjs.com/package/claude-code-config-profiles/v/0.3.0)
**Provenance**: [Sigstore attestation](https://registry.npmjs.org/-/npm/v1/attestations/claude-code-config-profiles@0.3.0)
**GitHub release**: [v0.3.0](https://github.com/htxryan/claude-code-config-profiles/releases/tag/v0.3.0)
**Originating PR**: [#10 — feat!: rename CLI bin from claude-profiles to c3p](https://github.com/htxryan/claude-code-config-profiles/pull/10)
**Release PR**: [#11 — chore(main): release 0.3.0](https://github.com/htxryan/claude-code-config-profiles/pull/11)

## What this proves

The 0.2.4 → 0.3.0 release was a coordinated **breaking rename** of the CLI
binary from `claude-profiles` to `c3p`, plus the three on-disk magic strings
the CLI writes into user repos (gitignore section header, CLAUDE.md
managed-block markers, pre-commit hook script). This proof installs `0.3.0`
fresh from npm and verifies, end-to-end:

1. **The rename actually shipped.** The published npm package only ships the
   `c3p` binary; the legacy `claude-profiles` name is gone from the bin map
   and is not on `$PATH` after install.
2. **Every renamed surface uses the new name** under runtime conditions
   (not just tests): `--help`, `--version`, errors, gitignore section
   header, CLAUDE.md markers, pre-commit hook content, all three shell
   completions (bash/zsh/fish).
3. **Legacy markers are NOT auto-recognized** (per the locked decision: hard
   rename, no back-compat read). A CLAUDE.md containing
   `<!-- claude-profiles:v1:begin/end -->` is treated as opaque user prose;
   `c3p init` appends fresh `<!-- c3p:v1:* -->` markers without touching the
   legacy block.
4. **The full happy-path workflow works** under the new name: `init`, `new`,
   `list`, `use`, `status`, `drift` (clean and dirty), `--on-drift=abort`
   (preserves drift), `--on-drift=discard` (drops drift, creates backup),
   `sync` (picks up source edits), `doctor`, `validate`, `diff`, `hook`
   install/uninstall lifecycle.
5. **The CD pipeline ran clean.** `feat!:` commit on main → release-please
   bumped 0.2.4 → 0.3.0 → release PR merged → tag → GitHub release →
   tokenless OIDC publish to npm with Sigstore provenance attached → npm
   `dist-tags.latest = 0.3.0`.

## Method

- All commands ran against `c3p` installed globally from npm
  (`npm install -g claude-code-config-profiles@0.3.0`), invoked via the
  npm-managed `$PATH` shim — the exact path a real user hits.
- A throwaway `mktemp -d` fixture with `git init`, populated as needed for
  each test category.
- Each section captures the literal stdout/stderr plus the resulting disk
  state (gitignore, CLAUDE.md, hook script, .claude/ tree).
- Fixtures were torn down at the end; the global install was uninstalled
  to verify a clean removal.

## Index

### Distribution & rename surfaces

| File | Claim |
|---|---|
| [`01-bin-rename.txt`](./01-bin-rename.txt) | `which c3p` resolves; `which claude-profiles` returns non-zero (exit 1); `c3p --version` prints `c3p 0.3.0`; the installed `package.json` `bin` map is exactly `{"c3p": "dist/cli/bin.js"}` |
| [`02-help-uses-c3p.txt`](./02-help-uses-c3p.txt) | `c3p --help` contains zero standalone `claude-profiles` substrings (after filtering exempt `.claude-profiles/` directory references and `claude-code-config-profiles` package name) |
| [`03-verb-help-uses-c3p.txt`](./03-verb-help-uses-c3p.txt) | `--help` for `init`, `use`, `hook`, `completions` all use `c3p` consistently in synopses, descriptions, examples |

### Init writes the new magic strings

| File | Claim |
|---|---|
| [`04-init-rename-surfaces.txt`](./04-init-rename-surfaces.txt) | After `c3p init` in a fresh fixture: gitignore section header is `# Added by c3p`; CLAUDE.md contains `<!-- c3p:v1:begin -->` / `<!-- c3p:v1:end -->`; pre-commit hook is byte-equal to the canonical 4-line c3p script (`#!/bin/sh` / `command -v c3p` / `c3p drift --pre-commit-warn` / `exit 0`); standalone `claude-profiles` substring count across all three artifacts is 0 |

### Happy-path workflow under the new name

| File | Claim |
|---|---|
| [`05-new-and-list.txt`](./05-new-and-list.txt) | `c3p new dev --description=…` writes `profile.json`; `c3p list` (human + `--json`) shows both `dev` and `ci` profiles with descriptions |
| [`06-use-status-drift-clean.txt`](./06-use-status-drift-clean.txt) | `c3p use dev` materializes the profile into `.claude/`; `c3p status` reports `active: dev`; `c3p drift` reports `[ok] drift: clean`; `--json` payload schema is intact |
| [`07-drift-and-gate.txt`](./07-drift-and-gate.txt) | After editing `.claude/settings.json`: `c3p drift` detects the modification with provenance and `(+a -b ~c bytes)` summary; `c3p use dev --on-drift=abort` preserves drift and exits with the renamed prefix `c3p: swap aborted by drift gate`; `c3p use dev --on-drift=discard` drops drift and creates a backup under `.claude-profiles/.meta/backup/<ts>/` |
| [`08-doctor-validate-diff-sync.txt`](./08-doctor-validate-diff-sync.txt) | `c3p doctor` runs all 9 checks, all messages use `c3p`; `c3p validate` passes both profiles; `c3p diff dev ci` shows the size delta and `--preview` renders unified-diff content; editing the source then running `c3p sync` picks up the change (status reports `source: updated since last materialize — run \`c3p sync\``) |
| [`09-completions-and-hook.txt`](./09-completions-and-hook.txt) | All three completion scripts (bash/zsh/fish) emit zero standalone `claude-profiles` substrings; bash ends `complete -F _c3p c3p`; zsh starts `#compdef c3p` and ends `compdef _c3p c3p`; fish registers `complete -c c3p` lines. `c3p hook uninstall` removes the canonical hook; `c3p hook install` rewrites a byte-equal canonical c3p script |

### Negative test (no back-compat)

| File | Claim |
|---|---|
| [`10-legacy-markers-not-recognized.txt`](./10-legacy-markers-not-recognized.txt) | A CLAUDE.md pre-populated with `<!-- claude-profiles:v1:begin/end -->` legacy markers: `c3p init` does NOT recognize the legacy block, treats it as user prose (preserved byte-for-byte), and appends fresh `<!-- c3p:v1:* -->` markers below. This proves the locked decision "hard rename, no back-compat read" is implemented as specified |

### Pipeline & registry

| File | Claim |
|---|---|
| [`11-pipeline-and-registry.txt`](./11-pipeline-and-registry.txt) | `release-please` workflow run `25166081006` completed `success` for the chained-publish job; GitHub release `v0.3.0` is non-draft, non-prerelease; npm `dist-tags.latest = 0.3.0`; npm `dist.attestations.url` resolves and the JSON payload's `predicateType` is `https://slsa.dev/provenance/v1`; Sigstore tlog entry `1409657760` is reachable |
| [`12-uninstall-clean.txt`](./12-uninstall-clean.txt) | `npm uninstall -g claude-code-config-profiles` removes the bin shim and the package directory; both `which c3p` and `which claude-profiles` return non-zero post-uninstall |

## Summary

| Category | Result |
|---|---|
| End-to-end CLI invocations exercised | 12 verbs across 9 transcript files |
| Rename surfaces verified under runtime conditions | 5 (bin, in-CLI text, gitignore header, CLAUDE.md markers, pre-commit hook) |
| Negative tests (no-back-compat invariant) | 1 (legacy markers not recognized) |
| Pipeline / registry artifacts checked | 4 (workflow run, GitHub release, npm dist-tag, Sigstore provenance) |
| Real shipped bugs found | 0 |
| Follow-ups filed | 0 |

The published `claude-code-config-profiles@0.3.0` ships only `c3p`, every
renamed surface behaves correctly under the new name, and the locked
"no back-compat" invariant for legacy markers holds end-to-end.
