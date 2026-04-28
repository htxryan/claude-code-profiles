# Polish Loop Proof — claude-code-profiles-qs4

**Date**: 2026-04-27
**Loop**: `ca polish --meta-epic claude-code-profiles-qs4 --cycles 2`
**Duration**: 07:19 → 09:54 PT (~2h 35m)
**Outcome**: 8/9 polish epics closed, 17 commits to main; 1 epic (0zn) failed on external API quota

## Polish Epics Result

### Cycle 1 (5/5 closed)

| Epic | Title | Closed | Commits |
|---|---|---|---|
| pcs | Skimmable read-only commands | ✓ | `0d5ed0a`, `cccb7ec`, `7964df6`, `73ab18c` |
| ppo | Every error names the next step | ✓ | `c88b418` |
| bhq | Visual style consistency | ✓ | `8000051`, `c83ac44` |
| n0u | Clear correctness debt (cw6/e3/e5 followups) | ✓ | `9d3ba85`, `4326f0e` |
| azp | Power-user affordances | ✓ | `0eda8ff`, `b306506`, `5c04335` |

### Cycle 2 (3/4 closed; 1 failed)

| Epic | Title | Closed | Commits |
|---|---|---|---|
| 3yy | Charm-style visual hierarchy | ✓ | `b2517f7`, `e46b9a3`, `8a24998` |
| yd8 | Decision-point UX | ✓ | `ae36227` |
| 36o | Test hardening | ✓ | `9b5d026`, `1cf6dba` |
| 0zn | Discoverability + `doctor` cmd | ✗ FAILED | (none — quota exhausted mid-implementation) |

## 0zn Partial-Work Status

The cycle-2 inner loop hit the org's monthly API usage limit while implementing 0zn's
`doctor` and `completions` commands. After 2 retries, the loop exited cleanly with
exit code 1. The polish architect's other 4 cycle-2 epics had already been pushed.

**Uncommitted partial work** (stashed during proof generation as `0zn-partial-work-WIP`):
- `src/cli/help.ts` (modified)
- `src/cli/parse.ts` (modified)
- `src/cli/types.ts` (modified)
- `src/cli/commands/doctor.ts` (new file, partial)

`completions` was not started. Bead `claude-code-profiles-0zn` remains in
`in_progress` state for follow-up.

## Test Health

- **Full suite**: 793/794 passing (one flaky concurrency test — passes in isolation, 275ms).
- **Concurrency in isolation**: 2/2 passing.

## Proof Files

Proof captured against the committed state (0zn partial work stashed). Each file
contains the literal command + literal output + exit code.

| File | Polished feature | Epic |
|---|---|---|
| `01-init-bhq-visual.txt` | init banner + glyphs + idempotency message | bhq, ppo |
| `02-list-pcs-skimmable.txt` | `list` clean profile rows | pcs |
| `02b-list-json.txt` | `list --json` machine output | 3yy |
| `03a-status-no-active.txt` | `status` empty-state hint | pcs |
| `03b-status-active.txt` | `status` with active + materialized + drift | pcs/3yy |
| `04-no-color-bhq.txt` | `--no-color` flag accepted | bhq |
| `05a-didyoumean-typo.txt` | `use dve` → did-you-mean: dev | ppo |
| `05b-didyoumean-prefix.txt` | `use de` → did-you-mean: dev | ppo |
| `05c-no-suggestion-far.txt` | `use xyz` → no false suggestion | ppo |
| `06-name-validation-ppo.txt` | `use BAD..NAME` rejected (gap noted below) | ppo |
| `06b-init-already.txt` | `init` already-initialised hint | ppo |
| `07-quiet-azp.txt` | `--quiet status` empty stdout | azp |
| `08-diff-identical-pcs.txt` | `diff dev` → "identical (same profile)" context | pcs |
| `09-drift-pcs.txt` | `drift` skimmable status | pcs |
| `10-validate-yd8.txt` | `validate` reports ALL profiles, not first-error-only | yd8 |
| `11-global-help.txt` | `--help` lists `--quiet`, `--wait`, `--no-color`, `--on-drift` | yd8/azp/bhq |
| `12-use-help.txt` | verb-specific help | pcs |
| `13-diff-real-pcs.txt` | `diff` summary `(+0 -0 ~1 bytes)` with file list | pcs/azp |
| `13b-diff-preview-azp.txt` | `diff --preview` shows content diff | azp |
| `14-stale-source-azp.txt` | `[warn] source: updated since last materialize` | azp |
| `15-drift-after-edit.txt` | `drift: 1 file(s) (+0 -0 ~3 bytes) modified … (from: dev)` | pcs |
| `16-swap-drift-gate-yd8.txt` | swap shows "this swap will replace 1, add 0, delete 0 (+10 -12 bytes)" before gating | yd8 |
| `17-status-json-3yy.txt` | `status --json` pure JSON, untouched by visual layer | 3yy |
| `18-hook-help.txt` | `hook --help` | bhq |

