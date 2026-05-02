package merge

import (
	"fmt"
	"sort"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
	"github.com/htxryan/c3p/internal/resolver"
)

// ReadFunc returns the bytes for a contributor file referenced by absPath.
// The merge package does not perform filesystem I/O itself (D5 owns FS IO);
// callers — D5 materialize, tests — supply this callback.
type ReadFunc func(absPath string) ([]byte, error)

// Options configures a Merge call.
type Options struct {
	// Read is the byte source for each PlanFile.AbsPath. Required: a nil
	// Read returns an error rather than silently fall back to disk.
	Read ReadFunc
}

// Merge transforms a ResolvedPlan into the materializable file set.
//
// Algorithm:
//  1. Group plan.Files by composite key (RelPath, Destination), preserving
//     canonical contributor order. cw6/T3: a single relPath may appear at
//     both .claude and projectRoot destinations (e.g. CLAUDE.md); each
//     destination forms its own merge group.
//  2. For each group, request each contributor's bytes via opts.Read.
//  3. Dispatch via the strategy registry keyed by MergePolicy.
//  4. Collect MergedFile slice sorted lex by Path then Destination.
//
// Returns:
//   - *errors.InvalidSettingsJsonError when a contributor's settings.json
//     fails to parse during deep-merge.
//   - *errors.MergeReadFailedError when opts.Read returns an error for a
//     contributor file.
func Merge(plan *resolver.ResolvedPlan, opts Options) ([]MergedFile, error) {
	if plan == nil {
		return nil, fmt.Errorf("merge: plan must not be nil")
	}
	if opts.Read == nil {
		return nil, fmt.Errorf("merge: opts.Read must not be nil (D2 is FS-IO-free; callers supply the byte source)")
	}

	type groupKey struct {
		destination resolver.PlanFileDestination
		relPath     string
	}
	type group struct {
		relPath     string
		destination resolver.PlanFileDestination
		entries     []resolver.PlanFile
	}
	// Composite struct key (rather than a string-joined one) so the
	// invariant "destination + relPath uniquely identifies a group" is
	// enforced by Go's type system instead of by a separator character
	// that could in principle collide if either field's value space ever
	// widened (e.g. a relPath containing the previous "::" delimiter).
	byKey := map[groupKey]*group{}
	groups := []*group{}
	for _, f := range plan.Files {
		key := groupKey{destination: f.Destination, relPath: f.RelPath}
		g, ok := byKey[key]
		if !ok {
			g = &group{relPath: f.RelPath, destination: f.Destination}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.entries = append(g.entries, f)
	}

	for _, g := range groups {
		// Within a single (relPath, destination) group, contributorIndex
		// values must be strictly ascending — the resolver's sort
		// guarantees this; if it breaks we want a loud failure rather than
		// silently merging out of canonical order.
		for i := 1; i < len(g.entries); i++ {
			if g.entries[i].ContributorIndex <= g.entries[i-1].ContributorIndex {
				return nil, fmt.Errorf(
					"ResolvedPlan invariant violated: PlanFile entries for %q (destination=%s) have non-ascending contributorIndex values",
					g.relPath, g.destination,
				)
			}
		}
	}

	out := make([]MergedFile, 0, len(groups))
	for _, g := range groups {
		// All entries in a group share mergePolicy (it's a function of
		// relPath, classified once in D1 by PolicyFor; destination-agnostic
		// per spec §12). Assert defensively — a regression in D1 that
		// emitted conflicting policies for the same relPath would otherwise
		// apply the wrong strategy to some contributor bytes silently.
		policy := g.entries[0].MergePolicy
		for _, e := range g.entries {
			if e.MergePolicy != policy {
				return nil, fmt.Errorf(
					"ResolvedPlan invariant violated: conflicting mergePolicy for %q (%s vs %s)",
					g.relPath, policy, e.MergePolicy,
				)
			}
		}

		inputs := make([]ContributorBytes, len(g.entries))
		for i, entry := range g.entries {
			if entry.ContributorIndex < 0 || entry.ContributorIndex >= len(plan.Contributors) {
				return nil, fmt.Errorf(
					"PlanFile %q references invalid contributorIndex %d",
					entry.RelPath, entry.ContributorIndex,
				)
			}
			contributor := plan.Contributors[entry.ContributorIndex]
			data, err := opts.Read(entry.AbsPath)
			if err != nil {
				return nil, pipelineerrors.NewMergeReadFailedError(
					entry.RelPath, contributor.ID, entry.AbsPath, err.Error(),
				)
			}
			inputs[i] = ContributorBytes{ID: contributor.ID, Bytes: data}
		}

		strategy, err := GetStrategy(policy)
		if err != nil {
			return nil, err
		}
		result, err := strategy(g.relPath, inputs)
		if err != nil {
			return nil, err
		}

		out = append(out, MergedFile{
			Path:          g.relPath,
			Bytes:         result.Bytes,
			Contributors:  result.Contributors,
			MergePolicy:   policy,
			Destination:   g.destination,
			SchemaVersion: MergedFileSchemaVersion,
		})
	}

	// Lex sort by path, then destination, so destination-collided pairs
	// (same path, different destinations) have a stable, deterministic
	// order in the output. ".claude" < "projectRoot" lexicographically,
	// which keeps the historical destination first.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Destination < out[j].Destination
	})

	return out, nil
}
