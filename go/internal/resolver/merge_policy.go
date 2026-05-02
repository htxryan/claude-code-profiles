package resolver

import (
	"path"
	"strings"
)

// PolicyFor maps a path inside `.claude/` to its merge policy.
//
//   - settings.json (any depth) → deep-merge (R8/R12)
//   - *.md                       → concat     (R9)
//   - everything else            → last-wins  (R10)
//
// "last-wins" files participate in conflict detection (R11). The other two
// are always mergeable and never conflict.
func PolicyFor(relPath string) MergePolicy {
	base := path.Base(relPath)
	if base == "settings.json" {
		return MergePolicyDeepMerge
	}
	if strings.HasSuffix(strings.ToLower(relPath), ".md") {
		return MergePolicyConcat
	}
	return MergePolicyLastWins
}

// IsMergeable returns true iff a relPath's merge policy is anything other
// than last-wins.
func IsMergeable(relPath string) bool {
	return PolicyFor(relPath) != MergePolicyLastWins
}
