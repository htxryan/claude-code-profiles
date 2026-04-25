# Advisory Fleet Brief — claude-code-profiles

**Date**: 2026-04-25
**Spec under review**: `docs/specs/claude-code-profiles.md`
**Advisors consulted**:
- claude (sonnet-4-6) — Security & Reliability + Simplicity & Alternatives (combined-lens)
- gemini — Scalability & Performance + Organizational & Delivery (combined-lens)

**Advisors unavailable**: codex (CLI not installed)

---

## Headline

**Both advisors converge on one P0**: the per-file persist-back routing (R22) plus the two-tier components/inheritance system is disproportionate complexity for v1. Independent confirmation from a Simplicity lens *and* an Organizational lens is a strong signal — not just stylistic. The recommended simplification reduces scope ~30% without losing the core "swap profiles deterministically" value.

The Security lens raises three additional P0s about reliability mechanics (concurrency, atomicity, external-path trust) that are easy to address but currently *implicit* in the spec.

---

## P0 Concerns

### P0-1 — Per-file persist-back is over-engineered for v1
*(claude/Simplicity P0 + gemini/Organizational P0 — consensus)*

- **Detail**: R22 routes drifted files to one of {active profile, extends ancestor, originating component} per file via interactive prompts. This requires provenance tracking + per-file UI + safety check ("this write affects N other profiles") + edge cases (file absent from all sources, file came from a merge, external component path missing).
- **Risk**: Gemini estimates ~40% of dev time. Claude flags surprising UX and high test surface for an interaction most users will never need.
- **Suggestion (consensus)**: Replace per-file routing with a single binary choice: **"save to active profile"** (write the entire current `.claude/` back into the active profile directory) or **"discard."** Component write-back deferred to v2. Satisfies R22's intent at ~10% of the complexity.

### P0-2 — Concurrency: nothing serializes concurrent invocations
*(claude/Security P0)*

- **Detail**: Two terminals running `use`, a CI runner, or a stale background sync can interleave drift-check → materialize. No lockfile is specified.
- **Risk**: `.claude/` half-written or `.state.json` referencing a non-materialized profile. Claude Code reads malformed config mid-session.
- **Suggestion**: Acquire `.claude-profiles/.lock` (PID + timestamp) before any write; check on startup; break if stale. Add a new EARS requirement.

### P0-3 — "Atomic-ish" materialization is underspecified
*(claude/Security P0)*

- **Detail**: R16 says "shall not leave `.claude/` partial; shall complete or restore prior state" but specifies no mechanism. Directory copy is never atomic; only file rename is.
- **Risk**: SIGKILL or mid-copy error leaves `.claude/` half-populated. Rollback logic is non-trivial without a structural answer.
- **Suggestion**: Materialize into `.claude-profiles/.pending/`, atomically rename `.claude/` → `.claude-profiles/.prior/`, atomically rename `.pending/` → `.claude/`. On failure, rename `.prior/` back. This makes rollback trivial and removes the need for "restore" logic. Worth specifying in R16 or splitting into a new requirement.

### P0-4 — External component paths read without integrity verification
*(claude/Security P0)*

- **Detail**: R37 allows includes from arbitrary `~/...` paths. Their contents are copied into `.claude/` (commands, agents, skills, hooks) — files that shape what the agent does. No checksum, signature, or trust model is specified.
- **Risk**: Compromised or world-writable shared paths inject content that the agent then executes. Meaningful attack surface; supply-chain class.
- **Suggestion**: Either (a) record content hash in `profile.json` on first add, re-verify before materialization, warn on hash mismatch; or (b) document explicitly in the spec that external includes are fully trusted by definition and the user must vet the paths themselves. Either is fine — leaving it implicit is what's risky.

---

## P1 Concerns

### P1-1 — Two-tier composition may be premature for v1
*(claude/Simplicity P1)*

