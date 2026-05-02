package merge

import "github.com/htxryan/c3p/internal/resolver"

// MergedFileSchemaVersion is the schemaVersion stamp for MergedFile. Bumped
// only when consumers (D5/D7) must update for a breaking shape change.
const MergedFileSchemaVersion = 1

// MergedFile is one merged output, ready for materialization (D5).
//
// Invariants enforced by the orchestrator:
//   - Path is the relative posix path inside Destination's root.
//   - Bytes is the exact byte content to write.
//   - Contributors lists the contributor ids that actually contributed bytes
//     to the output, in canonical order.
//   - MergePolicy mirrors the strategy used.
//   - Destination mirrors the destination of the contributing PlanFiles.
//     The merge engine groups by (RelPath, Destination) so two MergedFile
//     entries may share the same Path if their destinations differ (cw6/T3).
//   - SchemaVersion is stamped from MergedFileSchemaVersion so D5/D7 can
//     branch on it if the shape ever changes in a breaking way.
type MergedFile struct {
	Path          string
	Bytes         []byte
	Contributors  []string
	MergePolicy   resolver.MergePolicy
	Destination   resolver.PlanFileDestination
	SchemaVersion int
}

// ContributorBytes is one contributor's bytes for a given relPath. Strategies
// receive an ordered slice of these in canonical resolution order
// (oldest → newest, profile last). ID is the same identifier carried on
// Contributor.ID in ResolvedPlan.
type ContributorBytes struct {
	ID    string
	Bytes []byte
}

// StrategyResult is the return value of a MergeStrategy: the merged bytes
// and the contributor ids that actually contributed.
type StrategyResult struct {
	Bytes        []byte
	Contributors []string
}

// MergeStrategy is the strategy contract. Pure function: given an ordered
// list of contributor bytes for a single relPath, return the merged bytes
// plus the contributor ids that actually contributed. May return a
// pipeline error (e.g. *errors.InvalidSettingsJsonError for unparseable
// JSON) on per-strategy failure.
type MergeStrategy func(relPath string, inputs []ContributorBytes) (StrategyResult, error)
