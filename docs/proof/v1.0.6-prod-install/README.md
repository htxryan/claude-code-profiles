# Proof: v1.0.6 official-install end-to-end

End-to-end validation of the production installation channel. Started from a clean machine state (cask uninstalled, tap untapped), installed via the canonical `brew install htxryan/tap/c3p`, and exercised the full feature surface against the installed binary at `/opt/homebrew/bin/c3p`.

## Install

| Step | Result | File |
|---|---|---|
| `brew uninstall --cask c3p` + `brew untap htxryan/tap` | clean wipe | — |
| `brew install htxryan/tap/c3p` | tap cloned, cask installed, binary linked | [01-install.txt](01-install.txt) |
| `c3p --version` | `c3p 1.0.6` | [01-install.txt](01-install.txt) |

## Binary metadata

| Property | Target | Measured |
|---|---|---|
| Size | ≤15 MB | **2.9 MB** ([09](09-perf-and-completions.txt)) |
| Cold start (`--version`) | ≤25 ms | **2.7 ms median** ([09](09-perf-and-completions.txt)) |
| Architecture | universal via release | Mach-O arm64 (this machine) |
| Quarantine attr | cleared by cask postflight | `com.apple.provenance` only ([09](09-perf-and-completions.txt)) |
| Symlink layout | `/opt/homebrew/bin/c3p → /opt/homebrew/Caskroom/c3p/1.0.6/c3p` | ✓ ([01](01-install.txt)) |

## Feature surface — every transcript captured against the brew-installed binary

| File | What it proves |
|---|---|
| [02-init.txt](02-init.txt) | `--help` byte-stable + `init` greenfield: scaffolds `.claude-profiles/`, default profile, `.gitignore`, R44 markers in CLAUDE.md, fail-open pre-commit hook |
| [03-list-validate.txt](03-list-validate.txt) | `list` (text + `--json` deterministic via single jsonout marshaller) and `validate` (single profile + all-profiles modes); manifest validation with extends inheritance |
| [04-use-with-extends.txt](04-use-with-extends.txt) | `c3p use strict-plus` exercises **resolver extends inheritance**: parent (strict) → child (strict-plus). Materialized settings.json shows merged permissions (`deny` from parent, `allow` overridden by child); root CLAUDE.md spliced from parent (R45). state.json records `resolvedSources` with `kind: ancestor` for the parent. |
| [05-drift-and-swap.txt](05-drift-and-swap.txt) | `drift` (clean → modified detection R20); `--preview`; `--json` shape with full provenance and `fastPathHits`/`slowPathHits` counters; **R21/PR29 hard-block**: `CI=true c3p use` with drift exits 1 immediately; `--on-drift=discard` with backup snapshot |
| [06-on-drift-modes.txt](06-on-drift-modes.txt) | All three on-drift modes: `discard`, `persist` (writes drift back to active profile first — observed: drifted `NewTool` permission landed in default's settings.json), `abort` (exit 1, .claude/ untouched) |
| [07-edge-cases.txt](07-edge-cases.txt) | **PR6 #1 typo suggestion** (`stric` → "Did you perhaps mean: strict?"); typo with no near-match; double-init guard; `doctor` (8 health checks all OK, `--json` shape); `diff default strict-plus` |
| [08-sync-new-hook-completions.txt](08-sync-new-hook-completions.txt) | `sync` re-materializes; `new minimal` scaffolds; `hook uninstall`/`install` atomic; **completions for bash + zsh + fish**, all well-formed |
| [09-perf-and-completions.txt](09-perf-and-completions.txt) | Cold-start timing (5×wall + 20×subprocess statistics); binary size; quarantine-xattr cleared; bash completion script sources cleanly and registers `_c3p_complete` function |

## Notable behaviors confirmed end-to-end

- **R44 markers + R45 splice**: managed CLAUDE.md block correctly populated from active profile's CLAUDE.md, including via `extends` inheritance ([04](04-use-with-extends.txt))
- **Two-tier fingerprint**: state.json includes `size+mtimeMs` (fast path) and `contentHash` (slow path); drift `--json` exposes per-path counters ([05](05-drift-and-swap.txt))
- **R21 hard-block**: non-TTY use without `--on-drift` fails immediately with the canonical error message — proves CI scripts never hang on hidden prompts ([05](05-drift-and-swap.txt))
- **R23a backup retention**: discarded drift produces a snapshot under `.meta/backup/<ISO>/`; `doctor` reports the count ([05](05-drift-and-swap.txt), [07](07-edge-cases.txt))
- **R23b persist round-trip**: edits made to live `.claude/` propagate back into the active profile when `--on-drift=persist` is chosen ([06](06-on-drift-modes.txt))
- **Cask postflight**: `xattr -dr com.apple.quarantine` ran during install — the binary opens without the macOS Gatekeeper "downloaded from internet" prompt ([09](09-perf-and-completions.txt))

## Reproducing

On any macOS machine:

```bash
brew uninstall --cask c3p 2>/dev/null
brew untap htxryan/tap 2>/dev/null
brew install htxryan/tap/c3p
c3p --version    # → c3p 1.0.6

# Optional: re-run the full battery
cd "$(mktemp -d)" && git init -q
c3p init
c3p list
# ... see individual transcripts for the rest
```

On Linux: `c3p` is shipped as a Linux binary in the GitHub Releases archives but the Homebrew Cask is macOS-only. Linux users:
```bash
curl -L https://github.com/htxryan/claude-code-config-profiles/releases/download/v1.0.6/c3p_1.0.6_linux_amd64.tar.gz | tar xz
sudo mv c3p /usr/local/bin/
c3p --version
```

On Windows: `winget install htxryan.c3p` once `microsoft/winget-pkgs#367984` is accepted by Microsoft's review automation.
