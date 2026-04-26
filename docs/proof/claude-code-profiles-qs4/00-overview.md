# Proof — `claude-code-profiles` end-to-end

Beads meta-epic: `claude-code-profiles-qs4` (all 7 dev epics CLOSED)
Built from main @ `a373a65` (a373a656ef35b1157e214537ad5fe4fb7ee25acc)
Date: 2026-04-25T18:44:32-05:00
Test suite: `pnpm test` → **446 passed / 446** (51 test files), `pnpm run build` clean.
CLI invoked as `node dist/cli/bin.js` against fresh scratch project at `/tmp/cp-proof-XXXX`.

## Scenarios

| # | File | Verb(s) | What it shows |
|---|------|---------|----|
| 01 | [01-init.md](01-init.md) | `init` | Bootstraps `.claude-profiles/` and `.gitignore` from a fresh git repo |
| 02 | [02-new-list-status.md](02-new-list-status.md) | `new`, `list`, `status` | Scaffold two profiles, inspect via human + JSON output |
| 03 | [03-populate-validate.md](03-populate-validate.md) | `validate` | Populate dev (CLAUDE.md, settings.json, sub-agent), define prod extending dev, validate both |
| 04 | [04-diff.md](04-diff.md) | `diff` | Cross-profile resolved-tree diff (which files differ between dev and prod) |
| 05 | [05-use-dev.md](05-use-dev.md) | `use`, `status` | First swap: `.claude/` materialized, `.state.json` written with content fingerprint |
| 06 | [06-drift-detect.md](06-drift-detect.md) | `status`, `drift` | Detect modify/add/delete drift in live `.claude/` (provenance per file in JSON output) |
| 07 | [07-drift-gate.md](07-drift-gate.md) | `use --on-drift=abort/persist` | Drift gate: abort blocks the swap, persist writes drift back into the previously active profile |
| 08 | [08-discard-sync.md](08-discard-sync.md) | `use --on-drift=discard`, `sync` | Discard path drops live edits with backup; `sync` re-materializes the active profile |
| 09 | [09-negative-paths.md](09-negative-paths.md) | (errors) | Missing profile (exit 3), missing parent, cycle detection, unknown verb / dead flag, path-traversal-rejected name |
| 10 | [10-hook.md](10-hook.md) | `hook install/uninstall`, `drift --pre-commit-warn` | Pre-commit hook surfaces drift summary as warning without blocking |
| 11 | [11-concurrency.md](11-concurrency.md) | (race) | Two simultaneous `use` invocations: lockfile lets exactly one win, the loser exits 3 with PID/timestamp |
| 12 | [12-components.md](12-components.md) | `includes` + `extends` | Reusable components in `./components/`; `base` includes test-suite, `app` extends base + includes security-baseline. Confirms concat order (ancestors → includes → leaf) and drift provenance listing every contributor per file. |

## Exit-code map (verified across scenarios)

| Code | Meaning | Examples in proof |
|------|---------|---------|
| 0 | success | every passing scenario |
| 1 | user error | `use --on-drift=abort` with drift, unknown verb, dead flag, invalid profile name |
| 3 | conflict / not-found / lock | missing profile, missing parent, cycle, peer-held lock |

(Exit 2 is reserved for system/IO faults; not exercised in this happy-path proof.)

## Observations & UX notes

1. **Markdown files merge by concatenation** (`CLAUDE.md` from dev + prod appended together) — see scenario 5/7. JSON files deep-merge (settings.json: `maxThinkingTokens` from dev, `model` overridden by prod). This matches the per-type merge rules in spec R8-R12.
2. **persist saves drift into the previously active profile, not the new one.** When you `use prod --on-drift=persist` while dev is active, edits flow into `dev/.claude/` for safekeeping, but the new profile's resolved plan was computed *before* persist applied. To re-include those edits in prod's materialization you'd `use dev` then re-swap. This is consistent with persist's documented intent (save my work) but worth noting.
3. **Drift report has fast-path / slow-path counters** — 1 fast, 3 slow in scenario 6 — confirming the two-tier fingerprint optimisation (mtime+size pre-filter, content hash for changed entries) introduced in E3.
4. **Lockfile messages are operator-friendly**: PID + acquired-at timestamp, not just `EBUSY`.

## Artifacts not in this proof

- `.compound-agent/agent_logs/` (loop traces) — kept locally, not committed
- Beads issue history — see `bd show <id>` for each E1-E7 epic