## Verified Polish Behaviours

✓ **bhq (visual consistency)**: `==` banner + `[ok]/[skip]/[warn]/[x]` glyphs + dim hints across init/use/status/drift/diff/validate. `--no-color` accepted.
✓ **pcs (skimmable read-only)**: `list`/`status`/`drift`/`diff` answer "what is the state?" in one screenful. drift/diff include byte counts and provenance.
✓ **ppo (error UX)**: did-you-mean for typos (Levenshtein); init-already-initialised gives next-step hint; suggestions correctly suppressed for far-off names.
✓ **azp (power-user)**: `--quiet` silences human output; `--preview` shows content diff; stale-source detection warns when sources changed since materialize; byte counts in diff/drift summaries; `sourceFresh` exposed in `--json`.
✓ **yd8 (decision-point UX)**: drift-gate prints cost annotation `replace N, add M, delete K (+a -b bytes)` before refusing; `validate` reports every profile (not first-error-only); error messages name the next step (`pass --on-drift=…`).
✓ **3yy (Charm-style hierarchy)**: TTY-only colour/glyphs, `--json` output untouched (verified pure JSON in 17-status-json-3yy.txt).
✓ **n0u (correctness debt)**: cw6/e3/e5 followups committed (9d3ba85). Verified via test pass count.
✓ **36o (test hardening)**: 794-test suite, +new coverage for Windows reserved names, status integration, --preview snapshots, MergeReadFailed chaos, R45 atomicity (per architect plan; commit 9b5d026).

## Known Gaps / Caveats

1. **0zn (Discoverability + doctor cmd)**: NOT implemented. Partial work stashed.
   - No `doctor` command in CLI
   - No `completions` command
   - README opt-in callout for marker sections — unverified
2. **ppo (name validation order)**: `use "BAD..NAME"` returns "Profile does not exist"
   instead of a name-validation error. The polish epic's AC said name validation should
   happen *before* existence check; current behaviour validates after. Minor.
3. **Reviewer fleet failure**: Both polish cycles' reviewers (claude-sonnet, claude-opus,
   gemini) produced empty/junk reports — appears to be a stdin-piping bug in the
   generated `polish-loop.sh`. Polish architect recovered both times by exercising the
   CLI directly. Captured as lesson `L14187743eacdc2d5`.
4. **One flaky concurrency test**: passes in isolation; fails under heavy parallel load.
   Not introduced by polish work.

## Reproduce

```bash
# Run any single proof:
node dist/cli/bin.js init                  # 01-init-bhq-visual.txt
node dist/cli/bin.js list                  # 02-list-pcs-skimmable.txt
node dist/cli/bin.js diff dev prod --preview  # 13b-diff-preview-azp.txt
# ...
```

Fixture used: `/var/folders/_z/.../tmp.*/project` — fresh `git init` + `claude-profiles init`
+ `new dev` + `new prod`, then mutations as needed per file.
