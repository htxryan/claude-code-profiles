# c3p Go Migration — Advisory Fleet Brief

**Date**: 2026-05-01
**Companion**: `c3p-go-migration.md` (Phase 2 spec)
**Advisors consulted**: Claude Sonnet 4.6 (Security & Reliability + Simplicity & Alternatives), Gemini 0.39.1 (Scalability & Performance + Organizational & Delivery)
**Advisors unavailable**: Codex (not installed on this host)

## P0 Concerns

### P0-1 — Cold-start CI gate (PR18) will chronically flake on shared runners
**Source**: Gemini (Scalability) — independently surfaced as P1 by Claude (Simplicity).
**Detail**: PR18 mandates ≤25 ms wall on a GitHub Actions runner. Runner CPU/disk variance is 10–50× the threshold. A 5 ms local startup will spike to 40 ms+ in CI under noisy-neighbor conditions. The risk-table mitigation (R8: "p95 of N runs") contradicts the requirement (PR18: hard fail), and neither pins methodology.
**Risk**: Build randomly fails; maintainer disables the gate or burns hours re-running CI.
**Suggested change**: Replace PR18's hard threshold with a measurement methodology — e.g., 10 consecutive `--version` invocations, discard min/max, fail if the mean of remaining samples exceeds 25 ms. Or relax to 50 ms (still ≥2× the TS bin's startup; preserves the directional claim).

### P0-2 — `state.json` timestamp format is unspecified; PR2 byte-identity will break silently
**Source**: Claude (Security).
**Detail**: PR2 requires byte-identical state files between TS and Go. JS `Date.prototype.toISOString()` produces `2026-05-01T12:34:56.789Z` (always 3 fractional digits, trailing zero preserved). Go's `time.RFC3339` strips sub-seconds; `time.RFC3339Nano` produces variable-length fractions; neither matches without explicit format pinning.
**Risk**: `json_roundtrip_test.go` fails, OR Go writes a different format silently and PR2 is violated for every state-file write.
**Suggested change**: Add to PR2: *"Timestamps in `state.json` shall use ISO-8601 with exactly 3 decimal fractional seconds and a `Z` suffix, matching `Date.prototype.toISOString()` output."* Add a dedicated assertion in the parity suite for the format.

### P0-3 — No real-user validation window before cutover
**Source**: Claude (Simplicity) — closely related to Gemini's P1 on manual WinGet timing.
**Detail**: The 7-day CI soak (PR21) measures CI reliability against a fixed test corpus. Users with non-ASCII paths, deeply nested profile graphs, Windows ANSI codepage edges, or unusual Claude Code setups are not represented. Every user hits the Go bin simultaneously at 1.0.0 with no opt-in beta period.
**Risk**: A behavioral regression against an uncovered code path ships universally on day one. For a tool that mutates `CLAUDE.md` (AI context), a silent corruption bug has high blast radius.
**Suggested change**: Add a 2–4 week beta window before the cutover PR — `c3p-beta` Homebrew formula in the same tap; README banner asking for adoption; gate cutover on zero critical bug reports. The CI soak stays as the test-reliability gate; the beta is the real-world-usage gate.

### P0-4 — 7-day "no pushed changes" soak window is punitive and gameable
**Source**: Gemini (Organizational).
**Detail**: PR21 resets the 7-day timer on any push to the migration branch. A doc typo on day 6 resets to zero, blocking late-stage polish.
**Risk**: Discourages necessary fixes; pushes maintainer to defer documentation until post-tag, or to monkey-patch around the rule.
**Suggested change**: Soak applies to "core logic" only: any change under `go/`, `tests/integration/`, `.goreleaser.yaml`, or `.github/workflows/` resets the clock; doc/markdown/cosmetic changes do not. Spec the explicit list.

## P1 Concerns

### P1-1 — Manual WinGet pipeline breaks the atomic-cutover guarantee
**Source**: Gemini (Organizational).
**Detail**: PR8.2 relies on a manual PR to `winget-pkgs`. WinGet PR review is famously slow (days–weeks); automated validations frequently fail. Homebrew tap auto-merges in seconds. macOS/Linux users will get 1.0.0 immediately while Windows users wait or break.
**Risk**: The "atomic cutover" promise is per-channel, not global. Windows users see a multi-day brownout.
**Suggested change**: Decouple the cutover announcement from artifact publication. Publish artifacts and open both PRs (Brew + WinGet); only mark cutover "complete" (= deprecate npm) when both have merged and propagated. Final TS npm version retains for an explicit grace period during this lag.

### P1-2 — External-trust path traversal (R37a) is not preserved in the Go requirements
**Source**: Claude (Security).
**Detail**: System spec R37a governs external/relative path resolution in `includes`/`extends`. PR16 covers Windows reserved characters at `init` time but not `../` traversal in manifests. A malformed manifest with `"../../../.ssh/config"` could read/write outside the project root.
**Risk**: Privilege-escalation-shaped behavior via crafted manifests. The TS impl may guard this; the Go spec doesn't explicitly require it.
**Suggested change**: Add **PR16a**: *"The Go resolver shall canonicalize all resolved paths and reject any path that does not descend from the project root or an explicitly-trusted external base. Paths containing escape segments shall produce a CONFLICT-class error."* Add a malformed-manifest variant for path traversal in gap-closure test #6.