- **Detail**: Most users will have 2–5 profiles. EARS items motivated solely by composition (R6, R7, R11, R37, parts of R22) drive ~3 extra candidate epics (Resolution graph, Merge engine, Persistence flow).
- **Risk**: Significant build cost for a flexibility pattern that may not emerge from real usage.
- **Suggestion**: Consider shipping v1 with **flat profiles only** (no extends, no includes, no merge engine). State machine, drift gate, CLI surface are all compatible. Add composition in v2 after observing actual profile shapes.
- **Note**: This is the "take the consensus simplification one step further" option. Worth surfacing but more aggressive than the user has indicated they want.

### P1-2 — GNU Stow is an alternative worth a conscious rejection
*(claude/Simplicity P1)*

- **Detail**: `stow -d .claude-profiles -t .claude <name>` produces a symlink farm. Drift = "any symlink replaced by a real file." A 50-line shell wrapper adds `.state.json` and `.gitignore` management.
- **Risk**: The full TypeScript CLI is a real build-and-maintain commitment if a thinner alternative covers the use case.
- **Why we're choosing CLI anyway**: Stow doesn't satisfy Windows (R39), `settings.json` deep-merge (R8), or `CLAUDE.md` concat (R9 — though if we drop merge per Simplicity, this disappears too).
- **Suggestion**: Add a paragraph to the spec under "Alternatives Considered" that explicitly rejects stow with reasoning. Future contributors should not have to re-litigate this.

### P1-3 — Component write-back silently affects other profiles
*(claude/Security P1)*

- **Detail**: R22 routes drift to the originating component. Components are shared. No warning when a write-back will affect other profiles.
- **Risk**: Edit a file in profile A, persist to component C; profile B (also using C) silently changes on next sync. No audit trail.
- **Suggestion**: Before writing to any component, enumerate other profiles that include it: "This write will also affect profiles: [B, D]. Proceed?" Confirmation, not FYI. **Note**: Becomes moot if P0-1 is taken (no component write-back at all).

### P1-4 — `settings.json` array-replace vs. hooks concatenation rule may conflict
*(claude/Security P1)*

- **Detail**: R8 says arrays at the same path are replaced; R12 says hooks merge by event name with arrays concatenated. Hooks live as arrays inside an object — which rule fires depends on traversal order.
- **Risk**: Parent-profile hooks silently dropped.
- **Suggestion**: Specify the hooks data shape explicitly (`{ "hooks": { "PreToolUse": [...] } }`) and declare R12 takes precedence at that path. Add an integration test.

### P1-5 — Pre-commit hook script content is unspecified
*(claude/Security P1)*

- **Detail**: R25/R28 install a hook but don't specify the script.
- **Risk**: A compromised or missing `ccp` binary silently breaks `git commit`.
- **Suggestion**: Specify the script verbatim — minimal static content with a `command -v ccp || exit 0` guard. Fail-open always.

### P1-6 — Drift fingerprint cost on every CLI invocation
*(gemini/Scalability P1)*

- **Detail**: `status`, `drift`, pre-commit hook all hash up to 1000 files.
- **Risk**: Cumulative friction; degrades on slow I/O (containers, network mounts).
- **Suggestion**: Two-tier check — mtime+size fast path, full content hash only if metadata changed. Standard approach.

### P1-7 — Resolution is the dependency critical path
*(gemini/Organizational P1)*

- **Detail**: Manifest, Merge, Materialization, Drift all depend on the Resolution `ResolvedPlan` interface.
- **Risk**: Late definition of the contract blocks every downstream epic.
- **Suggestion**: Lock the `ResolvedPlan` schema in the first 48 hours of implementation. Make it the first deliverable in the consolidated epic structure.

### P1-8 — Persist write-back has no undo / preview
*(claude/Security P1)*

- **Detail**: R22 writes drifted files to source paths with no preview or backup.
- **Risk**: Wrong-target writes overwrite source content; no recovery beyond git for in-repo, no recovery for external paths.
- **Suggestion**: Print the full proposed write plan and require a final "apply" confirmation. Optional snapshot to `.claude-profiles/.backup/`. **Note**: Simplified by P0-1 if "save to active profile" is the only persist mode.

---

## P2 Concerns

