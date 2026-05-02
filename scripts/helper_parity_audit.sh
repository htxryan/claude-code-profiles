#!/usr/bin/env bash
# helper_parity_audit.sh — PR4 fitness function for the c3p Go rewrite.
#
# What this enforces (epic claude-code-profiles-248 / F1):
#   The TS test harness (tests/helpers/fixture.ts, tests/cli/integration/spawn.ts)
#   and the Go test harness (go/tests/integration/helpers/fixture.go,
#   go/tests/integration/helpers/spawn.go) must expose semantically
#   equivalent public surfaces. A divergent helper (TS adds RootFiles, Go
#   doesn't, or vice versa) produces false-green tests where the Go bin is
#   "passing" while not exercising the same fixture shape — the worst kind
#   of green.
#
# How it works:
#   We extract the public-surface signatures from each helper file (struct
#   fields, exported functions, type names) and check that each required
#   symbol appears on BOTH sides. The check matches against the union of
#   each side's helper files, so it doesn't matter which file holds the
#   symbol — only that both languages have it.
#
# How to fix a failure:
#   Touch BOTH helpers in the same PR. If you genuinely need a Go-only or
#   TS-only field (e.g. a Go-side context.Context for cancellation), add
#   it to the "Allowed divergences" block at the bottom of this file and
#   rerun.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

TS_FIXTURE="$REPO_ROOT/tests/helpers/fixture.ts"
TS_SPAWN="$REPO_ROOT/tests/cli/integration/spawn.ts"
GO_FIXTURE="$REPO_ROOT/go/tests/integration/helpers/fixture.go"
GO_SPAWN="$REPO_ROOT/go/tests/integration/helpers/spawn.go"

for f in "$TS_FIXTURE" "$TS_SPAWN" "$GO_FIXTURE" "$GO_SPAWN"; do
  if [[ ! -f "$f" ]]; then
    echo "ERROR: missing helper file: $f" >&2
    echo "       helper-parity audit cannot run without all four files" >&2
    exit 2
  fi
done

# Build the per-side haystack once. Concatenating fixture+spawn means a
# symbol can live in either file without breaking the check — the contract
# is "TS and Go agree on the public surface", not "TS and Go agree on
# file layout".
TS_HAYSTACK="$(cat "$TS_FIXTURE" "$TS_SPAWN")"
GO_HAYSTACK="$(cat "$GO_FIXTURE" "$GO_SPAWN")"

# Required-symbol checklist. Each row is:
#   TS_REGEX	GO_REGEX	HUMAN_LABEL
# (tab-separated). Both regexes must match in their respective haystack.
declare -a CHECKS=(
  # public types
  $'export interface ProfileSpec\ttype ProfileSpec struct\tProfileSpec type'
  $'export interface ComponentSpec\ttype ComponentSpec struct\tComponentSpec type'
  $'export interface FixtureSpec\ttype FixtureSpec struct\tFixtureSpec type'
  $'export interface Fixture\ttype Fixture struct\tFixture type'
  $'export interface SpawnResult\ttype SpawnResult struct\tSpawnResult type'
  $'export interface SpawnOptions\ttype SpawnOptions struct\tSpawnOptions type'
  # public functions
  $'export async function makeFixture\tfunc MakeFixture\tmakeFixture/MakeFixture entrypoint'
  $'export async function runCli\tfunc RunCli\trunCli/RunCli entrypoint'
  $'export async function ensureBuilt\tfunc EnsureBuilt\tensureBuilt/EnsureBuilt entrypoint'
  # Fixture lifecycle parity — TS exposes a cleanup callback on the Fixture
  # object; Go exposes a Cleanup() method on *Fixture. The Go impl is a
  # no-op (t.TempDir handles real removal) but the surface match is
  # load-bearing for the F1 epic's explicit Fixture contract.
  $'cleanup:[[:space:]]+\\(\\)[[:space:]]+=>\tfunc[[:space:]]+\\(\\*Fixture\\)[[:space:]]+Cleanup\\(\\)[[:space:]]+error\tFixture.cleanup/Cleanup'
  # ProfileSpec field parity
  $'manifest\\?\tManifest[[:space:]]+any\tProfileSpec.manifest'
  $'files\\?\tFiles[[:space:]]+map\\[string\\]string\tProfileSpec.files'
  $'rootFiles\\?\tRootFiles[[:space:]]+map\\[string\\]string\tProfileSpec.rootFiles'
  # FixtureSpec field parity
  $'profiles\\?\tProfiles[[:space:]]+map\\[string\\]ProfileSpec\tFixtureSpec.profiles'
  $'components\\?\tComponents[[:space:]]+map\\[string\\]ComponentSpec\tFixtureSpec.components'
  $'external\\?\tExternal[[:space:]]+map\\[string\\]ComponentSpec\tFixtureSpec.external'
  # SpawnOptions field parity
  $'cwd\\?\tCwd[[:space:]]+string\tSpawnOptions.cwd'
  $'env\\?\tEnv[[:space:]]+map\\[string\\]string\tSpawnOptions.env'
  $'args:\tArgs[[:space:]]+\\[\\]string\tSpawnOptions.args'
  $'stdin\\?\tStdin[[:space:]]+string\tSpawnOptions.stdin'
  $'timeoutMs\\?\tTimeoutMs[[:space:]]+int\tSpawnOptions.timeoutMs'
  # SpawnResult field parity
  $'stdout:\tStdout[[:space:]]+string\tSpawnResult.stdout'
  $'stderr:\tStderr[[:space:]]+string\tSpawnResult.stderr'
  $'exitCode:\tExitCode[[:space:]]+int\tSpawnResult.exitCode'
  $'signal:\tSignal[[:space:]]+string\tSpawnResult.signal'
)

failures=()

for spec in "${CHECKS[@]}"; do
  IFS=$'\t' read -r ts_pat go_pat label <<< "$spec"
  if ! grep -Eq "$ts_pat" <<< "$TS_HAYSTACK"; then
    failures+=("MISSING IN TS  ($label): pattern /$ts_pat/")
  fi
  if ! grep -Eq "$go_pat" <<< "$GO_HAYSTACK"; then
    failures+=("MISSING IN GO  ($label): pattern /$go_pat/")
  fi
done

if (( ${#failures[@]} > 0 )); then
  echo "Helper-parity audit FAILED — public surfaces have diverged." >&2
  echo "" >&2
  for f in "${failures[@]}"; do
    echo "  • $f" >&2
  done
  echo "" >&2
  echo "Either:" >&2
  echo "  1) update both helpers in this PR so the surfaces match, or" >&2
  echo "  2) document the intentional divergence in scripts/helper_parity_audit.sh" >&2
  echo "     under the Allowed divergences block." >&2
  exit 1
fi

echo "Helper-parity audit OK — TS and Go test harness surfaces are in sync."

# Allowed divergences (intentional, documented):
#
# - Go's RunCli takes a *testing.T because the Go test runtime needs
#   access for t.Helper() and t.Fatal-on-build-failure semantics. The TS
#   surface has no analog; this is a language-idiom difference, not a
#   semantic divergence.
# - Go's RunCli takes a context.Context for cancellation; TS uses an
#   internal setTimeout. Same semantic (deadline), different idiom.
# - Go exposes BinPath() and a build-on-first-call cache; TS hard-codes
#   dist/cli/bin.js. These are build-detail helpers, not public surface.
# - Go exposes CleanupBuiltBin() for TestMain to remove the cached binary;
#   TS doesn't need an analog because Vitest manages the dist/ artifact.
