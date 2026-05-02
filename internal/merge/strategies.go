package merge

import (
	"fmt"

	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
)

// GetStrategy returns the strategy implementation registered for policy.
// The registry is the single dispatch point for D2; tests can either call
// GetStrategy or import strategies directly for unit testing.
func GetStrategy(policy resolver.MergePolicy) (MergeStrategy, error) {
	switch policy {
	case resolver.MergePolicyDeepMerge:
		return DeepMergeStrategy, nil
	case resolver.MergePolicyConcat:
		return ConcatStrategy, nil
	case resolver.MergePolicyLastWins:
		return LastWinsStrategy, nil
	default:
		return nil, fmt.Errorf("no merge strategy registered for policy %q", policy)
	}
}