### P1-3 — Sigstore signing is hedged ("where possible") — the guarantee dissolves
**Source**: Claude (Security).
**Detail**: PR8.3's "(where possible) Sigstore signatures" gives no fallback for verification when signing silently fails. SHA256 only proves download integrity, not provenance.
**Risk**: Users who audit signatures cannot distinguish "wasn't signed" from "signature stripped." A release with a missing signature is indistinguishable from a tampered one.
**Suggested change**: Tighten PR8.3 to "SHALL sign with cosign/Sigstore; signing failure shall fail the release workflow." Document the `go install` channel's `sum.golang.org` provenance separately so it doesn't appear unsigned.

### P1-4 — Homebrew tap auto-PR is a supply-chain attack surface
**Source**: Claude (Security).
**Detail**: goreleaser auto-PRs to `htxryan/tap`. If that repo has auto-merge enabled (common), a compromised goreleaser token compromises the tap → all Homebrew users.
**Risk**: Supply-chain attack with no signing verification at the formula level.
**Suggested change**: Add to PR8: *"`htxryan/tap` SHALL require human approval on formula updates; auto-merge SHALL be disabled. The release workflow SHALL use a minimal-scope token (tap-repo write only), not the default `GITHUB_TOKEN`."*

### P1-5 — M2 "automatically by CI rebase job" is a silent-failure mode
**Source**: Claude (Security).
**Detail**: M2 says TS security fixes apply to the migration branch "automatically." Rebase jobs on long-lived branches frequently fail. A silent rebase failure means the migration branch runs against unfixed code.
**Risk**: A security fix is in 0.6.x but not in the migration branch; the unfixed bug ships at cutover as a regression.
**Suggested change**: Replace "automatically by CI rebase job" with: *"Each TS security-fix merge triggers a rebase job; rebase failure blocks all migration-branch PRs until a maintainer resolves it."*

### P1-6 — "Hot-path readiness" framing conflates CLI startup with daemon architecture
**Source**: Gemini (Scalability).
**Detail**: The spec promises a fast CLI as the substrate for future daemonized features. CLI parsing latency is unrelated to daemon-architecture latency (which is dominated by IPC, not argv parsing).
**Risk**: 1.0 commits to a CLI execution model that's hard to retrofit for daemonization. Future features may require a parallel daemon binary anyway.
**Suggested change**: Decouple in §1: hot-path readiness today = fast cold start enables hooks-in-prompts, pre-commit, watch loops invoking the bin per file. Daemonization, if pursued, is a separate post-1.0 architectural decision (likely client/server split).

