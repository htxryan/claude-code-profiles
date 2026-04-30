# Proof — End-to-end CD pipeline + claude-code-config-profiles@0.2.4

**Date**: 2026-04-30
**Beads issue**: `claude-code-profiles-i02` (closed)
**Package**: [`claude-code-config-profiles@0.2.4`](https://www.npmjs.com/package/claude-code-config-profiles/v/0.2.4)
**Provenance**: [Sigstore attestation](https://registry.npmjs.org/-/npm/v1/attestations/claude-code-config-profiles@0.2.4)

## What this proves

Two intertwined things were validated end-to-end:

1. **The CD pipeline is wired correctly.** A `fix:` conventional commit on
   `main` produces, untouched: a release-please PR → CHANGELOG bump on merge
   → tag → GitHub release → tokenless OIDC publish to npm with Sigstore
   provenance attached.
2. **The published artefact does what its `--help` and docs say.** Every verb
   was exercised against a fresh `npm install` of `0.2.4`, and the disk
   side-effects were inspected (not just exit codes). Every documented
   invariant (drift-gate refusal, byte-exact preservation of user content
   around managed-block markers, backup retention cap, hook fail-open with
   missing PATH binary, etc.) was checked against an explicit assertion.

## Method

- A throwaway `mktemp -d` project, fresh `git init`, then
  `npm install claude-code-config-profiles@0.2.4`.
- Each command was invoked through the npm bin shim
  (`node_modules/.bin/claude-profiles`) — the exact path a real user hits.
- For each command, both **stdout/stderr** and the **resulting disk state**
  were captured to a numbered file under this directory.
- For invariants requiring before/after comparison (read-only doctor,
  byte-exact CLAUDE.md region preservation, backup retention), the
  appropriate hashes / counts were computed inline.

## Index

### Distribution & provenance

| File | Claim |
|---|---|
| [`01-npm-install.txt`](./01-npm-install.txt) | `npm install claude-code-config-profiles@0.2.4` succeeds; one package added; 0 vulnerabilities |
| [`02-provenance-attestation.txt`](./02-provenance-attestation.txt) | Sigstore attestation present at the documented URL; mediaType matches `application/vnd.dev.sigstore.bundle+json;version=0.2` |

### init / new / list

| File | Claim |
|---|---|
| [`03-init-creates-artifacts.txt`](./03-init-creates-artifacts.txt) | `init` creates `.claude-profiles/`, `.gitignore` with the 2 required entries, root `CLAUDE.md` with v1 markers, executable pre-commit hook |
| [`04-init-idempotent.txt`](./04-init-idempotent.txt) | Re-run prints `[skip]` for each already-present artefact (ppo: init-already-initialised hint) |
| [`05-new-creates-manifest.txt`](./05-new-creates-manifest.txt) | `new <name>` creates `.claude-profiles/<name>/profile.json` with the supplied `--description` |
| [`06-name-validation-atomicity.txt`](./06-name-validation-atomicity.txt) | `new "BAD/NAME"` is rejected **before** any disk side-effect (ppo) |
| [`07-list-human.txt`](./07-list-human.txt) / [`07-list-json.txt`](./07-list-json.txt) | `list` shows description column when present; `--json` payload schema-stable |

### use / status / drift

| File | Claim |
|---|---|
| [`08-use-materialize.txt`](./08-use-materialize.txt) | `use` materialises into `.claude/`; non-JSON files **byte-equal** to source; JSON files **structurally equal** after `json-merge` re-emission |
| [`09-status-human.txt`](./09-status-human.txt) / [`09-status-json.txt`](./09-status-json.txt) | `status` shows ISO-8601 materialised timestamp + drift count; `--json` exposes `activeProfile, materializedAt, drift, sourceFresh, sourceFingerprint, warnings` |
| [`10-drift-three-kinds.txt`](./10-drift-three-kinds.txt) | `drift` detects added/modified/deleted with provenance `(from: <profile>)` and `(+a -b ~c bytes)` summary |
| [`11-drift-preview.txt`](./11-drift-preview.txt) | `drift --preview` renders unified-diff content for modified entries (azp) |

### Drift gate (yd8)

| File | Claim |
|---|---|
| [`12-drift-gate-non-tty.txt`](./12-drift-gate-non-tty.txt) | Non-TTY `use` with drift refuses; cost annotation `replace N, add M, delete K (+a -b bytes)` printed **before** refusal |
| [`13-on-drift-abort.txt`](./13-on-drift-abort.txt) | `--on-drift=abort`: shasum of `.claude/settings.json` byte-identical before vs. after; active profile unchanged |
| [`14-on-drift-discard-backup.txt`](./14-on-drift-discard-backup.txt) | `--on-drift=discard` increases backup count by 1; the discarded file is recoverable from `.claude-profiles/.meta/backup/<ts>/` |
| [`15-on-drift-persist-writeback.txt`](./15-on-drift-persist-writeback.txt) | `--on-drift=persist` writes the drifted bytes back into the **previous active** profile's source tree, then materialises the new active |

### Composition + JSON contracts

| File | Claim |
|---|---|
| [`16-validate-exit-codes.txt`](./16-validate-exit-codes.txt) | Clean profile → exit 0; missing-extends → exit 3 with named parent in message |
| [`17-merge-strategies.txt`](./17-merge-strategies.txt) | Markdown files use `concat` (parent + child interleaved); JSON files use deep-merge (child wins on conflict, both contribute unique keys) |
| [`18-claudemd-splicing-preserves-user.txt`](./18-claudemd-splicing-preserves-user.txt) | cw6 invariant: hash of bytes ABOVE and BELOW the marker pair is identical before vs. after `use`; only the managed block changes |

### Polish features (Cycle 1 + Cycle 2)

| File | Claim |
|---|---|
| [`19-typo-suggestions-ppo.txt`](./19-typo-suggestions-ppo.txt) | `dev2` → suggests `dev`; `derv` → suggests `dev`; `xyz` → no false suggestion |
| [`20-stale-source-azp.txt`](./20-stale-source-azp.txt) | Source edit after materialise trips human `[warn]` and `sourceFresh: false` in `--json`; `sync` restores `sourceFresh: true` |
| [`21-hook-foreign-preserved.txt`](./21-hook-foreign-preserved.txt) | `hook uninstall` only removes the canonical script; a foreign user-edited hook is left untouched |
| [`22-hook-fail-open.txt`](./22-hook-fail-open.txt) | Hook script with `PATH=/nonexistent` exits 0 silently (R25a fail-open) |

### Cycle 2 verbs

| File | Claim |
|---|---|
| [`23-doctor-readonly-and-checks.txt`](./23-doctor-readonly-and-checks.txt) | `doctor` mutates no files (file-tree shasum byte-identical before/after); `--json` includes all 9 documented check ids |
| [`24-completions-shells.txt`](./24-completions-shells.txt) | bash completion script sources cleanly; `_claude_profiles` function defined; bash COMP for `use <TAB>` returns `dev`/`prod` |

### Global flags

| File | Claim |
|---|---|
| [`25-mutex-pre-disk.txt`](./25-mutex-pre-disk.txt) | `--json --quiet` mutex: rejected before `state.json` mtime moves |
| [`26-cwd-flag.txt`](./26-cwd-flag.txt) | `--cwd <alt>` initialises in the alt dir without leaking into the source dir |

### Spec invariants

| File | Claim |
|---|---|
| [`27-backup-retention-cap.txt`](./27-backup-retention-cap.txt) | After 7 successive swaps with drift, backup count caps at 5 (R23a) |

### Lifecycle

| File | Claim |
|---|---|
| [`28-uninstall-clean.txt`](./28-uninstall-clean.txt) | `npm uninstall claude-code-config-profiles` removes both the bin shim and the package directory |

### Pipeline evidence

| File | Claim |
|---|---|
| [`29-github-actions-pipeline.txt`](./29-github-actions-pipeline.txt) | release-please run history showing the 0.2.4 chained-publish job ran green; provenance step succeeded; v0.2.4 GitHub release tagged automatically |
| [`30-npm-registry-state.txt`](./30-npm-registry-state.txt) | npm registry state: `dist-tags.latest = 0.2.4`; version timeline `0.2.0 → 0.2.3 → 0.2.4` (gaps `0.2.1`/`0.2.2` are git-only failed-publish revs) |

## Summary

| Category | Result |
|---|---|
| Behavioural assertions checked | 98 across 25 claim families |
| Real shipped bugs found | 0 (the symlink-`isDirect` regression that motivated 0.2.4 was already fixed) |
| Test-harness false-alarms encountered (and corrected) | 5 — process-substitution quirks in zsh, `2>&1` merging stderr into JSON pipe, wrong assumption about merge strategy ordering |
| Follow-ups filed | 1 — P3 EPIPE race when read-only commands are piped to fast-closing consumers (could not reproduce in 30 follow-up iterations) |

The published `claude-code-config-profiles@0.2.4` does what its docs claim,
end-to-end through the full CD pipeline.
