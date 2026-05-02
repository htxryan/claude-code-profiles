// Suggestion helpers — Levenshtein-2 typo "did you mean" candidates.
// Mirrors src/cli/suggest.ts.
package cli

import (
	"fmt"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/resolver"
)

// EnrichMissingProfileError takes a *MissingProfileError and adds typo
// suggestions from the project's profile list. Returns a new error so
// callers can re-throw without mutating the original.
func EnrichMissingProfileError(err *pipelineerrors.MissingProfileError, projectRoot string) *pipelineerrors.MissingProfileError {
	candidates, listErr := resolver.ListProfiles(resolver.DiscoverOptions{ProjectRoot: projectRoot})
	if listErr != nil {
		return err
	}
	suggestions := levenshteinNeighbors(err.Missing, candidates, 2)
	if len(suggestions) == 0 {
		return err
	}
	return pipelineerrors.NewMissingProfileError(err.Missing, err.ReferencedBy, suggestions)
}

// AssertValidProfileName surfaces an actionable error when the user types a
// path-like profile name (slashes, dots). Returns nil when name is valid.
func AssertValidProfileName(name string) error {
	if resolver.IsValidProfileName(name) {
		return nil
	}
	return NewUserError(fmt.Sprintf("invalid profile name %q — names must be a bare directory name (no slashes, no leading dot, no '..')", name))
}

// levenshteinNeighbors returns members of candidates within editDistance of
// target, sorted by distance then lexicographically. Distance > maxDist are
// excluded entirely.
func levenshteinNeighbors(target string, candidates []string, maxDist int) []string {
	type scored struct {
		name string
		d    int
	}
	var hits []scored
	for _, c := range candidates {
		if c == target {
			continue
		}
		d := levenshtein(target, c)
		if d <= maxDist {
			hits = append(hits, scored{name: c, d: d})
		}
	}
	// Insertion sort — small N (typically <10 profiles).
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0; j-- {
			a, b := hits[j-1], hits[j]
			less := a.d > b.d || (a.d == b.d && a.name > b.name)
			if !less {
				break
			}
			hits[j-1], hits[j] = b, a
		}
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.name
	}
	return out
}

// levenshtein computes edit distance between a and b.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	la, lb := len(ar), len(br)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur := make([]int, lb+1)
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min3(
				prev[j]+1,
				cur[j-1]+1,
				prev[j-1]+cost,
			)
		}
		prev = cur
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
