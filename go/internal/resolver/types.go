package resolver

// ResolvedPlanSchemaVersion is the schemaVersion stamp on every ResolvedPlan.
// Bump only when consumers (D2/D5/D6/D7) must update for a breaking shape
// change. Per the D1 fitness function this should not change for >= 2 weeks
// once 1 ships.
const ResolvedPlanSchemaVersion = 1

// ProfileManifest is the raw shape of a profile.json manifest as accepted
// from disk. All fields are optional (R35); unknown fields produce a
// validation warning but do not abort (R36).
type ProfileManifest struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Extends     string   `json:"extends,omitempty"`
	Includes    []string `json:"includes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// IncludeSourceKind is the syntactic form of an includes entry, derived from
// the raw string. Per R37 the only valid forms are: bare component name,
// `./`-prefixed relative, absolute path, or `~/...`.
type IncludeSourceKind string

const (
	IncludeKindComponent IncludeSourceKind = "component"
	IncludeKindRelative  IncludeSourceKind = "relative"
	IncludeKindAbsolute  IncludeSourceKind = "absolute"
	IncludeKindTilde     IncludeSourceKind = "tilde"
)

// IncludeRef is one include reference after path classification but before
// file enumeration.
type IncludeRef struct {
	Raw          string            `json:"raw"`
	Kind         IncludeSourceKind `json:"kind"`
	ResolvedPath string            `json:"resolvedPath"`
	External     bool              `json:"external"`
}

// ContributorKind identifies the source of a contributor relative to the
// requested profile.
type ContributorKind string

const (
	ContributorAncestor ContributorKind = "ancestor"
	ContributorInclude  ContributorKind = "include"
	ContributorProfile  ContributorKind = "profile"
)

// Contributor is any source that contributes a `.claude/` subtree to the
// final plan. Order is significant.
type Contributor struct {
	Kind       ContributorKind  `json:"kind"`
	ID         string           `json:"id"`
	RootPath   string           `json:"rootPath"`
	ClaudeDir  string           `json:"claudeDir"`
	External   bool             `json:"external"`
	Manifest   *ProfileManifest `json:"manifest,omitempty"`
}

// PlanFileDestination is where a PlanFile materializes in the project tree.
type PlanFileDestination string

const (
	DestinationClaude      PlanFileDestination = ".claude"
	DestinationProjectRoot PlanFileDestination = "projectRoot"
)

// MergePolicy describes how a relPath should be merged across contributors.
type MergePolicy string

const (
	MergePolicyDeepMerge MergePolicy = "deep-merge"
	MergePolicyConcat    MergePolicy = "concat"
	MergePolicyLastWins  MergePolicy = "last-wins"
)

// PlanFile is a single file from a single contributor, identified by its
// path relative to that contributor's destination root.
type PlanFile struct {
	RelPath          string              `json:"relPath"`
	AbsPath          string              `json:"absPath"`
	ContributorIndex int                 `json:"contributorIndex"`
	MergePolicy      MergePolicy         `json:"mergePolicy"`
	Destination      PlanFileDestination `json:"destination"`
}

// WarningCode identifies a non-fatal warning code.
type WarningCode string

const (
	WarningUnknownManifestField WarningCode = "UnknownManifestField"
	WarningManifestParseError   WarningCode = "ManifestParseError"
	WarningMissingManifest      WarningCode = "MissingManifest"
	WarningDuplicateInclude     WarningCode = "DuplicateInclude"
)

// ResolutionWarning is a non-fatal warning emitted during resolution.
type ResolutionWarning struct {
	Code    WarningCode `json:"code"`
	Message string      `json:"message"`
	Source  string      `json:"source,omitempty"`
}

// ExternalTrustEntry is information about an external-path contributor for
// the first-use trust notice (R37a).
type ExternalTrustEntry struct {
	Raw          string `json:"raw"`
	ResolvedPath string `json:"resolvedPath"`
}

// ResolvedPlan is the cross-epic, load-bearing contract produced by Resolve.
type ResolvedPlan struct {
	SchemaVersion int                  `json:"schemaVersion"`
	ProfileName   string               `json:"profileName"`
	Chain         []string             `json:"chain"`
	Includes      []IncludeRef         `json:"includes"`
	Contributors  []Contributor        `json:"contributors"`
	Files         []PlanFile           `json:"files"`
	Warnings      []ResolutionWarning  `json:"warnings"`
	ExternalPaths []ExternalTrustEntry `json:"externalPaths"`
}