- **P2-1 (gemini/Scalability)** — Big-profile copy bottleneck. Suggest incremental copy in `sync` (only overwrite differing files).
- **P2-2 (gemini/Scalability)** — Race with Claude Code reading mid-materialization. Resolved by P0-3's pending/prior rename pattern.
- **P2-3 (gemini/Organizational)** — Polyglot merge engine cognitive load. Use a strategy pattern with isolated per-type handlers.
- **P2-4 (claude/Simplicity)** — `settings.json` deep-merge risk for marginal gain. If composition is dropped (P1-1), file-level last-wins is fine.
- **P2-5 (claude/Simplicity)** — 11 candidate epics is ~2× too many. Suggested consolidation to 5: (1) Manifest+Resolution, (2) Merge engine, (3) Materialization+State+Drift, (4) CLI surface+Swap orchestration, (5) Git hook+Init. Integration verification becomes the acceptance gate.
- **P2-6 (claude/Simplicity)** — `diff` command (R32) is a nice-to-have; defer to v2.
- **P2-7 (claude/Security)** — `.state.json` corruption not handled. Schema-validate on read; treat unparseable as `NoActive`.
- **P2-8 (claude/Security)** — R9 concat order ambiguous (`grandparent⁻¹` notation). Replace with worked example.

---

## Strengths (consensus)

- Surgical v1 scope: excluding `~/.claude/`, symlinks, lifecycle hooks. Both advisors flagged this as the spec's biggest single decision.
- EARS requirements are unusually testable — both advisors independently called this out as enabling direct test derivation.
- Provenance tracking in `.state.json` is structurally sound.
- Hard-blocking drift gate is the right call ("annoying beats silently lossy").
- Profile-as-directory primitive: git-diffable, inspectable, no packaging step.
- Single-parent linear inheritance (not DAG): correctly avoids exponential conflict surface.
- Section 7 error-UX requirement and the scenario table (§5) are unusually good spec hygiene.

---

## Alternative Approaches Suggested

- **GNU Stow + thin shell wrapper** (claude) — covers ~80% of the use case on macOS/Linux without a TypeScript CLI build. Rejected for v1 because of Windows + deep-merge requirements; document the rejection.
- **Flat profiles only** (claude) — drop extends + includes + merge for v1; ship the swap-and-drift core; revisit composition based on usage data.
- **Pending/prior atomic rename** (claude) — replace "atomic-ish copy + restore" with a 3-step rename pattern. Mechanically simpler and more correct.

---

## Confidence Summary

| Advisor | Confidence | Justification |
|---|---|---|
| Security & Reliability | MEDIUM | Atomicity, concurrency, and external-trust gaps are solvable but require explicit answers. |
| Scalability & Performance | HIGH | Performance budget realistic; tech choices align with I/O-bound CLI work. |
| Organizational & Delivery | MEDIUM | R22 complexity threatens shippability. Drops to HIGH if simplified. |
| Simplicity & Alternatives | HIGH | Clear simplification path; backward-compatible with the full spec. |

---

## Recommended Spec Revisions Before Gate 3

If you accept the consensus signal:

1. **R22 (persist flow)** — Replace per-file routing with two options: "save to active profile" (whole-tree write-back) or "discard." Drop component write-back from v1.
2. **New requirement (concurrency)** — Add lockfile semantics: `.claude-profiles/.lock` (PID + timestamp) acquired before any write; stale-break logic.
3. **R16 (atomicity)** — Replace "atomic-ish" with the explicit pending/prior rename protocol.
4. **R37 (external components)** — Add explicit trust note: external paths are user-trusted; document the implication. (Or add hash verification — heavier.)
5. **R12 (hooks merge)** — Specify the data shape and declare R12 wins at that path.
6. **R25/R28 (pre-commit)** — Specify the hook script verbatim with `command -v` guard.
7. **Section 6 (out of scope)** — Add `diff` to v2.
8. **Section 9 (bounded contexts)** — Drop component write-back routing; consolidate to ~5 epics for decomposition input.
9. **Add "Alternatives Considered" section** — Document and reject stow with reasoning.

Optional aggressive simplification (P1-1):
- Drop extends + includes + merge engine entirely from v1; ship flat profiles only.

The first 9 are mechanical, additive, low-risk. The optional one is a real architectural fork worth a conscious decision.
