#!/usr/bin/env bash
# jsonout_lint.sh — D7 fitness function: forbids json.Marshal calls outside
# the internal/cli/jsonout package. PR3 mandates a single deterministic
# JSON marshaller for every CLI envelope; this lint enforces that
# mechanically so a drift in HTML escaping (or a forgotten encoder option)
# can't sneak in via a parallel encoder somewhere else in the tree.
#
# State files (state_file.go) and other domain-package marshalling are
# explicitly allowed — the lint targets the CLI surface only. The grep
# scope is the cli/ directory and downward.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="$ROOT/internal/cli"
JSONOUT="internal/cli/jsonout"

# Match the function-call form only — `json.Marshal(`. Bare-word matches
# (comments, doc-strings) are false positives. We also catch the indented
# variant. Encoder use (json.NewEncoder + Encode) is allowed because
# state_file.go and our jsonout package both go through it; PR3's surface
# is only the cli marshaller.
PATTERN='json\.Marshal(Indent)?\('
if command -v rg >/dev/null 2>&1; then
  matches=$(rg -n --type go "$PATTERN" "$TARGET" 2>/dev/null || true)
else
  matches=$(grep -rn --include='*.go' "$PATTERN" "$TARGET" 2>/dev/null || true)
fi

# Filter: any hit OUTSIDE jsonout/ is a violation.
violations=$(echo "$matches" | grep -v "$JSONOUT" | grep -v '^$' || true)

if [ -n "$violations" ]; then
  echo "FAIL: json.Marshal called outside $JSONOUT/:"
  echo "$violations"
  echo
  echo "Route all CLI JSON output through internal/cli/jsonout (PR3)."
  exit 1
fi

echo "OK: no json.Marshal calls outside $JSONOUT/"
