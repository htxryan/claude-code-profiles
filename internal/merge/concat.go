package merge

import "fmt"

// newline byte. R9 separator between contributor chunks.
const concatNewline byte = '\n'

// ConcatStrategy implements R9: concat strategy for *.md.
//
// Concatenates each contributor's bytes in canonical order. A separator
// newline is inserted BETWEEN contributors only when the preceding chunk
// does not already end with one — this avoids double-newlines for files
// already terminated with `\n` while still keeping section boundaries
// clean otherwise. The trailing chunk's bytes are emitted verbatim: if
// the last contributor's bytes don't end in `\n`, neither does the merged
// output. (Callers that require POSIX-style trailing newlines should
// ensure each contributor source file has one.)
//
// Worked example (R9): base ← extended ← profile with
// profile.includes = [compA, compB] produces concat order
// base, extended, compA, compB, profile.
//
// Empty contributors are skipped entirely so an empty file does not
// produce a spurious blank line and does not appear in provenance.
// (Asymmetry with DeepMergeStrategy, which retains empty-{} contributors,
// is deliberate — see the comment on DeepMergeStrategy.)
func ConcatStrategy(relPath string, inputs []ContributorBytes) (StrategyResult, error) {
	if len(inputs) == 0 {
		return StrategyResult{}, fmt.Errorf("concat invoked with no inputs for %q", relPath)
	}

	nonEmpty := make([]ContributorBytes, 0, len(inputs))
	for _, in := range inputs {
		if len(in.Bytes) > 0 {
			nonEmpty = append(nonEmpty, in)
		}
	}

	// Initialize to an empty slice (not nil) so callers that distinguish
	// nil from len-zero see a stable contract for the all-empty-input case.
	out := []byte{}
	contributors := make([]string, 0, len(nonEmpty))
	for i, c := range nonEmpty {
		out = append(out, c.Bytes...)
		if i < len(nonEmpty)-1 && c.Bytes[len(c.Bytes)-1] != concatNewline {
			out = append(out, concatNewline)
		}
		contributors = append(contributors, c.ID)
	}

	return StrategyResult{
		Bytes:        out,
		Contributors: contributors,
	}, nil
}
