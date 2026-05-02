package merge

import "fmt"

// LastWinsStrategy implements R10: pick the last (highest contributor index)
// contributor's bytes and list only that contributor in the provenance.
//
// For non-mergeable file types the resolver throws on multi-contributor
// conflicts (R11) before merge runs, but ancestor-only chains and profile-
// overrides are still routed through this strategy, hence the multi-input
// case.
//
// Returns a fresh byte slice rather than aliasing the input — aligns with
// the "byte-stable pure function" guarantee. A future caller that mutates
// the output (e.g. zeroing after a write) will not corrupt input bytes
// still held by the orchestrator.
func LastWinsStrategy(relPath string, inputs []ContributorBytes) (StrategyResult, error) {
	if len(inputs) == 0 {
		return StrategyResult{}, fmt.Errorf("last-wins invoked with no inputs for %q", relPath)
	}
	winner := inputs[len(inputs)-1]
	out := make([]byte, len(winner.Bytes))
	copy(out, winner.Bytes)
	return StrategyResult{
		Bytes:        out,
		Contributors: []string{winner.ID},
	}, nil
}