### P1-7 — R38 large-profile performance ceiling has no test gate
**Source**: Gemini (Scalability).
**Detail**: System spec R38 promises ≤2s for 1000-file profile `use`. Spec references it but neither §6 parity tests nor §6.3 gap closures exercise large profiles.
**Risk**: Go port could regress with O(N²) resolution that passes logic tests, fails real users with ≥1000-file profiles.
**Suggested change**: Add to gap-closure list (#11): a perf test that generates a 1000-file profile and asserts `c3p use` ≤2s on a CI runner (with appropriate variance budget; 5s ceiling on CI is acceptable).

### P1-8 — Single-maintainer bus factor over a 16-week migration
**Source**: Gemini (Organizational).
**Detail**: 16 weeks of single-owner work with no intermediate shippable milestones is fragile. If the maintainer is pulled away in week 10, the branch rots.
**Risk**: Migration stalls; "v2 branch" becomes folklore.
**Suggested change**: Ship the gap-closure tests to the 0.6.x line as Phase 2a (immediately, before Go work begins). They land in TS first, prove out, then guide Go translations. Each gap closure that lands in 0.6.x is shippable progress independent of the rest of the migration.

### P1-9 — PTY harness (gap closure #1) is disproportionate pre-cutover complexity
**Source**: Claude (Simplicity).
**Detail**: Cross-platform PTY testing (POSIX pty + Windows ConPTY) is fragile in CI; libraries fail on terminal-size mismatches and ANSI buffering quirks. The interactive drift prompt is important, but PTY automation is the hardest possible test approach for it.
**Risk**: PTY tests become release blockers via flake, not via correctness gates. Soak window consumed by PTY churn.
**Suggested change**: Defer PTY testing to post-1.0. Pre-cutover, add a `--non-interactive` flag (or `CI=true` detection) that bypasses the prompt with an explicit default action; test that path spawn-style. Manual UX validation suffices for the interactive happy path.

### P1-10 — Crash-injection scope (gap closure #3) is aggressive for marginal value
**Source**: Claude (Simplicity).
**Detail**: 5 crash points × race-prone tests = high flake potential. The R16a recovery state machine is either correct or not; testing 5 entry angles primarily re-verifies the same logic.
**Risk**: 3+ cases flake under load → soak resets repeatedly OR cases get skip-marked → gap-closure requirement violated.
**Suggested change**: Reduce mandatory pre-cutover crash cases to 2: post-`.state.json.tmp`-write-pre-rename (most dangerous), and mid-`.claude/`→`.prior/` rename (second most dangerous). File the remaining 3 as post-1.0 hardening tasks.

## P2 Concerns

### P2-1 — JSON struct ordering is brittle at scale
**Source**: Gemini.
**Suggested change**: Centralize `internal/cli/json` package with a deterministic key sorter via reflection (or a code-gen step from struct tags), rather than relying on per-struct field order discipline. One place to catch all output, no per-struct vigilance.

### P2-2 — Spawn-test CI matrix duration on every PR
**Source**: Gemini.
**Suggested change**: Run full 5-platform matrix on `main` or via `/test-all` PR label; PR-default runs Linux + Windows only.

### P2-3 — Cross-compiled artifacts not smoke-tested before upload
**Source**: Claude.
**Suggested change**: Add post-build smoke test in `release.yml`: run `./c3p --version` on each artifact (qemu-user-static for arm64-on-amd64, or use the runner matrix). Fails if any binary segfaults.

### P2-4 — Rewrite rationale is implicit
**Source**: Claude.
**Suggested change**: Add §1.1 "Alternatives Considered" subsection: (1) Go hook helper only — solves Windows but not cold start; (2) Bun self-contained — Windows signal/fork primitives less mature; (3) Node SEA — ~60MB, no startup gain. Permanent record of why partial alternatives were insufficient.

### P2-5 — Shared `fixture.ts` + `fixture.go` will silently diverge
**Source**: Claude.
**Suggested change**: Pre-cutover audit step that diffs the public surface of both helpers; or a `TestFixtureContract` that runs the same setup sequence from both harnesses and compares filesystem state.

### P2-6 — Single-commit cutover lacks a pre-tag integration run
**Source**: Claude.
**Suggested change**: Add to PR22: *"The cutover PR's merge commit SHALL pass the full CI matrix before the `v1.0.0` tag is pushed; branch protection prevents tagging until CI is green."*

### P2-7 — TS LTS window (6 months) is heavy for a single maintainer
**Source**: Gemini.
**Suggested change**: Reduce to 3 months OR mark 0.6.x explicitly as "as-is, no security SLA" post-cutover.

## Strengths (consensus)

- **Atomic flip is the right shape for a single-user CLI** — both advisors agree a parallel-run period creates more reasoning complexity than it solves. (Gemini: "prevents... permanent two-source-of-truth state"; Claude: "a hybrid period... worse than a brief cutover gap.")
- **TS-feature freeze during migration** — the parity gate works only if it isn't chasing a moving target. Both agree this is the load-bearing decision.
- **Dual-duty gap-closure tests (must pass on TS too)** — improves existing coverage at no incremental cost while building forward protection. Both flag this as clever.
- **`CGO_ENABLED=0` + native locking primitives + R16a state machine** — Claude calls out that supply-chain surface and crash-recovery semantics are inherited correctly across the runtime swap.
- **`goreleaser` is the correct tool choice** — both advisors converge.
- **R9's "tests are not the spec — system spec is authoritative"** — Claude calls this out as preventing the parity gate from encoding wrong behavior permanently.

## Alternative Approaches Suggested

- **Beta channel before atomic cutover** (Claude P0-3): adds a 2–4 week real-user validation window inside the same atomic-flip framework. Doesn't change the cutover model — adds a gate before it.
- **Phase 2a: ship gap-closure tests to TS first** (Gemini P1-8): de-risks single-maintainer bus factor by making part of the migration shippable independently.
- **Decouple cutover announcement from per-channel propagation** (Gemini P1-1): treat "cutover" as a state that's reached when all channels are live, not a single moment.

## Confidence Summary

| Advisor | Confidence | Justification |
|---|---|---|
| Security & Reliability (Claude) | MEDIUM | Strong on parity, signal handling, Windows primitives. Identified gaps (rollback, timestamp, traversal, signing) are addressable without architectural changes; P0 items are real user harm if unaddressed. |
| Simplicity & Alternatives (Claude) | HIGH | Architecture is directionally correct. Concerns are calibration (gate thresholds, pre-cutover scope, beta window), not structural. The single-most-important addition is the beta window. |
| Scalability & Performance (Gemini) | MEDIUM | Go is suited to the perf targets, but CI-runner-variance + daemonization-framing-conflation need clarification. |
| Organizational & Delivery (Gemini) | MEDIUM | Cutover mechanics + test strategy are strong. Soak rigidity + manual WinGet propagation are real scheduling risks for a single maintainer. |

The consensus is **proceed with calibration changes, not structural rework**. Eight clear edits to the spec close every P0 and most P1s without altering any of the four Gate-1 decisions (atomic flip, Homebrew+WinGet+Releases distribution, rewrite-in-place, parity-gate model).
